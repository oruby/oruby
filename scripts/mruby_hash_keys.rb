# Core source overrides are installed after the mrbc bootstrap build is cloned.
require_relative "mruby_vm_compat"

# Resolve the Hash source wrapper before mruby's standard core source rule.
def oruby_hash_key_rules(build)
  output = File.join(build.build_dir, "src")
  source = File.join(__dir__, "mruby-overrides")
  build.cc.define_rules(output, source, build.exts.object)
  build.cc.define_rules(output, source, build.exts.presym_preprocessed)
  ORubyVMCompat.install(build)
end
