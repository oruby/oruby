package orubyredcarpet

import "github.com/oruby/oruby"

func init() {
	oruby.RegisterGemSpec(oruby.GemSpec{
		Name: "oruby-redcarpet", Version: "3.6.0", Feature: "oruby-redcarpet",
		Summary: "ORuby pure-Go Redcarpet (Goldmark)",
		Files:   []string{"lib/oruby-redcarpet.rb", "lib/redcarpet.rb"},
	})
	oruby.RegisterGemSpec(oruby.GemSpec{
		Name: "redcarpet", Version: "3.6.0", Feature: "oruby-redcarpet",
		Summary: "ORuby Redcarpet compatibility provider",
		Files:   []string{"lib/redcarpet.rb"},
	})
}
