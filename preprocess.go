package oruby

import (
	"bytes"
	"strings"
	"unicode"
)

// PreprocessRuby desugars Ruby 3+ syntax constructs unsupported by mruby's code generator,
// specifically dynamic alias/undef (alias :"#{...}" ...) and **nil parameter declarations.
func PreprocessRuby(source string) string {
	if !strings.Contains(source, "alias") &&
		!strings.Contains(source, "undef") &&
		!strings.Contains(source, "**nil") {
		return source
	}

	b := []byte(source)
	b = preprocessDynamicAliasAndUndef(b)
	b = preprocessNoKeywords(b)
	return string(b)
}

// PreprocessRubyBytes operates on byte slices.
func PreprocessRubyBytes(source []byte) []byte {
	if !bytes.Contains(source, []byte("alias")) &&
		!bytes.Contains(source, []byte("undef")) &&
		!bytes.Contains(source, []byte("**nil")) {
		return source
	}

	b := preprocessDynamicAliasAndUndef(source)
	b = preprocessNoKeywords(b)
	return b
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
		// Skip single line comment
		if src[i] == '#' {
			start := i
			for i < n && src[i] != '\n' {
				i++
			}
			out.Write(src[start:i])
			continue
		}
		// Skip single-quoted string
		if src[i] == '\'' {
			start := i
			i++
			for i < n {
				if src[i] == '\\' && i+1 < n {
					i += 2
					continue
				}
				if src[i] == '\'' {
					i++
					break
				}
				i++
			}
			out.Write(src[start:i])
			continue
		}
		// Skip double-quoted string
		if src[i] == '"' {
			start := i
			i++
			for i < n {
				if src[i] == '\\' && i+1 < n {
					i += 2
					continue
				}
				if src[i] == '"' {
					i++
					break
				}
				i++
			}
			out.Write(src[start:i])
			continue
		}

		if i+5 <= n && bytes.Equal(src[i:i+5], target) {
			// Check word boundary after target
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
		// Skip comments
		if src[i] == '#' {
			start := i
			for i < n && src[i] != '\n' {
				i++
			}
			out.Write(src[start:i])
			continue
		}
		// Skip single-quoted string
		if src[i] == '\'' {
			start := i
			i++
			for i < n {
				if src[i] == '\\' && i+1 < n {
					i += 2
					continue
				}
				if src[i] == '\'' {
					i++
					break
				}
				i++
			}
			out.Write(src[start:i])
			continue
		}
		// Skip double-quoted string
		if src[i] == '"' {
			start := i
			i++
			for i < n {
				if src[i] == '\\' && i+1 < n {
					i += 2
					continue
				}
				if src[i] == '"' {
					i++
					break
				}
				i++
			}
			out.Write(src[start:i])
			continue
		}

		// Check for keyword 'alias'
		if matchKeyword(src, i, "alias") {
			kStart := i
			i += 5
			// Must be followed by whitespace
			if i < n && isSpace(src[i]) {
				// Read arg1
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
						}
					}
				}
			}
			// Fallback: output 'alias' as-is
			out.Write(src[kStart:i])
			continue
		}

		// Check for keyword 'undef'
		if matchKeyword(src, i, "undef") {
			kStart := i
			i += 5
			if i < n && isSpace(src[i]) {
				// Read comma-separated arguments until newline, semicolon, or comment
				argsStart := i
				argsEnd := i
				anyDynamic := false
				var args [][]byte

				for argsEnd < n && src[argsEnd] != '\n' && src[argsEnd] != ';' && src[argsEnd] != '#' {
					// Skip leading spaces / commas
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
	// Check preceding character: must not be an identifier char or dot (e.g. obj.alias or def alias)
	if i > 0 {
		prev := src[i-1]
		if isIdentChar(prev) || prev == '.' {
			return false
		}
		// Also check if preceded by 'def '
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
	// Check following character: must be whitespace or line break
	if i+kwLen < len(src) {
		next := src[i+kwLen]
		if isIdentChar(next) {
			return false
		}
	}
	return true
}

// readRubySymbolOrIdent reads one argument to alias/undef.
// It returns (endIndex, hasDynamicInterpolation).
func readRubySymbolOrIdent(src []byte, start int) (int, bool) {
	n := len(src)
	if start >= n {
		return start, false
	}

	// Dynamic symbol: :"..."
	if start+1 < n && src[start] == ':' && src[start+1] == '"' {
		i := start + 2
		hasInterpolation := false
		for i < n {
			if src[i] == '\\' && i+1 < n {
				i += 2
				continue
			}
			if src[i] == '"' {
				i++ // closing quote
				return i, hasInterpolation
			}
			if src[i] == '#' && i+1 < n && src[i+1] == '{' {
				hasInterpolation = true
				i += 2
				braceDepth := 1
				for i < n && braceDepth > 0 {
					if src[i] == '\\' && i+1 < n {
						i += 2
						continue
					}
					// Handle nested single quotes inside #{...}
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
							i++ // closing '
						}
						continue
					}
					// Handle nested double quotes inside #{...}
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
							i++ // closing "
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

	// Single quoted symbol: :'...'
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

	// Simple symbol: :ident or :op
	if src[start] == ':' {
		i := start + 1
		for i < n && isSymbolChar(src[i]) {
			i++
		}
		return i, false
	}

	// Double quoted string or symbol without leading colon if used
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

	// Bare identifier or operator name
	i := start
	for i < n && isIdentOrOpChar(src[i]) {
		i++
	}
	return i, false
}

func normalizeSymbolArg(arg []byte) []byte {
	s := strings.TrimSpace(string(arg))
	if strings.HasPrefix(s, ":") {
		return []byte(s)
	}
	if strings.HasPrefix(s, `"`) {
		return []byte(":" + s)
	}
	// Bare identifier -> :ident
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
	// Allow operators like +, -, *, /, ==, !=, [], []=, !
	return strings.IndexByte("+-*/%&|^~<=>!?", c) >= 0
}

func isSymbolChar(c byte) bool {
	return isIdentOrOpChar(c)
}
