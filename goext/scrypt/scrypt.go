// Package scrypt provides a pure-Go implementation of the Ruby scrypt gem
// backed by golang.org/x/crypto/scrypt.
package scrypt

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/rb"
	"golang.org/x/crypto/scrypt"
)

func init() {
	oruby.LazyGem("scrypt", initSCrypt)
}

func parseCost(costStr string) (n, r, p int, err error) {
	parts := strings.Split(strings.TrimSuffix(costStr, "$"), "$")
	if len(parts) != 3 {
		return 0, 0, 0, fmt.Errorf("invalid scrypt cost: %q", costStr)
	}
	n64, err := strconv.ParseInt(parts[0], 16, 64)
	if err != nil {
		return 0, 0, 0, err
	}
	r64, err := strconv.ParseInt(parts[1], 16, 64)
	if err != nil {
		return 0, 0, 0, err
	}
	p64, err := strconv.ParseInt(parts[2], 16, 64)
	if err != nil {
		return 0, 0, 0, err
	}
	return int(n64), int(r64), int(p64), nil
}

// Ruby scrypt format: "<hexN>$<hexR>$<hexP>$<hexSalt>$<hexDigest>"
func hashSecret(secret []byte, saltStr string, keyLen int) (string, error) {
	parts := strings.Split(saltStr, "$")
	if len(parts) < 4 {
		return "", fmt.Errorf("invalid scrypt salt")
	}
	n, r, p, err := parseCost(strings.Join(parts[:3], "$") + "$")
	if err != nil {
		return "", err
	}
	saltHex := parts[3]
	dk, err := scrypt.Key(secret, []byte(saltHex), n, r, p, keyLen)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x$%x$%x$%s$%s", n, r, p, saltHex, hex.EncodeToString(dk)), nil
}

func initSCrypt(mrb *oruby.MrbState) any {
	mod := mrb.DefineModule("SCrypt")
	mrb.ConstSet(mod, mrb.Intern("VERSION"), mrb.StrNew("3.0.7"))

	errsMod := mrb.DefineModuleUnder(mod, "Errors")
	stdErr := mrb.ExcGet("StandardError")
	invHash := mrb.DefineClassUnder(errsMod, "InvalidHash", stdErr)
	invSalt := mrb.DefineClassUnder(errsMod, "InvalidSalt", stdErr)
	mrb.DefineClassUnder(errsMod, "InvalidSecret", stdErr)

	// SCrypt::Engine
	eng := mrb.DefineClassUnder(mod, "Engine", mrb.ObjectClass())
	mrb.ConstSet(eng, mrb.Intern("DEFAULTS"), mrb.HashNew().Value())

	eng.DefineClassMethod("scrypt", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		a := mrb.GetArgs().Slice()
		secret, err := rb.Bytes(mrb, a[0])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		salt, err := rb.Bytes(mrb, a[1])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		n, r, p, keyLen := int(a[2].Int64()), int(a[3].Int64()), int(a[4].Int64()), int(a[5].Int64())
		var dk []byte
		mrb.Blocking(func() {
			dk, err = scrypt.Key(secret, salt, n, r, p, keyLen)
		})
		if err != nil {
			return mrb.Raise(invHash, err.Error())
		}
		return mrb.Call(mrb.BytesValue(dk), "force_encoding", mrb.StrNew("ASCII-8BIT"))
	}, mrb.ArgsReq(6))

	eng.DefineClassMethod("calibrate", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		return mrb.StrNew("4000$8$1$")
	}, mrb.ArgsOpt(1)|mrb.ArgsKey(0, 1))
	eng.DefineClassMethod("calibrate!", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		return mrb.StrNew("4000$8$1$")
	}, mrb.ArgsOpt(1)|mrb.ArgsKey(0, 1))

	eng.DefineClassMethod("generate_salt", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		cost := "4000$8$1$"
		saltSize := 8
		parse := func(h oruby.Value) {
			if v, err := mrb.Funcall(h, mrb.Intern("[]"), mrb.Intern("cost").Value()); err == nil && v.IsString() {
				cost = v.String()
			}
			if v, err := mrb.Funcall(h, mrb.Intern("[]"), mrb.Intern("salt_size").Value()); err == nil && v.IsInt() {
				saltSize = int(v.Int64())
			}
		}
		if args := mrb.GetArgs(); args.Len() >= 1 && args.Item(0).IsHash() {
			parse(args.Item(0))
		}
		if kw := mrb.KeywordArgs().Value(); !kw.IsNil() && kw.IsHash() {
			parse(kw)
		}
		raw := make([]byte, saltSize)
		_, _ = rand.Read(raw)
		if !strings.HasSuffix(cost, "$") {
			cost += "$"
		}
		return mrb.StrNew(cost + hex.EncodeToString(raw))
	}, mrb.ArgsOpt(1)|mrb.ArgsKey(0, 1))

	eng.DefineClassMethod("hash_secret", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		a := mrb.GetArgs().Slice()
		secret, err := rb.Bytes(mrb, a[0])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		salt := a[1].String()
		keyLen := 32
		if len(a) >= 3 && a[2].IsInt() {
			keyLen = int(a[2].Int64())
		}
		var out string
		mrb.Blocking(func() {
			out, err = hashSecret(secret, salt, keyLen)
		})
		if err != nil {
			return mrb.Raise(invSalt, err.Error())
		}
		return mrb.StrNew(out)
	}, mrb.ArgsArg(2, 1))

	// SCrypt::Password < String (Ruby wrapper for clean String subclass behavior)
	rb.LoadRuby(mrb, "<internal:scrypt>", []byte(`
module SCrypt
  class Password < String
    attr_reader :cost, :salt, :digest

    def self.create(secret, options = {}, **kw)
      opts = options.merge(kw)
      key_len = opts[:key_len] || 32
      salt = Engine.generate_salt(opts)
      new(Engine.hash_secret(secret.to_s, salt, key_len))
    end

    def initialize(raw_hash)
      parts = raw_hash.to_s.split("$")
      raise Errors::InvalidHash, "invalid scrypt hash" unless parts.length == 5
      @cost = "#{parts[0]}$#{parts[1]}$#{parts[2]}$"
      @salt = parts[3]
      @digest = parts[4]
      super(raw_hash.to_s)
    end

    def ==(secret)
      expected = Engine.hash_secret(secret.to_s, "#{@cost}#{@salt}", @digest.bytesize / 2)
      SCrypt.__secure_eql(to_s, expected)
    end
    alias is_password? ==
  end
end
`))

	mod.DefineClassMethod("__secure_eql", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		a := mrb.GetArgs().Slice()
		return mrb.BoolValue(subtle.ConstantTimeCompare(a[0].Bytes(), a[1].Bytes()) == 1)
	}, mrb.ArgsReq(2))

	return nil
}
