package erubi

import (
	"strings"

	"github.com/oruby/oruby"
)

// eruby consts
const (
	Version    = "1.9.0"
	RangeFirst = 0
	RangeLast  = -1
	TextEnd    = "'.freeze;"
)

func init() {
	oruby.Gem("erubi", func(mrb *oruby.MrbState) interface{} {
		erubi := mrb.DefineModule("Erubi")
		erubi.Const("VERSION", Version)
		erubi.Const("RANGE_ALL", mrb.RangeNew(mrb.FixnumValue(0), mrb.FixnumValue(-1), true))
		erubi.Const("RANGE_FIRST", RangeFirst)
		erubi.Const("RANGE_LAST", RangeLast)
		erubi.Const("TEXT_END", TextEnd)
		erubi.DefineClassMethod("h", erubiH, mrb.ArgsReq(1))

		engine := mrb.DefineClassUnder(erubi, "Engine", mrb.ObjectClass())
		engine.SetAsGoClass(NewInit)

		erb := mrb.DefineClass("ERB", mrb.ObjectClass())
		erb.DefineClassMethod("version", erbVersion, mrb.ArgsNone())
		erb.DefineMethod("initialize", erbInit, mrb.ArgsArg(1, 3))
		return nil
	})
}

var htmlRubyEscaper = strings.NewReplacer(
	`&`, "&amp;",
	`'`, "&#39;", // "&#39;" is shorter than "&apos;" and apos was not in HTML until HTML5.
	`<`, "&lt;",
	`>`, "&gt;",
	`"`, "&quot;", // "&#34;" is shorter than "&quot;", but Ruby returns &quot;
)

// erubiH implements Erubi.h() to escape html characters
func erubiH(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	v := mrb.GetArgsFirst()
	s := htmlRubyEscaper.Replace(mrb.String(v))

	return mrb.StrNew(s)
}
