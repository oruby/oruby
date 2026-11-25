package orubylz4

import "github.com/oruby/oruby"

func init() {
	oruby.RegisterGemSpec(oruby.GemSpec{
		Name: "oruby-lz4", Version: "0.3.3", Feature: "oruby-lz4",
		Summary: "ORuby pure-Go LZ4",
		Files:   []string{"lib/oruby-lz4.rb", "lib/lz4-ruby.rb"},
	})
	oruby.RegisterGemSpec(oruby.GemSpec{
		Name: "lz4-ruby", Version: "0.3.3", Feature: "oruby-lz4",
		Summary: "ORuby lz4-ruby compatibility provider",
		Files:   []string{"lib/lz4-ruby.rb"},
	})
}
