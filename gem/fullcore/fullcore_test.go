package fullcore_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/fullcore"
)

func TestFullcoreCompatibilityImport(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	for _, name := range []string{"base64", "io", "load", "pp"} {
		if _, err := mrb.Resolve(name); err != nil {
			t.Errorf("Resolve(%q): %v", name, err)
		}
	}
	if got, err := mrb.Eval(`Base64.strict_encode64("fullcore")`); err != nil || mrb.String(got) != "ZnVsbGNvcmU=" {
		t.Fatalf("Base64.strict_encode64 = %q, %v; want ZnVsbGNvcmU=", mrb.String(got), err)
	}
}
