package digest_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/gem/digest"
	_ "github.com/oruby/oruby/gem/io/file"
	_ "github.com/oruby/oruby/gem/load"
)

func evalDigest(t *testing.T, code string) {
	t.Helper()
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	if _, err := mrb.Eval(code); err != nil {
		t.Fatalf("ruby eval failed:\n%s\nerror: %v", code, err)
	}
}

func TestDigestAlgorithms(t *testing.T) {
	evalDigest(t, `
require "digest"

raise "md5" unless Digest::MD5.hexdigest("abc") == "900150983cd24fb0d6963f7d28e17f72"
raise "sha1" unless Digest::SHA1.hexdigest("abc") == "a9993e364706816aba3e25717850c26c9cd0d89d"
raise "sha256" unless Digest::SHA256.hexdigest("abc") == "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
raise "sha384" unless Digest::SHA384.hexdigest("abc") == "cb00753f45a35e8bb5a03d699ac65007272c32ab0eded1631a8b605a43ff5bed8086072ba1e7cc2358baeca134c825a7"
raise "sha512" unless Digest::SHA512.hexdigest("abc") == "ddaf35a193617abacc417349ae20413112e6fa4e89a97ea20a9eeee64b55d39a2192992a274fc1a836ba3c23a3feebbd454d4423643ce80e2a9ac94fa54ca49f"

raise "Digest()" unless Digest(:SHA256) == Digest::SHA256
raise "SHA2(256)" unless Digest::SHA2.new(256).hexdigest("abc") == Digest::SHA256.hexdigest("abc")
raise "SHA2(512)" unless Digest::SHA2.new(512).hexdigest("abc") == Digest::SHA512.hexdigest("abc")
raise "base64digest" unless Digest::SHA256.base64digest("abc") == "ungWv48Bz+pBQUDeXa4iI7ADYaOWF3qctBD/YfIAFa0="
raise "hexencode" unless Digest.hexencode("\x00\xff\xab") == "00ffab"
raise "bubblebabble" unless Digest.bubblebabble("") == "xexax"
`)
}

func TestDigestIncrementalCloneAndReset(t *testing.T) {
	evalDigest(t, `
require "digest"

h = Digest::SHA256.new
h << "a"
h.update("b")
c = h.clone
h << "c"
c << "d"

raise "h" unless h.hexdigest == Digest::SHA256.hexdigest("abc")
raise "c" unless c.hexdigest == Digest::SHA256.hexdigest("abd")
raise "digest!" unless h.hexdigest! == Digest::SHA256.hexdigest("abc")
raise "reset after !" unless h.hexdigest == Digest::SHA256.hexdigest("")
raise "digest_length" unless h.digest_length == 32
raise "block_length" unless h.block_length == 64
`)
}

func TestDigestFile(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "sample.txt")
	if err := os.WriteFile(tmp, []byte("abc"), 0644); err != nil {
		t.Fatal(err)
	}
	evalDigest(t, `
require "digest"
got = Digest::SHA256.file(`+`"`+filepath.ToSlash(tmp)+`"`+`).hexdigest
raise "file digest mismatch: #{got}" unless got == "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
`)
}
