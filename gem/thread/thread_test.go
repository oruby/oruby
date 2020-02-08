package thread

import (
	"github.com/oruby/oruby"
	"testing"
)

func TestContext(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	v, err := mrb.Eval(`
		Thread.new { "yea" }.join
	`)
	if err != nil {
		t.Error(err)
	}
	if v.String() != "yea" {
		t.Errorf("expected 'yea' got '%v'", mrb.String(v))
	}
}

