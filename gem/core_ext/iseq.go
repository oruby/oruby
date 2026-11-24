package core_ext

import "github.com/oruby/oruby"

// RubyVM::InstructionSequence.compile (ruby/43_source_range.rb) checks the
// source with Kernel.__oruby_syntax_check(src, file, line): compiled as
// eval compiles it (Prism's rewrite, then mruby's codegen), not run; a
// SyntaxError is raised as eval raises it.
func initISeq(mrb *oruby.MrbState) {
	mrb.DefineClassMethod(mrb.KernelModule(), "__oruby_syntax_check", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		src := mrb.String(args.Item(0))
		if src == "" {
			return mrb.NilValue()
		}
		c := mrb.MrbCcontextNew()
		defer c.Free()
		c.Filename(mrb.String(args.Item(1)))
		if n := args.Item(2); n.IsInteger() {
			c.SetLineNo(n.Int())
		}
		c.SetNoExec(true)
		c.SetCaptureErrors(true)
		if _, err := mrb.LoadStringCxt(src, c); err != nil {
			return mrb.RaiseError(err)
		}
		return mrb.NilValue()
	}, oruby.ArgsReq(3))
}
