package regexp

import (
	"errors"
	"fmt"
	goregexp "regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/oruby/oruby"
)

var matchTimeout atomic.Int64

// MatchTimeout returns Regexp.timeout (0 = none). Fallback engines apply it
// to patterns they compile.
func MatchTimeout() time.Duration { return time.Duration(matchTimeout.Load()) }

// SetMatchTimeout sets Regexp.timeout (process-wide).
func SetMatchTimeout(d time.Duration) { matchTimeout.Store(int64(d)) }

// Engine runs a compiled pattern. All offsets are byte offsets into s.
//
// The gem uses Go's RE2 engine (regexp package) for every pattern it can
// express exactly. Patterns that need Ruby-only syntax (lookaround, atomic
// groups, backreferences, possessive quantifiers, \K, \G, ...) go to a
// fallback engine registered with RegisterFallback; see gem/regexp/full.
type Engine interface {
	// FindAt returns the leftmost match starting at or after byte pos, using
	// all of s as context for anchors and lookbehind. The result is
	// [start, end, g1start, g1end, ...] with -1 for groups that did not
	// participate, or nil when there is no match.
	FindAt(s string, pos int) ([]int, error)
	// NumSubexp returns the number of capture groups.
	NumSubexp() int
	// SubexpNames returns NumSubexp()+1 names; "" for unnamed groups and index 0.
	SubexpNames() []string
}

// FallbackFunc compiles a Ruby regexp that the RE2 engine cannot express.
// source is the Ruby pattern as written; options are Ruby Regexp options.
type FallbackFunc func(source string, options int) (Engine, error)

var (
	fallbackMu sync.RWMutex
	fallback   FallbackFunc
)

// RegisterFallback installs the engine used for patterns RE2 cannot express.
// Without one, such patterns raise RegexpError naming the unsupported construct.
func RegisterFallback(f FallbackFunc) {
	fallbackMu.Lock()
	fallback = f
	fallbackMu.Unlock()
}

func getFallback() FallbackFunc {
	fallbackMu.RLock()
	defer fallbackMu.RUnlock()
	return fallback
}

// Regexp is the Go value behind a Ruby Regexp object.
type Regexp struct {
	source  string // Ruby source as written
	options int    // Ruby options (IgnoreCase, Extended, Multiline)
	enc     string // Encoding name given by Regexp#initialize ("": from source)
	engine  Engine
	re2     *goregexp.Regexp // set when engine is RE2

	// startContext is set when the pattern uses assertions that look before
	// the search position (^, \A, \b, \B). RE2 cannot start a search inside
	// a string, so searches at pos > 0 use the fallback engine when present.
	startContext bool
	lineStart    bool
	ctxOnce      sync.Once
	ctxEngine    Engine

	namesOnce sync.Once
	names     []string
}

// RegexpError is a pattern compile error; the gem raises it as RegexpError.
type RegexpError struct{ msg string }

func (e *RegexpError) Error() string { return e.msg }

// NewRegexpError returns an error raised in Ruby as RegexpError.
func NewRegexpError(msg string) error { return &RegexpError{msg} }

// nativeSyntaxError identifies Ruby syntax the Go translators cannot express.
type nativeSyntaxError struct{ *RegexpError }

// NeedsNativeSyntax reports whether err calls for a native regexp engine.
func NeedsNativeSyntax(err error) bool {
	var native *nativeSyntaxError
	return errors.As(err, &native)
}

// compileRuby compiles a Ruby regexp: RE2 when the pattern can be expressed
// exactly, otherwise the registered fallback engine.
// compiled: the engine of each (source, options) compiled so far (engines are
// immutable and safe for concurrent use); every Regexp gets its own struct
type compiledKey struct {
	source  string
	options int
}
type compiledRe struct {
	re2          *goregexp.Regexp
	engine       Engine
	startContext bool
	lineStart    bool
}

var (
	compiled      sync.Map // compiledKey -> *compiledRe
	compiledCount atomic.Int32
)

const compiledMax = 4096 // patterns built at run time do not grow it forever

// compileRuby compiles a Ruby regexp, once per (source, options): a regexp
// literal is compiled each time it is evaluated
func compileRuby(source string, options int) (*Regexp, error) {
	key := compiledKey{source, options}
	if c, ok := compiled.Load(key); ok {
		cr := c.(*compiledRe)
		return &Regexp{source: source, options: options, re2: cr.re2, engine: cr.engine, startContext: cr.startContext, lineStart: cr.lineStart}, nil
	}
	r, err := compileRubyNew(source, options)
	if err == nil && compiledCount.Load() < compiledMax {
		compiled.Store(key, &compiledRe{r.re2, r.engine, r.startContext, r.lineStart})
		compiledCount.Add(1)
	}
	return r, err
}

func compileRubyNew(source string, options int) (*Regexp, error) {
	tr, err := translate(source, options, targetRE2)
	if err == nil {
		re, cerr := goregexp.Compile(tr.pattern)
		if cerr == nil {
			r := &Regexp{source: source, options: options, re2: re, startContext: tr.startContext, lineStart: tr.lineStart}
			r.engine = &re2Engine{re: re}
			return r, nil
		}
		// RE2 limits (e.g. repeat count > 1000): try the full engine
		err = &needsFullError{construct: cerr.Error()}
	}
	var nf *needsFullError
	if !errors.As(err, &nf) && !NeedsNativeSyntax(err) {
		return nil, err
	}
	fb := getFallback()
	if fb == nil && source == "(?!)" {
		// Regexp.union() with no patterns: matches nothing
		re := goregexp.MustCompile(`[^\x00-\x{10FFFF}]`)
		return &Regexp{source: source, options: options, re2: re, engine: &re2Engine{re: re}}, nil
	}
	if fb == nil {
		if nf == nil {
			return nil, err
		}
		return nil, &RegexpError{fmt.Sprintf(
			"%s: not supported by the RE2 regexp engine (load the full regexp engine, gem/regexp/full): /%s/",
			nf.construct, source)}
	}
	eng, err := fb(source, options)
	if err != nil {
		return nil, err
	}
	return &Regexp{source: source, options: options, engine: eng}, nil
}

// MustCompileRuby is compileRuby that panics on error (for Go callers and tests).
func MustCompileRuby(source string, options int) *Regexp {
	r, err := compileRuby(source, options)
	if err != nil {
		panic(err)
	}
	return r
}

func (r *Regexp) numSubexp() int { return r.engine.NumSubexp() }

// subexpNames returns the group names with encoded names decoded
func (r *Regexp) subexpNames() []string {
	r.namesOnce.Do(func() {
		names := r.engine.SubexpNames()
		r.names = make([]string, len(names))
		for i, n := range names {
			r.names[i] = decodeGroupName(n)
		}
	})
	return r.names
}

// find returns the first match at or after byte pos (absolute offsets).
func (r *Regexp) find(s string, pos int) []int {
	m, err := r.findErr(s, pos)
	if err != nil {
		return nil
	}
	return m
}

// findErr is find that reports engine errors (backtracking limits, timeouts).
func (r *Regexp) findErr(s string, pos int) ([]int, error) {
	if pos < 0 || pos > len(s) {
		return nil, nil
	}
	return r.searchEngine(s, pos).FindAt(s, pos)
}

func (r *Regexp) searchEngine(s string, pos int) Engine {
	// RE2 also treats the end after a final newline as a line start.
	if r.re2 != nil && (pos > 0 && r.startContext || r.lineStart && strings.HasSuffix(s, "\n")) {
		if eng := r.contextEngine(); eng != nil {
			return eng
		}
	}
	return r.engine
}

func (r *Regexp) contextEngine() Engine {
	r.ctxOnce.Do(func() {
		if fb := getFallback(); fb != nil {
			if eng, err := fb(r.source, r.options); err == nil {
				r.ctxEngine = eng
			}
		}
	})
	return r.ctxEngine
}

// findAll returns successive matches with Ruby's scan/gsub semantics: after
// an empty match the search moves on one character; an empty match right
// after a non-empty match is allowed.
func (r *Regexp) findAll(s string) ([][]int, error) {
	eng := r.searchEngine(s, 0)
	if e, ok := eng.(*re2Engine); ok {
		return e.findAllRuby(s), nil
	}
	var out [][]int
	pos := 0
	for pos <= len(s) {
		m, err := eng.FindAt(s, pos)
		if err != nil {
			return out, err
		}
		if m == nil {
			break
		}
		out = append(out, m)
		if m[1] > m[0] {
			pos = m[1]
			continue
		}
		if m[1] >= len(s) {
			break
		}
		_, w := utf8.DecodeRuneInString(s[m[1]:])
		pos = m[1] + w
	}
	return out, nil
}

func (r *Regexp) matchString(s string) bool {
	eng := r.searchEngine(s, 0)
	if re2, ok := eng.(*re2Engine); ok {
		return re2.re.MatchString(s)
	}
	match, err := eng.FindAt(s, 0)
	return err == nil && match != nil
}

// re2Engine is the Go regexp (RE2) engine.
type re2Engine struct{ re *goregexp.Regexp }

func (e *re2Engine) NumSubexp() int        { return e.re.NumSubexp() }
func (e *re2Engine) SubexpNames() []string { return e.re.SubexpNames() }

// FindAt searches s[pos:]. RE2 has no start offset, so assertions looking
// before pos see a string start there; Regexp.find routes such patterns to
// the fallback engine when one is registered.
func (e *re2Engine) FindAt(s string, pos int) ([]int, error) {
	m := e.re.FindStringSubmatchIndex(s[pos:])
	if m == nil {
		return nil, nil
	}
	if pos > 0 {
		for i := range m {
			if m[i] >= 0 {
				m[i] += pos
			}
		}
	}
	return m, nil
}

// findAllRuby uses Go's context-correct FindAll and adds the empty matches
// Go skips but Ruby keeps: Go drops an empty match that directly follows a
// non-empty one, Ruby does not ("abc".scan(/b*/) => ["", "b", "", ""]).
func (e *re2Engine) findAllRuby(s string) [][]int {
	all := e.re.FindAllStringSubmatchIndex(s, -1)
	if len(all) == 0 {
		return all
	}
	out := make([][]int, 0, len(all))
	for i, m := range all {
		out = append(out, m)
		if m[1] == m[0] {
			continue
		}
		end := m[1]
		if i+1 < len(all) && all[i+1][0] == end {
			continue
		}
		// would the pattern match empty exactly at end?
		if em := e.re.FindStringSubmatchIndex(s[end:]); em != nil && em[0] == 0 && em[1] == 0 {
			for j := range em {
				if em[j] >= 0 {
					em[j] += end
				}
			}
			out = append(out, em)
		}
	}
	return out
}

// QuoteMeta escapes s with Ruby Regexp.escape rules.
func QuoteMeta(s string) string {
	b := make([]byte, 0, len(s)+8)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '[', ']', '{', '}', '(', ')', '|', '-', '*', '.', '\\', '?', '+', '^', '$', '#':
			b = append(b, '\\', c)
		case ' ':
			b = append(b, '\\', ' ')
		case '\t':
			b = append(b, '\\', 't')
		case '\n':
			b = append(b, '\\', 'n')
		case '\r':
			b = append(b, '\\', 'r')
		case '\f':
			b = append(b, '\\', 'f')
		case '\v':
			b = append(b, '\\', 'v')
		default:
			b = append(b, c)
		}
	}
	return string(b)
}

// rstr returns the bytes of a Ruby String; unlike the C-string accessors it
// accepts strings containing NUL bytes.
func rstr(v oruby.MrbValue) string { return v.Value().String() }

// SourceOptions returns the Ruby source and options of the Go value behind a
// Ruby Regexp (mrb.Data(regexpValue)); ok is false for other values.
func SourceOptions(data interface{}) (source string, options int, ok bool) {
	r, ok := data.(*Regexp)
	if !ok || r == nil {
		return "", 0, false
	}
	return r.source, r.options, true
}
