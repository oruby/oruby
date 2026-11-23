package nokogiri

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jacoelho/xsd"
	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/internal/rb"
	"github.com/tim-riep/go-xslt/engine"
)

type xsdSchemaData struct {
	engine   *xsd.Engine
	rawXSD   string
	baseDir  string
	useXSD11 bool
	errors   []domError
}

type xsltStylesheetData struct {
	source     string
	baseDir    string
	hasExtCall bool
}

var (
	reXMLNSBinding = regexp.MustCompile(`xmlns:([A-Za-z_][A-Za-z0-9._-]*)\s*=\s*["']([^"']+)["']`)
	reXSLValueOf   = regexp.MustCompile(`<xsl:value-of\s+select\s*=\s*(?:"([^"]+)"|'([^']+)')\s*/>`)
	reExtCallOut   = regexp.MustCompile(`(?s)<__oruby_ext_call__\s+uri="([^"]+)"\s+fn="([^"]+)">(.*?)</__oruby_ext_call__>`)
	reExtArgOut    = regexp.MustCompile(`(?s)<__oruby_ext_arg__>(.*?)</__oruby_ext_arg__>`)
)

func registeredXSLTExtURIs(mrb *oruby.MrbState, xsltMod oruby.RClass) map[string]oruby.Value {
	reg, err := mrb.Funcall(xsltMod, mrb.Intern("registered_extensions"))
	if err != nil || reg.IsNil() || !reg.IsHash() {
		return nil
	}
	keys := mrb.HashKeys(reg)
	n := oruby.RArrayLen(keys)
	if n == 0 {
		return nil
	}
	out := make(map[string]oruby.Value, n)
	for i := 0; i < n; i++ {
		k := mrb.AryRef(keys, i)
		v, err := mrb.Funcall(reg, mrb.Intern("[]"), k)
		if err == nil && !v.IsNil() {
			out[k.String()] = v
		}
	}
	return out
}

func rewriteXSLTExtensions(src string, regURIs map[string]oruby.Value) (string, bool) {
	if len(regURIs) == 0 {
		return src, false
	}
	prefToURI := map[string]string{}
	for _, m := range reXMLNSBinding.FindAllStringSubmatch(src, -1) {
		if _, ok := regURIs[m[2]]; ok {
			prefToURI[m[1]] = m[2]
		}
	}
	if len(prefToURI) == 0 {
		return src, false
	}
	rewrote := false
	out := reXSLValueOf.ReplaceAllStringFunc(src, func(match string) string {
		sm := reXSLValueOf.FindStringSubmatch(match)
		sel := sm[1]
		if sel == "" {
			sel = sm[2]
		}
		sel = strings.TrimSpace(sel)
		colon := strings.IndexByte(sel, ':')
		open := strings.IndexByte(sel, '(')
		if colon <= 0 || open <= colon || !strings.HasSuffix(sel, ")") {
			return match
		}
		pref := sel[:colon]
		uri, ok := prefToURI[pref]
		if !ok {
			return match
		}
		fnName := sel[colon+1 : open]
		argsRaw := strings.TrimSpace(sel[open+1 : len(sel)-1])
		var sb strings.Builder
		sb.WriteString(`<__oruby_ext_call__ uri="`)
		sb.WriteString(html.EscapeString(uri))
		sb.WriteString(`" fn="`)
		sb.WriteString(html.EscapeString(fnName))
		sb.WriteString(`">`)
		if argsRaw != "" {
			for _, ae := range splitTopLevel(argsRaw, ',') {
				ae = strings.TrimSpace(ae)
				sb.WriteString(`<__oruby_ext_arg__><xsl:value-of select="`)
				sb.WriteString(html.EscapeString(ae))
				sb.WriteString(`"/></__oruby_ext_arg__>`)
			}
		}
		sb.WriteString(`</__oruby_ext_call__>`)
		rewrote = true
		return sb.String()
	})
	return out, rewrote
}

func resolveXSLTExtCalls(mrb *oruby.MrbState, out string, regURIs map[string]oruby.Value, method string) (string, error) {
	if !strings.Contains(out, "<__oruby_ext_call__") {
		return out, nil
	}
	var firstErr error
	replaced := reExtCallOut.ReplaceAllStringFunc(out, func(match string) string {
		if firstErr != nil {
			return match
		}
		sm := reExtCallOut.FindStringSubmatch(match)
		uri := html.UnescapeString(sm[1])
		fn := html.UnescapeString(sm[2])
		body := sm[3]
		cls, ok := regURIs[uri]
		if !ok {
			firstErr = fmt.Errorf("no XSLT extension registered for %s", uri)
			return match
		}
		inst, err := mrb.Funcall(cls, mrb.Intern("new"))
		if err != nil {
			firstErr = err
			return match
		}
		argMatches := reExtArgOut.FindAllStringSubmatch(body, -1)
		rubyArgs := make([]any, 0, len(argMatches))
		for _, am := range argMatches {
			rubyArgs = append(rubyArgs, mrb.StrNew(html.UnescapeString(am[1])))
		}
		res, err := mrb.Funcall(inst, mrb.Intern(fn), rubyArgs...)
		if err != nil {
			firstErr = err
			return match
		}
		s := mrb.String(res)
		if method == "text" {
			return s
		}
		return html.EscapeString(s)
	})
	return replaced, firstErr
}

func compileXSDSchema(src, filePath string) (*xsdSchemaData, error) {
	baseDir := ""
	if filePath != "" {
		baseDir = filepath.Dir(filePath)
	}
	// If the schema explicitly uses XSD 1.1 constructs (xs:assert / xsd:assert / minVersion="1.1"),
	// route to go-xslt's XSD 1.1 validator; otherwise compile with jacoelho/xsd (fast XSD 1.0).
	if strings.Contains(src, ":assert") || strings.Contains(src, "<assert") || strings.Contains(src, `minVersion="1.1"`) {
		return &xsdSchemaData{
			rawXSD:   src,
			baseDir:  baseDir,
			useXSD11: true,
		}, nil
	}

	var eng *xsd.Engine
	var err error
	if filePath != "" {
		if _, statErr := os.Stat(filePath); statErr == nil {
			eng, err = xsd.Compile(xsd.File(filePath))
		} else {
			eng, err = xsd.Compile(xsd.Bytes(filepath.Base(filePath), []byte(src)))
		}
	} else {
		eng, err = xsd.Compile(xsd.Bytes("schema.xsd", []byte(src)))
	}
	if err != nil {
		return nil, err
	}
	return &xsdSchemaData{
		engine:  eng,
		rawXSD:  src,
		baseDir: baseDir,
	}, nil
}

func (s *xsdSchemaData) validateXML(mrb *oruby.MrbState, xmlStr string) []domError {
	if s.useXSD11 || s.engine == nil {
		res := engine.ValidateContext(mrb.GoContext(), engine.ValidateRequest{
			Instance: xmlStr,
			Schemas:  []string{s.rawXSD},
			Version:  engine.XSD11,
			BaseDir:  s.baseDir,
		})
		if res.Valid {
			return nil
		}
		var errs []domError
		for _, d := range res.Diagnostics {
			if d.Severity == engine.SeverityError {
				msg := d.Message
				if d.Path != "" {
					msg = fmt.Sprintf("%s: %s", d.Path, msg)
				}
				errs = append(errs, domError{
					message: msg,
					line:    d.Line,
					column:  d.Col,
					level:   2,
				})
			}
		}
		if len(errs) == 0 {
			errs = append(errs, domError{message: "XSD validation failed", level: 2})
		}
		return errs
	}

	if err := s.engine.Validate(strings.NewReader(xmlStr)); err != nil {
		return []domError{{
			message: err.Error(),
			line:    1,
			level:   2,
		}}
	}
	return nil
}

func parseXSLTParams(mrb *oruby.MrbState, arg oruby.Value) (map[string]string, map[string]string) {
	params := map[string]string{}
	paramExprs := map[string]string{}
	if arg.IsNil() {
		return params, paramExprs
	}
	assign := func(k, v string) {
		// In Nokogiri::XSLT, parameter values are XPath expressions (e.g. "'hello'" or "42").
		// Send valid quoted/numeric expressions via ParamExprs so both quote_params and numeric params work!
		trimmed := strings.TrimSpace(v)
		if (len(trimmed) >= 2 && trimmed[0] == '\'' && trimmed[len(trimmed)-1] == '\'') ||
			(len(trimmed) >= 2 && trimmed[0] == '"' && trimmed[len(trimmed)-1] == '"') ||
			strings.HasPrefix(trimmed, "concat(") {
			paramExprs[k] = trimmed
		} else {
			paramExprs[k] = trimmed
			params[k] = v
		}
	}
	if arg.IsArray() {
		n := oruby.RArrayLen(arg)
		for i := 0; i+1 < n; i += 2 {
			k := mrb.String(mrb.AryRef(arg, i))
			v := mrb.String(mrb.AryRef(arg, i+1))
			assign(k, v)
		}
	} else if arg.IsHash() {
		keys := mrb.HashKeys(arg)
		for i := 0; i < oruby.RArrayLen(keys); i++ {
			kv := mrb.AryRef(keys, i)
			vv, err := mrb.Funcall(arg, mrb.Intern("[]"), kv)
			if err != nil {
				continue
			}
			assign(mrb.String(kv), mrb.String(vv))
		}
	}
	return params, paramExprs
}

func defineSchemaAndXSLT(mrb *oruby.MrbState, mod, xmlMod oruby.RClass) {
	// -------------------------------------------------------------------------
	// Nokogiri::XML::Schema (XSD 1.0 & 1.1)
	// -------------------------------------------------------------------------
	schemaCls := rb.DataClass(mrb.DefineClassUnder(xmlMod, "Schema", mrb.ObjectClass()))

	schemaCls.DefineClassMethod("__compile", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		a := mrb.GetArgs().Slice()
		src := a[0].String()
		filePath := ""
		if len(a) >= 2 && !a[1].IsNil() {
			filePath = a[1].String()
		}
		sd, err := compileXSDSchema(src, filePath)
		if err != nil {
			return rb.Raise(mrb, "Nokogiri::XML::SyntaxError", "%s", err.Error())
		}
		obj := mrb.ObjAlloc(oruby.MrbTTCData, schemaCls)
		rb.Set(mrb, obj.Value(), sd)
		return obj
	}, mrb.ArgsArg(1, 1))

	schemaCls.DefineMethod("errors", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		if iv := mrb.IVGet(self, mrb.Intern("@errors")); !iv.IsNil() {
			return iv
		}
		sd, ok := rb.Get[*xsdSchemaData](mrb, self)
		if !ok {
			return mrb.AryNew()
		}
		return makeErrorsArray(mrb, sd.errors)
	}, mrb.ArgsNone())

	schemaCls.DefineMethod("errors=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		mrb.IVSet(self, mrb.Intern("@errors"), v)
		return v
	}, mrb.ArgsReq(1))

	schemaCls.DefineMethod("__validate_xml", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		sd, ok := rb.Get[*xsdSchemaData](mrb, self)
		if !ok {
			return rb.Raise(mrb, "Nokogiri::XML::SyntaxError", "Schema not initialized")
		}
		xmlStr := mrb.GetArgsFirst().String()
		var errs []domError
		mrb.Blocking(func() {
			errs = sd.validateXML(mrb, xmlStr)
		})
		return makeErrorsArray(mrb, errs)
	}, mrb.ArgsReq(1))

	// -------------------------------------------------------------------------
	// Nokogiri::XML::RelaxNG (inherits from Schema)
	// -------------------------------------------------------------------------
	rngCls := rb.DataClass(mrb.DefineClassUnder(xmlMod, "RelaxNG", schemaCls))

	rngCls.DefineClassMethod("__compile", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		a := mrb.GetArgs().Slice()
		src := a[0].String()
		g, err := compileRelaxNG(src)
		if err != nil {
			return rb.Raise(mrb, "Nokogiri::XML::SyntaxError", "%s", err.Error())
		}
		obj := mrb.ObjAlloc(oruby.MrbTTCData, rngCls)
		rb.Set(mrb, obj.Value(), g)
		return obj
	}, mrb.ArgsArg(1, 1))

	rngCls.DefineMethod("__validate_xml", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		g, ok := rb.Get[*rngGrammar](mrb, self)
		if !ok {
			return rb.Raise(mrb, "Nokogiri::XML::SyntaxError", "RelaxNG schema not initialized")
		}
		xmlStr := mrb.GetArgsFirst().String()
		doc := parseXMLDocument(xmlStr, "", "UTF-8", false, false, false)
		errs := g.validate(doc)
		return makeErrorsArray(mrb, errs)
	}, mrb.ArgsReq(1))

	// -------------------------------------------------------------------------
	// Nokogiri::XSLT & Nokogiri::XSLT::Stylesheet (XSLT 1.0 / 2.0 / 3.0)
	// -------------------------------------------------------------------------
	xsltMod := mrb.DefineModuleUnder(mod, "XSLT")
	ssCls := rb.DataClass(mrb.DefineClassUnder(xsltMod, "Stylesheet", mrb.ObjectClass()))

	ssCls.DefineClassMethod("__compile", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		a := mrb.GetArgs().Slice()
		src := a[0].String()
		baseDir := ""
		if len(a) >= 2 && !a[1].IsNil() && a[1].String() != "" {
			baseDir = filepath.Dir(a[1].String())
		}
		regURIs := registeredXSLTExtURIs(mrb, xsltMod)
		rewritten, hasExt := rewriteXSLTExtensions(src, regURIs)
		// Validate compilation immediately so bad stylesheets raise RuntimeError like Nokogiri::XSLT
		_, diags := engine.Compile(engine.Request{
			Stylesheet: rewritten,
			BaseDir:    baseDir,
			Features:   engine.Features{BackwardsCompatible: true},
		})
		for _, d := range diags {
			if d.Severity == engine.SeverityError {
				return mrb.Raise(mrb.ERuntimeError(), fmt.Sprintf("XSLT compile error (%s): %s", d.Code, d.Message))
			}
		}
		obj := mrb.ObjAlloc(oruby.MrbTTCData, ssCls)
		rb.Set(mrb, obj.Value(), &xsltStylesheetData{source: rewritten, baseDir: baseDir, hasExtCall: hasExt})
		return obj
	}, mrb.ArgsArg(1, 1))

	ssCls.DefineMethod("__apply", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ss, ok := rb.Get[*xsltStylesheetData](mrb, self)
		if !ok {
			return mrb.Raise(mrb.ERuntimeError(), "Stylesheet not initialized")
		}
		a := mrb.GetArgs().Slice()
		xmlSrc := a[0].String()
		var paramsArg oruby.Value = mrb.NilValue()
		if len(a) >= 2 {
			paramsArg = a[1]
		}
		params, paramExprs := parseXSLTParams(mrb, paramsArg)
		req := engine.Request{
			Stylesheet: ss.source,
			Source:     xmlSrc,
			Params:     params,
			ParamExprs: paramExprs,
			BaseDir:    ss.baseDir,
			Features:   engine.Features{BackwardsCompatible: true},
		}
		var res engine.Result
		mrb.Blocking(func() {
			res = engine.TransformContext(mrb.GoContext(), req)
		})
		if res.HasErrors() {
			for _, d := range res.Diagnostics {
				if d.Severity == engine.SeverityError {
					return mrb.Raise(mrb.ERuntimeError(), fmt.Sprintf("XSLT error (%s): %s", d.Code, d.Message))
				}
			}
		}
		outStr := res.Output
		if ss.hasExtCall {
			regURIs := registeredXSLTExtURIs(mrb, xsltMod)
			var err error
			outStr, err = resolveXSLTExtCalls(mrb, outStr, regURIs, res.Method)
			if err != nil {
				return mrb.RaiseError(err)
			}
		}
		pair := mrb.AryNewCapa(2)
		mrb.AryPush(pair, mrb.StrNew(outStr))
		mrb.AryPush(pair, mrb.StrNew(res.Method))
		return pair
	}, mrb.ArgsArg(1, 1))
}
