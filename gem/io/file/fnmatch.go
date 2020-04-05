package file

import (
	"regexp"
	"runtime"
	"strings"
)

func fnmatch(name, pattern string, flags int) (bool, error) {
	r := ""
	i := 0
	l := len(pattern)

	if !(flags&fnmDotmatch != 0) {
		dotidx := strings.Index(name, "/.")
		if len(name) > 0 && name[0] == '.' {
			dotidx = 0
		}
		if dotidx >= 0 {
			idx := strings.Count(pattern, ".*")
			if idx > 0 {
				sps := strings.SplitN(name, ".", idx-1)
				if len(sps) > 0 {
					name = sps[0]
				}
			} else {
				// Explicitly unsupported leading dot
				if dotidx == 0 {
					return false, nil
				}
				name = name[:dotidx]
			}
		}
	}

	inEscape := false
	for i < l {
		if inEscape {
			inEscape = false
			r += regexp.QuoteMeta(string(pattern[i]))
			i += 1
		} else if strings.HasPrefix(pattern[i:], "**/") {
			if flags&fnmPathname != 0 {
				r += "(?:.*/)?"
			} else {
				r += "(?:.*)?/"
			}
			i += 3
		} else if pattern[i] == '\\'  {
			if flags&fnmNoescape != 0 {
				r += regexp.QuoteMeta("\\")
			} else {
				inEscape = true
			}
			i += 1
		} else if pattern[i] == '*' {
			if flags&fnmPathname != 0 {
				r += "[^/]+"
			} else {
				r += ".*"
			}
			i += 1
		} else if pattern[i] == '.' {
			r += "[.]"
			i += 1
		} else if pattern[i] == '?' {
			if flags&fnmPathname != 0 {
				r += "[^/.]"
			} else {
				r += "."
			}
			i += 1
		} else if pattern[i] == '{' && flags&fnmExtglob != 0 {
			i += 1
			j := i
			if j < l && pattern[j] == '}' {
				j += 1
			}
			for j < l && pattern[j] != '}' {
				j += 1
			}
			if j >= l {
				r += "\\{"
			} else {
				r = r + "(" + strings.ReplaceAll(pattern[i:j], ",", "|") + ")"
				i = j+1
			}
		} else if pattern[i] == '[' {
			i += 1
			j := i
			for j < l && pattern[j] != ']' {
				j += 1
			}
			if j >= l {
				r += "\\["  //# didn't find a closing ']', backtracking
			} else {
				stuff := strings.ReplaceAll(pattern[i:j], "\\", "\\\\")
				if flags&fnmPathname != 0 {
					stuff = strings.ReplaceAll(stuff, "/", "^/")
				}
				i = j+1
				if len(stuff) > 0 {
					r = r + "[" + stuff + "]"
				}
			}
		} else {
			r += regexp.QuoteMeta(string(pattern[i]))
			i += 1
		}
	}

	if flags&fnmCasefold != 0 || (flags&fnmSyscase != 0 && runtime.GOOS != "windows") {
		r = "(?ims)" + r + "\\z"
	} else {
		r = "(?ms)" + r + "\\z"
	}

	return regexp.MatchString(r, name)
}

