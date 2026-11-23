// Package full adds full Ruby regexp support to the regexp gem.
//
// The regexp gem uses Go's RE2 engine for every pattern RE2 can express
// exactly. Importing this package registers a backtracking fallback based on
// github.com/dlclark/regexp2 (pure Go, .NET-compatible) for patterns that
// need Ruby-only syntax: lookahead and lookbehind, atomic groups, possessive
// quantifiers, backreferences, \K, \G, \Z, \R, \X, conditionals and
// character class intersection.
//
// Standalone ORuby (gem/stdlib) imports it; embedded users who only need
// RE2-compatible patterns can import gem/regexp alone and avoid the module.
//
// The native engine handles recursive subexpression calls and the absent
// operator.
package full

import (
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/dlclark/regexp2/v2"
	rx "github.com/oruby/oruby/gem/regexp"
)

// MaxBacktrackingStack bounds backtracking memory per match (integer slots).
// A match exceeding it raises Regexp::TimeoutError instead of exhausting memory.
var MaxBacktrackingStack = 10_000_000

func init() {
	rx.RegisterFallback(compile)
}

type engine struct {
	re      *regexp2.Regexp
	names   []string // index = Ruby group number
	source  string
	options int
	opts    regexp2.RegexOptions
	timed   sync.Pool
}

func compile(source string, options int) (rx.Engine, error) {
	pattern, named, err := rx.TranslateFull(source, options)
	if rx.NeedsNativeSyntax(err) {
		return compileNative(source, options)
	}
	if err != nil {
		return nil, err
	}
	return compilePattern(source, options, pattern, named)
}

func compilePattern(source string, options int, pattern string, named bool) (*engine, error) {
	opts := regexp2.None
	if named {
		// with named groups, Ruby's plain (...) do not capture
		opts |= regexp2.ExplicitCapture
	}
	re, err := regexp2.Compile(pattern, opts,
		regexp2.OptionMaintainCaptureOrder(),
		regexp2.OptionMaxBacktrackingStackSize(MaxBacktrackingStack))
	if err != nil {
		msg := err.Error()
		msg = strings.TrimPrefix(msg, "error parsing regexp: ")
		return nil, rx.NewRegexpError(msg + ": /" + source + "/")
	}
	nums := re.GetGroupNumbers()
	names := make([]string, len(nums))
	for i, n := range nums {
		if name := re.GroupNameFromNumber(n); name != strconv.Itoa(n) {
			names[i] = name
		}
	}
	return &engine{re: re, names: names, source: source, options: options, opts: opts}, nil
}

func (e *engine) WithSearchOrigin(s string, pos int) (rx.Engine, error) {
	pattern, named, err := rx.TranslateFullAtEndDistance(e.source, e.options, utf8.RuneCountInString(s[pos:]))
	if err != nil {
		return nil, err
	}
	fixed, err := compilePattern(e.source, e.options, pattern, named)
	if err != nil {
		return nil, err
	}
	return fixed, nil
}

func (e *engine) NumSubexp() int        { return len(e.names) - 1 }
func (e *engine) SubexpNames() []string { return e.names }

// ErrBacktrackLimit is returned (as Regexp::TimeoutError) when a match
// exceeds MaxBacktrackingStack.
var ErrBacktrackLimit = errors.New("regexp match timeout: backtracking limit exceeded")

func (e *engine) FindAt(s string, pos int) ([]int, error) {
	return e.find(e.re, s, pos)
}

func (e *engine) FindAtDeadline(s string, pos int, deadline time.Time) ([]int, error) {
	re, _ := e.timed.Get().(*regexp2.Regexp)
	if re == nil {
		var err error
		re, err = regexp2.Compile(e.re.String(), e.opts,
			regexp2.OptionMaintainCaptureOrder(),
			regexp2.OptionMaxBacktrackingStackSize(MaxBacktrackingStack))
		if err != nil {
			return nil, err
		}
	}
	defer e.timed.Put(re)
	re.MatchTimeout = time.Until(deadline)
	if re.MatchTimeout <= 0 {
		return nil, rx.ErrMatchTimeout
	}
	match, err := e.find(re, s, pos)
	if time.Until(deadline) <= 0 {
		return nil, rx.ErrMatchTimeout
	}
	return match, err
}

func (e *engine) find(re *regexp2.Regexp, s string, pos int) ([]int, error) {
	m, err := re.FindStringMatchStartingAt(s, pos)
	if err != nil {
		if errors.Is(err, regexp2.ErrBacktrackingStackLimit) {
			return nil, ErrBacktrackLimit
		}
		return nil, err
	}
	if m == nil {
		return nil, nil
	}
	groups := m.Groups()
	out := make([]int, 2*len(e.names))
	for i := range e.names {
		if i >= len(groups) || len(groups[i].Captures) == 0 {
			out[2*i], out[2*i+1] = -1, -1
			continue
		}
		b, l := groups[i].ByteRange()
		out[2*i], out[2*i+1] = b, b+l
	}
	return out, nil
}
