# Runs ruby/spec against bin/oruby file by file, in parallel, and records a
# tally per spec file. Every file runs in its own process with a timeout, so a
# crash or hang only loses that file.
#
#   ruby scripts/spec_run.rb [options] [dir-or-file ...]
#
# Paths are relative to <suite>/ruby (default: language core library
# command_line security). Options:
#   -s DIR   suite with ruby/ and mspec/ checkouts (default ../oruby_test/spec)
#   -o DIR   output directory (default tmp/spec_run); logs/ + results.json
#   -j N     parallel jobs (default: CPU count)
#   -t SECS  per-file timeout (default 60)
#   -c FILE  compare with an earlier results.json and print changed files
#
# AOT variant of spec_run.rb: each spec file is compiled with `oruby-compiler -build
# BIN -bundle FILE` (the spec itself is the entry, as cmd/oruby-spec-smoke does) and
# the resulting executable is run in <suite>. The compile step has its own
# timeout (-T, default 300); -t bounds the run. Binaries are deleted after use.
#
# With -b FILE no compiling is done: FILE is one binary with mspec and all the specs
# bundled in (oruby-compiler -build FILE -I mspec/lib -bundle -add mspec/lib,ruby
# mspec/bin/mspec-run), run once per spec file as mspec-run is: BIN -B oruby.mspec FILE.
#
# RUBY_EXE is set to the oruby binary, so specs that spawn `ruby_exe` test the
# same build instead of the :target from the suite's default.mspec.
# RUBY_FLAGS defaults to an empty string for fixtures that read it directly.
require 'json'
require 'fileutils'
require 'optparse'
require 'etc'

root = File.expand_path('..', __dir__)
opts = { suite: File.expand_path('../oruby_test/spec', root), out: File.join(root, 'tmp/spec_run'),
         jobs: Etc.nprocessors, timeout: 60, ctimeout: 300, compare: nil }
OptionParser.new do |o|
  o.on('-s DIR') { |v| opts[:suite] = File.expand_path(v) }
  o.on('-o DIR') { |v| opts[:out] = File.expand_path(v) }
  o.on('-j N', Integer) { |v| opts[:jobs] = v }
  o.on('-t SECS', Integer) { |v| opts[:timeout] = v }
  o.on('-T SECS', Integer) { |v| opts[:ctimeout] = v }
  o.on('-b FILE') { |v| opts[:bin] = File.expand_path(v) }
  o.on('-c FILE') { |v| opts[:compare] = File.expand_path(v) }
end.parse!

oruby = File.join(root, 'bin/oruby')
abort "#{oruby} not found; run `make bin`" unless File.executable?(oruby)
compiler = File.join(root, 'tmp/aot/oruby-compiler')
bins = File.join(opts[:out], 'bin')
FileUtils.mkdir_p(File.dirname(compiler))
unless opts[:bin]
  system('go', 'build', '-o', compiler, './cmd/oruby-compiler', chdir: root) or abort 'oruby-compiler build failed'
end
FileUtils.rm_rf(bins)
FileUtils.mkdir_p(bins)
work = File.join(root, 'tmp/aot/work')
FileUtils.mkdir_p(work)
suite = opts[:suite]
targets = ARGV.empty? ? %w[language core library command_line security] : ARGV
files = targets.flat_map do |t|
  path = File.join(suite, 'ruby', t)
  File.file?(path) ? [path] : Dir["#{path}/**/*_spec.rb"].sort
end
abort 'no spec files selected' if files.empty?

logs = File.join(opts[:out], 'logs')
FileUtils.rm_rf(logs)
FileUtils.mkdir_p(logs)
queue = Queue.new
files.each { |f| queue << f }
results = Queue.new
tally_re = /(\d+) files?, (\d+) examples?, (\d+) expectations?, (\d+) failures?, (\d+) errors?/

child_env = { 'RUBY_EXE' => oruby, 'RUBY_FLAGS' => ENV.fetch('RUBY_FLAGS', '') }
workers = opts[:jobs].times.map do
  Thread.new do
    while (file = (queue.pop(true) rescue nil))
      rel = file.delete_prefix("#{suite}/ruby/")
      log = File.join(logs, rel.gsub('/', '__') + '.log')
      started = Time.now
      if opts[:bin]
        bin = opts[:bin]
      else
        bin = File.join(bins, rel.gsub('/', '__').sub(/\.rb\z/, ''))
        cpid = Process.spawn(child_env, compiler, '-build', bin, '-bundle', file, chdir: work, out: log, err: %i[child out], in: :close, pgroup: true)
        cstatus = nil
        loop do
          _, cstatus = Process.wait2(cpid, Process::WNOHANG)
          break if cstatus
          if Time.now - started > opts[:ctimeout]
            Process.kill('KILL', -cpid) rescue nil
            Process.wait(cpid) rescue nil
            cstatus = :timeout
            break
          end
          sleep 0.05
        end
        unless cstatus.respond_to?(:success?) && cstatus.success? && File.executable?(bin)
          results << { 'file' => rel, 'secs' => (Time.now - started).round(2), 'state' => cstatus == :timeout ? 'compile-timeout' : 'compile-fail' }
          next
        end
      end
      started = Time.now
      args = opts[:bin] ? ['-B', File.join(root, 'scripts/oruby.mspec'), file] : []
      pid = Process.spawn(child_env, bin, *args, chdir: suite, out: log, err: %i[child out], in: :close, pgroup: true)
      status = nil
      loop do
        _, status = Process.wait2(pid, Process::WNOHANG)
        break if status
        if Time.now - started > opts[:timeout]
          Process.kill('KILL', -pid) rescue nil
          Process.wait(pid) rescue nil
          status = :timeout
          break
        end
        sleep 0.05
      end
      File.delete(bin) rescue nil unless opts[:bin]
      text = File.binread(log).scrub
      r = { 'file' => rel, 'secs' => (Time.now - started).round(2) }
      if (m = text.match(tally_re))
        r.merge!('state' => 'tally', 'examples' => m[2].to_i, 'failures' => m[4].to_i, 'errors' => m[5].to_i)
      else
        r['state'] = status == :timeout ? 'timeout' : 'crash'
      end
      results << r
    end
  end
end
workers.each(&:join)

all = []
all << results.pop until results.empty?
all.sort_by! { |r| r['file'] }
File.write(File.join(opts[:out], 'results.json'), JSON.pretty_generate(all))

bad = ->(r) { r['state'] == 'tally' ? r['failures'] + r['errors'] : nil }
rows = all.group_by { |r| r['file'].split('/').first }
printf "%-13s %6s %6s %6s %8s %6s %6s\n", 'area', 'files', 'clean', 'crash', 'examples', 'bad', 'pass'
(rows.keys.sort + ['TOTAL']).each do |k|
  rs = k == 'TOTAL' ? all : rows[k]
  t = rs.select { |r| r['state'] == 'tally' }
  ex = t.sum { |r| r['examples'] }
  b = t.sum(&bad)
  printf "%-13s %6d %6d %6d %8d %6d %5.1f%%\n", k, rs.size, t.count { |r| bad.(r).zero? },
         rs.size - t.size, ex, b, ex.zero? ? 0 : 100.0 * (ex - b) / ex
end
all.reject { |r| r['state'] == 'tally' }.each { |r| puts "  #{r['state']}: #{r['file']}" }

if opts[:compare]
  old = JSON.parse(File.read(opts[:compare])).to_h { |r| [r['file'], r] }
  puts "\nchanges vs #{opts[:compare]}:"
  all.each do |r|
    o = old[r['file']] or next
    before = bad.(o)
    after = bad.(r)
    next if before == after && o['state'] == r['state']
    puts format('  %-60s %s -> %s', r['file'], before || o['state'], after || r['state'])
  end
end
