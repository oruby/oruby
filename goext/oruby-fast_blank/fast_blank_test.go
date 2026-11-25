package orubyfastblank_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/goext/oruby-fast_blank"
	_ "github.com/oruby/oruby/gem/stdlib"
)

func TestFastBlank(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	_, err := mrb.Eval(`
require "fast_blank"

raise "empty" unless "".blank?
raise "spaces" unless "   \t\r\n\f\v ".blank?
raise "nbsp" unless "\u00a0\u3000".blank?
raise "nonblank" if " a ".blank?
raise "blank_as" unless "  ".blank_as? && !"x".blank_as?
`)
	if err != nil {
		t.Fatalf("fast_blank test failed: %v", err)
	}
}
