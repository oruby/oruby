# frozen_string_literal: true

# Prism for code that requires it by its gem name, as IRB does: the prism gem
# is MRI's C extension, while oruby has the one Ruby library over the parser
# in libmruby, MRI 4.1's internal Ruby::Prism, under the name the gem gives
# it. scripts/gen_prism.rb copies this file to lib/oruby/4.1.0/.
require "ruby/prism"

Prism = Ruby::Prism unless defined?(::Prism)
