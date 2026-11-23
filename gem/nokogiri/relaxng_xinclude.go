package nokogiri

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/internal/rb"
)

// ---------------------------------------------------------------------------
// XPath Custom Ruby Handler Evaluation
// ---------------------------------------------------------------------------

// evalXPathWithHandler evaluates XPath expressions that invoke custom Ruby
// handler methods (e.g., `doc.xpath("//li[has_class(., 'active')]", nil, handler)`
// or `doc.xpath("custom_fn(//p, 'foo')", handler)`).
func evalXPathWithHandler(mrb *oruby.MrbState, ctx *domNode, expr string, nsMap map[string]string, vars map[string]any, handler oruby.Value) (oruby.Value, bool, error) {
	if handler.IsNil() {
		return mrb.NilValue(), false, nil
	}
	trimmed := strings.TrimSpace(expr)

	// Case 1: Top-level custom function call `fn(arg1, arg2, ...)`
	if fnName, argsRaw, ok := parseTopLevelCall(trimmed); ok && !isStdXPathFunc(fnName) && mrb.RespondTo(handler, mrb.Intern(fnName)) {
		res, err := invokeXPathHandler(mrb, ctx, nsMap, vars, handler, fnName, argsRaw)
		return res, true, err
	}

	// Case 2: Trailing predicate calling a custom function: `<base>[<pred>]`
	// Also handles `<base>[not(fn(...))]` and chained predicates `<base>[p1][fn(...)]`.
	if base, pred, ok := splitTrailingPredicate(trimmed); ok {
		neg := false
		inner := strings.TrimSpace(pred)
		if strings.HasPrefix(inner, "not(") && strings.HasSuffix(inner, ")") {
			neg = true
			inner = strings.TrimSpace(inner[4 : len(inner)-1])
		}
		if fnName, argsRaw, ok := parseTopLevelCall(inner); ok && !isStdXPathFunc(fnName) && mrb.RespondTo(handler, mrb.Intern(fnName)) {
			// Evaluate base (recursively in case base itself has a handler predicate)
			var candidates []*domNode
			if subVal, handled, err := evalXPathWithHandler(mrb, ctx, base, nsMap, vars, handler); err != nil {
				return mrb.NilValue(), true, err
			} else if handled {
				if ns, ok := getNodeSet(mrb, subVal); ok {
					candidates = ns.nodes
				}
			} else {
				res, err := evalXPath(ctx, base, nsMap, vars)
				if err != nil {
					return mrb.NilValue(), true, err
				}
				if res.isSet {
					candidates = res.nodes
				}
			}
			var matched []*domNode
			for _, cand := range candidates {
				rv, err := invokeXPathHandler(mrb, cand, nsMap, vars, handler, fnName, argsRaw)
				if err != nil {
					return mrb.NilValue(), true, err
				}
				truthy := isXPathTruthy(mrb, rv)
				if neg {
					truthy = !truthy
				}
				if truthy {
					matched = append(matched, cand)
				}
			}
			return wrapNodeSet(mrb, ctx.doc, matched), true, nil
		}
	}

	// Case 3: Mid-path predicate calling a custom function: `<prefix>[<custom_pred>]<suffix>`
	// e.g. `//a[eq(b, '2')]/b` or `//div[has_class(., 'x')]//span`.
	if prefix, fnName, argsRaw, neg, suffix, ok := findCustomPredicate(mrb, trimmed, handler); ok {
		var candidates []*domNode
		if subVal, handled, err := evalXPathWithHandler(mrb, ctx, prefix, nsMap, vars, handler); err != nil {
			return mrb.NilValue(), true, err
		} else if handled {
			if ns, ok := getNodeSet(mrb, subVal); ok {
				candidates = ns.nodes
			}
		} else {
			res, err := evalXPath(ctx, prefix, nsMap, vars)
			if err != nil {
				return mrb.NilValue(), true, err
			}
			if res.isSet {
				candidates = res.nodes
			}
		}
		var matched []*domNode
		for _, cand := range candidates {
			rv, err := invokeXPathHandler(mrb, cand, nsMap, vars, handler, fnName, argsRaw)
			if err != nil {
				return mrb.NilValue(), true, err
			}
			truthy := isXPathTruthy(mrb, rv)
			if neg {
				truthy = !truthy
			}
			if truthy {
				matched = append(matched, cand)
			}
		}
		if suffix == "" {
			return wrapNodeSet(mrb, ctx.doc, matched), true, nil
		}
		var out []*domNode
		seen := map[*domNode]bool{}
		subExpr := suffix
		if strings.HasPrefix(subExpr, "/") {
			subExpr = "." + subExpr
		} else if strings.HasPrefix(subExpr, "[") {
			subExpr = "." + subExpr
		}
		for _, m := range matched {
			if subVal, handled, err := evalXPathWithHandler(mrb, m, subExpr, nsMap, vars, handler); err != nil {
				return mrb.NilValue(), true, err
			} else if handled {
				if ns, ok := getNodeSet(mrb, subVal); ok {
					for _, n := range ns.nodes {
						if !seen[n] {
							seen[n] = true
							out = append(out, n)
						}
					}
				}
			} else {
				res, err := evalXPath(m, subExpr, nsMap, vars)
				if err != nil {
					return mrb.NilValue(), true, err
				}
				if res.isSet {
					for _, n := range res.nodes {
						if !seen[n] {
							seen[n] = true
							out = append(out, n)
						}
					}
				}
			}
		}
		return wrapNodeSet(mrb, ctx.doc, out), true, nil
	}

	return mrb.NilValue(), false, nil
}

func findCustomPredicate(mrb *oruby.MrbState, s string, handler oruby.Value) (prefix, fnName, argsRaw string, neg bool, suffix string, ok bool) {
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		if c == '[' {
			closeIdx := findMatching(s, i, '[', ']')
			if closeIdx > i {
				pred := strings.TrimSpace(s[i+1 : closeIdx])
				isNeg := false
				inner := pred
				if strings.HasPrefix(inner, "not(") && strings.HasSuffix(inner, ")") {
					isNeg = true
					inner = strings.TrimSpace(inner[4 : len(inner)-1])
				}
				if fn, args, parsed := parseTopLevelCall(inner); parsed && !isStdXPathFunc(fn) && mrb.RespondTo(handler, mrb.Intern(fn)) {
					return strings.TrimSpace(s[:i]), fn, args, isNeg, strings.TrimSpace(s[closeIdx+1:]), true
				}
			}
		}
	}
	return "", "", "", false, "", false
}

func isXPathTruthy(mrb *oruby.MrbState, v oruby.Value) bool {
	if v.IsNil() || !v.Bool() {
		return false
	}
	if ns, ok := getNodeSet(mrb, v); ok {
		return len(ns.nodes) > 0
	}
	if v.IsArray() {
		return oruby.RArrayLen(v) > 0
	}
	if v.IsString() {
		return v.String() != ""
	}
	if v.IsInt() {
		return v.Int64() != 0
	}
	if v.IsFloat() {
		return v.Float64() != 0
	}
	return true
}

func invokeXPathHandler(mrb *oruby.MrbState, ctx *domNode, nsMap map[string]string, vars map[string]any, handler oruby.Value, fnName, argsRaw string) (oruby.Value, error) {
	var rubyArgs []any
	if strings.TrimSpace(argsRaw) != "" {
		argExprs := splitTopLevel(argsRaw, ',')
		for _, ae := range argExprs {
			ae = strings.TrimSpace(ae)
			if ae == "." {
				rubyArgs = append(rubyArgs, wrapNodeSet(mrb, ctx.doc, []*domNode{ctx}))
				continue
			}
			// Quoted string literal fast path
			if len(ae) >= 2 && ((ae[0] == '\'' && ae[len(ae)-1] == '\'') || (ae[0] == '"' && ae[len(ae)-1] == '"')) {
				rubyArgs = append(rubyArgs, mrb.StrNew(ae[1:len(ae)-1]))
				continue
			}
			res, err := evalXPath(ctx, ae, nsMap, vars)
			if err != nil {
				return mrb.NilValue(), err
			}
			if res.isSet {
				rubyArgs = append(rubyArgs, wrapNodeSet(mrb, ctx.doc, res.nodes))
			} else {
				switch v := res.val.(type) {
				case bool:
					rubyArgs = append(rubyArgs, mrb.BoolValue(v))
				case float64:
					rubyArgs = append(rubyArgs, mrb.FloatValue(v))
				case string:
					rubyArgs = append(rubyArgs, mrb.StrNew(v))
				default:
					rubyArgs = append(rubyArgs, mrb.NilValue())
				}
			}
		}
	}
	return mrb.Funcall(handler, mrb.Intern(fnName), rubyArgs...)
}

func parseTopLevelCall(s string) (name, args string, ok bool) {
	open := strings.IndexByte(s, '(')
	if open <= 0 || !strings.HasSuffix(s, ")") {
		return "", "", false
	}
	// Make sure the opening '(' matches the final ')'
	if findMatching(s, open, '(', ')') != len(s)-1 {
		return "", "", false
	}
	name = strings.TrimSpace(s[:open])
	if name == "" {
		return "", "", false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-') {
			return "", "", false
		}
	}
	return name, s[open+1 : len(s)-1], true
}

func splitTrailingPredicate(s string) (base, pred string, ok bool) {
	if !strings.HasSuffix(s, "]") {
		return "", "", false
	}
	depth := 0
	var quote byte
	openIdx := -1
	for i := len(s) - 1; i >= 0; i-- {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		if c == ']' {
			depth++
		} else if c == '[' {
			depth--
			if depth == 0 {
				openIdx = i
				break
			}
		}
	}
	if openIdx <= 0 {
		return "", "", false
	}
	return strings.TrimSpace(s[:openIdx]), s[openIdx+1 : len(s)-1], true
}

var stdXPathFuncs = map[string]bool{
	"last": true, "position": true, "count": true, "id": true,
	"local-name": true, "namespace-uri": true, "name": true,
	"string": true, "concat": true, "starts-with": true, "contains": true,
	"substring-before": true, "substring-after": true, "substring": true,
	"string-length": true, "normalize-space": true, "translate": true,
	"boolean": true, "not": true, "true": true, "false": true, "lang": true,
	"number": true, "sum": true, "floor": true, "ceiling": true, "round": true,
	"matches": true, "ends-with": true, "lower-case": true, "upper-case": true,
}

func isStdXPathFunc(name string) bool { return stdXPathFuncs[name] }

// ---------------------------------------------------------------------------
// XInclude (http://www.w3.org/2001/XInclude)
// ---------------------------------------------------------------------------

const xincludeNS = "http://www.w3.org/2001/XInclude"

func doXInclude(root *domNode, baseDir string) error {
	if baseDir == "" && root.doc != nil && root.doc.url != "" {
		baseDir = filepath.Dir(root.doc.url)
	}
	var includes []*domNode
	var collect func(n *domNode)
	collect = func(n *domNode) {
		for c := n.first; c != nil; c = c.next {
			if c.kind == ElementNode && c.name == "include" && (c.nsURI == xincludeNS || c.prefix == "xi") {
				includes = append(includes, c)
			} else {
				collect(c)
			}
		}
	}
	collect(root)

	for _, inc := range includes {
		href := ""
		if a, ok := inc.getAttr("href"); ok {
			href = a.value
		}
		parseMode := "xml"
		if a, ok := inc.getAttr("parse"); ok && a.value != "" {
			parseMode = strings.ToLower(a.value)
		}
		targetPath := href
		if baseDir != "" && !filepath.IsAbs(href) {
			targetPath = filepath.Join(baseDir, href)
		}
		data, err := os.ReadFile(targetPath)
		if err != nil {
			// Check for <xi:fallback>
			var fallback *domNode
			for c := inc.first; c != nil; c = c.next {
				if c.kind == ElementNode && c.name == "fallback" && (c.nsURI == xincludeNS || c.prefix == "xi") {
					fallback = c
					break
				}
			}
			if fallback != nil {
				frag := newFragment(inc.doc)
				for c := fallback.first; c != nil; {
					nxt := c.next
					c.unlink()
					frag.appendChild(c)
					c = nxt
				}
				inc.replaceWith(frag)
				continue
			}
			return fmt.Errorf("XInclude could not load %q: %w", href, err)
		}
		if parseMode == "text" {
			txt := newNode(inc.doc, TextNode, "#text", string(data))
			inc.replaceWith(txt)
		} else {
			subDoc := parseXMLDocument(string(data), targetPath, "UTF-8", false, false, false)
			if err := doXInclude(subDoc, filepath.Dir(targetPath)); err != nil {
				return err
			}
			if el := subDoc.rootElement(); el != nil {
				cp := el.clone(true, inc.doc)
				inc.replaceWith(cp)
			} else {
				inc.unlink()
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// RelaxNG Validator (Pure-Go XML syntax RelaxNG)
// ---------------------------------------------------------------------------

type rngGrammar struct {
	start   *domNode
	defines map[string]*domNode
	ns      string
}

func compileRelaxNG(src string) (*rngGrammar, error) {
	doc := parseXMLDocument(src, "", "UTF-8", true, true, false)
	if len(doc.errors) > 0 {
		return nil, fmt.Errorf("RelaxNG parse error: %s", doc.errors[0].message)
	}
	root := doc.rootElement()
	if root == nil {
		return nil, fmt.Errorf("empty RelaxNG schema")
	}
	g := &rngGrammar{defines: map[string]*domNode{}}
	if a, ok := root.getAttr("ns"); ok {
		g.ns = a.value
	}
	if root.name == "grammar" {
		for c := root.first; c != nil; c = c.next {
			if c.kind != ElementNode {
				continue
			}
			switch c.name {
			case "start":
				g.start = firstElemChild(c)
			case "define":
				if na, ok := c.getAttr("name"); ok {
					g.defines[na.value] = c
				}
			}
		}
	} else {
		g.start = root
	}
	if g.start == nil {
		return nil, fmt.Errorf("RelaxNG schema has no start pattern")
	}
	return g, nil
}

func firstElemChild(n *domNode) *domNode {
	for c := n.first; c != nil; c = c.next {
		if c.kind == ElementNode {
			return c
		}
	}
	return nil
}

func elemChildren(n *domNode) []*domNode {
	var out []*domNode
	for c := n.first; c != nil; c = c.next {
		if c.kind == ElementNode {
			out = append(out, c)
		}
	}
	return out
}

func (g *rngGrammar) validate(doc *domNode) []domError {
	root := doc
	if doc.kind == DocumentNode || doc.kind == HTMLDocumentNode {
		root = doc.rootElement()
	}
	if root == nil {
		return []domError{{message: "document has no root element", level: 2}}
	}
	var errs []domError
	if !g.matchSingleNode(g.start, root, g.ns, &errs) {
		if len(errs) == 0 {
			errs = append(errs, domError{message: fmt.Sprintf("element <%s> did not match RelaxNG schema", root.name), line: root.line, level: 2})
		}
	}
	return errs
}

func (g *rngGrammar) matchSingleNode(pat, node *domNode, inheritedNS string, errs *[]domError) bool {
	if pat == nil || node == nil {
		return false
	}
	if a, ok := pat.getAttr("ns"); ok {
		inheritedNS = a.value
	}
	switch pat.name {
	case "ref":
		if na, ok := pat.getAttr("name"); ok {
			if def, exists := g.defines[na.value]; exists {
				return g.matchSingleNode(firstElemChild(def), node, inheritedNS, errs)
			}
		}
		return false
	case "choice":
		for _, ch := range elemChildren(pat) {
			var sub []domError
			if g.matchSingleNode(ch, node, inheritedNS, &sub) {
				return true
			}
		}
		*errs = append(*errs, domError{message: fmt.Sprintf("element <%s> matched none of RelaxNG <choice> alternatives", node.name), line: node.line, level: 2})
		return false
	case "element":
		wantName := ""
		if na, ok := pat.getAttr("name"); ok {
			wantName = na.value
		}
		bodyPats := elemChildren(pat)
		if wantName == "" && len(bodyPats) > 0 {
			// Name class child (<name>, <anyName>, <nsName>)
			nc := bodyPats[0]
			bodyPats = bodyPats[1:]
			switch nc.name {
			case "name":
				wantName = strings.TrimSpace(nc.innerText())
			case "anyName":
				wantName = "*"
			case "nsName":
				wantName = "*"
			}
		}
		if wantName != "*" && node.name != wantName {
			*errs = append(*errs, domError{message: fmt.Sprintf("expected element <%s>, got <%s>", wantName, node.name), line: node.line, level: 2})
			return false
		}
		if inheritedNS != "" && node.nsURI != inheritedNS {
			*errs = append(*errs, domError{message: fmt.Sprintf("element <%s> expected namespace %q, got %q", node.name, inheritedNS, node.nsURI), line: node.line, level: 2})
			return false
		}
		return g.matchContent(bodyPats, node, inheritedNS, errs)
	default:
		return false
	}
}

func (g *rngGrammar) matchContent(pats []*domNode, elem *domNode, ns string, errs *[]domError) bool {
	// Separate attribute patterns from child-element/text patterns
	var attrPats, childPats []*domNode
	var collect func(list []*domNode)
	collect = func(list []*domNode) {
		for _, p := range list {
			switch p.name {
			case "attribute":
				attrPats = append(attrPats, p)
			case "optional":
				ch := elemChildren(p)
				if len(ch) == 1 && ch[0].name == "attribute" {
					// optional attribute
					attrPats = append(attrPats, p)
				} else {
					childPats = append(childPats, p)
				}
			case "group":
				collect(elemChildren(p))
			case "ref":
				if na, ok := p.getAttr("name"); ok {
					if def, ok2 := g.defines[na.value]; ok2 {
						ch := elemChildren(def)
						if len(ch) == 1 && ch[0].name == "element" {
							childPats = append(childPats, p)
						} else {
							collect(ch)
						}
					}
				}
			default:
				childPats = append(childPats, p)
			}
		}
	}
	collect(pats)

	// Validate attributes
	allowedAttrs := map[string]bool{}
	for _, ap := range attrPats {
		optional := false
		actual := ap
		if ap.name == "optional" {
			optional = true
			actual = firstElemChild(ap)
		}
		if actual == nil {
			continue
		}
		aname := ""
		if a, ok := actual.getAttr("name"); ok {
			aname = a.value
		}
		if aname != "" {
			allowedAttrs[aname] = true
			av, exists := elem.getAttr(aname)
			if !exists {
				if !optional {
					*errs = append(*errs, domError{message: fmt.Sprintf("missing required attribute %q on <%s>", aname, elem.name), line: elem.line, level: 2})
					return false
				}
			} else if valPat := firstElemChild(actual); valPat != nil {
				if !matchRNGTextValue(valPat, av.value) {
					*errs = append(*errs, domError{message: fmt.Sprintf("invalid value %q for attribute %q on <%s>", av.value, aname, elem.name), line: elem.line, level: 2})
					return false
				}
			}
		}
	}

	// Validate child content
	if len(childPats) == 0 {
		return true
	}
	if len(childPats) == 1 {
		switch childPats[0].name {
		case "text":
			return true
		case "empty":
			if len(elemChildren(elem)) > 0 || strings.TrimSpace(elem.innerText()) != "" {
				*errs = append(*errs, domError{message: fmt.Sprintf("element <%s> must be empty", elem.name), line: elem.line, level: 2})
				return false
			}
			return true
		case "data", "value":
			if !matchRNGTextValue(childPats[0], strings.TrimSpace(elem.innerText())) {
				*errs = append(*errs, domError{message: fmt.Sprintf("invalid text content %q in <%s>", strings.TrimSpace(elem.innerText()), elem.name), line: elem.line, level: 2})
				return false
			}
			return true
		}
	}

	// Match child elements sequentially against childPats
	children := elemChildren(elem)
	idx := 0
	for _, cp := range childPats {
		switch cp.name {
		case "text", "empty":
			continue
		case "optional":
			inner := firstElemChild(cp)
			if idx < len(children) && inner != nil {
				var sub []domError
				if g.matchSingleNode(inner, children[idx], ns, &sub) {
					idx++
				}
			}
		case "zeroOrMore":
			inner := firstElemChild(cp)
			for idx < len(children) && inner != nil {
				var sub []domError
				if !g.matchSingleNode(inner, children[idx], ns, &sub) {
					break
				}
				idx++
			}
		case "oneOrMore":
			inner := firstElemChild(cp)
			count := 0
			for idx < len(children) && inner != nil {
				var sub []domError
				if !g.matchSingleNode(inner, children[idx], ns, &sub) {
					if count == 0 {
						*errs = append(*errs, sub...)
					}
					break
				}
				idx++
				count++
			}
			if count == 0 {
				*errs = append(*errs, domError{message: fmt.Sprintf("expected at least one matching child in <%s>", elem.name), line: elem.line, level: 2})
				return false
			}
		case "interleave":
			// Match all sub-patterns in any order
			rem := append([]*domNode(nil), elemChildren(cp)...)
			for len(rem) > 0 && idx < len(children) {
				matched := false
				for ri, rp := range rem {
					var sub []domError
					if g.matchSingleNode(rp, children[idx], ns, &sub) {
						rem = append(rem[:ri], rem[ri+1:]...)
						idx++
						matched = true
						break
					}
				}
				if !matched {
					break
				}
			}
			for _, rp := range rem {
				if rp.name != "optional" && rp.name != "zeroOrMore" {
					*errs = append(*errs, domError{message: fmt.Sprintf("missing interleaved child in <%s>", elem.name), line: elem.line, level: 2})
					return false
				}
			}
		default:
			if idx >= len(children) {
				*errs = append(*errs, domError{message: fmt.Sprintf("missing expected child element in <%s>", elem.name), line: elem.line, level: 2})
				return false
			}
			if !g.matchSingleNode(cp, children[idx], ns, errs) {
				return false
			}
			idx++
		}
	}
	if idx < len(children) {
		*errs = append(*errs, domError{message: fmt.Sprintf("unexpected extra element <%s> inside <%s>", children[idx].name, elem.name), line: children[idx].line, level: 2})
		return false
	}
	return true
}

func matchRNGTextValue(pat *domNode, val string) bool {
	switch pat.name {
	case "text":
		return true
	case "value":
		return val == strings.TrimSpace(pat.innerText())
	case "data":
		t := ""
		if a, ok := pat.getAttr("type"); ok {
			t = a.value
		}
		switch t {
		case "integer", "int", "long", "short":
			_, err := strconv.ParseInt(val, 10, 64)
			return err == nil
		case "positiveInteger":
			n, err := strconv.ParseInt(val, 10, 64)
			return err == nil && n > 0
		case "decimal", "float", "double":
			_, err := strconv.ParseFloat(val, 64)
			return err == nil
		case "boolean":
			return val == "true" || val == "false" || val == "1" || val == "0"
		default:
			return true
		}
	case "choice":
		for _, ch := range elemChildren(pat) {
			if matchRNGTextValue(ch, val) {
				return true
			}
		}
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// DTD Validation (<!ELEMENT> & <!ATTLIST>)
// ---------------------------------------------------------------------------

type dtdElementDecl struct {
	name     string
	model    string
	allowed  map[string]bool
	anyChild bool
	empty    bool
}

type dtdAttrDecl struct {
	elem     string
	name     string
	required bool
}

var (
	reDTDElem   = regexp.MustCompile(`<!ELEMENT\s+([^\s>]+)\s+([^>]+)>`)
	reDTDAttr   = regexp.MustCompile(`<!ATTLIST\s+([^\s>]+)\s+([^>]+)>`)
	reDTDEntity = regexp.MustCompile(`<!ENTITY\s+([^\s>]+)\s+["']([^"']*)["']\s*>`)
)

func extractDTDElements(dtdText string) map[string]string {
	out := map[string]string{}
	for _, m := range reDTDElem.FindAllStringSubmatch(dtdText, -1) {
		out[m[1]] = strings.TrimSpace(m[2])
	}
	return out
}

func extractDTDEntities(dtdText string) map[string]string {
	out := map[string]string{}
	for _, m := range reDTDEntity.FindAllStringSubmatch(dtdText, -1) {
		out[m[1]] = m[2]
	}
	return out
}

func validateAgainstDTD(dtdText string, doc *domNode) []domError {
	elems := map[string]*dtdElementDecl{}
	for _, m := range reDTDElem.FindAllStringSubmatch(dtdText, -1) {
		name := m[1]
		model := strings.TrimSpace(m[2])
		decl := &dtdElementDecl{name: name, model: model, allowed: map[string]bool{}}
		if model == "ANY" {
			decl.anyChild = true
		} else if model == "EMPTY" {
			decl.empty = true
		} else {
			clean := strings.NewReplacer("(", " ", ")", " ", ",", " ", "|", " ", "*", " ", "+", " ", "?", " ").Replace(model)
			for _, tok := range strings.Fields(clean) {
				if tok != "#PCDATA" {
					decl.allowed[tok] = true
				}
			}
		}
		elems[name] = decl
	}
	var reqAttrs []dtdAttrDecl
	for _, m := range reDTDAttr.FindAllStringSubmatch(dtdText, -1) {
		elName := m[1]
		fields := strings.Fields(m[2])
		if len(fields) >= 3 {
			reqAttrs = append(reqAttrs, dtdAttrDecl{
				elem:     elName,
				name:     fields[0],
				required: fields[2] == "#REQUIRED",
			})
		}
	}

	var errs []domError
	var walk func(n *domNode)
	walk = func(n *domNode) {
		if n.kind == ElementNode {
			if len(elems) > 0 {
				decl, ok := elems[n.name]
				if !ok {
					errs = append(errs, domError{message: fmt.Sprintf("no DTD declaration for element <%s>", n.name), line: n.line, level: 2})
				} else {
					if decl.empty && len(elemChildren(n)) > 0 {
						errs = append(errs, domError{message: fmt.Sprintf("element <%s> is declared EMPTY", n.name), line: n.line, level: 2})
					} else if !decl.anyChild {
						for _, ch := range elemChildren(n) {
							if !decl.allowed[ch.name] {
								errs = append(errs, domError{message: fmt.Sprintf("element <%s> is not allowed inside <%s>", ch.name, n.name), line: ch.line, level: 2})
							}
						}
					}
				}
			}
			for _, ra := range reqAttrs {
				if ra.elem == n.name && ra.required {
					if _, has := n.getAttr(ra.name); !has {
						errs = append(errs, domError{message: fmt.Sprintf("element <%s> missing #REQUIRED attribute %q", n.name, ra.name), line: n.line, level: 2})
					}
				}
			}
		}
		for c := n.first; c != nil; c = c.next {
			walk(c)
		}
	}
	walk(doc)
	return errs
}

// ---------------------------------------------------------------------------
// Streaming XML SAX helper (token-by-token via encoding/xml)
// ---------------------------------------------------------------------------

// emitStreamingXMLSAX parses XML incrementally using Go's streaming xml.NewDecoder
// for token-by-token SAX event emission without building a DOM tree.
func emitStreamingXMLSAX(mrb *oruby.MrbState, handler oruby.Value, src string) error {
	call := func(name string, args ...any) error {
		if !mrb.RespondTo(handler, mrb.Intern(name)) {
			return nil
		}
		_, err := mrb.Funcall(handler, mrb.Intern(name), args...)
		return err
	}
	if err := call("start_document"); err != nil {
		return err
	}
	dec := xml.NewDecoder(strings.NewReader(src))
	dec.Strict = false
	attrCls := rb.ClassPath(mrb, "Nokogiri::XML::SAX::Parser::Attribute")
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			_ = call("error", mrb.StrNew(err.Error()))
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			attrsAry := mrb.AryNewCapa(len(t.Attr))
			nsAttrsAry := mrb.AryNewCapa(len(t.Attr))
			nsAry := mrb.AryNew()
			for _, a := range t.Attr {
				ak := a.Name.Local
				if a.Name.Space != "" && a.Name.Space != "xmlns" {
					ak = a.Name.Space + ":" + a.Name.Local
				} else if a.Name.Space == "xmlns" {
					ak = "xmlns:" + a.Name.Local
					pair := mrb.AryNewCapa(2)
					mrb.AryPush(pair, mrb.StrNew(a.Name.Local))
					mrb.AryPush(pair, mrb.StrNew(a.Value))
					mrb.AryPush(nsAry, pair)
				} else if a.Name.Local == "xmlns" {
					pair := mrb.AryNewCapa(2)
					mrb.AryPush(pair, mrb.NilValue())
					mrb.AryPush(pair, mrb.StrNew(a.Value))
					mrb.AryPush(nsAry, pair)
				}
				p := mrb.AryNewCapa(2)
				mrb.AryPush(p, mrb.StrNew(ak))
				mrb.AryPush(p, mrb.StrNew(a.Value))
				mrb.AryPush(attrsAry, p)
				if a.Name.Local != "xmlns" && a.Name.Space != "xmlns" && !attrCls.IsNil() {
					var au oruby.Value = mrb.NilValue()
					if a.Name.Space != "" {
						au = mrb.StrNew(a.Name.Space)
					}
					if obj, err := mrb.Funcall(attrCls, mrb.Intern("new"), mrb.StrNew(a.Name.Local), mrb.NilValue(), au, mrb.StrNew(a.Value)); err == nil {
						mrb.AryPush(nsAttrsAry, obj)
					}
				}
			}
			var uriVal oruby.Value = mrb.NilValue()
			if t.Name.Space != "" {
				uriVal = mrb.StrNew(t.Name.Space)
			}
			if err := call("start_element_namespace", mrb.StrNew(t.Name.Local), nsAttrsAry, mrb.NilValue(), uriVal, nsAry); err != nil {
				return err
			}
			if err := call("start_element", mrb.StrNew(t.Name.Local), attrsAry); err != nil {
				return err
			}
		case xml.EndElement:
			var uriVal oruby.Value = mrb.NilValue()
			if t.Name.Space != "" {
				uriVal = mrb.StrNew(t.Name.Space)
			}
			if err := call("end_element_namespace", mrb.StrNew(t.Name.Local), mrb.NilValue(), uriVal); err != nil {
				return err
			}
			if err := call("end_element", mrb.StrNew(t.Name.Local)); err != nil {
				return err
			}
		case xml.CharData:
			if len(t) > 0 {
				if err := call("characters", mrb.StrNew(string(t))); err != nil {
					return err
				}
			}
		case xml.Comment:
			if err := call("comment", mrb.StrNew(string(t))); err != nil {
				return err
			}
		case xml.ProcInst:
			if t.Target == "xml" {
				_ = call("xmldecl", mrb.StrNew("1.0"), mrb.StrNew("UTF-8"), mrb.NilValue())
			} else if err := call("processing_instruction", mrb.StrNew(t.Target), mrb.StrNew(string(t.Inst))); err != nil {
				return err
			}
		}
	}
	return call("end_document")
}

// ---------------------------------------------------------------------------
// True Incremental XML PushParser (emits SAX events on each `<<` chunk)
// ---------------------------------------------------------------------------

type xmlPushScanner struct {
	buf     string
	started bool
	ended   bool
	nsStack []map[string]string
}

func newXMLPushScanner() *xmlPushScanner {
	return &xmlPushScanner{
		nsStack: []map[string]string{{"xml": "http://www.w3.org/XML/1998/namespace"}},
	}
}

func (s *xmlPushScanner) lookupNS(prefix string) string {
	for i := len(s.nsStack) - 1; i >= 0; i-- {
		if uri, ok := s.nsStack[i][prefix]; ok {
			return uri
		}
	}
	return ""
}

func (s *xmlPushScanner) write(mrb *oruby.MrbState, handler oruby.Value, chunk string) error {
	call := func(name string, args ...any) error {
		if !mrb.RespondTo(handler, mrb.Intern(name)) {
			return nil
		}
		_, err := mrb.Funcall(handler, mrb.Intern(name), args...)
		return err
	}
	if !s.started {
		s.started = true
		if err := call("start_document"); err != nil {
			return err
		}
	}
	s.buf += chunk
	return s.drain(mrb, handler, call, false)
}

func (s *xmlPushScanner) finish(mrb *oruby.MrbState, handler oruby.Value) error {
	call := func(name string, args ...any) error {
		if !mrb.RespondTo(handler, mrb.Intern(name)) {
			return nil
		}
		_, err := mrb.Funcall(handler, mrb.Intern(name), args...)
		return err
	}
	if !s.started {
		s.started = true
		if err := call("start_document"); err != nil {
			return err
		}
	}
	if err := s.drain(mrb, handler, call, true); err != nil {
		return err
	}
	if !s.ended {
		s.ended = true
		return call("end_document")
	}
	return nil
}

func (s *xmlPushScanner) drain(mrb *oruby.MrbState, handler oruby.Value, call func(string, ...any) error, eof bool) error {
	attrCls := rb.ClassPath(mrb, "Nokogiri::XML::SAX::Parser::Attribute")
	for len(s.buf) > 0 {
		lt := strings.IndexByte(s.buf, '<')
		if lt < 0 {
			if eof {
				txt := decodeXMLEntities(s.buf)
				s.buf = ""
				if txt != "" {
					if err := call("characters", mrb.StrNew(txt)); err != nil {
						return err
					}
				}
			} else {
				// Emit all text up to any trailing '&' (potential partial entity)
				cut := len(s.buf)
				if amp := strings.LastIndexByte(s.buf, '&'); amp >= 0 && len(s.buf)-amp < 12 {
					cut = amp
				}
				if cut > 0 {
					txt := decodeXMLEntities(s.buf[:cut])
					s.buf = s.buf[cut:]
					if txt != "" {
						if err := call("characters", mrb.StrNew(txt)); err != nil {
							return err
						}
					}
				}
			}
			return nil
		}
		if lt > 0 {
			txt := decodeXMLEntities(s.buf[:lt])
			s.buf = s.buf[lt:]
			if txt != "" {
				if err := call("characters", mrb.StrNew(txt)); err != nil {
					return err
				}
			}
		}

		// Now s.buf[0] == '<'
		if len(s.buf) < 2 {
			return nil
		}
		// Comment
		if strings.HasPrefix(s.buf, "<!--") {
			end := strings.Index(s.buf[4:], "-->")
			if end < 0 {
				return nil
			}
			body := s.buf[4 : 4+end]
			s.buf = s.buf[4+end+3:]
			if err := call("comment", mrb.StrNew(body)); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix("<!--", s.buf) {
			return nil
		}
		// CDATA
		if strings.HasPrefix(s.buf, "<![CDATA[") {
			end := strings.Index(s.buf[9:], "]]>")
			if end < 0 {
				return nil
			}
			body := s.buf[9 : 9+end]
			s.buf = s.buf[9+end+3:]
			if err := call("cdata_block", mrb.StrNew(body)); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix("<![CDATA[", s.buf) {
			return nil
		}
		// Processing instruction / XML decl
		if strings.HasPrefix(s.buf, "<?") {
			end := strings.Index(s.buf[2:], "?>")
			if end < 0 {
				return nil
			}
			inner := strings.TrimSpace(s.buf[2 : 2+end])
			s.buf = s.buf[2+end+2:]
			target := inner
			content := ""
			if sp := strings.IndexAny(inner, " \t\r\n"); sp >= 0 {
				target = inner[:sp]
				content = strings.TrimSpace(inner[sp+1:])
			}
			if target == "xml" {
				if err := call("xmldecl", mrb.StrNew("1.0"), mrb.StrNew("UTF-8"), mrb.NilValue()); err != nil {
					return err
				}
			} else {
				if err := call("processing_instruction", mrb.StrNew(target), mrb.StrNew(content)); err != nil {
					return err
				}
			}
			continue
		}
		// DOCTYPE / declaration
		if strings.HasPrefix(s.buf, "<!") {
			inBracket := false
			end := -1
			for i := 2; i < len(s.buf); i++ {
				if s.buf[i] == '[' {
					inBracket = true
				} else if s.buf[i] == ']' {
					inBracket = false
				} else if s.buf[i] == '>' && !inBracket {
					end = i
					break
				}
			}
			if end < 0 {
				return nil
			}
			s.buf = s.buf[end+1:]
			continue
		}
		// Closing tag </name>
		if strings.HasPrefix(s.buf, "</") {
			gt := strings.IndexByte(s.buf, '>')
			if gt < 0 {
				return nil
			}
			qname := strings.TrimSpace(s.buf[2:gt])
			s.buf = s.buf[gt+1:]
			pref, local := splitQName(qname)
			uri := s.lookupNS(pref)
			var prefVal, uriVal oruby.Value = mrb.NilValue(), mrb.NilValue()
			if pref != "" {
				prefVal = mrb.StrNew(pref)
			}
			if uri != "" {
				uriVal = mrb.StrNew(uri)
			}
			if err := call("end_element_namespace", mrb.StrNew(local), prefVal, uriVal); err != nil {
				return err
			}
			if err := call("end_element", mrb.StrNew(qname)); err != nil {
				return err
			}
			if len(s.nsStack) > 1 {
				s.nsStack = s.nsStack[:len(s.nsStack)-1]
			}
			continue
		}
		// Opening or self-closing tag <name ...>
		gt := findTagEnd(s.buf)
		if gt < 0 {
			return nil
		}
		rawTag := s.buf[1:gt]
		s.buf = s.buf[gt+1:]
		selfClosing := false
		trimmed := strings.TrimSpace(rawTag)
		if strings.HasSuffix(trimmed, "/") {
			selfClosing = true
			trimmed = strings.TrimSpace(trimmed[:len(trimmed)-1])
		}
		qname, attrs := parseTagAttrs(trimmed)
		scope := map[string]string{}
		nsAry := mrb.AryNew()
		attrsAry := mrb.AryNewCapa(len(attrs))
		nsAttrsAry := mrb.AryNew()

		for _, kv := range attrs {
			p := mrb.AryNewCapa(2)
			mrb.AryPush(p, mrb.StrNew(kv[0]))
			mrb.AryPush(p, mrb.StrNew(kv[1]))
			mrb.AryPush(attrsAry, p)
			if kv[0] == "xmlns" {
				scope[""] = kv[1]
				np := mrb.AryNewCapa(2)
				mrb.AryPush(np, mrb.NilValue())
				mrb.AryPush(np, mrb.StrNew(kv[1]))
				mrb.AryPush(nsAry, np)
			} else if strings.HasPrefix(kv[0], "xmlns:") {
				pref := kv[0][6:]
				scope[pref] = kv[1]
				np := mrb.AryNewCapa(2)
				mrb.AryPush(np, mrb.StrNew(pref))
				mrb.AryPush(np, mrb.StrNew(kv[1]))
				mrb.AryPush(nsAry, np)
			}
		}
		s.nsStack = append(s.nsStack, scope)
		for _, kv := range attrs {
			if kv[0] == "xmlns" || strings.HasPrefix(kv[0], "xmlns:") {
				continue
			}
			ap, al := splitQName(kv[0])
			var apVal, auVal oruby.Value = mrb.NilValue(), mrb.NilValue()
			if ap != "" {
				apVal = mrb.StrNew(ap)
				if u := s.lookupNS(ap); u != "" {
					auVal = mrb.StrNew(u)
				}
			}
			if obj, err := mrb.Funcall(attrCls, mrb.Intern("new"), mrb.StrNew(al), apVal, auVal, mrb.StrNew(kv[1])); err == nil {
				mrb.AryPush(nsAttrsAry, obj)
			}
		}
		pref, local := splitQName(qname)
		uri := s.lookupNS(pref)
		var prefVal, uriVal oruby.Value = mrb.NilValue(), mrb.NilValue()
		if pref != "" {
			prefVal = mrb.StrNew(pref)
		}
		if uri != "" {
			uriVal = mrb.StrNew(uri)
		}
		if err := call("start_element_namespace", mrb.StrNew(local), nsAttrsAry, prefVal, uriVal, nsAry); err != nil {
			return err
		}
		if err := call("start_element", mrb.StrNew(qname), attrsAry); err != nil {
			return err
		}
		if selfClosing {
			if err := call("end_element_namespace", mrb.StrNew(local), prefVal, uriVal); err != nil {
				return err
			}
			if err := call("end_element", mrb.StrNew(qname)); err != nil {
				return err
			}
			s.nsStack = s.nsStack[:len(s.nsStack)-1]
		}
	}
	return nil
}

func findTagEnd(s string) int {
	var q byte
	for i := 1; i < len(s); i++ {
		c := s[i]
		if q != 0 {
			if c == q {
				q = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			q = c
		} else if c == '>' {
			return i
		}
	}
	return -1
}

func parseTagAttrs(s string) (string, [][2]string) {
	i := 0
	for i < len(s) && s[i] != ' ' && s[i] != '\t' && s[i] != '\n' && s[i] != '\r' {
		i++
	}
	name := s[:i]
	var attrs [][2]string
	for i < len(s) {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
			i++
		}
		if i >= len(s) {
			break
		}
		kStart := i
		for i < len(s) && s[i] != '=' && s[i] != ' ' && s[i] != '\t' && s[i] != '\n' && s[i] != '\r' {
			i++
		}
		k := s[kStart:i]
		for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
			i++
		}
		val := ""
		if i < len(s) && s[i] == '=' {
			i++
			for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
				i++
			}
			if i < len(s) && (s[i] == '"' || s[i] == '\'') {
				q := s[i]
				i++
				vStart := i
				for i < len(s) && s[i] != q {
					i++
				}
				val = decodeXMLEntities(s[vStart:i])
				if i < len(s) {
					i++
				}
			}
		}
		if k != "" {
			attrs = append(attrs, [2]string{k, val})
		}
	}
	return name, attrs
}
