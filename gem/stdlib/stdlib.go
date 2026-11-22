// Package stdlib registers ORuby's standard-library providers, including
// libraries implemented in Go, Ruby sources, and other ORuby gems.
package stdlib

import (
	"crypto/rand"
	"embed"
	"os"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/base64"
	_ "github.com/oruby/oruby/gem/bcrypt"
	_ "github.com/oruby/oruby/gem/bcrypt_pbkdf"
	_ "github.com/oruby/oruby/gem/bigdecimal"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/gem/coverage"
	_ "github.com/oruby/oruby/gem/date"
	_ "github.com/oruby/oruby/gem/digest"
	_ "github.com/oruby/oruby/gem/ed25519"
	_ "github.com/oruby/oruby/gem/env"
	_ "github.com/oruby/oruby/gem/erubi"
	_ "github.com/oruby/oruby/gem/etc"
	_ "github.com/oruby/oruby/gem/ffi"
	_ "github.com/oruby/oruby/gem/http"
	"github.com/oruby/oruby/gem/internal/rb"
	_ "github.com/oruby/oruby/gem/io"
	_ "github.com/oruby/oruby/gem/io/file"
	_ "github.com/oruby/oruby/gem/io/popen"
	_ "github.com/oruby/oruby/gem/json"
	_ "github.com/oruby/oruby/gem/load"
	_ "github.com/oruby/oruby/gem/marshal"
	_ "github.com/oruby/oruby/gem/msgpack"
	_ "github.com/oruby/oruby/gem/nethttp"
	_ "github.com/oruby/oruby/gem/process"
	_ "github.com/oruby/oruby/gem/regexp"
	_ "github.com/oruby/oruby/gem/regexp/full"
	_ "github.com/oruby/oruby/gem/socket"
	_ "github.com/oruby/oruby/gem/sqlite3"
	_ "github.com/oruby/oruby/gem/thread"
	_ "github.com/oruby/oruby/gem/time"
	_ "github.com/oruby/oruby/gem/transcode"
	_ "github.com/oruby/oruby/gem/uri"
	_ "github.com/oruby/oruby/gem/websocket"
	_ "github.com/oruby/oruby/gem/yaml"
	_ "github.com/oruby/oruby/gem/zlib"
)

//go:embed ruby/*.rb
var rubyFS embed.FS
var benchmarkStart = time.Now()

func load(mrb *oruby.MrbState, name string) {
	src, err := rubyFS.ReadFile("ruby/" + name + ".rb")
	if err != nil {
		panic(err)
	}
	rb.LoadRuby(mrb, name+".rb", src)
}

func init() {
	// require names of libraries provided by eagerly loaded oruby gems
	oruby.LazyGem("stringio", func(mrb *oruby.MrbState) interface{} {
		// MRI's StringIO is C: as <internal:> code its gets sets the
		// caller's $_ (go-mrb.c svar_start), and backtraces skip its frames
		src, err := rubyFS.ReadFile("ruby/stringio.rb")
		if err != nil {
			panic(err)
		}
		rb.LoadRuby(mrb, "<internal:stringio>", src)
		return nil
	})
	oruby.GemAlias("tempfile", "io/file")
	oruby.LazyGem("strscan", func(mrb *oruby.MrbState) interface{} {
		load(mrb, "strscan")
		return nil
	})
	for _, name := range []string{"set", "fiber", "enumerator", "rational", "continuation", "thread.rb", "io/nonblock", "io/wait", "English"} {
		oruby.LazyGem(name, func(*oruby.MrbState) interface{} { return nil })
	}
	oruby.LazyGem("io/console", func(mrb *oruby.MrbState) interface{} {
		mrb.Require("stringio")
		load(mrb, "console")
		return nil
	})
	oruby.LazyGem("prettyprint", func(mrb *oruby.MrbState) interface{} {
		load(mrb, "prettyprint")
		return nil
	})
	oruby.LazyGem("pp", func(mrb *oruby.MrbState) interface{} {
		load(mrb, "pp")
		return nil
	})
	oruby.LazyGem("benchmark", func(mrb *oruby.MrbState) interface{} {
		mod := mrb.DefineModule("Benchmark")
		mod.DefineClassMethod("__clock", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
			return mrb.FloatValue(time.Since(benchmarkStart).Seconds())
		}, mrb.ArgsNone())
		load(mrb, "benchmark")
		return nil
	})

	oruby.LazyGem("random/formatter", func(mrb *oruby.MrbState) interface{} {
		load(mrb, "formatter")
		return nil
	})
	oruby.LazyGem("securerandom", func(mrb *oruby.MrbState) interface{} {
		mrb.Require("random/formatter")
		mod := mrb.DefineModule("SecureRandom")
		mod.DefineClassMethod("gen_random", secureBytes, mrb.ArgsReq(1))
		mod.DefineClassMethod("bytes", secureBytes, mrb.ArgsReq(1))
		load(mrb, "securerandom")
		return nil
	})
	oruby.LazyGem("tmpdir", func(mrb *oruby.MrbState) interface{} {
		dir := mrb.ClassGet("Dir")
		dir.DefineClassMethod("__mkdtemp", mkdtemp, mrb.ArgsReq(2))
		dir.DefineClassMethod("__systmpdir", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
			return mrb.StrNew(strings.TrimSuffix(os.TempDir(), "/"))
		}, mrb.ArgsNone())
		load(mrb, "tmpdir")
		return nil
	})
	oruby.LazyGem("find", func(mrb *oruby.MrbState) interface{} {
		mod := mrb.DefineModule("Find")
		mod.DefineClassMethod("__children", findChildren, mrb.ArgsReq(1))
		load(mrb, "find")
		return nil
	})
	oruby.LazyGem("shellwords", func(mrb *oruby.MrbState) interface{} {
		mod := mrb.DefineModule("Shellwords")
		mod.DefineClassMethod("__split", shellSplit, mrb.ArgsReq(1))
		load(mrb, "shellwords")
		return nil
	})
	oruby.LazyGem("abbrev", func(mrb *oruby.MrbState) interface{} {
		load(mrb, "abbrev")
		return nil
	})
	oruby.LazyGem("monitor", func(mrb *oruby.MrbState) interface{} {
		load(mrb, "monitor")
		return nil
	})
	oruby.LazyGem("timeout", func(mrb *oruby.MrbState) interface{} {
		load(mrb, "timeout")
		return nil
	})
	oruby.LazyGem("syslog", initSyslog)
	oruby.LazyGem("fcntl", func(mrb *oruby.MrbState) interface{} {
		mod := mrb.DefineModule("Fcntl")
		for name, v := range map[string]int{
			"F_DUPFD": syscall.F_DUPFD, "F_GETFD": syscall.F_GETFD, "F_SETFD": syscall.F_SETFD,
			"F_GETFL": syscall.F_GETFL, "F_SETFL": syscall.F_SETFL, "F_GETLK": syscall.F_GETLK,
			"F_SETLK": syscall.F_SETLK, "F_SETLKW": syscall.F_SETLKW, "FD_CLOEXEC": syscall.FD_CLOEXEC,
			"F_RDLCK": syscall.F_RDLCK, "F_UNLCK": syscall.F_UNLCK, "F_WRLCK": syscall.F_WRLCK,
			"O_CREAT": syscall.O_CREAT, "O_EXCL": syscall.O_EXCL, "O_NOCTTY": syscall.O_NOCTTY,
			"O_TRUNC": syscall.O_TRUNC, "O_APPEND": syscall.O_APPEND, "O_NONBLOCK": syscall.O_NONBLOCK,
			"O_NDELAY": syscall.O_NONBLOCK, "O_RDONLY": syscall.O_RDONLY, "O_RDWR": syscall.O_RDWR,
			"O_WRONLY": syscall.O_WRONLY, "O_ACCMODE": syscall.O_ACCMODE, "F_DUPFD_CLOEXEC": syscall.F_DUPFD_CLOEXEC,
		} {
			mod.Const(name, v)
		}
		mod.Const("VERSION", "1.1.0")
		return nil
	})
	oruby.LazyGem("rbconfig/sizeof", func(mrb *oruby.MrbState) interface{} {
		mrb.Require("rbconfig")
		load(mrb, "sizeof")
		return nil
	})
}

func secureBytes(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	n, err := rb.Int(mrb, mrb.GetArgsFirst())
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	if n < 0 {
		return mrb.Raise(mrb.EArgumentError(), "negative string size (or size too big)")
	}
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return mrb.Raise(mrb.ERuntimeError(), err.Error())
	}
	v := mrb.BytesValue(buf)
	return mrb.Call(v, "force_encoding", mrb.StrNew("ASCII-8BIT"))
}

func mkdtemp(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	dir, pattern := args.Item(0).String(), args.Item(1).String()
	name, err := os.MkdirTemp(dir, pattern)
	if err != nil {
		return mrb.RaiseError(err)
	}
	return mrb.StrNew(name)
}

// findChildren returns sorted directory entries of path, or nil when path
// is not a directory (symlinks are not followed). Errors raise Errno.
func findChildren(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	path := mrb.GetArgsFirst().String()
	fi, err := os.Lstat(path)
	if err != nil {
		return mrb.RaiseError(err)
	}
	if !fi.IsDir() {
		return mrb.NilValue()
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return mrb.RaiseError(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	ary := mrb.AryNew()
	for _, n := range names {
		ary.Push(mrb.StrNew(n))
	}
	return ary
}

func shellSplit(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s, err := rb.Str(mrb, mrb.GetArgsFirst())
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	words, err := splitWords(s)
	if err != nil {
		return mrb.Raise(mrb.EArgumentError(), err.Error())
	}
	ary := mrb.AryNew()
	for _, w := range words {
		ary.Push(mrb.StrNew(w))
	}
	return ary
}

// splitWords splits like a POSIX shell (Shellwords.shellsplit)
func splitWords(s string) ([]string, error) {
	var words []string
	var cur strings.Builder
	inWord := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		case c == '\\':
			inWord = true
			if i+1 < len(s) {
				i++
				if s[i] != '\n' {
					cur.WriteByte(s[i])
				}
			}
		case c == '\'':
			inWord = true
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				return nil, errUnmatched(s)
			}
			cur.WriteString(s[i+1 : i+1+j])
			i += j + 1
		case c == '"':
			inWord = true
			i++
			closed := false
			for ; i < len(s); i++ {
				if s[i] == '"' {
					closed = true
					break
				}
				if s[i] == '\\' && i+1 < len(s) && strings.IndexByte("$`\"\\\n", s[i+1]) >= 0 {
					i++
					if s[i] == '\n' {
						continue
					}
				}
				cur.WriteByte(s[i])
			}
			if !closed {
				return nil, errUnmatched(s)
			}
		default:
			inWord = true
			cur.WriteByte(c)
		}
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words, nil
}

type unmatched string

func (u unmatched) Error() string { return "Unmatched quote: " + string(u) }

func errUnmatched(s string) error { return unmatched(s) }
