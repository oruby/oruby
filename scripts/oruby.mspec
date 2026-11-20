# mspec configuration for oruby, loaded by scripts/spec_run.rb (mspec-run -B).
# mspec's helpers/numeric.rb knows the Fixnum range only for the engines it
# lists and defines raising fixnum_max/fixnum_min on Object for any other
# RUBY_ENGINE. oruby's (mruby with MRB_INT64 and MRB_NO_BOXING) is the whole
# int64 range: 2**63-1 is still immediate, 2**63 is the first Bignum.
# A spec file loads the helpers (its spec_helper) after this file, so the
# real ones are defined before the first example, in place of mspec's (no
# module or constant of our own, which ancestors and Marshal would show).
fixnum_helpers = Object.new
def fixnum_helpers.before(_state)
  return if @done
  @done = true
  Object.class_eval do
    def fixnum_max = 9223372036854775807
    def fixnum_min = -9223372036854775808
    private :fixnum_max, :fixnum_min
  end
end
MSpec.register :before, fixnum_helpers
