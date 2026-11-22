// Package console implements Ruby's io/console standard library in Go using
// native termios/ioctl (Unix) and Console API (Windows).
package console

import (
	_ "embed"
	"os"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/internal/rb"
	goio "github.com/oruby/oruby/gem/io"
)

//go:embed console.rb
var consoleRb []byte

const (
	flushInput  = 1
	flushOutput = 2
	flushBoth   = 3
)

func init() {
	oruby.LazyGem("io/console", initConsole)
	oruby.GemAlias("io/console/size", "io/console")
}

func initConsole(mrb *oruby.MrbState) any {
	mrb.Require("io")
	mrb.Require("stringio")

	cIO := mrb.ClassGet("IO")
	consoleMod := mrb.DefineModuleUnder(cIO, "Console")
	mrb.ConstSet(consoleMod, mrb.Intern("VERSION"), mrb.StrNew("0.8.2"))

	modeCls := mrb.DefineClassUnder(consoleMod, "Mode", mrb.ObjectClass())
	rb.DataClass(modeCls)
	mrb.ConstSet(cIO, mrb.Intern("ConsoleMode"), modeCls)

	defineModeMethods(mrb, modeCls)
	defineIOMethods(mrb, cIO, modeCls)

	rb.LoadRuby(mrb, "<internal:io/console>", consoleRb)
	return nil
}

func ioFd(mrb *oruby.MrbState, self oruby.Value) (int, error) {
	if f, ok := mrb.Data(self).(*os.File); ok {
		return goio.RawFd(f), nil
	}
	v, err := mrb.Funcall(self, mrb.Intern("fileno"))
	if err != nil {
		return -1, err
	}
	return v.Int(), nil
}

func getMode(mrb *oruby.MrbState, self oruby.Value) *termMode {
	m, _ := rb.Get[*termMode](mrb, self)
	return m
}

func optKwInt(mrb *oruby.MrbState, kw oruby.Value, name string, def int) int {
	if kw.IsNil() || !kw.IsHash() {
		return def
	}
	v, err := mrb.Funcall(kw, mrb.Intern("[]"), mrb.Intern(name).Value())
	if err != nil || v.IsNil() {
		return def
	}
	return v.Int()
}

func optKwBool(mrb *oruby.MrbState, kw oruby.Value, name string, def bool) bool {
	if kw.IsNil() || !kw.IsHash() {
		return def
	}
	v, err := mrb.Funcall(kw, mrb.Intern("[]"), mrb.Intern(name).Value())
	if err != nil || v.IsNil() {
		return def
	}
	return v.Bool()
}

func defineModeMethods(mrb *oruby.MrbState, cls oruby.RClass) {
	cls.DefineMethod("dup", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.DataWrapInterface(cls, getMode(mrb, self).clone()).Value()
	}, mrb.ArgsNone())

	cls.DefineMethod("echo?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.BoolValue(getMode(mrb, self).echo())
	}, mrb.ArgsNone())

	cls.DefineMethod("echo=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		on := mrb.GetArgsFirst().Bool()
		getMode(mrb, self).setEcho(on)
		return mrb.BoolValue(on)
	}, mrb.ArgsReq(1))

	cls.DefineMethod("echo", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		on := mrb.GetArgsFirst().Bool()
		cp := getMode(mrb, self).clone()
		cp.setEcho(on)
		return mrb.DataWrapInterface(cls, cp).Value()
	}, mrb.ArgsReq(1))

	cls.DefineMethod("raw!", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		kw := mrb.KeywordArgs().Value()
		minV := optKwInt(mrb, kw, "min", 1)
		timeV := optKwInt(mrb, kw, "time", 0)
		intr := optKwBool(mrb, kw, "intr", false)
		getMode(mrb, self).setRaw(minV, timeV, intr)
		return self
	}, mrb.ArgsAny())

	cls.DefineMethod("raw", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		kw := mrb.KeywordArgs().Value()
		minV := optKwInt(mrb, kw, "min", 1)
		timeV := optKwInt(mrb, kw, "time", 0)
		intr := optKwBool(mrb, kw, "intr", false)
		cp := getMode(mrb, self).clone()
		cp.setRaw(minV, timeV, intr)
		return mrb.DataWrapInterface(cls, cp).Value()
	}, mrb.ArgsAny())

	cls.DefineMethod("cooked!", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		getMode(mrb, self).setCooked()
		return self
	}, mrb.ArgsNone())

	cls.DefineMethod("cooked", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		cp := getMode(mrb, self).clone()
		cp.setCooked()
		return mrb.DataWrapInterface(cls, cp).Value()
	}, mrb.ArgsNone())

	cls.DefineMethod("min", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.FixnumValue(getMode(mrb, self).min())
	}, mrb.ArgsNone())

	cls.DefineMethod("min=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst().Int()
		getMode(mrb, self).setMin(v)
		return mrb.FixnumValue(v)
	}, mrb.ArgsReq(1))

	cls.DefineMethod("time", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.FixnumValue(getMode(mrb, self).time())
	}, mrb.ArgsNone())

	cls.DefineMethod("time=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst().Int()
		getMode(mrb, self).setTime(v)
		return mrb.FixnumValue(v)
	}, mrb.ArgsReq(1))
}

func defineIOMethods(mrb *oruby.MrbState, cIO oruby.RClass, modeCls oruby.RClass) {
	cIO.DefineMethod("console_mode", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		fd, err := ioFd(mrb, self)
		if err != nil {
			return mrb.RaiseError(err)
		}
		m, err := getTermMode(fd)
		if err != nil {
			return mrb.RaiseError(err)
		}
		return mrb.DataWrapInterface(modeCls, m).Value()
	}, mrb.ArgsNone())

	cIO.DefineMethod("console_mode=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		fd, err := ioFd(mrb, self)
		if err != nil {
			return mrb.RaiseError(err)
		}
		modeVal := mrb.GetArgsFirst()
		m, ok := rb.Get[*termMode](mrb, modeVal)
		if !ok {
			return mrb.ETypeError().Raise("expected IO::ConsoleMode")
		}
		if err := setTermMode(fd, m); err != nil {
			return mrb.RaiseError(err)
		}
		return modeVal
	}, mrb.ArgsReq(1))

	cIO.DefineMethod("echo?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		fd, err := ioFd(mrb, self)
		if err != nil {
			return mrb.RaiseError(err)
		}
		m, err := getTermMode(fd)
		if err != nil {
			return mrb.RaiseError(err)
		}
		return mrb.BoolValue(m.echo())
	}, mrb.ArgsNone())

	cIO.DefineMethod("echo=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		fd, err := ioFd(mrb, self)
		if err != nil {
			return mrb.RaiseError(err)
		}
		on := mrb.GetArgsFirst().Bool()
		m, err := getTermMode(fd)
		if err != nil {
			return mrb.RaiseError(err)
		}
		m.setEcho(on)
		if err := setTermMode(fd, m); err != nil {
			return mrb.RaiseError(err)
		}
		return mrb.BoolValue(on)
	}, mrb.ArgsReq(1))

	cIO.DefineMethod("raw!", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		fd, err := ioFd(mrb, self)
		if err != nil {
			return mrb.RaiseError(err)
		}
		kw := mrb.KeywordArgs().Value()
		minV := optKwInt(mrb, kw, "min", 1)
		timeV := optKwInt(mrb, kw, "time", 0)
		intr := optKwBool(mrb, kw, "intr", false)
		m, err := getTermMode(fd)
		if err != nil {
			return mrb.RaiseError(err)
		}
		m.setRaw(minV, timeV, intr)
		if err := setTermMode(fd, m); err != nil {
			return mrb.RaiseError(err)
		}
		return self
	}, mrb.ArgsAny())

	cIO.DefineMethod("cooked!", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		fd, err := ioFd(mrb, self)
		if err != nil {
			return mrb.RaiseError(err)
		}
		m, err := getTermMode(fd)
		if err != nil {
			return mrb.RaiseError(err)
		}
		m.setCooked()
		if err := setTermMode(fd, m); err != nil {
			return mrb.RaiseError(err)
		}
		return self
	}, mrb.ArgsNone())

	cIO.DefineMethod("winsize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		fd, err := ioFd(mrb, self)
		if err != nil {
			return mrb.RaiseError(err)
		}
		rows, cols, err := getWinsize(fd)
		if err != nil {
			return mrb.RaiseError(err)
		}
		ary := mrb.AryNewCapa(2)
		mrb.AryPush(ary, mrb.FixnumValue(rows))
		mrb.AryPush(ary, mrb.FixnumValue(cols))
		return ary
	}, mrb.ArgsNone())

	cIO.DefineMethod("winsize=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		fd, err := ioFd(mrb, self)
		if err != nil {
			return mrb.RaiseError(err)
		}
		sizeVal := mrb.GetArgsFirst()
		if !sizeVal.IsArray() || oruby.RArrayLen(sizeVal) < 2 {
			return mrb.EArgumentError().Raise("expected [rows, cols] Array")
		}
		rows := mrb.AryRef(sizeVal, 0).Int()
		cols := mrb.AryRef(sizeVal, 1).Int()
		if err := setWinsize(fd, rows, cols); err != nil {
			return mrb.RaiseError(err)
		}
		return sizeVal
	}, mrb.ArgsReq(1))

	cIO.DefineMethod("iflush", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		fd, err := ioFd(mrb, self)
		if err != nil {
			return mrb.RaiseError(err)
		}
		if err := flushConsole(fd, flushInput); err != nil {
			return mrb.RaiseError(err)
		}
		return self
	}, mrb.ArgsNone())

	cIO.DefineMethod("oflush", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		fd, err := ioFd(mrb, self)
		if err != nil {
			return mrb.RaiseError(err)
		}
		if err := flushConsole(fd, flushOutput); err != nil {
			return mrb.RaiseError(err)
		}
		return self
	}, mrb.ArgsNone())

	cIO.DefineMethod("ioflush", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		fd, err := ioFd(mrb, self)
		if err != nil {
			return mrb.RaiseError(err)
		}
		if err := flushConsole(fd, flushBoth); err != nil {
			return mrb.RaiseError(err)
		}
		return self
	}, mrb.ArgsNone())

	cIO.DefineMethod("ttyname", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		fd, err := ioFd(mrb, self)
		if err != nil {
			return mrb.RaiseError(err)
		}
		name, err := ttyName(fd)
		if err != nil {
			return mrb.RaiseError(err)
		}
		return mrb.StrNew(name)
	}, mrb.ArgsNone())
}
