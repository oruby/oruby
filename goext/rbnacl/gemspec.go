package rbnacl

import "github.com/oruby/oruby"

func init() {
	oruby.RegisterGemSpec(oruby.GemSpec{
		Name: "rbnacl", Version: "7.1.2", Feature: "rbnacl",
		Summary: "ORuby pure-Go RbNaCl",
		Files:   []string{"lib/rbnacl.rb"},
	})
}
