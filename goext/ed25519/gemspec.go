package ed25519

import "github.com/oruby/oruby"

func init() {
	oruby.RegisterGemSpec(oruby.GemSpec{
		Name: "ed25519", Version: "1.4.0", Feature: "ed25519",
		Summary: "ORuby pure-Go Ed25519",
		Files:   []string{"lib/ed25519.rb"},
	})
}
