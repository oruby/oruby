package scrypt

import "github.com/oruby/oruby"

func init() {
	oruby.RegisterGemSpec(oruby.GemSpec{
		Name: "scrypt", Version: "3.0.7", Feature: "scrypt",
		Summary: "ORuby pure-Go scrypt",
		Files:   []string{"lib/scrypt.rb"},
	})
}
