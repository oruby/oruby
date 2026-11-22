# Core source overrides are installed after the mrbc bootstrap build is cloned.
require_relative "mruby_vm_compat"
require_relative "mruby_hash_codes"

# Resolve the generated Hash source before mruby's standard core source rule.
def oruby_hash_key_rules(build)
  output = File.join(build.build_dir, "src")
  source = File.join(build.build_dir, "oruby-hash")
  upstream = File.join(MRUBY_ROOT, "src/hash.c")
  wrapper = File.join(__dir__, "mruby-overrides/hash.c")
  generated = File.join(source, "hash.c")
  Rake::FileTask.define_task(generated => [upstream, wrapper, __FILE__, File.join(__dir__, "mruby_hash_codes.rb")]) do
    hash = File.binread(upstream)
    ORubyHashCodes.patch(hash)
    content = File.binread(wrapper)
    ORubyVMCompat.replace(content, '#include "../../mruby/src/hash.c"', hash)
    FileUtils.mkdir_p(source)
    File.binwrite(generated, content)
  end
  build.cc.define_rules(output, source, build.exts.object)
  build.cc.define_rules(output, source, build.exts.presym_preprocessed)
  ORubyVMCompat.install(build)
end
