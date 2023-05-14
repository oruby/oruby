package thread

import (
	"testing"

	"github.com/oruby/oruby"
)

func TestContext(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	v, err := mrb.Eval(`
		t = Thread.new { "yea" }.join
	`)
	if err != nil {
		t.Error(err)
		return
	}

	if v.String() != "yea" {
		t.Errorf("expected 'yea' got '%v'", mrb.String(v))
	}
}
