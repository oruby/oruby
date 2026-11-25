package bcrypt_pbkdf

import "github.com/oruby/oruby"

func init() {
	oruby.RegisterGemSpec(oruby.GemSpec{
		Name: "bcrypt_pbkdf", Version: "1.1.1", Feature: "bcrypt_pbkdf",
		Summary: "ORuby pure-Go bcrypt_pbkdf",
		Files:   []string{"lib/bcrypt_pbkdf.rb"},
	})
}
