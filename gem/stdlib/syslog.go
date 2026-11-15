package stdlib

import (
	"fmt"
	"log/syslog"
	"os"

	"github.com/oruby/oruby"
)

// Syslog state. The Ruby module (syslog.rb) keeps ident/options/facility/
// mask; Go only owns the connection to the system logger.
var syslogWriters = map[*oruby.MrbState]*syslog.Writer{}

func initSyslog(mrb *oruby.MrbState) interface{} {
	mod := mrb.DefineModule("Syslog")
	mod.DefineClassMethod("__open", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		ident, facility := args.Item(0).String(), args.Item(1).Int()
		if w := syslogWriters[mrb]; w != nil {
			w.Close()
		}
		w, err := syslog.New(syslog.Priority(facility)|syslog.LOG_INFO, ident)
		if err != nil {
			// no system logger available: messages are dropped (LOG_PERROR still works)
			w = nil
		}
		syslogWriters[mrb] = w
		return mrb.NilValue()
	}, mrb.ArgsReq(2))
	mod.DefineClassMethod("__close", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		if w := syslogWriters[mrb]; w != nil {
			w.Close()
		}
		delete(syslogWriters, mrb)
		return mrb.NilValue()
	}, mrb.ArgsNone())
	mod.DefineClassMethod("__write", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		pri, msg, perror, ident := args.Item(0).Int(), args.Item(1).String(), args.Item(2).Bool(), args.Item(3).String()
		if perror {
			fmt.Fprintf(os.Stderr, "%s: %s\n", ident, msg)
		}
		w := syslogWriters[mrb]
		if w == nil {
			return mrb.NilValue()
		}
		var err error
		switch syslog.Priority(pri) & 7 {
		case syslog.LOG_EMERG:
			err = w.Emerg(msg)
		case syslog.LOG_ALERT:
			err = w.Alert(msg)
		case syslog.LOG_CRIT:
			err = w.Crit(msg)
		case syslog.LOG_ERR:
			err = w.Err(msg)
		case syslog.LOG_WARNING:
			err = w.Warning(msg)
		case syslog.LOG_NOTICE:
			err = w.Notice(msg)
		case syslog.LOG_INFO:
			err = w.Info(msg)
		default:
			err = w.Debug(msg)
		}
		_ = err
		return mrb.NilValue()
	}, mrb.ArgsReq(4))
	load(mrb, "syslog")
	return nil
}
