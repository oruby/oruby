package oj

import "github.com/oruby/oruby"

func init() {
	oruby.RegisterGemSpec(oruby.GemSpec{
		Name: "oj", Version: "3.16.7", Feature: "oj",
		Summary: "ORuby pure-Go Oj JSON",
		Files:   []string{"lib/oj.rb"},
	})
}
