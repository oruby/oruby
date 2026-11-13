package oruby

import "fmt"

func initPrint(mrb *MrbState) interface{} {
	// Override oruby print functions so mrb output behave as go output
	kernel := mrb.KernelModule()

	kernel.DefineMethod("__printstr__", printStr, mrb.ArgsAny())
	kernel.DefineMethod("print", printPrint, mrb.ArgsAny())
	kernel.DefineMethod("puts", printPuts, mrb.ArgsAny())
	kernel.DefineMethod("p", printP, mrb.ArgsAny())
	kernel.DefineMethod("printf", printPrintf, mrb.ArgsAny())
	kernel.DefineMethod("defined?", mrubyDefined, mrb.ArgsAny())

	if mrb.RespondTo(kernel.Value(), mrb.Intern("Integer")) {
		kernel.DefineAlias("_mrb_original_Integer", "Integer")
		kernel.DefineMethod("Integer", kernelInteger, mrb.ArgsAny())
	}
	if mrb.RespondTo(kernel.Value(), mrb.Intern("Float")) {
		kernel.DefineAlias("_mrb_original_Float", "Float")
		kernel.DefineMethod("Float", kernelFloat, mrb.ArgsAny())
	}

	kernel.DefineMethod("at_exit", kernelAtExit, mrb.ArgsBlock())
	if mrb.RespondTo(kernel.Value(), mrb.Intern("exit")) {
		kernel.DefineAlias("_mrb_original_exit", "exit")
		kernel.DefineMethod("exit", kernelExit, mrb.ArgsOpt(1))
	}

	return nil
}

func kernelAtExit(mrb *MrbState, self Value) MrbValue {
	block := mrb.GetArgsBlock()
	if block.IsNil() {
		return mrb.Raise(mrb.EArgumentError(), "tried to create Proc object without a block")
	}

	listVal := mrb.GetGV("$__at_exit_list__")
	var list RArray
	if listVal.IsNil() {
		list = mrb.AryNew()
		mrb.SetGV("$__at_exit_list__", list.Value())
	} else {
		list = mrb.GVGetObj(mrb.Intern("$__at_exit_list__")).RArray()
	}

	list.Unshift(block)
	return block
}

func (mrb *MrbState) RunAtExitHandlers() {
	listVal := mrb.GetGV("$__at_exit_list__")
	if listVal.IsNil() {
		return
	}
	list := mrb.GVGetObj(mrb.Intern("$__at_exit_list__")).RArray()
	mrb.SetGV("$__at_exit_list__", mrb.NilValue())
	for list.Len() > 0 {
		handler := list.Shift()
		if !handler.IsNil() {
			mrb.Yield(handler, mrb.NilValue())
		}
	}
}

func kernelExit(mrb *MrbState, self Value) MrbValue {
	args := mrb.GetArgs()
	mrb.RunAtExitHandlers()
	return mrb.Call(self, "_mrb_original_exit", args.SliceIntf()...)
}

func kernelInteger(mrb *MrbState, self Value) MrbValue {
	args := mrb.GetArgs()
	if args.Len() == 0 {
		return mrb.Raise(mrb.EArgumentError(), "wrong number of arguments (given 0, expected 1..2)")
	}
	arg := args.Item(0)

	to_int := mrb.Intern("to_int")
	if mrb.RespondTo(arg, to_int) {
		res, err := mrb.FuncallWithBlock(arg, to_int)
		if err != nil {
			return mrb.RaiseError(err)
		}
		return res
	}

	if arg.Type() != MrbTTString {
		to_i := mrb.Intern("to_i")
		if mrb.RespondTo(arg, to_i) {
			res, err := mrb.FuncallWithBlock(arg, to_i)
			if err != nil {
				return mrb.RaiseError(err)
			}
			return res
		}
	}

	res, err := mrb.FuncallWithBlock(self, mrb.Intern("_mrb_original_Integer"), args.SliceIntf()...)
	if err != nil {
		return mrb.RaiseError(err)
	}
	return res
}

func kernelFloat(mrb *MrbState, self Value) MrbValue {
	args := mrb.GetArgs()
	if args.Len() == 0 {
		return mrb.Raise(mrb.EArgumentError(), "wrong number of arguments (given 0, expected 1)")
	}
	arg := args.Item(0)

	to_f := mrb.Intern("to_f")
	if arg.Type() != MrbTTString && mrb.RespondTo(arg, to_f) {
		res, err := mrb.FuncallWithBlock(arg, to_f)
		if err != nil {
			return mrb.RaiseError(err)
		}
		return res
	}

	res, err := mrb.FuncallWithBlock(self, mrb.Intern("_mrb_original_Float"), args.SliceIntf()...)
	if err != nil {
		return mrb.RaiseError(err)
	}
	return res
}

func mrubyDefined(mrb *MrbState, self Value) MrbValue {
	arg := mrb.GetArgsFirst()
	if arg.IsNil() && mrb.GetArgsCount() == 0 {
		return mrb.NilValue()
	}
	return arg
}

func printStr(mrb *MrbState, self Value) MrbValue {
	arg := mrb.GetArgsFirst()
	if arg.IsNil() && mrb.GetArgsCount() == 0 {
		return mrb.NilValue()
	}

	fmt.Print(mrb.Intf(arg))
	return arg
}

func printPrint(mrb *MrbState, self Value) MrbValue {
	args := mrb.GetArgs()
	for i := 0; i < args.Len(); i++ {
		fmt.Print(mrb.Intf(args.Item(i)))
	}
	return mrb.NilValue()
}

func printPuts(mrb *MrbState, self Value) MrbValue {
	args := mrb.GetArgs()
	toS := mrb.Intern("to_s")
	for i := 0; i < args.Len(); i++ {
		v, err := mrb.FuncallWithBlock(args.Item(i), toS)
		if err != nil {
			return mrb.RaiseError(err)
		}

		s := mrb.String(v)
		if len(s) > 0 && s[len(s)-1] == '\n' {
			fmt.Print(s)
		} else {
			fmt.Println(s)
		}
	}
	if args.Len() == 0 {
		fmt.Println("")
	}

	return mrb.NilValue()
}

func printP(mrb *MrbState, self Value) MrbValue {
	args := mrb.GetArgs()
	inspect := mrb.Intern("inspect")
	for i := 0; i < args.Len(); i++ {
		v, err := mrb.FuncallWithBlock(args.Item(i), inspect)
		if err != nil {
			return mrb.RaiseError(err)
		}

		fmt.Println(mrb.String(v))
	}

	switch args.Len() {
	case 0:
		return mrb.NilValue()
	case 1:
		return args.Item(0)
	default:
		return mrb.AryNewFromValues(args.Slice()...)
	}
}

func printPrintf(mrb *MrbState, self Value) MrbValue {
	args := mrb.GetArgs()
	v, err := mrb.FuncallWithBlock(mrb.KernelModule(), mrb.Intern("sprintf"), args.SliceIntf()...)
	if err != nil {
		return mrb.RaiseError(err)
	}
	fmt.Print(mrb.String(v))
	return mrb.NilValue()
}
