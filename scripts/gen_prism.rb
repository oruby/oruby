# Generates MRI 4.1's bundled Ruby::Prism library for oruby from the Prism
# sources mruby vendors (mruby/mrbgems/mruby-compiler/lib/prism), so the Ruby
# side matches the serialization format of the parser in libmruby.
#
#   ruby scripts/gen_prism.rb [OUT]     (default lib/oruby/4.1.0/ruby)
#
# Writes OUT/prism.rb and OUT/prism/ the way MRI ships them in
# rubylibdir/ruby/: Prism's lib/ with its templated files rendered, the
# constant renamed to Ruby::Prism and the library paths to ruby/prism/...
# The FFI backend is replaced by scripts/prism/oruby.rb, and OUT/../ripper.rb
# (scripts/prism/ripper.rb) makes Ripper Prism's Ripper translation. The
# output is generated (and ignored by git); it is rewritten only when an
# input changed.
require 'fileutils'
require 'digest'
require 'rbconfig'
require 'tmpdir'

root = File.expand_path('..', __dir__)
prism = File.join(root, 'mruby/mrbgems/mruby-compiler/lib/prism')
out = File.expand_path(ARGV[0] || File.join(root, 'lib/oruby/4.1.0/ruby'))
backend = File.join(__dir__, 'prism/oruby.rb')
ripper = File.join(__dir__, 'prism/ripper.rb')
abort "gen_prism: #{prism} is empty (git submodule update --init --recursive)" unless File.exist?(File.join(prism, 'templates/template.rb'))

TEMPLATES = %w[compiler dispatcher dot_visitor dsl inspect_visitor mutation_compiler node reflection serialize visitor].freeze

inputs = [__FILE__, backend, ripper, File.join(prism, 'config.yml'), File.join(prism, 'templates/template.rb')] +
         Dir[File.join(prism, 'templates/lib/**/*.erb')] + Dir[File.join(prism, 'lib/**/*.rb')]
digest = Digest::SHA256.new
inputs.sort.each { |f| digest << f.delete_prefix(root) << "\0" << File.binread(f) }
stamp = File.join(out, 'prism/.oruby-stamp')
exit if File.exist?(stamp) && File.read(stamp) == digest.hexdigest

# Prism -> Ruby::Prism, ::Prism -> ::Ruby::Prism; a :Prism Symbol stays
def rename(src)
  src.gsub(/(?<![\w:])(::)?Prism\b/) { "#{$1}Ruby::Prism" }
end

# "prism/x" -> "ruby/prism/x" for require and autoload; require_relative
# paths stay relative
def repath(src)
  src.each_line.map { |l| l.include?('require_relative') ? l : l.gsub(/(["'])prism(?=["\/])/, '\1ruby/prism') }.join
end

def patch(src, from, to, file)
  abort "gen_prism: #{file} no longer has #{from.inspect}" unless src.include?(from)
  src.sub(from, to)
end

Dir.mktmpdir('gen_prism') do |tmp|
  stage = File.join(tmp, 'ruby')
  FileUtils.mkdir_p(stage)
  FileUtils.cp_r(File.join(prism, 'lib/.'), stage)
  FileUtils.rm_f(File.join(stage, 'prism/ffi.rb'))
  TEMPLATES.each do |name|
    dest = File.join(stage, "prism/#{name}.rb")
    system(RbConfig.ruby, 'templates/template.rb', "lib/prism/#{name}.rb", dest, chdir: prism, exception: true)
  end

  Dir[File.join(stage, '**/*.rb')].each do |f|
    src = repath(rename(File.binread(f)))
    if f == File.join(stage, 'prism.rb')
      src = patch(src, "Ruby::Prism::BACKEND = :FFI\n\n  require_relative \"prism/ffi\"",
                  "Ruby::Prism::BACKEND = :ORUBY\n\n  require_relative \"prism/oruby\"", 'prism.rb')
    end
    File.binwrite(f, src)
  end
  File.binwrite(File.join(stage, 'prism/oruby.rb'), File.binread(backend).delete("\r"))
  File.write(File.join(stage, 'prism/.oruby-stamp'), digest.hexdigest)

  FileUtils.mkdir_p(out)
  FileUtils.rm_rf([File.join(out, 'prism.rb'), File.join(out, 'prism')])
  FileUtils.mv(File.join(stage, 'prism.rb'), out)
  FileUtils.mv(File.join(stage, 'prism'), out)
  File.binwrite(File.join(File.dirname(out), 'ripper.rb'), File.binread(ripper).delete("\r"))
end
puts "gen_prism: wrote #{out}/prism.rb, #{out}/prism/ and #{File.dirname(out)}/ripper.rb"
