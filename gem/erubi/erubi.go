package erubi

import (
	"github.com/oruby/oruby"
	"html"
)

// eruby consts
const (
	VERSION     = "1.9.0"
	RANGE_FIRST = 0
	RANGE_LAST  = -1
	TEXT_END    = "'.freeze;"
)

func init() {
	oruby.Gem("erubi", func(mrb *oruby.MrbState) {
		erubi := mrb.DefineModule("Erubi")
		erubi.Const("VERSION", VERSION)
		erubi.Const("RANGE_ALL", mrb.RangeNew(mrb.FixnumValue(0), mrb.FixnumValue(-1), true))
		erubi.Const("RANGE_FIRST", RANGE_FIRST)
		erubi.Const("RANGE_LAST", RANGE_LAST)
		erubi.Const("TEXT_END", TEXT_END)
		erubi.DefineClassMethod("h", erubiH, mrb.ArgsReq(1))

		engine := mrb.DefineClassUnder(erubi, "Engine", mrb.ObjectClass())
		engine.SetAsGoClass(NewInit)

		erb := mrb.DefineClass("ERB", mrb.ObjectClass())
		erb.DefineClassMethod("version", ERBversion, mrb.ArgsNone())
		erb.DefineMethod("initialize", ERBinit, mrb.ArgsArg(1, 3))
	})
}

// erubiH implements Erubi.h() to escape html characters using Go html.EscapeString
// there are minor differences, for example &quot; vs &#37; but shoud work just fine
func erubiH(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	v := mrb.GetArgsFirst()
	s := html.EscapeString(mrb.String(v))

	return mrb.StringValue(s)
}
