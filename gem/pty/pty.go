// Package pty implements Ruby's pty standard library backed by github.com/creack/pty.
package pty

import (
	_ "embed"
	"os"
	"os/exec"
	"runtime"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/rb"
)

//go:embed pty.rb
var ptyRb []byte

func init() {
	oruby.LazyGem("pty", initPTY)
}

func initPTY(mrb *oruby.MrbState) any {
	mrb.Require("io")
	mrb.Require("process")

	mod := mrb.DefineModule("PTY")
	mod.DefineClassMethod("__open", ptyOpen, mrb.ArgsNone())
	mod.DefineClassMethod("__spawn", ptySpawn, mrb.ArgsReq(4))

	rb.LoadRuby(mrb, "<internal:pty>", ptyRb)
	return nil
}

func ptyOpen(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	ptmx, tty, err := openPTY()
	if err != nil {
		return mrb.RaiseError(err)
	}
	cIO := mrb.ClassGet("IO")
	cFile := mrb.ClassGet("File")
	mVal := mrb.DataWrapInterface(cIO, ptmx).Value()
	sVal := mrb.DataWrapInterface(cFile, tty).Value()
	mrb.Call(mVal, "sync=", mrb.TrueValue())
	mrb.Call(sVal, "sync=", mrb.TrueValue())

	ary := mrb.AryNewCapa(2)
	mrb.AryPush(ary, mVal)
	mrb.AryPush(ary, sVal)
	return ary
}

func ptySpawn(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	envVal := args.Item(0)
	cmdVal := args.Item(1)
	argvVal := args.Item(2)
	chdirVal := args.Item(3)

	var cmd *exec.Cmd
	if argvVal.IsNil() {
		if runtime.GOOS == "windows" {
			cmd = exec.Command("cmd", "/c", cmdVal.String())
		} else {
			cmd = exec.Command("/bin/sh", "-c", cmdVal.String())
		}
	} else {
		n := oruby.RArrayLen(argvVal)
		cmdArgs := make([]string, n)
		for i := 0; i < n; i++ {
			cmdArgs[i] = mrb.AryRef(argvVal, i).String()
		}
		cmd = exec.Command(cmdVal.String(), cmdArgs...)
	}

	if !envVal.IsNil() && envVal.IsHash() {
		env := os.Environ()
		keys := mrb.HashKeys(envVal)
		for i := 0; i < oruby.RArrayLen(keys); i++ {
			k := mrb.AryRef(keys, i)
			v, err := mrb.Funcall(envVal, mrb.Intern("[]"), k)
			if err != nil {
				continue
			}
			ks := k.String()
			if v.IsNil() {
				// unset
				filtered := env[:0]
				prefix := ks + "="
				for _, e := range env {
					if len(e) < len(prefix) || e[:len(prefix)] != prefix {
						filtered = append(filtered, e)
					}
				}
				env = filtered
			} else {
				env = append(env, ks+"="+v.String())
			}
		}
		cmd.Env = env
	}

	if !chdirVal.IsNil() {
		cmd.Dir = chdirVal.String()
	}

	reader, writer, pid, err := startPTY(cmd)
	if err != nil {
		return mrb.RaiseError(err)
	}

	cIO := mrb.ClassGet("IO")
	rVal := mrb.DataWrapInterface(cIO, reader).Value()
	wVal := mrb.DataWrapInterface(cIO, writer).Value()
	mrb.Call(rVal, "sync=", mrb.TrueValue())
	mrb.Call(wVal, "sync=", mrb.TrueValue())

	ary := mrb.AryNewCapa(3)
	mrb.AryPush(ary, rVal)
	mrb.AryPush(ary, wVal)
	mrb.AryPush(ary, mrb.FixnumValue(pid))
	return ary
}
