package brotli

import "github.com/oruby/oruby"

func init() {
	oruby.RegisterGemSpec(oruby.GemSpec{
		Name: "brotli", Version: "0.6.0", Feature: "brotli",
		Summary: "ORuby pure-Go Brotli",
		Files:   []string{"lib/brotli.rb"},
	})
}
