/*
** mirb - Embeddable Interactive Ruby Shell
**
** This program takes code from the user in
** an interactive way and executes it
** immediately. It's a REPL...
 */
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/oruby/oruby"
)

const historyFileName = ".orbi_history"

func getHistoryPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, historyFileName), nil
}

func p(mrb *oruby.MrbState, obj oruby.MrbValue, prompt int) {
	val := mrb.Call(obj, "inspect")
	if prompt != 0 {
		if mrb.Exc() == nil {
			print(" => ")
		} else {
			val = mrb.Call(mrb.Exc(), "inspect")
		}
	}
	if !val.IsString() {
		println(mrb.ObjAsString(obj).String())
		return
	}
	println(mrb.String(val))
}

// * Guess if the user might want to enter more
// * or if he wants an evaluation of his code now */
func isCodeBlockOpen(parser oruby.MrbParserState) bool {
	codeBlockOpen := false

	// check for heredoc
	if parser.ParsingHeredoc() != nil {
		return true
	}

	// check for unterminated string
	if parser.LexStrTerm() {
		return true
	}

	// check if parser error are available
	if parser.NErr() > 0 {
		unexpectedEnd := "syntax error, unexpected $end"
		message := parser.ErrorBuffer(0).Message

		/* a parser error occur, we have to check if */
		/* we need to read one more line or if there is */
		/* a different issue which we have to show to */
		/* the user */

		switch message {
		case unexpectedEnd:
			codeBlockOpen = true
		case "syntax error, unexpected keyword_end":
			codeBlockOpen = false
		case "syntax error, unexpected tREGEXP_BEG":
			codeBlockOpen = false
		}
		return codeBlockOpen
	}

	switch parser.LState() {
	/* all states which need more code */

	case oruby.ExprBeg:
		/* beginning of a statement, */
		/* that means previous line ended */
		codeBlockOpen = false
		break
	case oruby.ExprDot:
		/* a message dot was the last token, */
		/* there has to come more */
		codeBlockOpen = true
		break
	case oruby.ExprClass:
		/* a class keyword is not enough! */
		/* we need also a name of the class */
		codeBlockOpen = true
		break
	case oruby.ExprFname:
		/* a method name is necessary */
		codeBlockOpen = true
		break
	case oruby.ExprValue:
		/* if, elsif, etc. without condition */
		codeBlockOpen = true
		break

		/* now all the states which are closed */
	case oruby.ExprArg:
		/* an argument is the last token */
		codeBlockOpen = false
		break

	/* all states which are unsure */
	case oruby.ExprCmdarg:
		break
	case oruby.ExprEnd:
		/* an expression was ended */
		break
	case oruby.ExprEndarg:
		/* closing parenthese */
		break
	case oruby.ExprEndfn:
		/* definition end */
		break
	case oruby.ExprMid:
		/* jump keyword like break, return, ... */
		break
	case oruby.ExprMaxState:
		/* don't know what to do with this token */
		break
	default:
		/* this state is unexpected! */
		break
	}

	return codeBlockOpen
}

type Args struct {
	rfp     string
	alib    string
	verbose bool
	debug   bool
	libs    []string
}

func usage(name string) {
	usageMsg := []string{
		"switches:",
		"-d           set $DEBUG to true (same as `oruby -d`)",
		"-r library   same as `oruby -r`",
		"-v           print version number, then run in verbose mode",
		"--verbose    run in verbose mode",
		"--version    print the version",
		"--copyright  print the copyright",
	}

	fmt.Printf("Usage: %v [switches] [programfile] [arguments]\n", name)
	for _, msg := range usageMsg {
		fmt.Printf("  %v\n", msg)
	}
}

func parseArgs(mrb *oruby.MrbState, args *Args) error {
	flag.BoolVar(&args.debug, "d", false, "set $DEBUG to true (same as `oruby -d`)")
	flag.StringVar(&args.alib, "r", "", "same as `oruby -r`")
	flag.BoolVar(&args.verbose, "verbose", false, "run in verbose mode")
	v := flag.Bool("v", false, "print version number, then run in verbose mode")
	version := flag.Bool("version", false, "print the version")
	copyright := flag.Bool("copyright", false, "print the copyright")

	flag.Parse()

	if *v {
		if !args.verbose {
			mrb.ShowVersion()
		}
		args.verbose = true
		return nil
	}

	if *version {
		mrb.ShowVersion()
		return nil
	}

	if *copyright {
		mrb.ShowCopyright()
		return nil
	}

	if args.alib != "" {
		args.libs = append(args.libs, args.alib)
	}

	args.rfp = "" // flag.Args()[0]

	return nil
}

// Print a short remark for the user
func printHint() {
	print("mirb - Embeddable Interactive Ruby Shell\n\n")
}

func checkKeyword(buf, word string) bool {
	return strings.TrimSpace(buf) == word
}

func declLvUnderscore(mrb *oruby.MrbState, cxt *oruby.MrbcContext) {
	parser, err := mrb.ParseString("_=nil", cxt)
	if err != nil {
		mrb.Close()
		log.Fatal(err)
		return
	}
	defer parser.Free()

	proc, err := mrb.GenerateCode(parser)
	if err != nil {
		mrb.Close()
		log.Fatal(err)
		return
	}

	mrb.VMRun(proc, mrb.TopSelf(), 0)
}

const MaxCodeSize = 4096
const MaxLineSize = 1024

func main() {
	rubyCode := ""     // max 4096
	lastCodeLine := "" // max 1024
	var historyPath string
	var line string
	var result oruby.Value
	args := Args{}
	codeBlockOpen := false
	stackKeep := 0

	/* new interpreter instance */
	mrb := oruby.MrbOpen()
	if mrb == nil {
		log.Fatal("Invalid mrb interpreter, exiting mirb\n")
		return
	}
	defer mrb.Close()

	err := parseArgs(mrb, &args)
	if err != nil {
		usage(os.Args[0])
		return
	}

	ARGV := mrb.Value(os.Args)
	mrb.DefineGlobalConst("ARGV", ARGV)
	mrb.GVSet(mrb.Intern("$DEBUG"), mrb.BoolValue(args.debug))

	historyPath, err = getHistoryPath()
	if err != nil {
		log.Fatal("failed to get history path")
		return
	}
	MIRB_USING_HISTORY()
	MIRB_READ_HISTORY(historyPath)

	printHint()

	cxt := mrb.MrbcContextNew()
	defer cxt.Free()
	// MIRB_UNDERSCORE
	declLvUnderscore(mrb, cxt)

	/* Load libraries */
	for _, lib := range args.libs {
		_, err = cxt.LoadFile(lib)
		if err != nil {
			log.Fatalf("Cannot open library file. (%v)\n", lib)
			return
		}
	}

	cxt.SetCaptureErrors(true)
	cxt.SetLineNo(1)
	cxt.Filename("(imrb)")
	cxt.SetDumpResult(args.verbose)

	ai := mrb.GCArenaSave()

	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
			p(mrb, mrb.Exc(), 0)
			mrb.ExcClear()
		}
	}()

	c := make(chan os.Signal)
	go func() {
		<-c
		mrb.Close()
		os.Exit(1)
	}()

	for {
		//if args.rfp != "" {
		//	if (fgets(lastCodeLine, MaxLineSize, args.rfp) != nil) {
		//		goto done
		//	}
		//	break;
		//}

		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		line = MIRB_READLINE(codeBlockOpen, "* ", "> ")
		signal.Stop(c)

		if line == "" {
			println()
			break
		}

		if len(line) > MaxLineSize {
			fmt.Print("input string too long\n")
			continue
		}

		lastCodeLine = line + "\n"
		MIRB_ADD_HISTORY(line)
		MIRB_LINE_FREE(line)

		//done:
		if codeBlockOpen {
			if len(rubyCode)+len(lastCodeLine) > MaxCodeSize {
				fmt.Print("concatenated input string too long\n")
				continue
			}
			rubyCode += lastCodeLine
		} else {
			if checkKeyword(lastCodeLine, "quit") || checkKeyword(lastCodeLine, "exit") {
				break
			}
			rubyCode = lastCodeLine
		}

		/* parse code */
		parser := mrb.ParserNew()
		strFree := parser.SetS(rubyCode)
		parser.SetLineNo(cxt.LineNo())
		parser.Parse(cxt)
		codeBlockOpen = isCodeBlockOpen(parser)
		strFree()

		if codeBlockOpen {
			/* no evaluation of code */
		} else {
			if parser.NWarn() > 0 {
				/* warning */
				warn := parser.WarnBuffer(0)
				fmt.Printf("line %d: %s\n", warn.LineNo, warn.Message)
			}
			if parser.NErr() > 0 {
				/* syntax error */
				er := parser.ErrorBuffer(0)
				fmt.Printf("line %d: %s\n", er.LineNo, er.Message)
			} else {
				/* generate bytecode */
				proc, err := mrb.GenerateCode(parser)
				if err != nil {
					fmt.Print(err)
					parser.Free()
					break
				}

				if args.verbose {
					mrb.CodedumpAll(proc)
				}
				/* adjust stack length of toplevel environment */
				mrb.TopAdjustStackLength(proc.NLocals())

				/* pass a proc for evaluation */
				/* evaluate the bytecode */
				result = mrb.VMRun(proc, mrb.TopSelf(), stackKeep)
				stackKeep = proc.NLocals()
				/* did an exception occur? */
				if mrb.Exc() != nil {
					p(mrb, mrb.Exc(), 0)
					mrb.ExcClear()
				} else {
					/* no */
					if !mrb.RespondTo(result, mrb.Intern("inspect")) {
						result = mrb.AnyToS(result)
					}
					p(mrb, result, 1)
					// MIRB_UNDERSCORE
					mrb.SetLastStackValue(result)
				}
			}
			rubyCode = ""
			lastCodeLine = ""
			mrb.GCArenaRestore(ai)
		}
		parser.Free()
		cxt.SetLineNo(cxt.LineNo() + 1)
	}

	MIRB_WRITE_HISTORY(historyPath)
}
