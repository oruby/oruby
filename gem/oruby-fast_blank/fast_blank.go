// Package orubyfastblank implements the Ruby fast_blank gem in pure Go,
// registered as "oruby-fast_blank" and aliased to "fast_blank".
package orubyfastblank

import (
	"unicode"
	"unicode/utf8"

	"github.com/oruby/oruby"
)

func init() {
	oruby.LazyGem("oruby-fast_blank", initFastBlank)
	oruby.GemAlias("fast_blank", "oruby-fast_blank")
}

func isBlank(s string) bool {
	for i := 0; i < len(s); {
		b := s[i]
		if b < utf8.RuneSelf {
			switch b {
			case ' ', '\t', '\n', '\r', '\f', '\v':
				i++
				continue
			default:
				return false
			}
		}
		r, w := utf8.DecodeRuneInString(s[i:])
		if !unicode.IsSpace(r) {
			return false
		}
		i += w
	}
	return true
}

func initFastBlank(mrb *oruby.MrbState) any {
	strCls := mrb.StringClass()
	fn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.BoolValue(isBlank(self.String()))
	}
	strCls.DefineMethod("blank?", fn, mrb.ArgsNone())
	strCls.DefineMethod("blank_as?", fn, mrb.ArgsNone())
	return nil
}
