package main

import (
	// C extensions compiled in through the Ruby C API (standalone oruby
	// runs one mrb_state, which the C API binds to)
	_ "github.com/oruby/oruby/gem/oj"
	_ "github.com/oruby/oruby/gem/rbs"
	_ "github.com/oruby/oruby/gem/stdlib"
)
