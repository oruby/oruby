package base64

import (
	"github.com/oruby/oruby"
	"testing"
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
		"Tm93IGlzIHRoZSB0aW1lIGZvciBhbGwgZ29vZCBjb2RlcnMKdG8gbGVhcm4gUnVieQ==")
}

func TestBase64StrictDecode(t *testing.T) {
	testBase64(t, `Base64.strict_decode64 "U2VuZCByZWluZm9yY2VtZW50cw=="`,
		"Send reinforcements")
}
