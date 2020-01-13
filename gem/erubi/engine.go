package erubi

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Engine as close as possible to Erubi::Engine implementation
type Engine struct {
	escape     bool
	trim       bool
	ensure     bool
	freeze     bool
	filename   string
	bufvar     string
	bufval     string
	src        string
	escapefunc string
	regex      *regexp.Regexp
	preamble   string
	postamble  string
}

func getB(h map[string]interface{}, key string, def bool) bool {
	if ret, ok := h[key]; ok {
		switch v := ret.(type) {
		case bool:
			return v
		case nil:
			return false
		case string:
			return v != ""
		case int:
			return v != 0
		default:
			return true
		}
	}
	return def
}

func getS(h map[string]interface{}, key string, def string) string {
	if ret, ok := h[key]; ok {
		switch s := ret.(type) {
		case string:
			return s
		case nil:
			return ""
		case fmt.Stringer:
			return s.String()
		default:
			return fmt.Sprintf("%v", s)
		}
	}
	return def
}

// New Engine constructor with options
func New(options map[string]interface{}) *Engine {
	reg := getS(options, "regex", "(?ms)<%(={1,2}|-|\\#|%)?(.*?)([-=])?%>([ \t]*\r?\n)?")

	escapeHTML := getB(options, "escape_html", false)
	outvar := getS(options, "outvar", "_buf")

	e := &Engine{
		escape:     getB(options, "escape", escapeHTML),
		trim:       getB(options, "trim", true),
		ensure:     getB(options, "ensure", false),
		freeze:     getB(options, "freeze", false),
		filename:   getS(options, "filename", ""),
		bufvar:     getS(options, "bufvar", outvar),
		bufval:     getS(options, "bufval", "::String.new"),
		src:        getS(options, "src", ""),
		escapefunc: getS(options, "escapefunc", "::Erubi.h"),
		regex:      regexp.MustCompile(reg),
		preamble:   getS(options, "preamble", ""),
		postamble:  getS(options, "postamble", ""),
	}

	return e
}

// NewInit constructor from inpit string and options hash
func NewInit(input string, options map[string]interface{}) (*Engine, error) {
	engine := New(options)
	err := engine.Init(input)
	return engine, err
}

// Src code generated from the template, which can be evaled
func (e *Engine) Src() string { return e.src }

// Filename of the template, if one was given
func (e *Engine) Filename() string { return e.filename }

// Bufvar is variable name used for the buffer variable
func (e *Engine) Bufvar() string { return e.filename }

// Init engine with string
func (e *Engine) Init(input string) error {
	if e.freeze {
		e.src += "# frozen_string_literal: true\n"
	}

	if e.ensure {
		e.src += fmt.Sprintf("begin; __original_outvar = %v if defined?(%v); ", e.bufvar, e.bufvar)
	}

	if e.escape {
		e.src += "__erubi = ::Erubi;"
		e.escapefunc = "__erubi.h"
	}

	if e.preamble == "" {
		e.preamble = fmt.Sprintf("%v = %v;", e.bufvar, e.bufval)
	}

	if e.postamble == "" {
		e.postamble = fmt.Sprintf("%v.to_s\n", e.bufvar)
	}

	e.src += e.preamble

	pos := 0
	isBol := true

	// "(?ms)<%(={1,2}|-|\\#|%)?(.*?)([-=])?%>([ \t]*\r?\n)?"
	for _, loc := range e.regex.FindAllStringSubmatchIndex(input, -1) {
		text := input[pos:loc[0]]
		pos = loc[1]
		var ch uint8 = 0
		indicator := ""
		code := ""
		tailch := ""
		rspace := ""

		if loc[2] >= 0 {
			indicator = input[loc[2]:loc[3]]
			ch = indicator[0]
		}
		if loc[4] >= 0 {
			code = input[loc[4]:loc[5]]
		}
		if loc[6] >= 0 {
			tailch = input[loc[6]:loc[7]]
		}
		if loc[8] >= 0 {
			rspace = input[loc[8]:loc[9]]
		}

		lspace := ""

		if ch != '=' {
			if text == "" {
				if isBol {
					lspace = ""
				}
			} else if text[len(text)-1] == '\n' {
				lspace = ""
			} else {
				rindex := strings.LastIndexByte(text, '\n')
				if rindex >= 0 {
					s := text[rindex+1:]
					if regexp.MustCompile("\\A[ \t]*\\z").MatchString(s) {
						lspace = s
						text = text[:rindex+1]
					} else {
						if isBol && regexp.MustCompile("\\A[ \t]*\\z").MatchString(text) {
							lspace = text
							text = ""
						}
					}
				}
			}
		}

		isBol = rspace != ""
		if text != "" {
			e.addText(text)
		}

		switch ch {
		case '=':
			if tailch != "" {
				rspace = ""
			}
			if lspace != "" {
				e.addText(lspace)
			}
			e.addExpression(indicator, code)
			if rspace != "" {
				e.addText(rspace)
			}
		case '#':
			n := strings.Count(code, "\n")
			if rspace != "" {
				n++
			}
			if e.trim && lspace != "" && rspace != "" {
				e.addCode(strings.Repeat("\n", n))
			} else {
				if lspace != "" {
					e.addText(lspace)
				}
				e.addCode(strings.Repeat("\n", n))
				if rspace != "" {
					e.addText(rspace)
				}
			}
		case '%':
			e.addText("#{lspace}#{prefix||='<%'}#{code}#{tailch}#{postfix||='%>'}#{rspace}")
		case 0, '-':
			if e.trim && lspace != "" && rspace != "" {
				e.addCode(lspace + code + rspace)
			} else {
				if lspace != "" {
					e.addText(lspace)
				}
				e.addCode(code)
				if rspace != "" {
					e.addText(rspace)
				}
			}
		default:
			return e.handle(indicator, code, tailch, rspace, lspace)
		}
	}

	if pos == 0 {
		e.addText(input)
	} else {
		e.addText(input[pos:])
	}
	if e.src[len(e.src)-1] != '\n' {
		e.src += "\n"
	}

	e.addPostamble()

	if e.ensure {
		e.src += "; ensure\n  " + e.bufvar + " = __original_outvar\nend\n"
	}

	return nil
}

// Add raw text to the template
func (e *Engine) addText(text string) {
	if text == "" {
		return
	}
	t := regexp.MustCompile("['\\\\]")
	e.src += fmt.Sprintf(" %v << '%v'.freeze;", e.bufvar, t.ReplaceAllString(text, "\\\\\\&"))
}

// Add ruby code to the template
func (e *Engine) addCode(code string) {
	e.src += code
	if code != "" && code[len(code)-1] != '\n' {
		e.src += ";"
	}
}

// Add the given ruby expression result to the template,
//escaping it based on the indicator given and escape flag.
func (e *Engine) addExpression(indicator, code string) {
	if (indicator == "=") && !e.escape {
		e.addExpressionResult(code)
	} else {
		e.addExpressionResultEscaped(code)
	}
}

// Add the result of Ruby expression to the template
func (e *Engine) addExpressionResult(code string) {
	e.src += " " + e.bufvar + " << (" + code + ").to_s;"
}

// Add the escaped result of Ruby expression to the template
func (e *Engine) addExpressionResultEscaped(code string) {
	e.src += " " + e.bufvar + " << " + e.escapefunc + "((" + code + "));"
}

// Add the given postamble to the src.  Can be overridden in subclasses
// to make additional changes to src that depend on the current state.
func (e *Engine) addPostamble() {
	e.src += e.postamble
}

// Raise an exception, as the base engine class does not support handling other indicators.
func (e *Engine) handle(indicator, code, tailch, rspace, lspace string) error {
	return errors.New("ArgumentError: Invalid indicator: " + indicator)
}
