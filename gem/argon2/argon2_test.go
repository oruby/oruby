package argon2_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/argon2"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/gem/load"
)

func TestArgon2(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	_, err := mrb.Eval(`
require "argon2"

h = Argon2::Password.create("s3cr3t", t_cost: 1, m_cost: 12, p_cost: 1)
raise "phc prefix" unless h.start_with?("$argon2id$v=19$m=4096,t=1,p=1$")
raise "verify ok" unless Argon2::Password.verify_password("s3cr3t", h)
raise "verify bad" if Argon2::Password.verify_password("wrong", h)

# Instance with pepper secret & profile
hasher = Argon2::Password.new(profile: :unsafe_cheapest, secret: "pepper")
h2 = hasher.create("pw")
raise "pepper ok" unless Argon2::Password.verify_password("pw", h2, "pepper")
raise "pepper bad" if Argon2::Password.verify_password("pw", h2, "nope")

# Argon2i via Engine
hi = Argon2::Engine.hash_argon2i_encode("pw", "1234567890123456", 1, 10, 1)
raise "argon2i" unless hi.start_with?("$argon2i$") && Argon2::Engine.argon2_verify("pw", hi)
`)
	if err != nil {
		t.Fatalf("argon2 test failed: %v", err)
	}
}
