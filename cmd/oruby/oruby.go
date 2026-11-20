package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/signal"
)

// options holds the switches MRI's ruby accepts, as far as ORuby supports
// them; see ruby.c's proc_options
type options struct {
	script     string   // program file ("" with -e, "-" for stdin)
	eLines     []string // -e
	scriptArgs []string

	mrbfile, checkSyntax, debug, help bool
	version, copyright, showVersion   bool
	verbose                           int // -1 unset, 0 nil, 1 false, 2 true
	warnCats                          map[string]bool
	libs, incs                        []string
	dashN, dashP, dashA, dashL        bool
	dashS, searchPath, xflag          bool
	xdir, chdir                       string
	fieldSep                          *string
	rs                                *string // -0; nil string for $/ = nil
	rsNil                             bool
	inplace                           *string
	kcode                             string // source encoding from -K
	extEnc, intEnc                    string
	extSet, intSet, uflag             bool
	backtraceLimit                    string
	features                          map[string]bool
}

// features --enable/--disable know; an argument matches a unique prefix
var featureNames = []string{"gems", "error_highlight", "did_you_mean", "syntax_suggest", "rubyopt",
	"frozen-string-literal", "jit", "yjit", "rjit", "zjit"}

func newOptions() *options {
	return &options{verbose: -1, warnCats: map[string]bool{}, features: map[string]bool{}}
}

type cliError struct {
	msg   string
	class string
}

func (e *cliError) Error() string { return e.msg }

func runtimeErr(format string, a ...any) error {
	return &cliError{msg: fmt.Sprintf(format, a...), class: "RuntimeError"}
}

// setFeature applies --enable/--disable to a comma-separated feature list
func (o *options) setFeature(list string, on bool, flag string) {
	for _, name := range strings.Split(list, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if name == "all" {
			for _, f := range featureNames {
				o.features[f] = on
			}
			continue
		}
		var match []string
		for _, f := range featureNames {
			if f == name || strings.HasPrefix(f, name) || strings.ReplaceAll(f, "-", "_") == name {
				match = append(match, f)
			}
		}
		if len(match) == 0 {
			fmt.Fprintf(os.Stderr, "%s: warning: unknown argument for --%s: '%s'\n", progName(), flag, name)
			continue
		}
		o.features[match[0]] = on
	}
}

func (o *options) setEncodings(arg, flag string) error {
	parts := strings.Split(arg, ":")
	if len(parts) > 2 {
		return runtimeErr("extra argument for %s: %s", flag, parts[2])
	}
	if parts[0] != "" {
		o.extEnc, o.extSet = parts[0], true
	}
	if len(parts) == 2 && parts[1] != "" {
		if o.uflag {
			return runtimeErr("default_internal already set to UTF-8")
		}
		o.intEnc, o.intSet = parts[1], true
	}
	return nil
}

func kcodeEncoding(c byte) string {
	switch c {
	case 'a', 'A', 'n', 'N':
		return "ASCII-8BIT"
	case 'e', 'E':
		return "EUC-JP"
	case 's', 'S':
		return "Windows-31J"
	case 'u', 'U':
		return "UTF-8"
	}
	return ""
}

// switches RUBYOPT may hold, as in MRI
const envSwitches = "dEIKrUvwW"

// parse reads the switches in args. fromEnv applies RUBYOPT's restrictions;
// it returns the arguments left after the switches.
func (o *options) parse(args []string, fromEnv bool) ([]string, error) {
	i := 0
	next := func(rest string) (string, bool) {
		if rest != "" {
			return rest, true
		}
		if i+1 < len(args) {
			i++
			return args[i], true
		}
		return "", false
	}
	for ; i < len(args); i++ {
		arg := args[i]
		if fromEnv && arg != "" && arg[0] != '-' {
			arg = "-" + arg
		}
		if arg == "--" {
			i++
			break
		}
		if len(arg) < 2 || arg[0] != '-' {
			break
		}
		if arg[1] == '-' {
			if err := o.parseLong(arg, fromEnv, next); err != nil {
				return nil, err
			}
			continue
		}
		s := arg[1:]
	cluster:
		for len(s) > 0 {
			c := s[0]
			s = s[1:]
			if fromEnv && !strings.ContainsRune(envSwitches, rune(c)) {
				return nil, runtimeErr("invalid switch in RUBYOPT: -%c", c)
			}
			switch c {
			case 'a':
				o.dashA = true
			case 'b':
				o.mrbfile = true
			case 'c':
				o.checkSyntax = true
			case 'd':
				o.debug = true
				o.verbose = 2
			case 'h':
				o.help = true
			case 'l':
				o.dashL = true
			case 'n':
				o.dashN = true
			case 'p':
				o.dashP = true
			case 's':
				o.dashS = true
			case 'S':
				o.searchPath = true
			case 'U':
				if !fromEnv && o.intSet {
					return nil, runtimeErr("default_internal already set to %s", o.intEnc)
				}
				o.uflag = true
			case 'v':
				o.showVersion = true
				o.verbose = 2
			case 'w':
				o.verbose = 2
			case 'y':
			case 'W':
				switch {
				case s == "":
					o.verbose = 2
				case s[0] == ':':
					cat := s[1:]
					on := true
					if strings.HasPrefix(cat, "no-") {
						on, cat = false, cat[3:]
					}
					o.warnCats[cat] = on
				default:
					lvl, err := strconv.Atoi(s[:1])
					if err == nil && lvl >= 0 && lvl <= 2 {
						o.verbose = lvl
					}
				}
				break cluster
			case 'e':
				v, ok := next(s)
				if !ok {
					return nil, runtimeErr("no code specified for -e")
				}
				o.eLines = append(o.eLines, v)
				break cluster
			case 'r':
				if v, ok := next(s); ok {
					o.libs = append(o.libs, v)
				}
				break cluster
			case 'I':
				if v, ok := next(s); ok {
					if !filepath.IsAbs(v) {
						if wd, err := os.Getwd(); err == nil {
							v = filepath.Join(wd, v)
						}
					}
					o.incs = append(o.incs, v)
				}
				break cluster
			case 'C', 'X':
				v, ok := next(s)
				if !ok {
					return nil, runtimeErr("Can't chdir")
				}
				o.chdir = v
				break cluster
			case 'x':
				o.xflag = true
				o.xdir = s
				break cluster
			case 'F':
				fs := s
				o.fieldSep = &fs
				break cluster
			case 'i':
				ext := s
				o.inplace = &ext
				break cluster
			case 'K':
				if len(s) > 0 {
					if enc := kcodeEncoding(s[0]); enc != "" {
						o.kcode = enc
					}
					s = s[1:]
				}
			case 'E':
				v, ok := next(s)
				if !ok {
					return nil, runtimeErr("missing argument for -E")
				}
				if err := o.setEncodings(v, "-E"); err != nil {
					return nil, err
				}
				break cluster
			case '0':
				n := 0
				for n < len(s) && n < 4 && s[n] >= '0' && s[n] <= '7' {
					n++
				}
				v := int64(0)
				if n > 0 {
					v, _ = strconv.ParseInt(s[:n], 8, 32)
				}
				s = s[n:]
				switch {
				case v > 0377:
					o.rs, o.rsNil = nil, true
				case v == 0 && n >= 1:
					p := ""
					o.rs, o.rsNil = &p, false
				default:
					p := string(rune(v))
					o.rs, o.rsNil = &p, false
				}
			default:
				return nil, runtimeErr("invalid option -%c  (-h will show valid options)", c)
			}
		}
	}
	return args[i:], nil
}

func (o *options) parseLong(arg string, fromEnv bool, next func(string) (string, bool)) error {
	name, val, hasVal := strings.Cut(arg[2:], "=")
	value := func() (string, bool) {
		if hasVal {
			return val, true
		}
		return next("")
	}
	switch {
	case name == "version":
		o.version = true
	case name == "copyright":
		o.copyright = true
	case name == "help":
		o.help = true
	case name == "yydebug" || name == "dump" || name == "parser" || name == "crash-report":
	case name == "verbose":
		o.verbose = 2
	case name == "debug":
		o.debug = true
		o.verbose = 2
	case name == "enable" || name == "disable":
		v, ok := value()
		if !ok {
			return runtimeErr("missing argument for --%s", name)
		}
		o.setFeature(v, name == "enable", name)
		return nil
	case strings.HasPrefix(name, "enable-"):
		o.setFeature(name[7:], true, "enable")
		return nil
	case strings.HasPrefix(name, "disable-"):
		o.setFeature(name[8:], false, "disable")
		return nil
	case name == "encoding" || name == "external-encoding" || name == "internal-encoding":
		v, ok := value()
		if !ok {
			return runtimeErr("missing argument for --%s", name)
		}
		switch name {
		case "encoding":
			return o.setEncodings(v, "--encoding")
		case "external-encoding":
			o.extEnc, o.extSet = v, true
		default:
			o.intEnc, o.intSet = v, true
		}
		return nil
	case name == "backtrace-limit":
		v, ok := value()
		if ok {
			o.backtraceLimit = v
		}
	case strings.HasPrefix(name, "jit") || strings.HasPrefix(name, "yjit") || strings.HasPrefix(name, "zjit") || strings.HasPrefix(name, "rjit"):
	default:
		return runtimeErr("invalid option --%s  (-h will show valid options)", name)
	}
	if fromEnv && (name == "version" || name == "copyright" || name == "help" || name == "yydebug" || name == "dump") {
		return runtimeErr("invalid switch in RUBYOPT: --%s", name)
	}
	return nil
}

// merge folds RUBYOPT's switches (env) into the command line's (o): -r and
// -I come after the command line's, scalar settings only where the command
// line left them unset
func (o *options) merge(env *options) {
	o.libs = append(o.libs, env.libs...)
	o.incs = append(o.incs, env.incs...)
	if o.verbose < 0 {
		o.verbose = env.verbose
	}
	for k, v := range env.warnCats {
		if _, ok := o.warnCats[k]; !ok {
			o.warnCats[k] = v
		}
	}
	for k, v := range env.features {
		if _, ok := o.features[k]; !ok {
			o.features[k] = v
		}
	}
	o.debug = o.debug || env.debug
	o.showVersion = o.showVersion || env.showVersion
	if !o.extSet && env.extSet {
		o.extEnc, o.extSet = env.extEnc, true
	}
	if !o.intSet && env.intSet {
		o.intEnc, o.intSet = env.intEnc, true
	}
	if !o.intSet && env.uflag {
		o.uflag = true
	}
	if o.kcode == "" {
		o.kcode = env.kcode
	}
	if o.backtraceLimit == "" {
		o.backtraceLimit = env.backtraceLimit
	}
}

func (o *options) feature(name string, def bool) bool {
	if v, ok := o.features[name]; ok {
		return v
	}
	return def
}

func progName() string { return os.Args[0] }

func usage() {
	fmt.Printf(`Usage: %s [switches] [--] [programfile] [arguments]
  -0[octal]       specify record separator (\0, if no argument)
  -a              autosplit mode with -n or -p (splits $_ into $F)
  -b              load and execute RiteBinary (mrb) file
  -c              check syntax only
  -Cdirectory     cd to directory before executing your script
  -d              set debugging flags (set $DEBUG to true)
  -e 'command'    one line of script. Several -e's allowed. Omit [programfile]
  -Eex[:in]       specify the default external and internal character encodings
  -Fpattern       split() pattern for autosplit (-a)
  -Idirectory     specify $LOAD_PATH directory (may be used more than once)
  -Kkcode         specifies KANJI (Japanese) code-set
  -l              enable line ending processing
  -n              assume 'while gets(); ... end' loop around your script
  -p              assume loop like -n but print line also like sed
  -rlibrary       require the library before executing your script
  -s              enable some switch parsing for switches after script name
  -S              look for the script using PATH environment variable
  -v              print the version number, then turn on verbose mode
  -w              turn warnings on for your script
  -W[level=2|:category]   set warning level; 0=silence, 1=medium, 2=verbose
  -x[directory]   strip off text before #!ruby line and perhaps cd to directory
  --enable/--disable=feature  enable or disable features (gems, frozen-string-literal, rubyopt, all)
  --copyright     print the copyright
  --version       print the version
`, progName())
}

// fatal prints MRI's "ruby: message (Class)" and exits with status 1
func fatal(class, format string, a ...any) {
	fmt.Fprintf(os.Stderr, "%s: %s (%s)\n", progName(), fmt.Sprintf(format, a...), class)
	os.Exit(1)
}

// findScript resolves -S: RUBYPATH, then PATH, unless the name has a separator
func findScript(name string) string {
	if strings.ContainsRune(name, filepath.Separator) {
		return name
	}
	for _, env := range []string{"RUBYPATH", "PATH"} {
		for _, dir := range filepath.SplitList(os.Getenv(env)) {
			if dir == "" {
				continue
			}
			p := filepath.Join(dir, name)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p
			}
		}
	}
	return name
}

var rubyShebang = regexp.MustCompile(`^#!.*ruby`)

// stripToRubyLine drops the text before the first #!...ruby line (-x); the
// dropped lines become empty ones, so line numbers stay as in the file
func stripToRubyLine(src []byte) ([]byte, bool) {
	lines := bytes.SplitAfter(src, []byte("\n"))
	for i, l := range lines {
		if rubyShebang.Match(l) {
			var b bytes.Buffer
			for j := 0; j < i; j++ {
				b.WriteByte('\n')
			}
			for _, rest := range lines[i:] {
				b.Write(rest)
			}
			return b.Bytes(), true
		}
	}
	return nil, false
}

// dashSArgs turns leading -name[=value] script arguments into globals (-s)
func dashSArgs(args []string) (map[string]string, []string) {
	vars := map[string]string{}
	i := 0
	for ; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			i++
			break
		}
		if len(a) < 2 || a[0] != '-' {
			break
		}
		name, val, hasVal := strings.Cut(a[1:], "=")
		name = strings.ReplaceAll(name, "-", "_")
		if !hasVal {
			val = "\x00true"
		}
		vars[name] = val
	}
	return vars, args[i:]
}

func main() {
	exitCode := 0
	defer func() {
		// report Go panics instead of exiting silently with status 0
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "oruby: panic: %v\n%s", r, debug.Stack())
			os.Exit(2)
		}
		os.Exit(exitCode)
	}()
	exitCode = run()
}

func run() int {
	o := newOptions()
	rest, err := o.parse(os.Args[1:], false)
	if err != nil {
		fatal("RuntimeError", "%s", err.Error())
	}
	if o.feature("rubyopt", true) {
		if env := strings.Fields(os.Getenv("RUBYOPT")); len(env) > 0 {
			eo := newOptions()
			if _, err := eo.parse(env, true); err != nil {
				fatal("RuntimeError", "%s", err.Error())
			}
			o.merge(eo)
		}
	}
	if o.help {
		usage()
		return 0
	}

	mrb, err := oruby.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v: Invalid mrb_state, exiting oruby\n", progName())
		return 1
	}
	defer mrb.Close()
	// MRI's handlers: SIGINT raises Interrupt, SIGTERM & co. SignalException
	signal.InstallDefaults(mrb)

	if o.copyright {
		mrb.ShowCopyright()
		return 0
	}
	if o.showVersion || o.version {
		mrb.Eval(`$stdout.puts RUBY_DESCRIPTION; $stdout.flush`)
		if o.version {
			return 0
		}
	}

	if len(o.eLines) == 0 {
		if len(rest) == 0 {
			if o.showVersion {
				return 0
			}
			o.script = "-"
		} else {
			o.script, rest = rest[0], rest[1:]
		}
	}
	if o.searchPath && o.script != "" && o.script != "-" {
		o.script = findScript(o.script)
	}
	var switchVars map[string]string
	if o.dashS {
		switchVars, rest = dashSArgs(rest)
	}
	o.scriptArgs = rest

	for _, dir := range []string{o.chdir, o.xdir} {
		if dir != "" {
			if err := os.Chdir(dir); err != nil {
				fatal("Errno::ENOENT", "Can't chdir to %s", dir)
			}
		}
	}

	// the program is read before -r libraries are required
	var src []byte
	cmdline := "-e"
	switch {
	case len(o.eLines) > 0:
		src = []byte(strings.Join(o.eLines, "\n"))
	case o.script == "-":
		cmdline = "-"
		src, err = readAll(os.Stdin)
		if err != nil {
			fatal("LoadError", "%v", err)
		}
	default:
		cmdline = o.script
		if !(o.mrbfile || strings.EqualFold(filepath.Ext(o.script), ".mrb")) {
			src, err = os.ReadFile(o.script)
			if err != nil {
				if os.IsNotExist(err) {
					if o.searchPath {
						fatal("LoadError", "cannot load such file -- %s", o.script)
					}
					fatal("LoadError", "No such file or directory -- %s", o.script)
				}
				fatal("LoadError", "%v -- %s", err, o.script)
			}
		}
	}
	origLen := len(src)
	if src != nil && len(o.eLines) == 0 {
		// a shebang line not naming ruby means -x
		if o.xflag || bytes.HasPrefix(src, []byte("#!")) && !rubyShebang.Match(firstLine(src)) {
			stripped, ok := stripToRubyLine(src)
			if !ok {
				fatal("LoadError", "no Ruby script found in input")
			}
			src = stripped
		}
	}

	if v, ok := o.features["frozen-string-literal"]; ok {
		oruby.SetFrozenStringLiteral(v)
	}

	ai := mrb.GCArenaSave()
	mrb.DefineGlobalConst("ARGV", mrb.Value(o.scriptArgs))
	mrb.SetGV("$DEBUG", o.debug)
	switch o.verbose {
	case 0:
		mrb.SetGV("$VERBOSE", mrb.NilValue())
	case 2:
		mrb.SetGV("$VERBOSE", mrb.TrueValue())
	default:
		mrb.SetGV("$VERBOSE", mrb.FalseValue())
	}
	for _, g := range []struct {
		name string
		on   bool
	}{{"$-a", o.dashA}, {"$-l", o.dashL}, {"$-p", o.dashP}} {
		mrb.SetGV(g.name, g.on)
	}
	if o.inplace != nil {
		mrb.SetGV("$-i", *o.inplace)
	}
	for name, val := range switchVars {
		if val == "\x00true" {
			mrb.SetGV("$"+name, true)
		} else {
			mrb.SetGV("$"+name, val)
		}
	}

	// settings applied in Ruby: encodings, separators, warning categories
	var pre strings.Builder
	if o.kcode != "" {
		fmt.Fprintf(&pre, "Encoding.default_external = %q\n", o.kcode)
	}
	if o.extSet {
		fmt.Fprintf(&pre, "Encoding.default_external = %q\n", o.extEnc)
	}
	if o.intSet {
		fmt.Fprintf(&pre, "Encoding.default_internal = %q\n", o.intEnc)
	} else if o.uflag {
		pre.WriteString("Encoding.default_internal = 'UTF-8'\n")
	}
	if o.rsNil {
		pre.WriteString("$/ = nil\n")
	} else if o.rs != nil {
		fmt.Fprintf(&pre, "$/ = %q.freeze\n", *o.rs)
	}
	if o.fieldSep != nil {
		fmt.Fprintf(&pre, "$; = Regexp.new(%q)\n", *o.fieldSep)
	}
	if o.dashL {
		pre.WriteString("$\\ = $/\n")
	}
	for cat, on := range o.warnCats {
		fmt.Fprintf(&pre, "Warning[:%s] = %v if Warning.categories.include?(:%s)\n", cat, on, cat)
	}
	fmt.Fprintf(&pre, "Process.instance_variable_set(:@argv0, %q.dup.freeze)\n", cmdline)
	mrb.SetGV("$0", cmdline)
	if o.backtraceLimit != "" {
		if n, err := strconv.Atoi(o.backtraceLimit); err == nil {
			mrb.SetGV("$__backtrace_limit__", n)
		}
	}
	if pre.Len() > 0 {
		if _, err := mrb.LoadString(pre.String()); err != nil {
			if code, done := reportException(mrb, err); done {
				return code
			}
		}
	}

	// -I entries go first in $LOAD_PATH, before RUBYLIB's
	if len(o.incs) > 0 {
		mrb.SetGV("$__oruby_incs__", mrb.Value(o.incs))
		mrb.LoadString("$LOAD_PATH.unshift(*$__oruby_incs__); $__oruby_incs__ = nil")
	}

	// RubyGems is loaded at startup, as MRI does, unless disabled (the mspec
	// runner skips it for speed unless asked for explicitly)
	gems := o.feature("gems", os.Getenv("MSPEC_RUNNER") == "")
	if gems {
		mrb.LoadString("begin; require 'rubygems'; rescue LoadError => e; warn \"`RubyGems' were not loaded: #{e}\" if $VERBOSE; end")
	}

	c := mrb.MrbCcontextNew()
	defer c.Free()
	c.SetDumpResult(false)
	c.SetNoExec(o.checkSyntax)

	for _, lib := range o.libs {
		if lib == "rubygems" && gems {
			continue
		}
		mrb.SetGV("$__oruby_lib__", lib)
		if _, err := mrb.LoadString("require $__oruby_lib__"); err != nil {
			code, _ := reportException(mrb, err)
			return code
		}
	}

	if o.dashN || o.dashP {
		// MRI's -n/-p Kernel#chomp, #chop, #sub and #gsub on $_: core code
		// (<internal:...>), so $_ is the caller's (go-mrb.c's svar_start)
		dc := mrb.MrbCcontextNew()
		dc.Filename("<internal:dash_n>")
		_, _ = mrb.LoadBytesCxt([]byte(dashNMethods), dc)
		dc.Free()
	}

	// DATA: the main script file opened just past its __END__ line
	if src != nil && len(o.eLines) == 0 && o.script != "" && o.script != "-" {
		if off := oruby.DataOffset(src); off >= 0 {
			mrb.SetGV("$__oruby_data__", o.script)
			if _, err := mrb.LoadString(fmt.Sprintf("Object.const_set(:DATA, File.open($__oruby_data__)).seek(%d); $__oruby_data__ = nil", off+origLen-len(src))); err != nil {
				code, _ := reportException(mrb, err)
				return code
			}
		}
	}

	c.Filename(cmdline)
	mrb.SetMainScript(cmdline)
	// a program not read from a file is kept for #source_range (core_ext)
	if src != nil && (cmdline == "-e" || cmdline == "-") && mrb.RespondTo(mrb.KernelModule().Value(), mrb.Intern("__oruby_keep_main_source")) {
		mrb.Call(mrb.KernelModule().Value(), "__oruby_keep_main_source", cmdline, string(src))
	}
	var v oruby.Value
	switch {
	case src == nil:
		v, err = c.LoadIrepFile(o.script)
	default:
		if o.dashN || o.dashP {
			src = loopWrap(src, o)
		}
		if o.kcode != "" {
			src = oruby.PreprocessRubyWith(src, &oruby.PreprocessOptions{SourceEncoding: o.kcode})
		}
		if len(src) == 0 {
			src = []byte("nil")
		}
		if mrb.RespondTo(mrb.KernelModule().Value(), mrb.Intern("__oruby_main_binding")) {
			src = mainBindingInject(src)
		}
		v, err = c.LoadBytes(src)
	}
	_ = v
	if err != nil {
		code, _ := reportException(mrb, err)
		return code
	}
	mrb.GCArenaRestore(ai)

	if o.checkSyntax {
		fmt.Println("Syntax OK")
		return 0
	}
	return finish(mrb)
}

// mainBindingInject has the main script hand its binding to
// TOPLEVEL_BINDING first (Kernel.__oruby_main_binding): at the start of its
// first line of code, after a shebang, magic comments and =begin blocks,
// so that no line number moves
func mainBindingInject(src []byte) []byte {
	const call = "::Kernel.__oruby_main_binding(binding); "
	pos, inDoc := 0, false
	if bytes.HasPrefix(src, []byte("\xef\xbb\xbf")) { // a UTF-8 BOM stays first
		pos = 3
	}
	for pos < len(src) {
		end := bytes.IndexByte(src[pos:], '\n')
		line := src[pos:]
		if end >= 0 {
			line = src[pos : pos+end]
		}
		t := bytes.TrimLeft(line, " \t\r")
		switch {
		case inDoc:
			if bytes.HasPrefix(line, []byte("=end")) {
				inDoc = false
			}
		case bytes.HasPrefix(line, []byte("=begin")):
			inDoc = true
		case len(t) == 0 || t[0] == '#':
		case bytes.HasPrefix(line, []byte("__END__")):
			return src
		default:
			out := make([]byte, 0, len(src)+len(call))
			out = append(out, src[:pos]...)
			out = append(out, call...)
			return append(out, src[pos:]...)
		}
		if end < 0 {
			break
		}
		pos += end + 1
	}
	return src
}

func firstLine(src []byte) []byte {
	if i := bytes.IndexByte(src, '\n'); i >= 0 {
		return src[:i]
	}
	return src
}

func readAll(f *os.File) ([]byte, error) {
	var b bytes.Buffer
	_, err := b.ReadFrom(f)
	return b.Bytes(), err
}

// dashNMethods are the Kernel methods MRI's -n and -p define, on $_
const dashNMethods = `module Kernel
  def chomp(*args) = ($_ = args.empty? ? $_.chomp($/) : $_.chomp(*args))
  def chop = ($_ = $_.chop)
  def sub(*args, &blk) = ($_ = $_.sub(*args, &blk))
  def gsub(*args, &blk) = ($_ = $_.gsub(*args, &blk))
  module_function :chomp, :chop, :sub, :gsub
end
`

// loopWrap puts the program in -n/-p's `while gets` loop; BEGIN blocks are
// moved before the loop, so they run once
func loopWrap(src []byte, o *options) []byte {
	begins, body := oruby.SplitBeginBlocks(src)
	var b bytes.Buffer
	b.Write(begins)
	b.WriteString("while gets;")
	if o.dashL {
		b.WriteString("$_.chomp!($/);")
	}
	if o.dashA {
		b.WriteString("$F = $; ? $_.split($;) : $_.split;")
	}
	b.Write(body)
	b.WriteString("\n")
	if o.dashP {
		b.WriteString("print $_\n")
	}
	b.WriteString("end\n")
	return b.Bytes()
}

// finish runs at_exit handlers; an exception or exit in one sets the status
func finish(mrb *oruby.MrbState) int {
	mrb.RunAtExitHandlers()
	if mrb.Exc() != nil {
		code, _ := reportException(mrb, mrb.Err())
		return code
	}
	if st, ok := mrb.AtExitStatus(); ok {
		return st
	}
	return 0
}

// reportException prints an uncaught exception as MRI does and returns the
// exit status: SystemExit's status, 1 otherwise. done is false when there
// was no pending exception.
func reportException(mrb *oruby.MrbState, err error) (int, bool) {
	exc := mrb.Exc()
	if exc == nil {
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", progName(), err)
			return 1, true
		}
		return 0, false
	}
	mrb.ExcClear()
	// at_exit handlers see the exception that ends the program in $!
	mrb.SetGV("$!", exc.Value())
	if exc.IsKindOf(mrb.ClassGet("SystemExit")) {
		status := mrb.Call(exc.Value(), "status")
		code := 0
		if !status.IsNil() {
			code = int(status.Int())
		}
		mrb.RunAtExitHandlers()
		// an exit in a handler decides the status (MRI's)
		if st, ok := mrb.AtExitStatus(); ok {
			code = st
		}
		return code, true
	}
	mrb.SetGV("$__oruby_top_exc__", exc.Value())
	// at_exit handlers run before the error is printed, as in MRI
	mrb.RunAtExitHandlers()
	mrb.ExcClear()
	// an uncaught SignalException ends the process with its signal; only
	// Interrupt is reported
	signo := 0
	if mrb.ClassDefined("SignalException") && exc.IsKindOf(mrb.ClassGet("SignalException")) {
		if v := mrb.Call(exc.Value(), "signo"); v.IsInteger() {
			signo = v.Int()
		}
		if !exc.IsKindOf(mrb.ClassGet("Interrupt")) {
			mrb.SetGV("$__oruby_top_exc__", mrb.NilValue())
			mrb.LoadString("$stdout.flush rescue nil; $stderr.flush rescue nil")
			oruby.SignalKillSelf(syscall.Signal(signo))
		}
	}
	_, perr := mrb.LoadString(`begin
  e = $__oruby_top_exc__
  $__oruby_top_exc__ = nil
  $stderr.write(e.full_message(highlight: false))
  $stderr.flush
rescue Exception
  $stderr.write("#{e.class}: #{e.message rescue '?'}\n") rescue nil
end`)
	if perr != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", progName(), err)
	}
	if signo > 0 {
		mrb.LoadString("$stdout.flush rescue nil")
		oruby.SignalKillSelf(syscall.Signal(signo))
	}
	return 1, true
}
