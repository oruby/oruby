package hiredis

import "github.com/oruby/oruby"

func init() {
	oruby.RegisterGemSpec(oruby.GemSpec{
		Name: "hiredis", Version: "0.6.3", Feature: "hiredis",
		Summary: "ORuby pure-Go Hiredis RESP driver",
		Files:   []string{"lib/hiredis.rb"},
	})
}
