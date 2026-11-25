package orubyescapeutils

import "github.com/oruby/oruby"

func init() {
	oruby.RegisterGemSpec(oruby.GemSpec{
		Name: "oruby-escape_utils", Version: "1.3.0", Feature: "oruby-escape_utils",
		Summary: "ORuby pure-Go EscapeUtils",
		Files:   []string{"lib/oruby-escape_utils.rb", "lib/escape_utils.rb"},
	})
	oruby.RegisterGemSpec(oruby.GemSpec{
		Name: "escape_utils", Version: "1.3.0", Feature: "oruby-escape_utils",
		Summary: "ORuby escape_utils compatibility provider",
		Files:   []string{"lib/escape_utils.rb"},
	})
}
