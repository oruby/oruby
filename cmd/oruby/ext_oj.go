package main

import (
	// Oj C extension compiled in through the Ruby C API. Kept in its own file
	// so `oruby build` can omit it if a project selects goext/oruby-oj instead.
	_ "github.com/oruby/oruby/gem/oj"
)
