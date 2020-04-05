package io

import (
	"errors"
	"github.com/oruby/oruby"
	"io"
	"os"
)

type stringio struct {
	mrb *oruby.MrbState
	value oruby.Value
	pos int
	closed bool
}

func (sio *stringio) Write(p []byte) (int, error) {
	if sio.closed {
		return 0, os.ErrClosed
	}

	v := sio.value.String()
	if len(v) > sio.pos {
		sio.mrb.StrResize(sio.value, 0)
		wr := v[:sio.pos] + string(p)

		if sio.pos+len(p) < len(v) {
			wr += v[sio.pos+len(p):]
		}
		sio.mrb.StrCatCstr(sio.value, wr)
	} else {
		sio.mrb.StrCatBytes(sio.value, p)
	}

	sio.pos += len(p)
	return len(p), nil
}

func (sio *stringio) WriteString(s string) (int, error) {
	return sio.Write([]byte(s))
}

func (sio *stringio) Close() error {
	if sio.closed {
		return os.ErrClosed
	}
	sio.closed = true
	return nil
}

func (sio *stringio) Read(p []byte) (int, error) {
	if sio.closed {
		return 0, os.ErrClosed
	}

	v := sio.value.String()
	copy(p, v[sio.pos:])

	sio.pos += len(p)
	return len(p), nil
}

// Seek seeks in the buffer of this WriterSeeker instance
func (sio *stringio) Seek(offset int64, whence int) (int64, error) {
	if sio.closed {
		return 0, os.ErrClosed
	}

	l := oruby.RStringLen(sio.value)

	newPos, offs := 0, int(offset)
	switch whence {
	case io.SeekStart:
		newPos = offs
	case io.SeekCurrent:
		newPos = sio.pos + offs
	case io.SeekEnd:
		newPos = l - offs
	}
	if newPos < 0 {
		return 0, errors.New("negative result pos")
	} else if newPos >= l {
		newPos = l-1
	}
	sio.pos = newPos
	return int64(newPos), nil
}

func initStringIO(mrb *oruby.MrbState, cIO oruby.RClass) {
	stringIO := mrb.DefineClass("StringIO", cIO)
	stringIO.DefineClassMethod("open", sioOpen, mrb.ArgsReq(1))
	stringIO.DefineMethod("initialize", sioInit, mrb.ArgsReq(1))
	stringIO.DefineMethod("tty?", sioIsTty, mrb.ArgsNone())
	stringIO.DefineMethod("fileno", sioFileno, mrb.ArgsNone())
	stringIO.DefineMethod("fcntl", mrb.NotImplemented, mrb.ArgsNone())
	stringIO.DefineAlias("isatty", "tty?")
	stringIO.DefineMethod("truncate", sioTruncate, mrb.ArgsNone())
	stringIO.DefineMethod("size", sioSize, mrb.ArgsNone())
	stringIO.DefineMethod("string=", sioSetString, mrb.ArgsReq(1))
	stringIO.DefineMethod("string", sioString, mrb.ArgsReq(1))
}

func sioOpen(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	arg := mrb.GetArgs().Item(0)
	block := mrb.GetArgsBlock()
	ret, err := mrb.ObjNew(mrb.ClassPtr(self), arg)
	if err != nil {
		return mrb.RaiseError(err)
	}

	if block.IsNil() {
		return ret
	}

	return mrb.YieldCont(block, self, ret)
}

func sioInit(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	arg := mrb.GetArgs().Item(0)

	switch arg.Type() {
	case oruby.MrbTTString:
	case oruby.MrbTTFalse:
		arg = mrb.StrNew("")
	default:
		return mrb.EArgumentError().Raisef("No implicit conversion of %v into String", mrb.TypeName(arg))
	}

		mrb.SetIV(self, "@string", arg)
	sio := &stringio{
		mrb,
		arg,
		0,
		false,
	}

	//_, err := sio.Buffer.Write(arg.Bytes())
	//if err != nil {		return mrb.RaiseError(err)	}

	mrb.DataSetInterface(self, sio)
	return self
}

func sioTruncate(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	sio := mrb.Data(self).(*stringio)
	if sio.closed {
		return mrb.ESystemCallError().RaiseError(os.ErrClosed)
	}
	sio.pos = 0
	sio.mrb.StrResize(sio.value, 0)
	return oruby.Int(0)
}

func sioSize(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	length := mrb.GetIV(self, "@string").Len()
	return oruby.Int(length)
}

func sioIsTty(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.FalseValue()
}

func sioFileno(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.NilValue()
}

func sioString(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.GetIV(self, "@string")
}

func sioSetString(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	str := mrb.GetArgsFirst()
	if !str.IsString() {
		return mrb.ETypeError().Raisef("no implicit conversion of %v into String", mrb.TypeName(str))
	}

	mrb.SetIV(self, "@string", str)

	sio := mrb.Data(self).(*stringio)
	sio.value = str
	sio.pos = 0
	sio.closed = false

	return str
}
