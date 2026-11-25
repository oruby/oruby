package bcrypt

import "github.com/oruby/oruby"

func init() {
	oruby.RegisterGemSpec(oruby.GemSpec{
		Name: "bcrypt", Version: "3.1.20", Feature: "bcrypt",
		Summary: "ORuby pure-Go bcrypt",
		Files:   []string{"lib/bcrypt.rb"},
	})
}
