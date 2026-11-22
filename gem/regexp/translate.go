package regexp

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Ruby (Onigmo) regexp syntax is translated before compiling, because Go's
// RE2 and regexp2 (.NET) syntax differ from Ruby in meaning, not only in
// features:
//
//   - ^ and $ are always line anchors in Ruby            -> (?m)
//   - Ruby /m and (?m) mean "dot matches newline"        -> (?s)
//   - \w \d \s are ASCII in Ruby, \s includes \v         -> explicit classes
//   - \h, [[:alpha:]] (Unicode), \p{Alpha}, {,n}         -> classes / {0,n}
//   - named groups make plain (...) non-capturing        -> (?:...)
//   - /x comments and whitespace                         -> stripped here
//
// targetRE2 output is Go regexp syntax; constructs RE2 cannot express return
// needsFullError. targetFull output is regexp2 syntax for gem/regexp/full.

type target int

const (
	targetRE2 target = iota
	targetFull
)

type needsFullError struct{ construct string }

func (e *needsFullError) Error() string {
	return e.construct + " requires the full regexp engine"
}

type translation struct {
	pattern      string
	named        bool
	startContext bool
	lineStart    bool
}

// TranslateFull converts a Ruby pattern to regexp2 (.NET) syntax for the full
// regexp engine. named reports whether the pattern defines named groups.
func TranslateFull(source string, options int) (pattern string, named bool, err error) {
	return translateFull(source, options, `\G`)
}

// TranslateFullAtEndDistance fixes \G at a position the given number of
// characters from the end of the haystack, independently of the search start.
func TranslateFullAtEndDistance(source string, options, remaining int) (pattern string, named bool, err error) {
	return translateFull(source, options, `(?=[\s\S]{`+strconv.Itoa(remaining)+`}\z)`)
}

func translateFull(source string, options int, startAnchor string) (string, bool, error) {
	tr, err := translateWithStartAnchor(source, options, targetFull, startAnchor)
	if err != nil {
		return "", false, err
	}
	return tr.pattern, tr.named, nil
}

func translate(source string, options int, t target) (translation, error) {
	return translateWithStartAnchor(source, options, t, `\G`)
}

func translateWithStartAnchor(source string, options int, t target, startAnchor string) (translation, error) {
	if strings.Contains(source, `\g`) {
		expanded, err := expandCalls(source, options&Extended != 0)
		if err != nil {
			return translation{}, err
		}
		source = expanded
	}
	tr := &translator{src: source, t: t, x: options&Extended != 0, lastAtom: -1, named: hasNamedGroups(source), startAnchor: startAnchor}
	if err := tr.run(); err != nil {
		return translation{}, err
	}
	prefix := "(?m"
	if options&IgnoreCase != 0 {
		prefix += "i"
	}
	if options&Multiline != 0 {
		prefix += "s"
	}
	prefix += ")"
	return translation{pattern: prefix + tr.out.String(), named: tr.named, startContext: tr.ctx, lineStart: tr.lineStart}, nil
}

type groupFrame struct {
	x         bool // /x state to restore
	atomStart int  // out offset where the group starts
}

type translator struct {
	src         string
	startAnchor string
	i           int
	t           target
	out         strings.Builder
	x           bool
	stack       []groupFrame
	named       bool
	ctx         bool
	lineStart   bool
	ncap        int
	lastAtom    int  // out offset of the last atom; -1 if none
	quant       bool // last token was a quantifier
	keep        bool // \K seen at top level
	topAlt      bool // top-level | seen
}

func (tr *translator) errorf(format string, args ...interface{}) error {
	return &RegexpError{fmt.Sprintf(format, args...) + ": /" + tr.src + "/"}
}

func (tr *translator) full(construct string) error {
	if tr.t == targetRE2 {
		return &needsFullError{construct: construct}
	}
	return nil
}

func (tr *translator) atom(s string) {
	tr.lastAtom = tr.out.Len()
	tr.quant = false
	tr.out.WriteString(s)
}

func (tr *translator) assertion(s string) {
	tr.lastAtom = -1
	tr.quant = false
	tr.out.WriteString(s)
}

// hasNamedGroups reports whether src defines (?<name>...) or (?'name'...).
func hasNamedGroups(src string) bool {
	depth := 0
	for i := 0; i < len(src); i++ {
		switch src[i] {
		case '\\':
			i++
		case '[':
			depth++
		case ']':
			if depth > 0 {
				depth--
			}
		case '(':
			if depth > 0 || i+3 >= len(src) || src[i+1] != '?' {
				continue
			}
			switch src[i+2] {
			case '<':
				if src[i+3] != '=' && src[i+3] != '!' {
					return true
				}
			case '\'':
				return true
			case 'P':
				if src[i+3] == '<' {
					return true
				}
			}
		}
	}
	return false
}

func (tr *translator) run() error {
	src := tr.src
	for tr.i < len(src) {
		c := src[tr.i]
		if tr.x {
			if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v' {
				tr.i++
				continue
			}
			if c == '#' {
				for tr.i < len(src) && src[tr.i] != '\n' {
					tr.i++
				}
				continue
			}
		}
		var err error
		switch c {
		case '\\':
			err = tr.escape()
		case '[':
			var cls string
			cls, err = tr.class()
			if err == nil {
				tr.atom(cls)
			}
		case '(':
			err = tr.group()
		case ')':
			if len(tr.stack) == 0 {
				return tr.errorf("unmatched close parenthesis")
			}
			f := tr.stack[len(tr.stack)-1]
			tr.stack = tr.stack[:len(tr.stack)-1]
			tr.x = f.x
			tr.out.WriteByte(')')
			tr.lastAtom = f.atomStart
			tr.quant = false
			tr.i++
		case '|':
			if len(tr.stack) == 0 {
				tr.topAlt = true
			}
			tr.assertion("|")
			tr.i++
		case '*', '+', '?':
			err = tr.quantifier(string(c))
			tr.i++
			if err == nil {
				err = tr.quantSuffix(false)
			}
		case '{':
			err = tr.interval()
		case '^':
			tr.ctx = true
			tr.lineStart = true
			if tr.t == targetFull {
				// Ruby treats an empty string as a line, but a trailing
				// newline does not create another line at the end.
				tr.assertion(`(?:\A|^(?!\z))`)
			} else {
				tr.assertion("^")
			}
			tr.i++
		case '$':
			tr.assertion("$")
			tr.i++
		case '.':
			tr.atom(".")
			tr.i++
		default:
			r, w := utf8.DecodeRuneInString(src[tr.i:])
			tr.atom(literal(r))
			tr.i += w
		}
		if err != nil {
			return err
		}
	}
	if len(tr.stack) > 0 {
		return tr.errorf("end pattern with unmatched parenthesis")
	}
	if tr.keep && tr.topAlt {
		return tr.errorf("\\K with top-level alternation is not supported")
	}
	return nil
}

// quantifier emits q for the last atom; a quantifier on a quantified atom
// (Ruby allows /a{2}+/ and /a**/) wraps the atom in a group first.
func (tr *translator) quantifier(q string) error {
	if tr.lastAtom < 0 {
		return tr.errorf("target of repeat operator is not specified")
	}
	if tr.quant {
		s := tr.out.String()
		tr.out.Reset()
		tr.out.WriteString(s[:tr.lastAtom] + "(?:" + s[tr.lastAtom:] + ")")
	}
	tr.out.WriteString(q)
	tr.quant = true
	return nil
}

// quantSuffix handles lazy (?) and possessive (+) suffixes. In Ruby {n,m}+
// is not possessive but a quantifier applied to the interval.
func (tr *translator) quantSuffix(interval bool) error {
	if tr.i >= len(tr.src) {
		return nil
	}
	switch tr.src[tr.i] {
	case '?':
		tr.out.WriteByte('?')
		tr.i++
	case '+':
		if interval {
			return nil
		}
		if err := tr.full("possessive quantifier"); err != nil {
			return err
		}
		tr.out.WriteByte('+')
		tr.i++
	}
	return nil
}

// interval parses {n}, {n,}, {,m}, {n,m}; anything else is a literal brace.
func (tr *translator) interval() error {
	src := tr.src
	j := tr.i + 1
	readNum := func() (int, bool) {
		k := j
		for j < len(src) && src[j] >= '0' && src[j] <= '9' {
			j++
		}
		if k == j {
			return 0, false
		}
		n, err := strconv.Atoi(src[k:j])
		return n, err == nil
	}
	lo, hasLo := readNum()
	hi, hasHi, comma := 0, false, false
	if j < len(src) && src[j] == ',' {
		comma = true
		j++
		hi, hasHi = readNum()
	}
	if j >= len(src) || src[j] != '}' || (!hasLo && !hasHi) {
		tr.atom(`\{`)
		tr.i++
		return nil
	}
	var q string
	switch {
	case !comma:
		q = fmt.Sprintf("{%d}", lo)
		hi = lo
	case !hasHi:
		q = fmt.Sprintf("{%d,}", lo)
	default:
		if hasHi && lo > hi {
			return tr.errorf("upper bound must be greater than lower bound")
		}
		q = fmt.Sprintf("{%d,%d}", lo, hi)
	}
	if lo > 1000 || hi > 1000 {
		if err := tr.full("repeat count over 1000"); err != nil {
			return err
		}
	}
	tr.i = j + 1
	if err := tr.quantifier(q); err != nil {
		return err
	}
	if !comma && tr.i < len(src) && src[tr.i] == '?' {
		return nil // {n}? repeats the entire fixed interval zero or one time.
	}
	return tr.quantSuffix(true)
}

func (tr *translator) push(x bool) {
	tr.stack = append(tr.stack, groupFrame{x: x, atomStart: tr.out.Len()})
}

func (tr *translator) captureRef(ref string, delimited bool) (string, error) {
	if n, err := strconv.Atoi(ref); err == nil {
		if tr.named {
			return "", tr.errorf("numbered backref/call is not allowed. (use name)")
		}
		if n < 0 {
			n += tr.ncap + 1
		}
		if n <= 0 {
			return "", tr.errorf("invalid backref number/name")
		}
		return strconv.Itoa(n), nil
	}
	if !delimited || ref == "" {
		return "", tr.errorf("invalid backref number/name")
	}
	if strings.ContainsAny(ref, "+-") {
		return "", tr.errorf("backreference with recursion level %s is not supported", ref)
	}
	return encodeGroupName(ref), nil
}

func (tr *translator) group() error {
	src := tr.src
	rest := src[tr.i:]
	if !strings.HasPrefix(rest, "(?") {
		tr.push(tr.x)
		if tr.named {
			tr.out.WriteString("(?:")
		} else {
			tr.ncap++
			tr.out.WriteString("(")
		}
		tr.i++
		tr.lastAtom, tr.quant = -1, false
		return nil
	}
	open := func(construct, emit string, n int) error {
		if construct != "" {
			if err := tr.full(construct); err != nil {
				return err
			}
		}
		tr.push(tr.x)
		tr.out.WriteString(emit)
		tr.i += n
		tr.lastAtom, tr.quant = -1, false
		return nil
	}
	switch {
	case strings.HasPrefix(rest, "(?#"):
		end := strings.IndexByte(rest, ')')
		if end < 0 {
			return tr.errorf("end pattern in group")
		}
		tr.i += end + 1
		return nil
	case strings.HasPrefix(rest, "(?:"):
		return open("", "(?:", 3)
	case strings.HasPrefix(rest, "(?="):
		return open("lookahead (?=...)", "(?=", 3)
	case strings.HasPrefix(rest, "(?!"):
		return open("negative lookahead (?!...)", "(?!", 3)
	case strings.HasPrefix(rest, "(?<="):
		return open("lookbehind (?<=...)", "(?<=", 4)
	case strings.HasPrefix(rest, "(?<!"):
		return open("negative lookbehind (?<!...)", "(?<!", 4)
	case strings.HasPrefix(rest, "(?>"):
		return open("atomic group (?>...)", "(?>", 3)
	case strings.HasPrefix(rest, "(?~"):
		return &nativeSyntaxError{&RegexpError{"absent operator (?~...) requires the native fallback: /" + tr.src + "/"}}
	case strings.HasPrefix(rest, "(?("):
		if err := tr.full("conditional (?(cond)...)"); err != nil {
			return err
		}
		end := strings.IndexByte(rest[3:], ')')
		if end < 0 {
			return tr.errorf("invalid conditional pattern")
		}
		cond := rest[3 : 3+end]
		delimited := strings.HasPrefix(cond, "<") && strings.HasSuffix(cond, ">") ||
			strings.HasPrefix(cond, "'") && strings.HasSuffix(cond, "'") && len(cond) >= 2
		if delimited {
			cond = cond[1 : len(cond)-1]
		}
		cond, err := tr.captureRef(cond, delimited)
		if err != nil {
			return err
		}
		return open("", "(?("+cond+")", 3+end+1)
	case strings.HasPrefix(rest, "(?<"), strings.HasPrefix(rest, "(?'"), strings.HasPrefix(rest, "(?P<"):
		start := 3
		closer := byte('>')
		if rest[2] == '\'' {
			closer = '\''
		} else if rest[2] == 'P' {
			start = 4
		}
		end := strings.IndexByte(rest[start:], closer)
		if end <= 0 {
			return tr.errorf("invalid group name")
		}
		name := encodeGroupName(rest[start : start+end])
		if r := rest[start]; r == '-' || r >= '0' && r <= '9' {
			return tr.errorf("invalid group name <%s>", rest[start:start+end])
		}
		tr.ncap++
		emit := "(?P<" + name + ">"
		if tr.t == targetFull {
			emit = "(?<" + name + ">"
		}
		return open("", emit, start+end+1)
	}
	// option group (?imx-imx) or (?imx-imx:...)
	j := tr.i + 2
	on, off := "", ""
	x := tr.x
	neg := false
	for ; j < len(src); j++ {
		c := src[j]
		switch c {
		case 'i', 'm', 'x':
			g := map[byte]string{'i': "i", 'm': "s", 'x': ""}[c]
			if c == 'x' {
				x = !neg
			}
			if neg {
				off += g
			} else {
				on += g
			}
			continue
		case '-':
			if neg {
				return tr.errorf("undefined group option")
			}
			neg = true
			continue
		}
		break
	}
	if j >= len(src) || (src[j] != ':' && src[j] != ')') {
		return tr.errorf("undefined group option")
	}
	flags := on
	if off != "" {
		flags += "-" + off
	}
	if src[j] == ')' {
		// applies to the rest of the enclosing group
		tr.x = x
		if flags != "" {
			tr.out.WriteString("(?" + flags + ")")
		}
		tr.i = j + 1
		return nil
	}
	tr.push(tr.x)
	tr.x = x
	if flags != "" {
		tr.out.WriteString("(?" + flags + ":")
	} else {
		tr.out.WriteString("(?:")
	}
	tr.i = j + 1
	tr.lastAtom, tr.quant = -1, false
	return nil
}

func (tr *translator) escape() error {
	src := tr.src
	if tr.i+1 >= len(src) {
		return tr.errorf("too short escape sequence")
	}
	c := src[tr.i+1]
	tr.i += 2
	ascii := func(re2, full string) {
		if tr.t == targetRE2 {
			tr.atom(re2)
		} else {
			tr.atom(full)
		}
	}
	switch c {
	case 'A':
		tr.ctx = true
		tr.assertion(`\A`)
	case 'z':
		tr.assertion(`\z`)
	case 'Z':
		if err := tr.full(`\Z`); err != nil {
			return err
		}
		tr.assertion(`\Z`)
	case 'b', 'B':
		tr.ctx = true
		tr.assertion(`\` + string(c))
	case 'G':
		if err := tr.full(`\G`); err != nil {
			return err
		}
		tr.ctx = true
		tr.assertion(tr.startAnchor)
	case 'K':
		if err := tr.full(`\K`); err != nil {
			return err
		}
		if len(tr.stack) > 0 {
			return tr.errorf("\\K inside a group is not supported")
		}
		if tr.topAlt {
			return tr.errorf("\\K with top-level alternation is not supported")
		}
		s := tr.out.String()
		tr.out.Reset()
		tr.out.WriteString("(?<=" + s + ")")
		tr.keep = true
		tr.lastAtom, tr.quant = -1, false
	case 'd':
		ascii(`\d`, `[0-9]`)
	case 'D':
		ascii(`\D`, `[^0-9]`)
	case 'w':
		ascii(`\w`, `[a-zA-Z0-9_]`)
	case 'W':
		ascii(`\W`, `[^a-zA-Z0-9_]`)
	case 's':
		tr.atom(`[\t\n\v\f\r ]`)
	case 'S':
		tr.atom(`[^\t\n\v\f\r ]`)
	case 'h':
		tr.atom(`[0-9a-fA-F]`)
	case 'H':
		tr.atom(`[^0-9a-fA-F]`)
	case 'R', 'X':
		if err := tr.full(`\` + string(c)); err != nil {
			return err
		}
		tr.atom(`\` + string(c))
	case '1', '2', '3', '4', '5', '6', '7', '8', '9':
		first := tr.i - 1
		j := tr.i
		for j < len(src) && src[j] >= '0' && src[j] <= '9' {
			j++
		}
		num := src[first:j]
		n, err := strconv.Atoi(num)
		if len(num) > 1 && (err != nil || n > tr.ncap) {
			end := first
			for end < j && end-first < 3 && src[end] <= '7' {
				end++
			}
			if end > first {
				value, _ := strconv.ParseUint(src[first:end], 8, 16)
				if value > 255 {
					return tr.errorf("invalid octal escape")
				}
				tr.i = end
				tr.atom(fmt.Sprintf(`\x%02x`, value))
			} else {
				tr.atom(string(c))
			}
			return nil
		}
		tr.i = j
		if tr.named {
			return tr.errorf("numbered backref/call is not allowed. (use name)")
		}
		if err := tr.full("backreference \\" + num); err != nil {
			return err
		}
		tr.atom(`\` + num)
	case 'k':
		if tr.i >= len(src) || (src[tr.i] != '<' && src[tr.i] != '\'') {
			return tr.errorf("invalid backref")
		}
		closer := byte('>')
		if src[tr.i] == '\'' {
			closer = '\''
		}
		end := strings.IndexByte(src[tr.i+1:], closer)
		if end <= 0 {
			return tr.errorf("invalid backref")
		}
		ref := src[tr.i+1 : tr.i+1+end]
		tr.i += end + 2
		if err := tr.full(`backreference \k<` + ref + `>`); err != nil {
			return err
		}
		ref, err := tr.captureRef(ref, true)
		if err != nil {
			return err
		}
		tr.atom(`\k<` + ref + `>`)
	case 'g':
		return tr.errorf("subexpression call \\g is not supported")
	case 'p', 'P':
		frag, single, neg, err := tr.property(c == 'P')
		if err != nil {
			return err
		}
		_ = single
		if neg {
			tr.atom("[^" + frag + "]")
		} else {
			tr.atom("[" + frag + "]")
		}
	default:
		tr.i -= 2
		r, err := tr.escapedChar()
		if err != nil {
			return err
		}
		tr.atom(literal(r))
	}
	return nil
}

// escapedChar reads a character escape at tr.i (pointing at '\\') and
// returns the character it denotes.
func (tr *translator) escapedChar() (rune, error) {
	src := tr.src
	c := src[tr.i+1]
	tr.i += 2
	hex := func(maxDigits int) (rune, int) {
		j := tr.i
		for j < len(src) && j-tr.i < maxDigits && isHex(src[j]) {
			j++
		}
		if j == tr.i {
			return 0, 0
		}
		n, _ := strconv.ParseUint(src[tr.i:j], 16, 32)
		return rune(n), j - tr.i
	}
	switch c {
	case 't':
		return '\t', nil
	case 'n':
		return '\n', nil
	case 'r':
		return '\r', nil
	case 'f':
		return '\f', nil
	case 'v':
		return '\v', nil
	case 'a':
		return '\a', nil
	case 'e':
		return 0x1b, nil
	case '0':
		j := tr.i
		for j < len(src) && j-tr.i < 2 && src[j] >= '0' && src[j] <= '7' {
			j++
		}
		n, _ := strconv.ParseUint("0"+src[tr.i:j], 8, 32)
		tr.i = j
		return rune(n), nil
	case 'x':
		r, n := hex(2)
		if n == 0 {
			return 0, tr.errorf("invalid hex escape")
		}
		tr.i += n
		return r, nil
	case 'u':
		if tr.i < len(src) && src[tr.i] == '{' {
			end := strings.IndexByte(src[tr.i:], '}')
			if end < 0 {
				return 0, tr.errorf("invalid Unicode escape")
			}
			body := strings.Fields(src[tr.i+1 : tr.i+end])
			tr.i += end + 1
			if len(body) != 1 {
				return 0, tr.errorf("multiple code points in \\u{} are not supported here")
			}
			n, err := strconv.ParseUint(body[0], 16, 32)
			if err != nil || n > unicode.MaxRune {
				return 0, tr.errorf("invalid Unicode escape")
			}
			return rune(n), nil
		}
		r, n := hex(4)
		if n != 4 {
			return 0, tr.errorf("invalid Unicode escape")
		}
		tr.i += n
		return r, nil
	case 'c':
		if tr.i >= len(src) {
			return 0, tr.errorf("too short control escape")
		}
		ch := src[tr.i]
		tr.i++
		return rune(ch & 0x1f), nil
	case 'C':
		if tr.i+1 >= len(src) || src[tr.i] != '-' {
			return 0, tr.errorf("invalid control-code syntax")
		}
		ch := src[tr.i+1]
		tr.i += 2
		return rune(ch & 0x1f), nil
	case 'M':
		return 0, tr.errorf("meta escape \\M- is not supported")
	}
	tr.i -= 1
	r, w := utf8.DecodeRuneInString(src[tr.i:])
	tr.i += w
	return r, nil
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// literal returns r escaped for use outside a character class.
func literal(r rune) string {
	if r < utf8.RuneSelf {
		if strings.ContainsRune(`\.+*?()|[]{}^$`, r) {
			return `\` + string(r)
		}
		if r < 0x20 || r == 0x7f {
			return fmt.Sprintf(`\x{%x}`, r)
		}
		return string(r)
	}
	if !unicode.IsPrint(r) {
		return fmt.Sprintf(`\x{%x}`, r)
	}
	return string(r)
}

// classLiteral returns r escaped for use inside a character class.
func classLiteral(r rune) string {
	if r < utf8.RuneSelf {
		if strings.ContainsRune(`\]-[^&`, r) {
			return `\` + string(r)
		}
		if r < 0x20 || r == 0x7f {
			return fmt.Sprintf(`\x{%x}`, r)
		}
		return string(r)
	}
	if !unicode.IsPrint(r) {
		return fmt.Sprintf(`\x{%x}`, r)
	}
	return string(r)
}

// property parses {Name}, {^Name} after \p / \P at tr.i.
func (tr *translator) property(neg bool) (frag, single string, negated bool, err error) {
	src := tr.src
	if tr.i >= len(src) || src[tr.i] != '{' {
		return "", "", false, tr.errorf("invalid character property name")
	}
	end := strings.IndexByte(src[tr.i:], '}')
	if end < 0 {
		return "", "", false, tr.errorf("invalid character property name")
	}
	name := src[tr.i+1 : tr.i+end]
	tr.i += end + 1
	if strings.HasPrefix(name, "^") {
		neg = !neg
		name = name[1:]
	}
	frag, single, ok := propertyFragment(name)
	if !ok {
		return "", "", false, tr.errorf("invalid character property name {%s}", name)
	}
	return frag, single, neg, nil
}

var posixFragments = map[string]string{
	"alpha":  `\p{L}\p{Nl}`,
	"digit":  `\p{Nd}`,
	"alnum":  `\p{L}\p{Nl}\p{Nd}`,
	"upper":  `\p{Lu}`,
	"lower":  `\p{Ll}`,
	"space":  `\t\n\v\f\r \x{85}\p{Z}`,
	"blank":  `\t\p{Zs}`,
	"cntrl":  `\p{Cc}`,
	"punct":  `\p{P}$+<=>\^` + "`" + `|~`,
	"graph":  `\p{L}\p{M}\p{N}\p{P}\p{S}\p{Cf}\p{Co}`,
	"print":  `\p{L}\p{M}\p{N}\p{P}\p{S}\p{Cf}\p{Co}\p{Zs}`,
	"xdigit": `0-9a-fA-F`,
	"word":   `\p{L}\p{M}\p{Nl}\p{Nd}\p{Pc}\x{200c}\x{200d}`,
	"ascii":  `\x00-\x7f`,
	"any":    `\x00-\x{10ffff}`,
}

var categoryLongNames = map[string]string{
	"letter": "L", "uppercaseletter": "Lu", "lowercaseletter": "Ll", "titlecaseletter": "Lt",
	"modifierletter": "Lm", "otherletter": "Lo", "mark": "M", "nonspacingmark": "Mn",
	"spacingmark": "Mc", "enclosingmark": "Me", "number": "N", "decimalnumber": "Nd",
	"letternumber": "Nl", "othernumber": "No", "punctuation": "P", "connectorpunctuation": "Pc",
	"dashpunctuation": "Pd", "openpunctuation": "Ps", "closepunctuation": "Pe",
	"initialpunctuation": "Pi", "finalpunctuation": "Pf", "otherpunctuation": "Po",
	"symbol": "S", "mathsymbol": "Sm", "currencysymbol": "Sc", "modifiersymbol": "Sk",
	"othersymbol": "So", "separator": "Z", "spaceseparator": "Zs", "lineseparator": "Zl",
	"paragraphseparator": "Zp", "other": "C", "control": "Cc", "format": "Cf",
	"privateuse": "Co", "surrogate": "Cs", "unassigned": "Cn",
}

func normalizeProp(s string) string {
	s = strings.ToLower(s)
	return strings.NewReplacer(" ", "", "_", "", "-", "").Replace(s)
}

// propertyFragment maps a Ruby character property or POSIX class name to
// a class fragment. single is set when the fragment is one \p{X}.
func propertyFragment(name string) (frag, single string, ok bool) {
	n := normalizeProp(name)
	if f, ok := posixFragments[n]; ok {
		return f, "", true
	}
	if c, ok := categoryLongNames[n]; ok {
		return `\p{` + c + `}`, c, true
	}
	for k := range unicode.Categories {
		if normalizeProp(k) == n {
			return `\p{` + k + `}`, k, true
		}
	}
	for k := range unicode.Scripts {
		if normalizeProp(k) == n {
			return `\p{` + k + `}`, k, true
		}
	}
	return "", "", false
}

// -- character classes ----------------------------------------------------

type cset struct {
	neg   bool
	items []citem
	and   []*cset // intersection operands (the first operand is the set itself without and)
}

type citem struct {
	frag    string // fragment usable inside [...] in the target
	sub     *cset  // nested/negated class
	negFrag string // exact in-class form of a negated item, if the target has one
}

type posixRangeError struct{ error }

func validateLiteralRange(source string, extended bool) error {
	options := 0
	if extended {
		options = Extended
	}
	_, err := translate(source, options, targetFull)
	if _, ok := err.(*posixRangeError); ok {
		return err
	}
	return nil
}

func (tr *translator) class() (string, error) {
	cs, err := tr.parseClass()
	if err != nil {
		return "", err
	}
	return tr.emitClass(cs)
}

func (tr *translator) parseClass() (*cset, error) {
	src := tr.src
	tr.i++ // '['
	cs := &cset{}
	if tr.i < len(src) && src[tr.i] == '^' {
		cs.neg = true
		tr.i++
	}
	cur := &cset{} // operand collecting items
	var operands []*cset
	first := true
	for {
		if tr.i >= len(src) {
			return nil, tr.errorf("premature end of char-class")
		}
		c := src[tr.i]
		if c == ']' && !first {
			tr.i++
			break
		}
		first = false
		switch {
		case c == '[' && strings.HasPrefix(src[tr.i:], "[:"):
			end := strings.Index(src[tr.i:], ":]")
			if end < 0 {
				// not a POSIX bracket: nested class
				sub, err := tr.parseClass()
				if err != nil {
					return nil, err
				}
				cur.items = append(cur.items, citem{sub: sub})
				continue
			}
			name := src[tr.i+2 : tr.i+end]
			tr.i += end + 2
			neg := strings.HasPrefix(name, "^")
			if neg {
				name = name[1:]
			}
			frag, ok := posixFragments[normalizeProp(name)]
			if !ok {
				return nil, tr.errorf("invalid POSIX bracket type [:%s:]", name)
			}
			if tr.i+1 < len(src) && src[tr.i] == '-' && src[tr.i+1] != ']' &&
				!strings.HasPrefix(src[tr.i+1:], "&&") {
				return nil, &posixRangeError{tr.errorf("unmatched range specifier in char-class")}
			}
			if neg {
				cur.items = append(cur.items, citem{sub: &cset{neg: true, items: []citem{{frag: frag}}}})
			} else {
				cur.items = append(cur.items, citem{frag: frag})
			}
		case c == '[':
			sub, err := tr.parseClass()
			if err != nil {
				return nil, err
			}
			cur.items = append(cur.items, citem{sub: sub})
		case c == '&' && strings.HasPrefix(src[tr.i:], "&&"):
			tr.i += 2
			operands = append(operands, cur)
			cur = &cset{}
		case c == '\\' && tr.i+1 < len(src) && strings.IndexByte("dDwWsShHpP", src[tr.i+1]) >= 0:
			it, err := tr.classShorthand()
			if err != nil {
				return nil, err
			}
			cur.items = append(cur.items, it)
		default:
			lo, err := tr.classChar()
			if err != nil {
				return nil, err
			}
			if tr.i+1 < len(src) && src[tr.i] == '-' && src[tr.i+1] != ']' {
				save := tr.i
				tr.i++
				if strings.HasPrefix(src[tr.i:], "[:") && strings.Contains(src[tr.i+2:], ":]") {
					return nil, &posixRangeError{tr.errorf("char-class value at end of range")}
				}
				if src[tr.i] == '[' || (src[tr.i] == '\\' && tr.i+1 < len(src) && strings.IndexByte("dDwWsShHpP", src[tr.i+1]) >= 0) {
					tr.i = save
					cur.items = append(cur.items, citem{frag: classLiteral(lo)})
					continue
				}
				hi, err := tr.classChar()
				if err != nil {
					return nil, err
				}
				if hi < lo {
					return nil, tr.errorf("empty range in char class")
				}
				cur.items = append(cur.items, citem{frag: classLiteral(lo) + "-" + classLiteral(hi)})
				continue
			}
			cur.items = append(cur.items, citem{frag: classLiteral(lo)})
		}
	}
	if len(operands) == 0 {
		cs.items = cur.items
		return cs, nil
	}
	cs.and = append(operands, cur)
	for _, op := range cs.and {
		if len(op.items) == 0 {
			return nil, tr.errorf("empty char-class")
		}
	}
	return cs, nil
}

func (tr *translator) classChar() (rune, error) {
	src := tr.src
	if src[tr.i] == '\\' {
		if tr.i+1 >= len(src) {
			return 0, tr.errorf("too short escape sequence")
		}
		if src[tr.i+1] == 'b' {
			tr.i += 2
			return '\b', nil
		}
		return tr.escapedChar()
	}
	r, w := utf8.DecodeRuneInString(src[tr.i:])
	tr.i += w
	return r, nil
}

func (tr *translator) classShorthand() (citem, error) {
	c := tr.src[tr.i+1]
	tr.i += 2
	re2 := tr.t == targetRE2
	neg := func(frag, re2Form string) citem {
		it := citem{sub: &cset{neg: true, items: []citem{{frag: frag}}}}
		if re2 {
			it.negFrag = re2Form
		}
		return it
	}
	switch c {
	case 'd':
		return citem{frag: "0-9"}, nil
	case 'D':
		return neg("0-9", `\D`), nil
	case 'w':
		return citem{frag: "a-zA-Z0-9_"}, nil
	case 'W':
		return neg("a-zA-Z0-9_", `\W`), nil
	case 's':
		return citem{frag: `\t\n\v\f\r `}, nil
	case 'S':
		return neg(`\t\n\v\f\r `, `\S`), nil
	case 'h':
		return citem{frag: "0-9a-fA-F"}, nil
	case 'H':
		return neg("0-9a-fA-F", ""), nil
	}
	frag, single, negated, err := tr.property(c == 'P')
	if err != nil {
		return citem{}, err
	}
	if !negated {
		return citem{frag: frag}, nil
	}
	it := citem{sub: &cset{neg: true, items: []citem{{frag: frag}}}}
	if single != "" {
		it.negFrag = `\P{` + single + `}`
	}
	return it, nil
}

// flatContent returns the class body when cs can be written as one [...].
func flatContent(cs *cset) (string, bool) {
	if cs.and != nil {
		return "", false
	}
	var b strings.Builder
	for _, it := range cs.items {
		switch {
		case it.sub == nil:
			b.WriteString(it.frag)
		case it.negFrag != "":
			b.WriteString(it.negFrag)
		case !it.sub.neg:
			inner, ok := flatContent(it.sub)
			if !ok {
				return "", false
			}
			b.WriteString(inner)
		default:
			return "", false
		}
	}
	return b.String(), true
}

func (tr *translator) emitClass(cs *cset) (string, error) {
	// [[:^alpha:]], [\H], [^\P{L}]: a single negated item flips the class
	if cs.and == nil && len(cs.items) == 1 && cs.items[0].sub != nil && cs.items[0].sub.neg {
		if inner, ok := flatContent(&cset{items: cs.items[0].sub.items}); ok && inner != "" {
			if cs.neg {
				return "[" + inner + "]", nil
			}
			return "[^" + inner + "]", nil
		}
	}
	if body, ok := flatContent(cs); ok {
		if body == "" {
			return "", tr.errorf("empty char-class")
		}
		if cs.neg {
			return "[^" + body + "]", nil
		}
		return "[" + body + "]", nil
	}
	if err := tr.full("character class intersection or negated nested class"); err != nil {
		return "", err
	}
	expr, err := tr.classExpr(cs)
	if err != nil {
		return "", err
	}
	return expr, nil
}

// classExpr writes a class as a regexp2 expression matching one character:
// union -> alternation, negation -> (?!...)(?s:.), intersection -> lookaheads.
func (tr *translator) classExpr(cs *cset) (string, error) {
	var body string
	if cs.and != nil {
		parts := make([]string, 0, len(cs.and))
		for _, op := range cs.and {
			e, err := tr.classExpr(op)
			if err != nil {
				return "", err
			}
			parts = append(parts, e)
		}
		var b strings.Builder
		for _, p := range parts[:len(parts)-1] {
			b.WriteString("(?=" + p + ")")
		}
		b.WriteString(parts[len(parts)-1])
		body = "(?:" + b.String() + ")"
	} else {
		var flat strings.Builder
		var alts []string
		for _, it := range cs.items {
			switch {
			case it.sub == nil:
				flat.WriteString(it.frag)
			case it.negFrag != "":
				flat.WriteString(it.negFrag)
			default:
				if inner, ok := flatContent(it.sub); ok && !it.sub.neg {
					flat.WriteString(inner)
					continue
				}
				e, err := tr.classExpr(it.sub)
				if err != nil {
					return "", err
				}
				alts = append(alts, e)
			}
		}
		if flat.Len() > 0 {
			alts = append([]string{"[" + flat.String() + "]"}, alts...)
		}
		if len(alts) == 0 {
			return "", tr.errorf("empty char-class")
		}
		body = "(?:" + strings.Join(alts, "|") + ")"
	}
	if cs.neg {
		return "(?:(?!" + body + ")(?s:.))", nil
	}
	return body, nil
}
