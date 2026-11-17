package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/oruby/oruby"
)

type Args struct {
	disableGems    bool
	rfp            string
	cmdline        string
	fname          bool
	mrbfile        bool
	checkSyntax    bool
	verbose        bool
	debug          bool
	libs           []string
	scriptArgs     []string
	eline          string
	dashN          bool
	dashP          bool
	backtraceLimit string
	warningLevel   int
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
	rawArgs := os.Args[1:]
	var scriptArgs []string
	var rfp string
	var cmdlines []string

	for i := 0; i < len(rawArgs); i++ {
		arg := rawArgs[i]
		if arg == "" {
			continue
		}
		if arg == "--" {
			if rfp == "" && len(cmdlines) == 0 && i+1 < len(rawArgs) {
				rfp = rawArgs[i+1]
				scriptArgs = rawArgs[i+2:]
			} else {
				scriptArgs = append(scriptArgs, rawArgs[i+1:]...)
			}
			break
		}

		if (rfp != "" || len(cmdlines) > 0) && (!strings.HasPrefix(arg, "-") || arg == "-") {
			scriptArgs = append(scriptArgs, rawArgs[i:]...)
			break
		}

		if strings.HasPrefix(arg, "-") && arg != "-" {
			switch {
			case arg == "-b":
				args.mrbfile = true
			case arg == "-c":
				args.checkSyntax = true
			case arg == "-d":
				args.debug = true
			case arg == "-v":
				mrb.ShowVersion()
				args.verbose = true
			case arg == "--verbose":
				args.verbose = true
			case arg == "--version":
				mrb.ShowVersion()
				os.Exit(0)
			case arg == "--copyright":
				mrb.ShowCopyright()
				os.Exit(0)
			case arg == "-e":
				if i+1 < len(rawArgs) {
					i++
					cmdlines = append(cmdlines, rawArgs[i])
				}
			case strings.HasPrefix(arg, "-e"):
				cmdlines = append(cmdlines, arg[2:])
			case arg == "-r":
				if i+1 < len(rawArgs) {
					i++
					args.libs = append(args.libs, rawArgs[i])
				}
			case strings.HasPrefix(arg, "-r"):
				args.libs = append(args.libs, arg[2:])
			case arg == "-n":
				args.dashN = true
			case arg == "-p":
				args.dashP = true
			case arg == "--disable-gems" || arg == "--disable=gems" || arg == "--disable-all" || arg == "--disable=all":
				args.disableGems = true
			case strings.HasPrefix(arg, "--disable") || strings.HasPrefix(arg, "--enable"):
			case arg == "-w":
				args.verbose = true
				args.warningLevel = 2
			case arg == "-W0":
				args.warningLevel = 0
			case arg == "-W1":
				args.warningLevel = 1
			case arg == "-W2" || arg == "-W":
				args.warningLevel = 2
			case strings.HasPrefix(arg, "-W"):
				if len(arg) > 2 {
					if lvl, err := strconv.Atoi(arg[2:]); err == nil {
						args.warningLevel = lvl
					}
				}
			case strings.HasPrefix(arg, "--backtrace-limit="):
				args.backtraceLimit = strings.TrimPrefix(arg, "--backtrace-limit=")
			case arg == "--backtrace-limit":
				if i+1 < len(rawArgs) {
					i++
					args.backtraceLimit = rawArgs[i]
				}
			case strings.HasPrefix(arg, "-i"):
			case arg == "-n" || arg == "-p" || arg == "-l" || arg == "-a":
			default:
			}
		} else {
			if rfp == "" && len(cmdlines) == 0 {
				rfp = arg
			} else {
				scriptArgs = append(scriptArgs, arg)
			}
		}
	}

	args.rfp = rfp
	if len(cmdlines) > 0 {
		args.eline = strings.Join(cmdlines, "\n")
		args.cmdline = args.eline
	} else if rfp != "" {
		args.cmdline = rfp
	}
	args.scriptArgs = scriptArgs

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
	args := Args{warningLevel: -1}
	var v oruby.MrbValue
	exitCode := 0
	defer func() {
		// report Go panics instead of exiting silently with status 0
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "oruby: panic: %v\n%s", r, debug.Stack())
			os.Exit(2)
		}
		os.Exit(exitCode)
	}()

	mrb, err := oruby.New()
	if err != nil {
		exitCode = exitFailure("%v: Invalid mrb_state, exiting oruby\n", os.Args[0])
		return
	}
	defer mrb.Close()

	if okToContinue, err := parseArgs(mrb, &args); !okToContinue {
		if err != nil {
			exitCode = exitFailure("%s", err.Error())
		}
		return
	}

	ai := mrb.GCArenaSave()
	mrb.DefineGlobalConst("ARGV", mrb.Value(args.scriptArgs))
	mrb.GVSet(mrb.Intern("$DEBUG"), mrb.BoolValue(args.debug))
	mrb.GVSet(mrb.Intern("$VERBOSE"), mrb.BoolValue(args.verbose))

	c := mrb.MrbCcontextNew()
	defer c.Free()

	c.SetDumpResult(false)
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
	mrb.Eval(fmt.Sprintf(`Process.instance_variable_set(:@argv0, %q.dup.freeze)`, cmdline))
	if args.backtraceLimit != "" {
		if n, err := strconv.Atoi(args.backtraceLimit); err == nil {
			mrb.SetGV("$__backtrace_limit__", n)
		}
	}
	switch args.warningLevel {
	case 0:
		mrb.SetGV("$VERBOSE", mrb.NilValue())
	case 1:
		mrb.SetGV("$VERBOSE", mrb.FalseValue())
	case 2:
		mrb.SetGV("$VERBOSE", mrb.TrueValue())
	default:
		mrb.SetGV("$VERBOSE", mrb.FalseValue())
	}

	// RubyGems is loaded at startup, as MRI does, unless --disable-gems
	if !args.disableGems && os.Getenv("MSPEC_RUNNER") == "" {
		mrb.Eval("begin; require 'rubygems'; rescue LoadError => e; warn \"`RubyGems' were not loaded: #{e}\" if $VERBOSE; end")
	}

	/* Load libraries */
	for _, lib := range args.libs {
		if lib == "rubygems" {
			continue
		}
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
		ciBase.EnvSet(nil)
		mrb.EnvUnshare(e, false)
		c.CleanupLocalVariables()
	}

	/* set program file name */
	c.Filename(cmdline)

	/* Load program */
	if args.dashN || args.dashP {
		mrb.Eval(`
module Kernel
  def chomp(sep = $/)
    $_ = $_&.chomp(sep)
  end
  def chop
    $_ = $_&.chop
  end
  module_function :chomp, :chop
  private :chomp, :chop
end
`)
		var src string
		if args.rfp != "" {
			b, readErr := os.ReadFile(args.rfp)
			if readErr != nil {
				err = readErr
			} else {
				src = string(b)
			}
		} else {
			src = args.cmdline
		}
		if err == nil {
			post := ""
			if args.dashP {
				post = "print $_\n"
			}
			wrapped := fmt.Sprintf("while gets\n%s\n%send", src, post)
			v, err = c.LoadString(wrapped)
		}
	} else if args.mrbfile || strings.EqualFold(filepath.Ext(cmdline), ".mrb") {
		v, err = c.LoadIrepFile(args.rfp)
	} else if args.rfp != "" {
		v, err = c.LoadFile(args.rfp)
	} else {
		v, err = c.LoadString(args.cmdline)
	}

	if err != nil {
		if os.IsNotExist(err) || strings.Contains(err.Error(), "no such file or directory") {
			fmt.Fprintf(os.Stderr, "%s: cannot load such file -- %s (LoadError)\n", os.Args[0], args.rfp)
			exitCode = 1
			return
		}
		exc := mrb.Exc()
		if exc != nil && exc.IsKindOf(mrb.ClassGet("SystemExit")) {
			mrb.RunAtExitHandlers()
			statusVal := mrb.Call(exc.Value(), "status")
			exitCode = 0
			if !statusVal.IsNil() {
				exitCode = int(statusVal.Int())
			}
			return
		}
		exitCode = exitFailure("%v: %v\n", os.Args[0], err)
		return
	}

	mrb.GCArenaRestore(ai)

	if mrb.Err() != nil {
		exc := mrb.Exc()
		if exc != nil && exc.IsKindOf(mrb.ClassGet("SystemExit")) {
			mrb.RunAtExitHandlers()
			statusVal := mrb.Call(exc.Value(), "status")
			exitCode = 0
			if !statusVal.IsNil() {
				exitCode = int(statusVal.Int())
			}
			return
		}
		if !mrb.UndefP(v) {
			mrb.PrintError()
		}
		exitCode = 1
		return
	}

	if args.checkSyntax {
		fmt.Println("Syntax OK")
		exitCode = 0
		return
	}

	mrb.RunAtExitHandlers()
}
