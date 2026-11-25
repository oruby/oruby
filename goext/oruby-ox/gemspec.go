package orubyox

import "github.com/oruby/oruby"

func init() {
	oruby.RegisterGemSpec(oruby.GemSpec{
		Name: "oruby-ox", Version: "2.14.23", Feature: "oruby-ox",
		Summary: "ORuby pure-Go Ox XML",
		Files:   []string{"lib/oruby-ox.rb", "lib/ox.rb"},
	})
	oruby.RegisterGemSpec(oruby.GemSpec{
		Name: "ox", Version: "2.14.23", Feature: "oruby-ox",
		Summary: "ORuby Ox compatibility provider",
		Files:   []string{"lib/ox.rb"},
	})
}
