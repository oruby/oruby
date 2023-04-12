package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/oruby/oruby"
)

type Args struct {
	rfp         string
	cmdline     string
	fname       bool
	mrbfile     bool
	checkSyntax bool
	verbose     bool
	debug       bool
	libs        []string
	// args      []string // os.Args used instead
	eline string
}

func usage(name string) {
	usageMsg := []string{
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

	fmt.Printf("Usage: %v [switches] [programfile] [arguments]\n", name)
	for _, line := range usageMsg {
		fmt.Printf("  %v\n", line)
	}
}

func parseArgs(mrb *oruby.MrbState, args *Args) (bool, error) {
	flag.BoolVar(&args.mrbfile, "b", false, "load and execute RiteBinary (mrb) file")
	flag.BoolVar(&args.checkSyntax, "c", false, "check syntax only")
	flag.BoolVar(&args.debug, "d", false, "set debugging flags (set $DEBUG to true)")
	flag.StringVar(&args.eline, "e", "", "one line of script")
	rlib := flag.String("r", "", "load the library before executing your script")
	v := flag.Bool("v", false, "print version number, then run in verbose mode")
	flag.BoolVar(&args.verbose, "verbose", false, "run in verbose mode")
	version := flag.Bool("version", false, "print the version")
	copyright := flag.Bool("copyright", false, "print the copyright")

	flag.Parse()

	if *version {
		mrb.ShowVersion()
		return false, nil
	}
	if *copyright {
		mrb.ShowCopyright()
		return false, nil
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
			return false, fmt.Errorf("%v: No library specified for -r\n", os.Args[0])
		}
	}

	if len(flag.Args()) > 0 {
		args.rfp = flag.Args()[0]
		args.cmdline = args.rfp
	} // else if stdin {}

	if args.cmdline == "" && args.rfp == "" {
		return false, nil
	}

	return true, nil
}

func exitFailure(format string, v ...any) int {
	log.Printf(format, v...)
	return 1
}

func main() {
	args := Args{}
	var v oruby.MrbValue
	exitCode := 0
	defer func() { os.Exit(exitCode) }()

	mrb, err := oruby.New()
	if err != nil {
		exitCode = exitFailure("%v: Invalid mrb_state, exiting oruby\n", os.Args[0])
		return
	}
	defer mrb.Close()

	if okToContinue, err := parseArgs(mrb, &args); !okToContinue {
		if err != nil {
			exitCode = exitFailure(err.Error())
		}
		return
	}

	ai := mrb.GCArenaSave()
	mrb.DefineGlobalConst("ARGV", mrb.Value(os.Args))
	mrb.GVSet(mrb.Intern("$DEBUG"), mrb.BoolValue(args.debug))

	c := mrb.MrbcContextNew()
	defer c.Free()

	c.SetDumpResult(args.verbose)
	c.SetNoExec(args.checkSyntax)

	/* Set $0 */
	cmdline := args.cmdline
	if args.rfp != "" {
		if cmdline == "" {
			cmdline = "-"
		}
	} else {
		cmdline = "-e"
	}
	mrb.SetGV("$0", cmdline)

	/* Load libraries */
	for _, lib := range args.libs {
		var err error
		c.Filename(lib)
		if strings.EqualFold(filepath.Ext(lib), ".mrb") {
			v, err = c.LoadIrepFile(lib) // mrb_load_irep_file_cxt(mrb, lfp, c);
		} else {
			v, err = c.LoadDetectFile(lib)
		}

		if err != nil {
			exitCode = exitFailure("%v: Cannot open library file: %s\n", os.Args[0], lib)
			return
		}

		ciBase := mrb.Context().CiBase()
		e := ciBase.Env()
		ciBase.EnvSet(oruby.REnv{})
		mrb.EnvUnshare(e, false)
		c.CleanupLocalVariables()
	}

	/* set program file name */
	c.Filename(cmdline)

	/* Load program */
	if args.mrbfile || strings.EqualFold(filepath.Ext(cmdline), ".mrb") {
		v, err = c.LoadIrepFile(args.rfp)
	} else if args.rfp != "" {
		v, err = c.LoadFile(args.rfp)
	} else {
		v, err = c.LoadString(args.cmdline)
	}

	if err != nil {
		exitCode = exitFailure("%v: %v\n", os.Args[0], err)
		return
	}

	mrb.GCArenaRestore(ai)

	if mrb.Err() != nil {
		if !mrb.UndefP(v) {
			mrb.PrintError()
		}
		exitCode = 1
		return
	}

	if args.checkSyntax {
		fmt.Println("Syntax OK")
	}
}
