package regexp

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// Subexpression calls (\g<name>, \g<N>, \g<-N>) have no RE2 or regexp2
// equivalent. A call that is not recursive matches exactly what the called
// group's body matches, so it is expanded inline as a non-capturing copy of
// the body. Recursive calls are rejected. (Onigmo also updates the called
// group's capture on each call; the inlined copy does not capture.)

type groupSpan struct {
	name       string
	num        int
	bodyStart  int // index after the group header
	bodyEnd    int // index of the closing ')'
	open       int // index of '('
	groupsSeen int // capture groups opened before this one
}

type callRef struct {
	start, end int // span of \g<...> in the source
	ref        string
	groupsSeen int // capture groups opened before the call
}

// scanGroups finds capture groups and \g calls in src, honouring escapes,
// character classes and /x comments.
func scanGroups(src string, extended bool) ([]groupSpan, []callRef, error) {
	named := hasNamedGroups(src)
	var groups []groupSpan
	var calls []callRef
	type frame struct {
		span    *groupSpan
		capture bool
	}
	var stack []frame
	ncap := 0
	classDepth := 0
	for i := 0; i < len(src); i++ {
		c := src[i]
		if classDepth > 0 {
			switch c {
			case '\\':
				i++
			case '[':
				classDepth++
			case ']':
				classDepth--
			}
			continue
		}
		if extended && c == '#' {
			for i < len(src) && src[i] != '\n' {
				i++
			}
			continue
		}
		switch c {
		case '\\':
			if i+2 < len(src) && src[i+1] == 'g' && (src[i+2] == '<' || src[i+2] == '\'') {
				closer := byte('>')
				if src[i+2] == '\'' {
					closer = '\''
				}
				end := strings.IndexByte(src[i+3:], closer)
				if end < 0 {
					return nil, nil, fmt.Errorf("invalid subexp call")
				}
				calls = append(calls, callRef{start: i, end: i + 3 + end + 1, ref: src[i+3 : i+3+end], groupsSeen: ncap})
				i += 3 + end
				continue
			}
			i++
		case '[':
			classDepth = 1
			if i+1 < len(src) && src[i+1] == '^' {
				i++
			}
			if i+1 < len(src) && src[i+1] == ']' {
				i++
			}
		case '(':
			hdr, name, capture := groupHeader(src[i:], named)
			f := frame{capture: capture}
			if capture {
				ncap++
				f.span = &groupSpan{name: name, num: ncap, open: i, bodyStart: i + hdr, groupsSeen: ncap - 1}
			}
			stack = append(stack, f)
			i += hdr - 1
		case ')':
			if len(stack) == 0 {
				continue
			}
			f := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if f.span != nil {
				f.span.bodyEnd = i
				groups = append(groups, *f.span)
			}
		}
	}
	return groups, calls, nil
}

// groupHeader returns the header length of the group starting at s[0]=='(',
// its name, and whether it captures.
func groupHeader(s string, named bool) (int, string, bool) {
	if !strings.HasPrefix(s, "(?") {
		return 1, "", !named
	}
	switch {
	case strings.HasPrefix(s, "(?<="), strings.HasPrefix(s, "(?<!"):
		return 4, "", false
	case strings.HasPrefix(s, "(?P<"):
		if end := strings.IndexByte(s[4:], '>'); end > 0 {
			return 4 + end + 1, s[4 : 4+end], true
		}
	case strings.HasPrefix(s, "(?<"):
		if end := strings.IndexByte(s[3:], '>'); end > 0 {
			return 3 + end + 1, s[3 : 3+end], true
		}
	case strings.HasPrefix(s, "(?'"):
		if end := strings.IndexByte(s[3:], '\''); end > 0 {
			return 3 + end + 1, s[3 : 3+end], true
		}
	}
	return 2, "", false
}

func expandCalls(src string, extended bool) (string, error) {
	groups, calls, err := scanGroups(src, extended)
	if err != nil {
		return "", &RegexpError{err.Error() + ": /" + src + "/"}
	}
	if len(calls) == 0 {
		return src, nil
	}
	byName := map[string]groupSpan{}
	byNum := map[int]groupSpan{}
	for _, g := range groups {
		if g.name != "" {
			byName[g.name] = g
		}
		byNum[g.num] = g
	}
	resolve := func(c callRef) (groupSpan, error) {
		if n, err := strconv.Atoi(c.ref); err == nil {
			if strings.HasPrefix(c.ref, "-") || strings.HasPrefix(c.ref, "+") {
				n = c.groupsSeen + 1 + n
				if strings.HasPrefix(c.ref, "+") {
					n = c.groupsSeen + n - 1
				}
			}
			if n == 0 {
				return groupSpan{}, recursiveCall("0", src)
			}
			if g, ok := byNum[n]; ok {
				return g, nil
			}
			return groupSpan{}, fmt.Errorf("undefined group <%s> reference", c.ref)
		}
		if g, ok := byName[c.ref]; ok {
			return g, nil
		}
		return groupSpan{}, fmt.Errorf("undefined name <%s> reference", c.ref)
	}

	var inline func(g groupSpan, active map[int]bool) (string, error)
	inline = func(g groupSpan, active map[int]bool) (string, error) {
		if active[g.open] {
			return "", recursiveCall(nameOrNum(g), src)
		}
		active[g.open] = true
		defer delete(active, g.open)
		var b strings.Builder
		last := g.bodyStart
		for _, c := range calls {
			if c.start < g.bodyStart || c.end > g.bodyEnd {
				continue
			}
			target, err := resolve(c)
			if err != nil {
				return "", err
			}
			body, err := inline(target, active)
			if err != nil {
				return "", err
			}
			b.WriteString(uncapture(src[last:c.start]))
			b.WriteString("(?:" + body + ")")
			last = c.end
		}
		b.WriteString(uncapture(src[last:g.bodyEnd]))
		return b.String(), nil
	}

	var out strings.Builder
	last := 0
	for _, c := range calls {
		target, err := resolve(c)
		if err != nil {
			if NeedsNativeSyntax(err) {
				return "", err
			}
			return "", &RegexpError{err.Error() + ": /" + src + "/"}
		}
		if c.start > target.bodyStart && c.end <= target.bodyEnd {
			return "", recursiveCall(c.ref, src)
		}
		body, err := inline(target, map[int]bool{})
		if err != nil {
			if NeedsNativeSyntax(err) {
				return "", err
			}
			return "", &RegexpError{err.Error() + ": /" + src + "/"}
		}
		out.WriteString(src[last:c.start])
		out.WriteString("(?:" + body + ")")
		last = c.end
	}
	out.WriteString(src[last:])
	return out.String(), nil
}

func recursiveCall(ref, source string) error {
	return &nativeSyntaxError{&RegexpError{fmt.Sprintf("recursive subexpression call \\g<%s> requires the native fallback: /%s/", ref, source)}}
}

func nameOrNum(g groupSpan) string {
	if g.name != "" {
		return g.name
	}
	return strconv.Itoa(g.num)
}

// uncapture turns capture groups in s into non-capturing groups
func uncapture(s string) string {
	var b strings.Builder
	classDepth := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if classDepth > 0 {
			switch c {
			case '\\':
				b.WriteByte(c)
				i++
				if i < len(s) {
					b.WriteByte(s[i])
				}
				continue
			case '[':
				classDepth++
			case ']':
				classDepth--
			}
			b.WriteByte(c)
			continue
		}
		switch c {
		case '\\':
			b.WriteByte(c)
			i++
			if i < len(s) {
				b.WriteByte(s[i])
			}
			continue
		case '[':
			classDepth = 1
		case '(':
			hdr, _, capture := groupHeader(s[i:], false)
			if capture || (hdr > 2 && !strings.HasPrefix(s[i:], "(?<=") && !strings.HasPrefix(s[i:], "(?<!")) {
				b.WriteString("(?:")
				i += hdr - 1
				continue
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

// Group names may contain characters (such as '-') that Go and regexp2
// reject. Such names are encoded reversibly for the engines and decoded
// for Ruby (Regexp#names, MatchData).
const encodedNamePrefix = "oRb_"

func encodeGroupName(name string) string {
	for _, r := range name {
		if !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r > 0x7f) {
			return encodedNamePrefix + hex.EncodeToString([]byte(name))
		}
	}
	return name
}

func decodeGroupName(name string) string {
	if !strings.HasPrefix(name, encodedNamePrefix) {
		return name
	}
	b, err := hex.DecodeString(name[len(encodedNamePrefix):])
	if err != nil {
		return name
	}
	return string(b)
}
