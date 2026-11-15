package base64

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/core_ext"
)

func testBase64(t *testing.T, ruby, result string) {
	t.Helper()

	mrb := oruby.MrbOpen()
	defer mrb.Close()

	v, err := mrb.Eval(ruby)
	if err != nil {
		t.Fatal(err)
	}

	if mrb.String(v) != result {
		s := mrb.String(v)
		t.Errorf("Decoded string not OK, expected: \n%v got \n%v", result, s)
	}

}

func TestBase64Decode(t *testing.T) {
	testBase64(t, `
		Base64.decode64 'VGhpcyBpcyBsaW5lIG9uZQpUaGlzIGlzIGxpbmUgdHdvClRoaXMgaXMgbGluZSB0aHJlZQpBbmQgc28gb24uLi4K'
	`, "This is line one\nThis is line two\nThis is line three\nAnd so on...\n")
}

func TestBase64Encode(t *testing.T) {
	testBase64(t, `Base64.encode64 "Now is the time for all good coders\nto learn Ruby"`,
		"Tm93IGlzIHRoZSB0aW1lIGZvciBhbGwgZ29vZCBjb2RlcnMKdG8gbGVhcm4g\nUnVieQ==\n")
}

func TestBase64StrictDecode(t *testing.T) {
	testBase64(t, `Base64.strict_decode64 "U2VuZCByZWluZm9yY2VtZW50cw=="`,
		"Send reinforcements")
}

func TestBase64RubyCompatibility(t *testing.T) {
	cases := []struct {
		name string
		ruby string
		want string
	}{
		{"encode64 line terminator", `Base64.encode64('Send reinforcements')`, "U2VuZCByZWluZm9yY2VtZW50cw==\n"},
		{"decode64 invalid characters", `Base64.decode64('%3D')`, "\xDC"},
		{"decode64 missing padding", `Base64.decode64('MQ')`, "1"},
		{"decode64 malformed padding", `Base64.decode64('M=Q==')`, "1"},
		{"decode64 stops after padding", `Base64.decode64('MQ==garbage')`, "1"},
		{"decode64 wrong padding", `Base64.decode64('XU2VuZCByZWluZm9yY2VtZW50cw===')`, "]M\x95\xB9\x90\x81\xC9\x95\xA5\xB9\x99\xBD\xC9\x8D\x95\xB5\x95\xB9\xD1\xCC"},
		{"strict encode no wrapping", `Base64.strict_encode64("Now is the time for all good coders\nto learn Ruby")`, "Tm93IGlzIHRoZSB0aW1lIGZvciBhbGwgZ29vZCBjb2RlcnMKdG8gbGVhcm4gUnVieQ=="},
		{"url encode default padding", `Base64.urlsafe_encode64('1')`, "MQ=="},
		{"url encode without padding", `Base64.urlsafe_encode64('1', padding: false)`, "MQ"},
		{"url encode nil padding", `Base64.urlsafe_encode64('1', padding: nil)`, "MQ"},
		{"url encode with padding", `Base64.urlsafe_encode64('1', padding: true)`, "MQ=="},
		{"url encode slash replacement", `Base64.urlsafe_encode64('Where am I? Who am I? Am I? I?')`, "V2hlcmUgYW0gST8gV2hvIGFtIEk_IEFtIEk_IEk_"},
		{"url encode plus replacement", `Base64.urlsafe_encode64('"Being disintegrated makes me ve-ry an-gry!" <huff, huff>')`, "IkJlaW5nIGRpc2ludGVncmF0ZWQgbWFrZXMgbWUgdmUtcnkgYW4tZ3J5ISIgPGh1ZmYsIGh1ZmY-"},
		{"url decode unpadded", `Base64.urlsafe_decode64('MQ')`, "1"},
		{"url decode padded", `Base64.urlsafe_decode64('MQ==')`, "1"},
		{"url decode slash replacement", `Base64.urlsafe_decode64('V2hlcmUgYW0gST8gV2hvIGFtIEk_IEFtIEk_IEk_')`, "Where am I? Who am I? Am I? I?"},
		{"url decode plus replacement", `Base64.urlsafe_decode64('IkJlaW5nIGRpc2ludGVncmF0ZWQgbWFrZXMgbWUgdmUtcnkgYW4tZ3J5ISIgPGh1ZmYsIGh1ZmY-')`, `"Being disintegrated makes me ve-ry an-gry!" <huff, huff>`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			testBase64(t, tc.ruby, tc.want)
		})
	}
}

func TestBase64RejectsInvalidStrictInput(t *testing.T) {
	for _, ruby := range []string{
		`Base64.strict_decode64("U2VuZCByZWluZm9yY2VtZW50cw==\n")`,
		`Base64.strict_decode64("%3D")`,
		`Base64.urlsafe_decode64("MQ=")`,
		`Base64.urlsafe_encode64('1', false)`,
		`Base64.urlsafe_encode64('1', paddingg: false)`,
	} {
		t.Run(ruby, func(t *testing.T) {
			mrb := oruby.MrbOpen()
			defer mrb.Close()
			if _, err := mrb.Eval(ruby); err == nil {
				t.Fatalf("expected ArgumentError for %s", ruby)
			}
		})
	}
}

func TestBase64ResultEncodings(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	if _, err := mrb.Resolve("core_ext"); err != nil {
		t.Fatal(err)
	}
	if _, err := mrb.Resolve("base64"); err != nil {
		t.Fatal(err)
	}

	got, err := mrb.Eval(`
    Base64.encode64('HI').encoding == Encoding::US_ASCII &&
      Base64.strict_encode64('HI').encoding == Encoding::US_ASCII &&
      Base64.urlsafe_encode64('HI').encoding == Encoding::US_ASCII &&
      Base64.decode64('SEk=').encoding == Encoding::BINARY &&
      Base64.strict_decode64('SEk=').encoding == Encoding::BINARY &&
      Base64.urlsafe_decode64('SEk=').encoding == Encoding::BINARY
  `)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Bool() {
		t.Fatal("Base64 methods should return the Ruby-compatible result encodings")
	}
}
