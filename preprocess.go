package oruby

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"sync/atomic"
	"unicode"
)

var ffCounter uint64

// PreprocessRuby desugars Ruby 3+ syntax constructs unsupported by mruby's code generator:
// - dynamic alias/undef (alias :"#{...}" ...)
// - **nil parameter declarations
// - flip-flop range conditions in if/unless
// - safe navigation target setters in rescue => a&.b and for a&.b in c
// - BEGIN { ... } pre-execution blocks
func PreprocessRuby(source string) string {
	if !strings.Contains(source, "alias") &&
		!strings.Contains(source, "undef") &&
		!strings.Contains(source, "**nil") &&
		!strings.Contains(source, "..") &&
		!strings.Contains(source, "&.") &&
		!strings.Contains(source, "BEGIN") {
		return source
	}

	b := []byte(source)
	b = preprocessDynamicAliasAndUndef(b)
	b = preprocessNoKeywords(b)
	b = preprocessSafeNavigationTargets(b)
	b = preprocessFlipFlop(b)
	b = preprocessBeginBlocks(b)
	return string(b)
}

// PreprocessRubyBytes operates on byte slices.
func PreprocessRubyBytes(source []byte) []byte {
	if !bytes.Contains(source, []byte("alias")) &&
		!bytes.Contains(source, []byte("undef")) &&
		!bytes.Contains(source, []byte("**nil")) &&
		!bytes.Contains(source, []byte("..")) &&
		!bytes.Contains(source, []byte("&.")) &&
		!bytes.Contains(source, []byte("BEGIN")) {
		return source
	}

	b := preprocessDynamicAliasAndUndef(source)
	b = preprocessNoKeywords(b)
	b = preprocessSafeNavigationTargets(b)
	b = preprocessFlipFlop(b)
	b = preprocessBeginBlocks(b)
	return b
}

var (
	rescueSafeNavRegex = regexp.MustCompile(`(?m)\brescue\s+=>\s+([a-zA-Z_]\w*)&\.([a-zA-Z_]\w*)`)
	forSafeNavRegex    = regexp.MustCompile(`(?m)^(\s*)for\s+([a-zA-Z_]\w*)&\.([a-zA-Z_]\w*)\s+in\s+([^\n;]+)`)
)

func preprocessSafeNavigationTargets(src []byte) []byte {
	if !bytes.Contains(src, []byte("&.")) {
		return src
	}
	res := rescueSafeNavRegex.ReplaceAll(src, []byte(`rescue => __rescued_exc__; $1&.$2 = __rescued_exc__`))
	res = forSafeNavRegex.ReplaceAll(res, []byte(`${1}for __for_elem__ in $4; $2&.$3 = __for_elem__;`))
	return res
}

// Flip-flop regex matches conditions like `if (cond1)...(cond2)`, `while (cond1)..(cond2)`, or `(cond1..cond2 ? ... : ...)`
var (
	flipFlop3Regex        = regexp.MustCompile(`(?m)\b(if|unless|while|until|and|or)\s+(\((?:[^()]|\([^()]*\))*\)|[a-zA-Z0-9_]+)\s*\.{3}\s*(\((?:[^()]|\([^()]*\))*\)|[a-zA-Z0-9_]+)`)
	flipFlop2Regex        = regexp.MustCompile(`(?m)\b(if|unless|while|until|and|or)\s+(\((?:[^()]|\([^()]*\))*\)|[a-zA-Z0-9_]+)\s*\.{2}\s*(\((?:[^()]|\([^()]*\))*\)|[a-zA-Z0-9_]+)`)
	flipFlop3TernaryRegex = regexp.MustCompile(`(?m)([\(\s]|^)([a-zA-Z0-9_]+|\((?:[^()]|\([^()]*\))*\))\s*\.{3}\s*([a-zA-Z0-9_]+|\((?:[^()]|\([^()]*\))*\))\s*\?`)
	flipFlop2TernaryRegex = regexp.MustCompile(`(?m)([\(\s]|^)([a-zA-Z0-9_]+|\((?:[^()]|\([^()]*\))*\))\s*\.{2}\s*([a-zA-Z0-9_]+|\((?:[^()]|\([^()]*\))*\))\s*\?`)
)

func preprocessFlipFlop(src []byte) []byte {
	if !bytes.Contains(src, []byte("..")) {
		return src
	}
	res := flipFlop3Regex.ReplaceAllFunc(src, func(m []byte) []byte {
		sub := flipFlop3Regex.FindSubmatch(m)
		id := atomic.AddUint64(&ffCounter, 1)
		kw, left, right := sub[1], sub[2], sub[3]
		return []byte(fmt.Sprintf("%s __oruby_ff3__(%d, %s, %s)", kw, id, left, right))
	})
	res = flipFlop2Regex.ReplaceAllFunc(res, func(m []byte) []byte {
		sub := flipFlop2Regex.FindSubmatch(m)
		id := atomic.AddUint64(&ffCounter, 1)
		kw, left, right := sub[1], sub[2], sub[3]
		return []byte(fmt.Sprintf("%s __oruby_ff2__(%d, %s, %s)", kw, id, left, right))
	})
	res = flipFlop3TernaryRegex.ReplaceAllFunc(res, func(m []byte) []byte {
		sub := flipFlop3TernaryRegex.FindSubmatch(m)
		id := atomic.AddUint64(&ffCounter, 1)
		prefix, left, right := sub[1], sub[2], sub[3]
		return []byte(fmt.Sprintf("%s__oruby_ff3__(%d, %s, %s) ?", prefix, id, left, right))
	})
	res = flipFlop2TernaryRegex.ReplaceAllFunc(res, func(m []byte) []byte {
		sub := flipFlop2TernaryRegex.FindSubmatch(m)
		id := atomic.AddUint64(&ffCounter, 1)
		prefix, left, right := sub[1], sub[2], sub[3]
		return []byte(fmt.Sprintf("%s__oruby_ff2__(%d, %s, %s) ?", prefix, id, left, right))
	})
	return res
}

func preprocessBeginBlocks(src []byte) []byte {
	if !bytes.Contains(src, []byte("BEGIN")) {
		return src
	}
	var out bytes.Buffer
	out.Grow(len(src))
	i := 0
	n := len(src)

	for i < n {
		// Skip comments and strings
		if src[i] == '#' {
			start := i
			for i < n && src[i] != '\n' {
				i++
			}
			out.Write(src[start:i])
			continue
		}
		if (src[i] == '\'' || src[i] == '"') && (i == 0 || (!isIdentChar(src[i-1]) && src[i-1] != '$')) {
			q := src[i]
			start := i
			i++
			for i < n {
				if src[i] == '\\' && i+1 < n {
					i += 2
					continue
				}
				if src[i] == q {
					i++
					break
				}
				i++
			}
			out.Write(src[start:i])
			continue
		}

		if matchKeyword(src, i, "BEGIN") {
			j := i + 5
			for j < n && isSpace(src[j]) {
				j++
			}
			if j < n && src[j] == '{' {
				// Find matching }
				braceStart := j
				k := j + 1
				braceDepth := 1
				for k < n && braceDepth > 0 {
					if src[k] == '\\' && k+1 < n {
						k += 2
						continue
					}
					if (src[k] == '\'' || src[k] == '"') && (k == 0 || src[k-1] != '$') {
						q := src[k]
						k++
						for k < n {
							if src[k] == '\\' && k+1 < n {
								k += 2
								continue
							}
							if src[k] == q {
								k++
								break
							}
							k++
						}
						continue
					}
					if src[k] == '{' {
						braceDepth++
					} else if src[k] == '}' {
						braceDepth--
					}
					k++
				}
				if braceDepth == 0 {
					// Replace `BEGIN {` with `begin;` and `}` with `end`
					body := src[braceStart+1 : k-1]
					out.WriteString("begin;")
					out.Write(body)
					out.WriteString("end")
					i = k
					continue
				}
			}
		}

		out.WriteByte(src[i])
		i++
	}
	return out.Bytes()
}

func preprocessNoKeywords(src []byte) []byte {
	target := []byte("**nil")
	if !bytes.Contains(src, target) {
		return src
	}

	var out bytes.Buffer
	out.Grow(len(src) + 32)
	i := 0
	n := len(src)

	for i < n {
		if src[i] == '#' {
			start := i
			for i < n && src[i] != '\n' {
				i++
			}
			out.Write(src[start:i])
			continue
		}
		if (src[i] == '\'' || src[i] == '"') && (i == 0 || (!isIdentChar(src[i-1]) && src[i-1] != '$')) {
			q := src[i]
			start := i
			i++
			for i < n {
				if src[i] == '\\' && i+1 < n {
					i += 2
					continue
				}
				if src[i] == q {
					i++
					break
				}
				i++
			}
			out.Write(src[start:i])
			continue
		}

		if i+5 <= n && bytes.Equal(src[i:i+5], target) {
			boundary := true
			if i+5 < n {
				c := src[i+5]
				if isIdentChar(c) {
					boundary = false
				}
			}
			if boundary {
				out.WriteString("**__no_kw_disallow__")
				i += 5
				continue
			}
		}

		out.WriteByte(src[i])
		i++
	}

	return out.Bytes()
}

func preprocessDynamicAliasAndUndef(src []byte) []byte {
	var out bytes.Buffer
	out.Grow(len(src) + 64)
	i := 0
	n := len(src)

	for i < n {
		if src[i] == '#' {
			start := i
			for i < n && src[i] != '\n' {
				i++
			}
			out.Write(src[start:i])
			continue
		}
		if (src[i] == '\'' || src[i] == '"') && (i == 0 || (!isIdentChar(src[i-1]) && src[i-1] != '$')) {
			q := src[i]
			start := i
			i++
			for i < n {
				if src[i] == '\\' && i+1 < n {
					i += 2
					continue
				}
				if src[i] == q {
					i++
					break
				}
				i++
			}
			out.Write(src[start:i])
			continue
		}

		if matchKeyword(src, i, "alias") {
			kStart := i
			i += 5
			if i < n && isSpace(src[i]) {
				for i < n && isSpace(src[i]) {
					i++
				}
				arg1Start := i
				arg1End, hasInterpolation1 := readRubySymbolOrIdent(src, arg1Start)
				if arg1End > arg1Start {
					i = arg1End
					for i < n && isSpace(src[i]) {
						i++
					}
					arg2Start := i
					arg2End, hasInterpolation2 := readRubySymbolOrIdent(src, arg2Start)
					if arg2End > arg2Start {
						if hasInterpolation1 || hasInterpolation2 {
							arg1 := normalizeSymbolArg(src[arg1Start:arg1End])
							arg2 := normalizeSymbolArg(src[arg2Start:arg2End])
							out.WriteString("__oruby_alias__(" + string(arg1) + ", " + string(arg2) + ")")
							i = arg2End
							continue
						} else if src[arg1Start] == '$' || src[arg2Start] == '$' {
							arg1 := normalizeSymbolArg(src[arg1Start:arg1End])
							arg2 := normalizeSymbolArg(src[arg2Start:arg2End])
							out.WriteString("__oruby_alias_gvar__(" + string(arg1) + ", " + string(arg2) + ")")
							i = arg2End
							continue
						}
					}
				}
			}
			out.Write(src[kStart:i])
			continue
		}

		if matchKeyword(src, i, "undef") {
			kStart := i
			i += 5
			if i < n && isSpace(src[i]) {
				argsStart := i
				argsEnd := i
				anyDynamic := false
				var args [][]byte

				for argsEnd < n && src[argsEnd] != '\n' && src[argsEnd] != ';' && src[argsEnd] != '#' {
					for argsEnd < n && (isSpace(src[argsEnd]) || src[argsEnd] == ',') {
						argsEnd++
					}
					if argsEnd >= n || src[argsEnd] == '\n' || src[argsEnd] == ';' || src[argsEnd] == '#' {
						break
					}
					aStart := argsEnd
					aEnd, isDyn := readRubySymbolOrIdent(src, aStart)
					if aEnd <= aStart {
						break
					}
					if isDyn {
						anyDynamic = true
					}
					args = append(args, normalizeSymbolArg(src[aStart:aEnd]))
					argsEnd = aEnd
				}

				if anyDynamic && len(args) > 0 {
					var joined bytes.Buffer
					for idx, a := range args {
						if idx > 0 {
							joined.WriteString(", ")
						}
						joined.Write(a)
					}
					out.WriteString("__oruby_undef__(" + joined.String() + ")")
					i = argsEnd
					continue
				}
				out.Write(src[kStart:argsStart])
				continue
			}
			out.Write(src[kStart:i])
			continue
		}

		out.WriteByte(src[i])
		i++
	}

	return out.Bytes()
}

func matchKeyword(src []byte, i int, kw string) bool {
	kwLen := len(kw)
	if i+kwLen > len(src) {
		return false
	}
	if string(src[i:i+kwLen]) != kw {
		return false
	}
	if i > 0 {
		prev := src[i-1]
		if isIdentChar(prev) || prev == '.' {
			return false
		}
		j := i - 1
		for j >= 0 && isSpace(src[j]) {
			j--
		}
		if j >= 2 && string(src[j-2:j+1]) == "def" {
			if j-2 == 0 || !isIdentChar(src[j-3]) {
				return false
			}
		}
	}
	if i+kwLen < len(src) {
		next := src[i+kwLen]
		if isIdentChar(next) {
			return false
		}
	}
	return true
}

func readRubySymbolOrIdent(src []byte, start int) (int, bool) {
	n := len(src)
	if start >= n {
		return start, false
	}

	if start+1 < n && src[start] == ':' && src[start+1] == '"' {
		i := start + 2
		hasInterpolation := false
		for i < n {
			if src[i] == '\\' && i+1 < n {
				i += 2
				continue
			}
			if src[i] == '"' {
				i++
				return i, hasInterpolation
			}
			if src[i] == '#' && i+1 < n && src[i+1] == '{' {
				hasInterpolation = true
				i += 2
				braceDepth := 1
				for i < n && braceDepth > 0 {
					if src[kSafe(i, n)] == '\\' && i+1 < n {
						i += 2
						continue
					}
					if src[i] == '\'' {
						i++
						for i < n && src[i] != '\'' {
							if src[i] == '\\' && i+1 < n {
								i += 2
								continue
							}
							i++
						}
						if i < n {
							i++
						}
						continue
					}
					if src[i] == '"' {
						i++
						for i < n && src[i] != '"' {
							if src[i] == '\\' && i+1 < n {
								i += 2
								continue
							}
							i++
						}
						if i < n {
							i++
						}
						continue
					}
					if src[i] == '{' {
						braceDepth++
					} else if src[i] == '}' {
						braceDepth--
					}
					i++
				}
				continue
			}
			i++
		}
		return i, hasInterpolation
	}

	if start+1 < n && src[start] == ':' && src[start+1] == '\'' {
		i := start + 2
		for i < n {
			if src[i] == '\\' && i+1 < n {
				i += 2
				continue
			}
			if src[i] == '\'' {
				i++
				return i, false
			}
			i++
		}
		return i, false
	}

	if src[start] == ':' {
		i := start + 1
		for i < n && isSymbolChar(src[i]) {
			i++
		}
		return i, false
	}

	if src[start] == '"' {
		i := start + 1
		hasInterpolation := false
		for i < n {
			if src[i] == '\\' && i+1 < n {
				i += 2
				continue
			}
			if src[i] == '"' {
				i++
				return i, hasInterpolation
			}
			if src[i] == '#' && i+1 < n && src[i+1] == '{' {
				hasInterpolation = true
			}
			i++
		}
		return i, hasInterpolation
	}

	if src[start] == '$' {
		i := start + 1
		if i < n && strings.IndexByte("&`'+!@~=/\\,;.<>?*$\"", src[i]) >= 0 {
			return i + 1, false
		}
		for i < n && isIdentChar(src[i]) {
			i++
		}
		return i, false
	}

	i := start
	for i < n && isIdentOrOpChar(src[i]) {
		i++
	}
	return i, false
}

func kSafe(i, n int) int {
	if i < n {
		return i
	}
	return n - 1
}

func normalizeSymbolArg(arg []byte) []byte {
	s := strings.TrimSpace(string(arg))
	if strings.HasPrefix(s, "$") {
		return []byte(":" + s)
	}
	if strings.HasPrefix(s, ":") {
		return []byte(s)
	}
	if strings.HasPrefix(s, `"`) {
		return []byte(":" + s)
	}
	return []byte(":" + s)
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r'
}

func isIdentChar(c byte) bool {
	return unicode.IsLetter(rune(c)) || unicode.IsDigit(rune(c)) || c == '_'
}

func isIdentOrOpChar(c byte) bool {
	if isIdentChar(c) {
		return true
	}
	return strings.IndexByte("+-*/%&|^~<=>!?", c) >= 0
}

func isSymbolChar(c byte) bool {
	return isIdentOrOpChar(c)
}
