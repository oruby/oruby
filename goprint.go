package oruby

import "fmt"

func init() {
	if GemExists("print") {
		return
	}

	Gem("print", func(mrb *MrbState) interface{} {
		// Override oruby print functions so mrb output behave as go output
		kernel := mrb.KernelModule()

		kernel.DefineMethod("__printstr__", printStr, mrb.ArgsAny())
		kernel.DefineMethod("print", printPrint, mrb.ArgsAny())
		kernel.DefineMethod("puts", printPuts, mrb.ArgsAny())
		kernel.DefineMethod("p", printP, mrb.ArgsAny())
		kernel.DefineMethod("printf", printPrintf, mrb.ArgsAny())

		return nil
	})
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
		if len(s) == 0 || s[len(s)-1] != '\n' {
			fmt.Print(s)
			continue
		}
		fmt.Println(s)
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