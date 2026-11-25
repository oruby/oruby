package argon2

import "github.com/oruby/oruby"

func init() {
	oruby.RegisterGemSpec(oruby.GemSpec{
		Name: "argon2", Version: "2.3.0", Feature: "argon2",
		Summary: "ORuby pure-Go Argon2",
		Files:   []string{"lib/argon2.rb"},
	})
}
