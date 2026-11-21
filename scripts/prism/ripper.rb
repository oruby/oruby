# frozen_string_literal: true

# Ripper for oruby: Prism's Ripper translation (Ruby::Prism::Translation::Ripper),
# which gives Ripper's API over the Prism parser in place of MRI's parse.y
# based extension. scripts/gen_prism.rb copies this file to lib/oruby/4.1.0/.
require "ruby/prism"

Ripper = Ruby::Prism::Translation::Ripper unless defined?(Ripper)
