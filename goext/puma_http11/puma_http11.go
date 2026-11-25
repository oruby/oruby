// Package puma_http11 implements Puma's native puma/puma_http11 HTTP/1.1
// request parser extension in pure Go.
package puma_http11

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/rb"
)

const (
	maxFieldNameLen   = 256
	maxFieldValueLen  = 80 * 1024
	maxRequestURILen  = 12 * 1024
	maxFragmentLen    = 1024
	maxRequestPathLen = 8192
	maxQueryStringLen = 10 * 1024
	maxHeaderLen      = 1024 * (80 + 32)
)

const (
	phaseReqLine = iota
	phaseHeaders
	phaseDone
	phaseError
)

func init() {
	oruby.LazyGem("puma/puma_http11", initPumaHTTP11)
	oruby.GemAlias("puma_http11", "puma/puma_http11")
}

func initPumaHTTP11(mrb *oruby.MrbState) any {
	mod := mrb.DefineModule("Puma")
	stdErr := mrb.ExcGet("StandardError")
	mrb.DefineClassUnder(mod, "HttpParserError", stdErr)

	cls := mrb.DefineClassUnder(mod, "HttpParser", mrb.ObjectClass())
	rb.DataClass(cls)

	cls.DefineMethod("initialize", hpInit, mrb.ArgsNone())
	cls.DefineMethod("reset", hpReset, mrb.ArgsNone())
	cls.DefineMethod("finish", hpFinish, mrb.ArgsNone())
	cls.DefineMethod("finished?", hpFinished, mrb.ArgsNone())
	cls.DefineMethod("error?", hpError, mrb.ArgsNone())
	cls.DefineMethod("nread", hpNread, mrb.ArgsNone())
	cls.DefineMethod("body", hpBody, mrb.ArgsNone())
	cls.DefineMethod("execute", hpExecute, mrb.ArgsReq(3))

	return nil
}

type httpParser struct {
	phase     int
	lineStart int
	nread     int
	body      oruby.Value
}

func getHP(mrb *oruby.MrbState, self oruby.Value) *httpParser {
	hp, _ := rb.Get[*httpParser](mrb, self)
	return hp
}

func (hp *httpParser) reset(mrb *oruby.MrbState, self oruby.Value) {
	hp.phase = phaseReqLine
	hp.lineStart = 0
	hp.nread = 0
	hp.body = mrb.NilValue()
	mrb.IVSet(self, mrb.Intern("@__body"), hp.body)
}

func hpInit(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	hp := &httpParser{}
	rb.Set(mrb, self, hp)
	hp.reset(mrb, self)
	return self
}

func hpReset(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	getHP(mrb, self).reset(mrb, self)
	return mrb.NilValue()
}

func hpFinish(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	hp := getHP(mrb, self)
	if hp.phase != phaseDone {
		hp.phase = phaseError
	}
	return mrb.BoolValue(hp.phase == phaseDone)
}

func hpFinished(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.BoolValue(getHP(mrb, self).phase == phaseDone)
}

func hpError(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.BoolValue(getHP(mrb, self).phase == phaseError)
}

func hpNread(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.FixnumValue(getHP(mrb, self).nread)
}

func hpBody(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return getHP(mrb, self).body
}

func raiseParseErr(mrb *oruby.MrbState, hp *httpParser, format string, args ...any) oruby.MrbValue {
	hp.phase = phaseError
	return rb.Raise(mrb, "Puma::HttpParserError", format, args...)
}

func isTokenChar(c byte) bool {
	if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
		return true
	}
	switch c {
	case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	}
	return false
}

func isMethodChar(c byte) bool {
	return isTokenChar(c)
}

func isValidProtocol(b []byte) bool {
	// HTTP/d.d (e.g. HTTP/1.0, HTTP/1.1)
	if len(b) != 8 {
		return false
	}
	return b[0] == 'H' && b[1] == 'T' && b[2] == 'T' && b[3] == 'P' &&
		b[4] == '/' && b[5] >= '0' && b[5] <= '9' &&
		b[6] == '.' && b[7] >= '0' && b[7] <= '9'
}

func isValidProtocolPrefix(b []byte) bool {
	// Allow optional trailing '\r' when the line has not received '\n' yet
	if len(b) > 0 && b[len(b)-1] == '\r' {
		return isValidProtocol(b[:len(b)-1])
	}
	if len(b) > 8 {
		return false
	}
	const prefix = "HTTP/"
	for i, c := range b {
		switch i {
		case 0, 1, 2, 3, 4:
			if c != prefix[i] {
				return false
			}
		case 5, 7:
			if c < '0' || c > '9' {
				return false
			}
		case 6:
			if c != '.' {
				return false
			}
		}
	}
	return true
}

func isInvalidPathChar(c byte) bool {
	if c <= 0x20 || c >= 0x7f {
		return true
	}
	switch c {
	case '{', '}', '`', '\\':
		return true
	}
	return false
}

func isAbsoluteURI(b []byte) bool {
	idx := bytes.Index(b, []byte("://"))
	if idx <= 0 {
		return false
	}
	for i := 0; i < idx; i++ {
		c := b[i]
		isAlpha := (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
		isDigit := c >= '0' && c <= '9'
		if i == 0 && !isAlpha {
			return false
		}
		if !isAlpha && !isDigit && c != '+' && c != '-' && c != '.' {
			return false
		}
	}
	return true
}

func validatePartialRequestLine(rem []byte) error {
	sp1 := bytes.IndexByte(rem, ' ')
	if sp1 < 0 {
		if len(rem) > 20 {
			return fmt.Errorf("Invalid HTTP format, parsing fails.")
		}
		for _, c := range rem {
			if !isMethodChar(c) {
				return fmt.Errorf("Invalid HTTP format, parsing fails.")
			}
		}
		return nil
	}
	if sp1 == 0 || sp1 > 20 {
		return fmt.Errorf("Invalid HTTP format, parsing fails.")
	}
	for _, c := range rem[:sp1] {
		if !isMethodChar(c) {
			return fmt.Errorf("Invalid HTTP format, parsing fails.")
		}
	}
	rest := rem[sp1+1:]
	sp2 := bytes.IndexByte(rest, ' ')
	if sp2 < 0 {
		if len(rest) > maxRequestURILen+maxFragmentLen+1 {
			return fmt.Errorf("HTTP element REQUEST_URI is longer than the %d allowed length", maxRequestURILen)
		}
		for _, c := range rest {
			if c <= 0x20 || c >= 0x7f {
				return fmt.Errorf("Invalid HTTP format, parsing fails.")
			}
		}
		return nil
	}
	if sp2 == 0 {
		return fmt.Errorf("Invalid HTTP format, parsing fails.")
	}
	proto := rest[sp2+1:]
	if !isValidProtocolPrefix(proto) {
		return fmt.Errorf("Invalid HTTP format, parsing fails.")
	}
	return nil
}

func isOWS(c byte) bool {
	return c == ' ' || c == '\t'
}

func setEnvStr(mrb *oruby.MrbState, env oruby.Value, k, v string) {
	mrb.HashSet(env, mrb.StrNew(k), mrb.StrNew(v))
}

func setEnvHeader(mrb *oruby.MrbState, env oruby.Value, nameBytes, valBytes []byte) error {
	if len(nameBytes) == 0 || len(nameBytes) > maxFieldNameLen {
		return fmt.Errorf("HTTP element FIELD_NAME is longer than the %d allowed length (was %d)", maxFieldNameLen, len(nameBytes))
	}
	if len(valBytes) > maxFieldValueLen {
		return fmt.Errorf("HTTP element FIELD_VALUE is longer than the %d allowed length (was %d)", maxFieldValueLen, len(valBytes))
	}

	// Trim leading & trailing OWS
	for len(valBytes) > 0 && isOWS(valBytes[0]) {
		valBytes = valBytes[1:]
	}
	for len(valBytes) > 0 && isOWS(valBytes[len(valBytes)-1]) {
		valBytes = valBytes[:len(valBytes)-1]
	}

	// Reject CTL characters inside header value (allow HTAB 0x09 and printable / obs-text)
	for _, c := range valBytes {
		if (c < 0x20 && c != '\t') || c == 0x7f {
			return fmt.Errorf("Invalid HTTP format, parsing fails.")
		}
	}

	// Normalize header name: uppercase, '-' -> '_'
	var sb strings.Builder
	sb.Grow(5 + len(nameBytes))
	var norm [maxFieldNameLen]byte
	for i, c := range nameBytes {
		if !isTokenChar(c) {
			return fmt.Errorf("Invalid HTTP format, parsing fails.")
		}
		if c == '-' {
			norm[i] = '_'
		} else if c >= 'a' && c <= 'z' {
			norm[i] = c - ('a' - 'A')
		} else {
			norm[i] = c
		}
	}
	upper := string(norm[:len(nameBytes)])
	var key string
	if upper == "CONTENT_LENGTH" || upper == "CONTENT_TYPE" {
		key = upper
	} else {
		sb.WriteString("HTTP_")
		sb.WriteString(upper)
		key = sb.String()
	}

	keyVal := mrb.StrNew(key)
	valStr := string(valBytes)
	existing, err := mrb.Funcall(env, mrb.Intern("[]"), keyVal)
	if err == nil && !existing.IsNil() && existing.IsString() {
		mrb.HashSet(env, keyVal, mrb.StrNew(existing.String()+", "+valStr))
	} else {
		mrb.HashSet(env, keyVal, mrb.StrNew(valStr))
	}
	return nil
}

func parseRequestLine(mrb *oruby.MrbState, env oruby.Value, line []byte) error {
	// Split on exact single SP: METHOD SP TARGET SP PROTOCOL
	sp1 := bytes.IndexByte(line, ' ')
	if sp1 <= 0 {
		return fmt.Errorf("Invalid HTTP format, parsing fails.")
	}
	method := line[:sp1]
	for _, c := range method {
		if !isMethodChar(c) {
			return fmt.Errorf("Invalid HTTP format, parsing fails.")
		}
	}

	rest := line[sp1+1:]
	sp2 := bytes.IndexByte(rest, ' ')
	if sp2 <= 0 {
		return fmt.Errorf("Invalid HTTP format, parsing fails.")
	}
	target := rest[:sp2]
	proto := rest[sp2+1:]
	if !isValidProtocol(proto) {
		return fmt.Errorf("Invalid HTTP format, parsing fails.")
	}

	// Validate target has no control chars or spaces
	for _, c := range target {
		if c <= 0x20 || c >= 0x7f {
			return fmt.Errorf("Invalid HTTP format, parsing fails.")
		}
	}

	// Absolute-form URI (e.g. http://host:port/path?query): Puma's C parser
	// populates REQUEST_URI only and leaves REQUEST_PATH/QUERY_STRING/FRAGMENT nil.
	if isAbsoluteURI(target) {
		if len(target) > maxRequestURILen {
			return fmt.Errorf("HTTP element REQUEST_URI is longer than the %d allowed length (was %d)", maxRequestURILen, len(target))
		}
		setEnvStr(mrb, env, "REQUEST_METHOD", string(method))
		setEnvStr(mrb, env, "REQUEST_URI", string(target))
		setEnvStr(mrb, env, "SERVER_PROTOCOL", string(proto))
		return nil
	}

	// Split fragment (#...)
	uri := target
	var frag []byte
	hasFrag := false
	if idx := bytes.IndexByte(target, '#'); idx >= 0 {
		uri = target[:idx]
		frag = target[idx+1:]
		hasFrag = true
	}

	if len(uri) > maxRequestURILen {
		return fmt.Errorf("HTTP element REQUEST_URI is longer than the %d allowed length (was %d)", maxRequestURILen, len(uri))
	}
	if hasFrag && len(frag) > maxFragmentLen {
		return fmt.Errorf("HTTP element FRAGMENT is longer than the %d allowed length (was %d)", maxFragmentLen, len(frag))
	}

	// Split query (?...)
	pathPart := uri
	var query []byte
	hasQuery := false
	if idx := bytes.IndexByte(uri, '?'); idx >= 0 {
		pathPart = uri[:idx]
		query = uri[idx+1:]
		hasQuery = true
	}

	for _, c := range pathPart {
		if isInvalidPathChar(c) {
			return fmt.Errorf("Invalid HTTP format, parsing fails.")
		}
	}

	reqPath := pathPart

	if len(reqPath) > maxRequestPathLen {
		return fmt.Errorf("HTTP element REQUEST_PATH is longer than the %d allowed length (was %d)", maxRequestPathLen, len(reqPath))
	}
	if hasQuery && len(query) > maxQueryStringLen {
		return fmt.Errorf("HTTP element QUERY_STRING is longer than the %d allowed length (was %d)", maxQueryStringLen, len(query))
	}

	setEnvStr(mrb, env, "REQUEST_METHOD", string(method))
	setEnvStr(mrb, env, "REQUEST_PATH", string(reqPath))
	if hasQuery {
		setEnvStr(mrb, env, "QUERY_STRING", string(query))
	}
	setEnvStr(mrb, env, "REQUEST_URI", string(uri))
	if hasFrag {
		setEnvStr(mrb, env, "FRAGMENT", string(frag))
	}
	setEnvStr(mrb, env, "SERVER_PROTOCOL", string(proto))
	return nil
}

func hpExecute(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	hp := getHP(mrb, self)
	args := mrb.GetArgs()
	env := args.Item(0)
	dataVal := args.Item(1)
	start := args.Item(2).Int()

	if !env.IsHash() {
		return mrb.ETypeError().Raise("expected Hash for env")
	}
	if !dataVal.IsString() {
		return mrb.ETypeError().Raise("expected String for data")
	}
	data := dataVal.Bytes()
	if start >= len(data) || start < 0 {
		return raiseParseErr(mrb, hp, "Requested start is after data buffer end.")
	}
	if hp.phase == phaseError {
		return raiseParseErr(mrb, hp, "Invalid HTTP format, parsing fails.")
	}
	if hp.phase == phaseDone {
		return mrb.FixnumValue(hp.nread)
	}

	for {
		if hp.lineStart > maxHeaderLen {
			return raiseParseErr(mrb, hp, "HTTP element HEADER is longer than the %d allowed length (was %d)", maxHeaderLen, hp.lineStart)
		}

		rem := data[hp.lineStart:]
		lfIdx := bytes.IndexByte(rem, '\n')
		if lfIdx < 0 {
			// Incomplete line; validate partial length and incremental syntax
			if len(data) > maxHeaderLen {
				return raiseParseErr(mrb, hp, "HTTP element HEADER is longer than the %d allowed length (was %d)", maxHeaderLen, len(data))
			}
			if bytes.IndexByte(rem, 0) >= 0 {
				return raiseParseErr(mrb, hp, "Invalid HTTP format, parsing fails. Are you trying to open an SSL connection to a non-SSL Puma?")
			}
			if hp.phase == phaseReqLine {
				if err := validatePartialRequestLine(rem); err != nil {
					return raiseParseErr(mrb, hp, "%s", err.Error())
				}
			}
			hp.nread = len(data)
			return mrb.FixnumValue(hp.nread)
		}

		// Require strict CRLF (\r\n)
		if lfIdx == 0 || rem[lfIdx-1] != '\r' {
			return raiseParseErr(mrb, hp, "Invalid HTTP format, parsing fails. Are you trying to open an SSL connection to a non-SSL Puma?")
		}

		line := rem[:lfIdx-1]
		nextStart := hp.lineStart + lfIdx + 1
		if nextStart > maxHeaderLen {
			return raiseParseErr(mrb, hp, "HTTP element HEADER is longer than the %d allowed length (was %d)", maxHeaderLen, nextStart)
		}

		switch hp.phase {
		case phaseReqLine:
			if err := parseRequestLine(mrb, env, line); err != nil {
				return raiseParseErr(mrb, hp, "%s", err.Error())
			}
			hp.lineStart = nextStart
			hp.nread = nextStart
			hp.phase = phaseHeaders

		case phaseHeaders:
			if len(line) == 0 {
				// Blank line -> end of HTTP headers!
				hp.lineStart = nextStart
				hp.nread = nextStart
				hp.phase = phaseDone
				bodyBytes := data[nextStart:]
				hp.body = mrb.Call(mrb.BytesValue(bodyBytes), "b")
				mrb.IVSet(self, mrb.Intern("@__body"), hp.body)
				return mrb.FixnumValue(hp.nread)
			}

			colon := bytes.IndexByte(line, ':')
			if colon <= 0 {
				return raiseParseErr(mrb, hp, "Invalid HTTP format, parsing fails.")
			}
			if err := setEnvHeader(mrb, env, line[:colon], line[colon+1:]); err != nil {
				return raiseParseErr(mrb, hp, "%s", err.Error())
			}
			hp.lineStart = nextStart
			hp.nread = nextStart
		}
	}
}
