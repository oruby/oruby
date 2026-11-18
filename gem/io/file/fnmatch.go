package file

import (
	"unicode"
	"unicode/utf8"
)

// File.fnmatch and the per-component matching of Dir.glob, ported from MRI's
// dir.c (bracket, fnmatch_helper, fnmatch, ruby_brace_expand). Strings are
// UTF-8 byte strings; characters are runes, an invalid byte is one character.

// charLen is the length of the character at s[i]
func charLen(s string, i int) int {
	if i >= len(s) {
		return 0
	}
	_, n := utf8.DecodeRuneInString(s[i:])
	return n
}

func runeAt(s string, i int) rune {
	r, _ := utf8.DecodeRuneInString(s[i:])
	return r
}

// bracket matches the character at s[si] against the bracket expression
// starting at p[pi] (after '['); it returns the index after ']' or -1
func bracket(p string, pi int, s string, si int, flags int) int {
	nocase := flags&fnmCasefold != 0
	escape := flags&fnmNoescape == 0
	ok, not := false, false

	if pi >= len(p) {
		return -1
	}
	if p[pi] == '!' || p[pi] == '^' {
		not = true
		pi++
	}
	sc := runeAt(s, si)
	if nocase {
		sc = unicode.ToUpper(sc)
	}
	for {
		if pi >= len(p) {
			return -1
		}
		if p[pi] == ']' {
			break
		}
		t1 := pi
		if escape && p[t1] == '\\' {
			t1++
		}
		if t1 >= len(p) {
			return -1
		}
		r1 := charLen(p, t1)
		pi = t1 + r1
		if pi >= len(p) {
			return -1
		}
		c1 := runeAt(p, t1)
		if nocase {
			c1 = unicode.ToUpper(c1)
		}
		if p[pi] == '-' && pi+1 < len(p) && p[pi+1] != ']' {
			t2 := pi + 1
			if escape && p[t2] == '\\' {
				t2++
			}
			if t2 >= len(p) {
				return -1
			}
			pi = t2 + charLen(p, t2)
			if ok {
				continue
			}
			c2 := runeAt(p, t2)
			if nocase {
				c2 = unicode.ToUpper(c2)
			}
			if sc >= c1 && sc <= c2 {
				ok = true
			}
			continue
		}
		if ok {
			continue
		}
		if sc == c1 {
			ok = true
		}
	}
	if ok == not {
		return -1
	}
	return pi + 1
}

// fnmatchHelper matches one component (up to '/' with FNM_PATHNAME) of s
// from si against p from pi: ok, and where each stopped
func fnmatchHelper(p string, pi int, s string, si int, flags int) (bool, int, int) {
	period := flags&fnmDotmatch == 0
	pathname := flags&fnmPathname != 0
	escape := flags&fnmNoescape == 0
	nocase := flags&fnmCasefold != 0

	isEnd := func(str string, i int) bool { return i >= len(str) || (pathname && str[i] == '/') }
	unescape := func(i int) int {
		if escape && i < len(p) && p[i] == '\\' {
			return i + 1
		}
		return i
	}

	ptmp, stmp := -1, -1
	if period && si < len(s) && s[si] == '.' {
		if u := unescape(pi); u >= len(p) || p[u] != '.' {
			return false, pi, si // a leading period is matched only by one
		}
	}
	for {
		if pi < len(p) {
			switch p[pi] {
			case '*':
				for pi < len(p) && p[pi] == '*' {
					pi++
				}
				if isEnd(p, unescape(pi)) {
					return true, unescape(pi), si
				}
				if isEnd(s, si) {
					return false, pi, si
				}
				ptmp, stmp = pi, si
				continue
			case '?':
				if isEnd(s, si) {
					return false, pi, si
				}
				pi++
				si += charLen(s, si)
				continue
			case '[':
				if isEnd(s, si) {
					return false, pi, si
				}
				if t := bracket(p, pi+1, s, si, flags); t >= 0 {
					pi = t
					si += charLen(s, si)
					continue
				}
				goto failed
			}
		}
		// ordinary
		pi = unescape(pi)
		if isEnd(s, si) {
			return isEnd(p, pi), pi, si
		}
		if isEnd(p, pi) {
			goto failed
		}
		{
			r := charLen(p, pi)
			if r <= len(s)-si && p[pi:pi+r] == s[si:si+r] {
				pi += r
				si += r
				continue
			}
			if !nocase || unicode.ToUpper(runeAt(p, pi)) != unicode.ToUpper(runeAt(s, si)) {
				goto failed
			}
			pi += r
			si += charLen(s, si)
			continue
		}
	failed: // try the next position of the last '*'
		if ptmp >= 0 && stmp >= 0 {
			pi = ptmp
			stmp += charLen(s, stmp)
			si = stmp
			continue
		}
		return false, pi, si
	}
}

// fnmatchNoBrace is MRI's fnmatch: with FNM_PATHNAME a component at a time,
// "**/" matching any number of directories
func fnmatchNoBrace(p, s string, flags int) bool {
	if flags&fnmPathname == 0 {
		ok, _, _ := fnmatchHelper(p, 0, s, 0, flags)
		return ok
	}
	period := flags&fnmDotmatch == 0
	pi, si := 0, 0
	ptmp, stmp := -1, -1
	for {
		if pi+2 < len(p) && p[pi] == '*' && p[pi+1] == '*' && p[pi+2] == '/' {
			for pi+2 < len(p) && p[pi] == '*' && p[pi+1] == '*' && p[pi+2] == '/' {
				pi += 3
			}
			ptmp, stmp = pi, si
		}
		ok, np, ns := fnmatchHelper(p, pi, s, si, flags)
		if ok {
			pi, si = np, ns
			for si < len(s) && s[si] != '/' {
				si += charLen(s, si)
			}
			if pi < len(p) && si < len(s) {
				pi++
				si++
				continue
			}
			if pi >= len(p) && si >= len(s) {
				return true
			}
		}
		// failed: try the next directory for the last "**/"
		if ptmp >= 0 && stmp >= 0 && !(period && stmp < len(s) && s[stmp] == '.') {
			for stmp < len(s) && s[stmp] != '/' {
				stmp += charLen(s, stmp)
			}
			if stmp < len(s) {
				pi = ptmp
				stmp++
				si = stmp
				continue
			}
		}
		return false
	}
}

// braceExpand is MRI's ruby_brace_expand: the left-most outer {a,b} is
// expanded first, each alternative recursively; fn gets each pattern and
// returns false to stop. An unbalanced '{' yields nothing.
func braceExpand(str string, flags int, fn func(string) bool) bool {
	escape := flags&fnmNoescape == 0
	lbrace, rbrace := -1, -1
	nest := 0
	for p := 0; p < len(str); {
		if str[p] == '{' {
			if nest == 0 {
				lbrace = p
			}
			nest++
		}
		if str[p] == '}' && lbrace >= 0 {
			nest--
			if nest == 0 {
				rbrace = p
				break
			}
		}
		if str[p] == '\\' && escape {
			p++
			if p >= len(str) {
				break
			}
		}
		p += charLen(str, p)
	}
	if lbrace >= 0 && rbrace >= 0 {
		prefix, rest := str[:lbrace], str[rbrace+1:]
		p := lbrace
		for p < rbrace {
			p++
			t := p
			nest = 0
			for p < rbrace && !(str[p] == ',' && nest == 0) {
				if str[p] == '{' {
					nest++
				}
				if str[p] == '}' {
					nest--
				}
				if str[p] == '\\' && escape {
					p++
					if p == rbrace {
						break
					}
				}
				p += charLen(str, p)
			}
			if !braceExpand(prefix+str[t:p]+rest, flags, fn) {
				return false
			}
		}
		return true
	}
	if lbrace < 0 && rbrace < 0 {
		return fn(str)
	}
	return true
}

// fnmatch is File.fnmatch(pattern, name, flags): with FNM_EXTGLOB any of
// the brace expansions
func fnmatch(name, pattern string, flags int) (bool, error) {
	if flags&fnmExtglob == 0 {
		return fnmatchNoBrace(pattern, name, flags), nil
	}
	found := false
	braceExpand(pattern, flags, func(p string) bool {
		if fnmatchNoBrace(p, name, flags) {
			found = true
			return false
		}
		return true
	})
	return found, nil
}
