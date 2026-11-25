package zstd

import "github.com/oruby/oruby"

func init() {
	oruby.RegisterGemSpec(oruby.GemSpec{
		Name: "zstd-ruby", Version: "2.0.10", Feature: "zstd-ruby",
		Summary: "ORuby pure-Go Zstandard",
		Files:   []string{"lib/zstd-ruby.rb"},
	})
}
