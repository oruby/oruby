package oruby_test

import (
	"testing"

	"github.com/oruby/oruby"
)

func TestBopExtraSlots(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	mrb.AotInit()
	chk := func(name string, wantInt, wantStr, wantAry bool) {
		t.Helper()
		if g := mrb.AotIntBitOpsRedefined(); g != wantInt {
			t.Errorf("%s: int bit ops redefined = %v, want %v", name, g, wantInt)
		}
		if g := mrb.AotAryPushRedefined(); g != wantAry {
			t.Errorf("%s: Array#<< redefined = %v, want %v", name, g, wantAry)
		}
	}
	chk("boot", false, false, false)
	mrb.Eval(`class Foo; def %(x)=1; def <<(x)=2; def &(x)=3; end; class MyAry < Array; def <<(x)=1; end; class MyStr < String; def <<(x)=1; end; o = Object.new; def o.<<(x)=1; end`)
	chk("user classes", false, false, false)
	mrb.Eval(`class Integer; alias_method :old_mod, :%; def %(o) = old_mod(o); end`)
	chk("Integer#%", true, false, false)
	mrb.Eval(`class Integer; alias_method :%, :old_mod; end`)
	chk("restored", false, false, false)
	mrb.Eval(`class String; alias_method :old_cat, :<<; def <<(o) = old_cat(o); end`)
	chk("String#<<", false, true, false)
	mrb.Eval(`Array.prepend(Module.new { def <<(x) = super })`)
	chk("prepend Array", false, true, true)
}
