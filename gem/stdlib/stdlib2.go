package stdlib

import (
	"html"
	"math/big"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/rb"
)

// pure Ruby libraries (ruby/<name>.rb), loaded on require
var rubyLibs = map[string][]string{
	"delegate":    nil,
	"singleton":   nil,
	"forwardable": nil,
	"observer":    nil,
	"weakref":     {"delegate"},
	"prime":       {"singleton", "forwardable"},
	"getoptlong":  nil,
	"csv":         {"stringio"},
	"matrix":      nil,
	"open-uri":    {"uri", "stringio"},
	"optparse":    nil,
	"pathname":    nil,
	"tsort":       nil,
}

func init() {
	for name, deps := range rubyLibs {
		name, deps := name, deps
		oruby.LazyGem(name, func(mrb *oruby.MrbState) interface{} {
			for _, d := range deps {
				if _, err := mrb.Resolve(d); err != nil {
					return err
				}
			}
			if name == "open-uri" {
				return initOpenURI(mrb)
			}
			if name == "prime" {
				mrb.DefineClass("Prime", mrb.ObjectClass()).DefineClassMethod("__probably_prime", probablyPrime, mrb.ArgsReq(1))
			}
			load(mrb, name)
			return nil
		})
	}
	oruby.GemAlias("forwardable/impl", "forwardable")
	oruby.GemAlias("csv/table", "csv")
	oruby.GemAlias("csv/row", "csv")

	// Pathname is a core class in Ruby 4 (defined without require)
	oruby.Gem("pathname_core", func(mrb *oruby.MrbState) interface{} {
		for _, dep := range []string{"io", "io/file"} {
			mrb.Resolve(dep)
		}
		if _, err := mrb.Resolve("pathname"); err != nil {
			return err
		}
		return nil
	})

	oruby.LazyGem("open3", func(mrb *oruby.MrbState) interface{} {
		if _, err := mrb.Resolve("io"); err != nil {
			return err
		}
		mod := mrb.DefineModule("Open3")
		mod.DefineClassMethod("__spawn", open3Spawn, mrb.ArgsReq(5))
		load(mrb, "open3")
		return nil
	})

	oruby.LazyGem("cgi/escape", func(mrb *oruby.MrbState) interface{} {
		cgi := mrb.DefineClass("CGI", mrb.ObjectClass())
		cgi.DefineClassMethod("__escape", cgiEscape, mrb.ArgsReq(2))
		cgi.DefineClassMethod("__unescape", cgiUnescape, mrb.ArgsReq(2))
		cgi.DefineClassMethod("__escape_html", cgiEscapeHTML, mrb.ArgsReq(1))
		cgi.DefineClassMethod("__unescape_html", cgiUnescapeHTML, mrb.ArgsReq(1))
		load(mrb, "cgi_escape")
		return nil
	})
	oruby.GemAlias("cgi/util", "cgi/escape")
	oruby.GemAlias("cgi", "cgi/escape")
}

func probablyPrime(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	v := mrb.GetArgsFirst()
	n, ok := new(big.Int).SetString(mrb.String(v), 10)
	if !ok {
		return oruby.False
	}
	return mrb.BoolValue(n.ProbablyPrime(20))
}

// Open3.__spawn(env, argv, chdir, merge_err, stdin_mode)
// argv: [cmd] runs through /bin/sh when it is a single string with shell
// metacharacters, otherwise exec'ed directly.
// Returns [pid, stdin_w_fd, stdout_r_fd, stderr_r_fd_or_nil]
func open3Spawn(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	env := os.Environ()
	if !a[0].IsNil() {
		h := mrb.HashKeys(a[0])
		over := map[string]*string{}
		for i := 0; i < h.Len(); i++ {
			k := mrb.AryEntry(h, i)
			v := mrb.HashGet(a[0], k)
			key := mrb.String(k)
			if v.IsNil() {
				over[key] = nil
			} else {
				s := mrb.String(v)
				over[key] = &s
			}
		}
		var out []string
		for _, kv := range env {
			k := kv[:strings.IndexByte(kv, '=')]
			if _, ok := over[k]; !ok {
				out = append(out, kv)
			}
		}
		for k, v := range over {
			if v != nil {
				out = append(out, k+"="+*v)
			}
		}
		env = out
	}
	var argv []string
	for _, v := range oruby.RArraySlice(a[1]) {
		if v.Type() == oruby.MrbTTArray { // [cmd, argv0]
			argv = append(argv, mrb.String(mrb.AryEntry(v, 0)))
			continue
		}
		argv = append(argv, mrb.String(v))
	}
	if len(argv) == 0 {
		return rb.Raise(mrb, "ArgumentError", "wrong number of arguments (given 0, expected 1+)")
	}
	if len(argv) == 1 && strings.ContainsAny(argv[0], "*?{}[]<>()~&|\\$;'`\"\n# ") {
		argv = []string{"/bin/sh", "-c", argv[0]}
	}
	path := argv[0]
	if !strings.Contains(path, "/") {
		p, err := exec.LookPath(path)
		if err != nil {
			return rb.Raise(mrb, "Errno::ENOENT", "No such file or directory - %s", path)
		}
		path = p
	}
	pipe := func() (int, int, error) {
		var p [2]int
		if err := syscall.Pipe(p[:]); err != nil {
			return 0, 0, err
		}
		syscall.CloseOnExec(p[0])
		syscall.CloseOnExec(p[1])
		return p[0], p[1], nil
	}
	inR, inW, err := pipe()
	if err != nil {
		return mrb.RaiseError(err)
	}
	outR, outW, err := pipe()
	if err != nil {
		return mrb.RaiseError(err)
	}
	errR, errW := -1, -1
	merge := a[3].Bool()
	if !merge {
		errR, errW, err = pipe()
		if err != nil {
			return mrb.RaiseError(err)
		}
	}
	childErr := errW
	if merge {
		childErr = outW
	}
	dir := ""
	if !a[2].IsNil() {
		dir = mrb.String(a[2])
	}
	pid, err := syscall.ForkExec(path, argv, &syscall.ProcAttr{
		Dir:   dir,
		Env:   env,
		Files: []uintptr{uintptr(inR), uintptr(outW), uintptr(childErr)},
	})
	syscall.Close(inR)
	syscall.Close(outW)
	if errW >= 0 {
		syscall.Close(errW)
	}
	if err != nil {
		syscall.Close(inW)
		syscall.Close(outR)
		if errR >= 0 {
			syscall.Close(errR)
		}
		return mrb.RaiseError(err)
	}
	var errFd interface{}
	if errR >= 0 {
		errFd = errR
	}
	return mrb.Value([]interface{}{pid, inW, outR, errFd})
}

// CGI.__escape(str, space_as_plus)
func cgiEscape(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	s, err := rb.Str(mrb, a[0])
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	var b strings.Builder
	const hex = "0123456789ABCDEF"
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '.', c == '_', c == '~':
			b.WriteByte(c)
		case c == ' ' && a[1].Bool():
			b.WriteByte('+')
		default:
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
		}
	}
	return mrb.StrNew(b.String())
}

// CGI.__unescape(str, plus_as_space)
func cgiUnescape(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	s, err := rb.Str(mrb, a[0])
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '+' && a[1].Bool():
			b.WriteByte(' ')
		case c == '%' && i+2 < len(s) && ishex(s[i+1]) && ishex(s[i+2]):
			b.WriteByte(unhex(s[i+1])<<4 | unhex(s[i+2]))
			i += 2
		default:
			b.WriteByte(c)
		}
	}
	return mrb.StrNew(b.String())
}

func ishex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func unhex(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	}
	return c - 'A' + 10
}

var htmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;")

func cgiEscapeHTML(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	s, err := rb.Str(mrb, mrb.GetArgsFirst())
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	return mrb.StrNew(htmlEscaper.Replace(s))
}

// unescapes the entities Ruby's CGI.unescapeHTML knows: named amp/quot/gt/lt/apos
// and numeric (decimal/hex) references
func cgiUnescapeHTML(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	s, err := rb.Str(mrb, mrb.GetArgsFirst())
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	if !strings.Contains(s, "&") {
		return mrb.StrNew(s)
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '&' {
			b.WriteByte(s[i])
			continue
		}
		j := strings.IndexByte(s[i:], ';')
		if j < 0 {
			b.WriteString(s[i:])
			break
		}
		ent := s[i+1 : i+j]
		repl, ok := "", false
		switch ent {
		case "amp":
			repl, ok = "&", true
		case "quot":
			repl, ok = `"`, true
		case "gt":
			repl, ok = ">", true
		case "lt":
			repl, ok = "<", true
		case "apos":
			repl, ok = "'", true
		default:
			if strings.HasPrefix(ent, "#") && len(ent) > 1 {
				u := html.UnescapeString("&" + ent + ";")
				if u != "&"+ent+";" {
					repl, ok = u, true
				}
			}
		}
		if ok {
			b.WriteString(repl)
			i += j
		} else {
			b.WriteByte('&')
		}
	}
	return mrb.StrNew(b.String())
}
