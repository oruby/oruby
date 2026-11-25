package orubyfastblank

import "github.com/oruby/oruby"

func init() {
	oruby.RegisterGemSpec(oruby.GemSpec{
		Name: "oruby-fast_blank", Version: "1.0.1", Feature: "oruby-fast_blank",
		Summary: "ORuby pure-Go fast_blank",
		Files:   []string{"lib/oruby-fast_blank.rb", "lib/fast_blank.rb"},
	})
	oruby.RegisterGemSpec(oruby.GemSpec{
		Name: "fast_blank", Version: "1.0.1", Feature: "oruby-fast_blank",
		Summary: "ORuby fast_blank compatibility provider",
		Files:   []string{"lib/fast_blank.rb"},
	})
}
