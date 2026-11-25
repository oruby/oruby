package orubyxxhash

import "github.com/oruby/oruby"

func init() {
	oruby.RegisterGemSpec(oruby.GemSpec{
		Name: "oruby-xxhash", Version: "0.6.0", Feature: "oruby-xxhash",
		Summary: "ORuby pure-Go XXhash",
		Files:   []string{"lib/oruby-xxhash.rb", "lib/xxhash.rb"},
	})
	oruby.RegisterGemSpec(oruby.GemSpec{
		Name: "xxhash", Version: "0.6.0", Feature: "oruby-xxhash",
		Summary: "ORuby xxhash compatibility provider",
		Files:   []string{"lib/xxhash.rb"},
	})
}
