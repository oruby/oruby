package main

import (
	"flag"
	"fmt"
	"github.com/oruby/oruby"
	"log"
	"os"
)


type Args struct {
	rfp string
	cmdline string
	eline string
	mrbfile bool
	check_syntax bool
	verbose bool
	debug bool
	libs []string
}

func usage(name string) {
	usage_msg := []string{
		"switches:",
		"-b           load and execute RiteBinary (mrb) file",
		"-c           check syntax only",
		"-d           set debugging flags (set $DEBUG to true)",
		"-e 'command' one line of script",
		"-r library   load the library before executing your script",
		"-v           print version number, then run in verbose mode",
		"--verbose    run in verbose mode",
		"--version    print the version",
		"--copyright  print the copyright",
	}

	fmt.Printf("Usage: %v [switches] [programfile] [arguments]\n", name);
	for _, line := range usage_msg {
		fmt.Printf("  %v\n", line);
	}
}

func dup_arg_item(mrb *oruby.MrbState, item string) string {
	return item;
}

func parse_args(mrb *oruby.MrbState, args *Args) error {
	flag.BoolVar(&args.mrbfile,"b", false, "load and execute RiteBinary (mrb) file")
	flag.BoolVar(&args.check_syntax,"c", false, "check syntax only")
	flag.BoolVar(&args.debug,"d", false, "set debugging flags (set $DEBUG to true)")
	flag.StringVar(&args.eline,"e", "", "one line of script")
	rlib := flag.String("r", "", "load the library before executing your script")
	v    := flag.Bool("v", false, "print version number, then run in verbose mode")
	flag.BoolVar(&args.verbose,"verbose", false, "run in verbose mode")
	version := flag.Bool("version", false, "print the version")
	copyright := flag.Bool("copyright", false, "print the copyright")

	flag.Parse()

	if *version {
		mrb.ShowVersion()
		os.Exit(0)
		return nil
	}
	if *copyright {
		mrb.ShowCopyright()
		os.Exit(0)
		return nil
	}

	if *v {
		mrb.ShowVersion()
		args.verbose = true
	}

	if args.eline != "" {
		args.cmdline = args.cmdline + "\n" + args.eline
	}

	if rlib != nil {
		if *rlib == "" {
			return fmt.Errorf("%v: No library specified for -r\n", os.Args[0]);
		}
	}

	if len(flag.Args()) > 0 {
		args.rfp = flag.Args()[0]
		args.cmdline = args.rfp
	} // else if stdin {}

	return nil
}


func main() {
	args := Args{}

	mrb, err := oruby.New()
	if err != nil {
		log.Fatalf("%v: Invalid mrb_state, exiting oruby\n", os.Args[0]);
		return;
	}
	defer mrb.Close()

	err = parse_args(mrb, &args);
	if err != nil || (args.cmdline == "" && args.rfp == "") {
		os.Exit(1)
		return;
	}

	ai := mrb.GCArenaSave()
	mrb.DefineGlobalConst("ARGV", mrb.Value(os.Args));
	mrb.GVSet(mrb.Intern("$DEBUG"), mrb.BoolValue(args.debug));

	c := mrb.MrbcContextNew()
	defer c.Free()

	c.SetDumpResult(args.verbose)
	c.SetNoExec(args.check_syntax)

	/* Set $0 */
	zeroSym := mrb.Intern("$0");
	if args.rfp != "" {
		cmdline := args.cmdline
		if cmdline == "" {
			cmdline = "-";
		}
		c.Filename(cmdline)
		mrb.GVSet(zeroSym, mrb.StringValue(cmdline))
	} else {
		c.Filename("-e")
		mrb.GVSet(zeroSym, mrb.StringValue("-e"))
	}

	/* Load libraries */
	for _, lib := range args.libs {
		var err error
		if args.mrbfile {
			_, err = c.LoadIrepFile(lib)// mrb_load_irep_file_cxt(mrb, lfp, c);
		} else {
			_, err = c.LoadFile(lib)
		}

		if err != nil {
			log.Fatalf( "%v: Cannot open library file: %s\n", os.Args[0], lib);
			return
		}
	}

	/* Load program */
	var v oruby.MrbValue

	if args.mrbfile {
		v, err = c.LoadIrepFile(args.rfp)
	} else if args.rfp != "" {
		v, err = c.LoadFile(args.rfp)
	} else {
		v = c.LoadString(args.cmdline)
	}

	if err != nil {
		log.Fatalf( "%v: %v\n", os.Args[0], err);
		return
	}

	mrb.GCArenaRestore(ai)

	if mrb.Err() != nil {
		if !mrb.UndefP(v) {
			mrb.PrintError()
		}
		return
	}

	if args.check_syntax {
		fmt.Println("Syntax OK");
	}

	return;
}

