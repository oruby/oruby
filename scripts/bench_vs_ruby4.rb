# Benchmark comparison: Ruby 4.0 vs Ruby 4.0 (YJIT) vs ORuby AOT Compiler
require 'benchmark'
require 'fileutils'
require 'open3'

RUBY4_BIN = File.expand_path("~/.local/share/mise/installs/ruby/4.0.7/bin/ruby")
ORUBY_BIN = File.expand_path("../bin/oruby", __dir__)
ORUBY_COMPILER = File.expand_path("../bin/oruby-compiler", __dir__)

unless File.exist?(RUBY4_BIN)
  abort "Ruby 4 binary not found at #{RUBY4_BIN}. Run `mise install ruby@4.0.7`"
end

unless File.exist?(ORUBY_BIN)
  puts "Building oruby direct runner..."
  system("go", "build", "-o", ORUBY_BIN, "./cmd/oruby")
end

unless File.exist?(ORUBY_COMPILER)
  puts "Building oruby-compiler..."
  system("go", "build", "-o", ORUBY_COMPILER, "./cmd/oruby-compiler")
end

TMP_DIR = "/tmp/oruby_bench_#{Process.pid}"
FileUtils.mkdir_p(TMP_DIR)
at_exit { FileUtils.rm_rf(TMP_DIR) }

BENCHMARKS = {
  "Fibonacci (35) [Recursion & Arithmetic]" => %q{
def fib(n)
  return n if n <= 1
  fib(n - 1) + fib(n - 2)
end
puts fib(35)
  },

  "Tight Integer Loop [1,000,000 increments]" => %q{
i = 0
sum = 0
while i < 1_000_000
  sum += i
  i += 1
end
puts sum
  },

  "Language Spec Suite [10,000 iterations]" => %q{
class User
  def initialize(id, name, score)
    @id = id
    @name = name
    @score = score
  end
  def score; @score; end
  def bump!(amount); @score += amount; end
end

i = 0
sum = 0
while i < 10_000
  val = if i % 2 == 0 then "even" else "odd" end
  u = User.new(i, val, 10)
  u.bump!(5)
  sum += u.score

  doubled_sum = 0
  [1, 2, 3].each { |x| doubled_sum += x * 2 }
  sum += doubled_sum

  h = { "key" => 42 }
  sum += h["key"]
  i += 1
end
puts sum
  },

  "JSON 10k parse + generate [Phase 3 Fast JSON]" => %q{
require 'json' rescue nil
payload = '{"name":"Alice","scores":[98,95,100],"meta":{"active":true,"role":"admin"}}'
i = 0
while i < 10_000
  data = JSON.parse(payload)
  _out = JSON.generate(data)
  i += 1
end
puts i
  }
}

def time_cmd(cmd_array)
  t0 = Process.clock_gettime(Process::CLOCK_MONOTONIC)
  out, err, status = Open3.capture3(*cmd_array)
  t1 = Process.clock_gettime(Process::CLOCK_MONOTONIC)
  unless status.success?
    warn "Command failed: #{cmd_array.join(' ')}\nError: #{err}"
    return nil
  end
  ((t1 - t0) * 1000.0).round(2)
end

puts "=" * 98
puts " ORuby (Direct vs AOT) vs Ruby 4.0 Performance Benchmark"
puts " Ruby Version: #{`#{RUBY4_BIN} -v`.strip}"
puts "=" * 98
printf("%-45s | %-12s | %-12s | %-12s | %-12s\n", "Benchmark", "Ruby 4 (MRI)", "Ruby 4 (YJIT)", "ORuby Direct", "ORuby AOT")
puts "-" * 105

BENCHMARKS.each do |name, code|
  rb_file = File.join(TMP_DIR, "bench.rb")
  bin_file = File.join(TMP_DIR, "bench_bin")
  File.write(rb_file, code)

  # 1. Ruby 4 standard
  r4_time = time_cmd([RUBY4_BIN, rb_file])

  # 2. Ruby 4 YJIT
  yjit_time = time_cmd([RUBY4_BIN, "--yjit", rb_file])

  # 3. ORuby Direct Interpreter (cmd/oruby)
  direct_time = time_cmd([ORUBY_BIN, rb_file])

  # 4. ORuby AOT Compiler (cmd/oruby-compiler)
  compile_ok = system(ORUBY_COMPILER, "-build", bin_file, rb_file, out: File::NULL, err: File::NULL)
  oruby_time = if compile_ok && File.exist?(bin_file)
                 time_cmd([bin_file])
               else
                 "ERR"
               end

  r4_str = r4_time ? "#{r4_time} ms" : "N/A"
  yjit_str = yjit_time ? "#{yjit_time} ms" : "N/A"
  direct_str = direct_time ? "#{direct_time} ms" : "N/A"
  oruby_str = oruby_time.is_a?(Numeric) ? "#{oruby_time} ms" : oruby_time.to_s

  printf("%-45s | %-12s | %-12s | %-12s | %-12s\n", name, r4_str, yjit_str, direct_str, oruby_str)
end
puts "=" * 98
