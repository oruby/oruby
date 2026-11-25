package orubysnappy

import "github.com/oruby/oruby"

func init() {
	oruby.RegisterGemSpec(oruby.GemSpec{
		Name: "oruby-snappy", Version: "0.4.0", Feature: "oruby-snappy",
		Summary: "ORuby pure-Go Snappy",
		Files:   []string{"lib/oruby-snappy.rb", "lib/snappy.rb"},
	})
	oruby.RegisterGemSpec(oruby.GemSpec{
		Name: "snappy", Version: "0.4.0", Feature: "oruby-snappy",
		Summary: "ORuby Snappy compatibility provider",
		Files:   []string{"lib/snappy.rb"},
	})
}
