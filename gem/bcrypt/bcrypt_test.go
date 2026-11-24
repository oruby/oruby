package bcrypt_test

import (
	"testing"

	gobcrypt "golang.org/x/crypto/bcrypt"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/bcrypt"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/gem/load"
	_ "github.com/oruby/oruby/gem/regexp"
)

func TestBCryptPasswordAndVectors(t *testing.T) {
	mrb, err := oruby.New()
	if err != nil {
		t.Fatal(err)
	}
	defer mrb.Close()

	// Cross-verify against golang.org/x/crypto/bcrypt
	goHash, err := gobcrypt.GenerateFromPassword([]byte("hunter2"), 4)
	if err != nil {
		t.Fatal(err)
	}

	mrb.GVSet(mrb.Intern("$go_hash"), mrb.StrNew(string(goHash)))
	v, err := mrb.Eval(`
		require "bcrypt"

		# OpenBSD / crypt_blowfish known test vectors
		h1 = BCrypt::Engine.hash_secret("", "$2a$06$DCq7YPn5Rq63x1Lad4cll.")
		raise "vec1: #{h1}" unless h1 == "$2a$06$DCq7YPn5Rq63x1Lad4cll.TV4S6ytwfsfvkgY8jIucDrjc8deX1s."

		h2 = BCrypt::Engine.hash_secret("a", "$2a$06$m0CrhHm10qJ3lXRY.5zDGO")
		raise "vec2: #{h2}" unless h2 == "$2a$06$m0CrhHm10qJ3lXRY.5zDGO3rS2KdeeWLuGmsfGlMfOxih58VYVfxe"

		h3 = BCrypt::Engine.hash_secret("abc", "$2a$06$If6bvum7DFjUnE9p2uDeDu")
		raise "vec3: #{h3}" unless h3 == "$2a$06$If6bvum7DFjUnE9p2uDeDu0YHzrHM6tf.iqN8.yx.jNN1ILEf7h0i"

		# Verify hash created by golang.org/x/crypto/bcrypt
		gp = BCrypt::Password.new($go_hash)
		raise "go hash match failed" unless gp == "hunter2"
		raise "go hash wrong password matched" if gp == "wrong"
		raise "cost" unless gp.cost == 4

		# Create password from Ruby and return for Go verification
		pw = BCrypt::Password.create("my grand secret", cost: 4)
		raise "match failed" unless pw == "my grand secret"
		raise "is_password?" unless pw.is_password?("my grand secret")
		raise "wrong password" if pw == "nope"
		raise "version" unless pw.version == "2a"
		raise "cost" unless pw.cost == 4
		raise "salt len" unless pw.salt.bytesize == 29
		raise "checksum len" unless pw.checksum.bytesize == 31

		# Error handling
		bad_hash = false
		begin
			BCrypt::Password.new("not a hash")
		rescue BCrypt::Errors::InvalidHash
			bad_hash = true
		end
		raise "expected InvalidHash" unless bad_hash

		bad_salt = false
		begin
			BCrypt::Engine.hash_secret("x", "bad")
		rescue BCrypt::Errors::InvalidSalt
			bad_salt = true
		end
		raise "expected InvalidSalt" unless bad_salt

		nul_err = false
		begin
			BCrypt::Password.create("foo\0bar", cost: 4)
		rescue ArgumentError
			nul_err = true
		end
		raise "expected ArgumentError for NUL byte" unless nul_err

		pw.to_s
	`)
	if err != nil {
		t.Fatalf("eval failed: %v", err)
	}
	if err := gobcrypt.CompareHashAndPassword([]byte(mrb.String(v)), []byte("my grand secret")); err != nil {
		t.Fatalf("Go bcrypt failed to verify Ruby-created hash %q: %v", mrb.String(v), err)
	}
}
