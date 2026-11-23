package nokogiri

import (
	"fmt"
	"os"
	"path/filepath"
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
	source  string
	baseDir string
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
		// Validate compilation immediately so bad stylesheets raise RuntimeError like Nokogiri::XSLT
		_, diags := engine.Compile(engine.Request{
			Stylesheet: src,
			BaseDir:    baseDir,
			Features:   engine.Features{BackwardsCompatible: true},
		})
		for _, d := range diags {
			if d.Severity == engine.SeverityError {
				return mrb.Raise(mrb.ERuntimeError(), fmt.Sprintf("XSLT compile error (%s): %s", d.Code, d.Message))
			}
		}
		obj := mrb.ObjAlloc(oruby.MrbTTCData, ssCls)
		rb.Set(mrb, obj.Value(), &xsltStylesheetData{source: src, baseDir: baseDir})
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
		pair := mrb.AryNewCapa(2)
		mrb.AryPush(pair, mrb.StrNew(res.Output))
		mrb.AryPush(pair, mrb.StrNew(res.Method))
		return pair
	}, mrb.ArgsArg(1, 1))
}
