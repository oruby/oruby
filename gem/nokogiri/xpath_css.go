package nokogiri

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/andybalholm/cascadia"
	"github.com/antchfx/xpath"
	"golang.org/x/net/html"
)

// ---------------------------------------------------------------------------
// XPath 1.0 via github.com/antchfx/xpath
// ---------------------------------------------------------------------------

type domNavigator struct {
	root  *domNode
	cur   *domNode
	attr  int // -1 when not positioned on an attribute
	nsMap map[string]string
	uriTo map[string]string // reverse map: nsURI -> preferred prefix in query
}

func newDOMNavigator(start *domNode, nsMap map[string]string) *domNavigator {
	root := start
	if start.doc != nil && (start.doc.kind == DocumentNode || start.doc.kind == HTMLDocumentNode) {
		root = start.doc
	} else {
		for root.parent != nil {
			root = root.parent
		}
	}

	// Build prefix -> URI map: start with document root namespaces, overlay caller nsMap
	merged := map[string]string{}
	if rootElem := root.rootElement(); rootElem != nil {
		for _, ns := range rootElem.nsDefs {
			if ns.prefix != "" {
				merged[ns.prefix] = ns.href
			} else {
				merged["xmlns"] = ns.href
			}
		}
	}
	for k, v := range nsMap {
		merged[k] = v
	}

	uriTo := map[string]string{}
	// Caller-supplied prefixes take priority in reverse lookup
	for k, v := range nsMap {
		if k != "" && v != "" {
			uriTo[v] = k
		}
	}
	for k, v := range merged {
		if k != "" && v != "" {
			if _, exists := uriTo[v]; !exists {
				uriTo[v] = k
			}
		}
	}

	return &domNavigator{
		root:  root,
		cur:   start,
		attr:  -1,
		nsMap: merged,
		uriTo: uriTo,
	}
}

func (n *domNavigator) NodeType() xpath.NodeType {
	if n.attr >= 0 {
		return xpath.AttributeNode
	}
	switch n.cur.kind {
	case DocumentNode, HTMLDocumentNode, DocumentFragNode:
		return xpath.RootNode
	case ElementNode:
		return xpath.ElementNode
	case TextNode, CDATASectionNode:
		return xpath.TextNode
	case CommentNode:
		return xpath.CommentNode
	case PINode:
		return xpath.ElementNode
	default:
		return xpath.TextNode
	}
}

func (n *domNavigator) LocalName() string {
	if n.attr >= 0 {
		return n.cur.attrs[n.attr].name
	}
	if n.cur.kind == ElementNode {
		return n.cur.name
	}
	return ""
}

func (n *domNavigator) Prefix() string {
	if n.attr >= 0 {
		a := n.cur.attrs[n.attr]
		if a.nsURI != "" {
			if p, ok := n.uriTo[a.nsURI]; ok {
				return p
			}
		}
		return a.prefix
	}
	if n.cur.kind == ElementNode {
		if n.cur.nsURI != "" {
			if p, ok := n.uriTo[n.cur.nsURI]; ok {
				return p
			}
		}
		return n.cur.prefix
	}
	return ""
}

func (n *domNavigator) Value() string {
	if n.attr >= 0 {
		return n.cur.attrs[n.attr].value
	}
	return n.cur.innerText()
}

func (n *domNavigator) Copy() xpath.NodeNavigator {
	cp := *n
	return &cp
}

func (n *domNavigator) MoveToRoot() {
	n.cur = n.root
	n.attr = -1
}

func (n *domNavigator) MoveToParent() bool {
	if n.attr >= 0 {
		n.attr = -1
		return true
	}
	if n.cur.parent != nil {
		n.cur = n.cur.parent
		return true
	}
	return false
}

func (n *domNavigator) MoveToNextAttribute() bool {
	if n.cur.kind != ElementNode {
		return false
	}
	if n.attr+1 < len(n.cur.attrs) {
		n.attr++
		return true
	}
	return false
}

func (n *domNavigator) MoveToChild() bool {
	if n.attr >= 0 {
		return false
	}
	for c := n.cur.first; c != nil; c = c.next {
		if c.kind == DocumentTypeNode || c.kind == DTDNode {
			continue
		}
		n.cur = c
		return true
	}
	return false
}

func (n *domNavigator) MoveToFirst() bool {
	if n.attr >= 0 || n.cur.parent == nil {
		return false
	}
	for c := n.cur.parent.first; c != nil; c = c.next {
		if c.kind == DocumentTypeNode || c.kind == DTDNode {
			continue
		}
		n.cur = c
		return true
	}
	return false
}

func (n *domNavigator) MoveToNext() bool {
	if n.attr >= 0 {
		return false
	}
	for s := n.cur.next; s != nil; s = s.next {
		if s.kind == DocumentTypeNode || s.kind == DTDNode {
			continue
		}
		n.cur = s
		return true
	}
	return false
}

func (n *domNavigator) MoveToPrevious() bool {
	if n.attr >= 0 {
		return false
	}
	for s := n.cur.prev; s != nil; s = s.prev {
		if s.kind == DocumentTypeNode || s.kind == DTDNode {
			continue
		}
		n.cur = s
		return true
	}
	return false
}

func (n *domNavigator) MoveTo(other xpath.NodeNavigator) bool {
	o, ok := other.(*domNavigator)
	if !ok || o.root != n.root {
		return false
	}
	n.cur = o.cur
	n.attr = o.attr
	return true
}

func substituteXPathVars(expr string, vars map[string]any) string {
	if len(vars) == 0 || !strings.ContainsRune(expr, '$') {
		return expr
	}
	var sb strings.Builder
	sb.Grow(len(expr) + 16)
	inSingle := false
	inDouble := false
	for i := 0; i < len(expr); {
		ch := expr[i]
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			sb.WriteByte(ch)
			i++
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			sb.WriteByte(ch)
			i++
			continue
		}
		if ch == '$' && !inSingle && !inDouble {
			j := i + 1
			for j < len(expr) && (expr[j] == '_' || expr[j] == '-' || (expr[j] >= 'a' && expr[j] <= 'z') || (expr[j] >= 'A' && expr[j] <= 'Z') || (expr[j] >= '0' && expr[j] <= '9')) {
				j++
			}
			vname := expr[i+1 : j]
			if val, ok := vars[vname]; ok {
				sb.WriteString(formatXPathLiteral(val))
				i = j
				continue
			}
		}
		sb.WriteByte(ch)
		i++
	}
	return sb.String()
}

func formatXPathLiteral(v any) string {
	switch x := v.(type) {
	case bool:
		if x {
			return "true()"
		}
		return "false()"
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case string:
		if !strings.ContainsRune(x, '\'') {
			return "'" + x + "'"
		}
		if !strings.ContainsRune(x, '"') {
			return `"` + x + `"`
		}
		parts := strings.Split(x, "'")
		quoted := make([]string, len(parts))
		for i, p := range parts {
			quoted[i] = "'" + p + "'"
		}
		return "concat(" + strings.Join(quoted, `,"'",`) + ")"
	default:
		return "''"
	}
}

type xpathResult struct {
	nodes []*domNode
	attrs []*domAttr
	val   any // string, float64, bool when scalar
	isSet bool
}

func rewriteNokogiriXPath(expr string) (string, error) {
	// Rewrite nokogiri-builtin:css-class(a, b) to standard XPath 1.0
	for {
		idx := strings.Index(expr, "nokogiri-builtin:css-class(")
		prefixLen := len("nokogiri-builtin:css-class(")
		if idx < 0 {
			break
		}
		openIdx := idx + prefixLen - 1
		closeIdx := findMatching(expr, openIdx, '(', ')')
		if closeIdx < 0 {
			return "", fmt.Errorf("unterminated nokogiri-builtin:css-class() call")
		}
		argStr := strings.TrimSpace(expr[openIdx+1 : closeIdx])
		var args []string
		if argStr != "" {
			args = splitTopLevel(argStr, ',')
		}
		if len(args) != 2 {
			return "", fmt.Errorf("nokogiri-builtin:css-class requires 2 arguments, got %d", len(args))
		}
		a := strings.TrimSpace(args[0])
		b := strings.TrimSpace(args[1])
		// Match complete ASCII-whitespace-delimited words (0x20, 0x09, 0x0A, 0x0D) or empty second arg
		replacement := fmt.Sprintf("((string-length(%s) = 0) or contains(concat(' ', translate(string(%s), '\t\n\r', '   '), ' '), concat(' ', string(%s), ' ')))", b, a, b)
		expr = expr[:idx] + replacement + expr[closeIdx+1:]
	}
	// Rewrite leading id("foo") -> //*[@id="foo"]
	if strings.HasPrefix(expr, "id(") {
		if closeIdx := findMatching(expr, 2, '(', ')'); closeIdx > 2 {
			arg := strings.TrimSpace(expr[3:closeIdx])
			expr = fmt.Sprintf("//*[@id=%s]%s", arg, expr[closeIdx+1:])
		}
	}
	return expr, nil
}

func evalNamespaceAxis(start *domNode, expr string, nsMap map[string]string, vars map[string]any) (xpathResult, bool, error) {
	idx := strings.Index(expr, "namespace::")
	if idx < 0 {
		return xpathResult{}, false, nil
	}
	baseExpr := strings.TrimSpace(expr[:idx])
	nsTarget := strings.TrimSpace(expr[idx+len("namespace::"):])
	// Only handle simple trailing namespace::* or namespace::prefix
	if strings.ContainsAny(nsTarget, "/[]()") {
		return xpathResult{}, false, nil
	}
	var targets []*domNode
	if baseExpr == "" {
		if start.kind == ElementNode {
			targets = []*domNode{start}
		}
	} else if baseExpr == "//" {
		res, err := evalXPath(start, "//*", nsMap, vars)
		if err != nil {
			return xpathResult{}, true, err
		}
		targets = res.nodes
	} else if baseExpr == ".//" {
		res, err := evalXPath(start, ".//*", nsMap, vars)
		if err != nil {
			return xpathResult{}, true, err
		}
		targets = res.nodes
	} else {
		baseExpr = strings.TrimSuffix(baseExpr, "/")
		res, err := evalXPath(start, baseExpr, nsMap, vars)
		if err != nil {
			return xpathResult{}, true, err
		}
		targets = res.nodes
	}
	var out []*domNode
	for _, el := range targets {
		if el.kind != ElementNode {
			continue
		}
		scopes := el.namespaceScopes()
		// Add implicit xml namespace
		hasXML := false
		for _, ns := range scopes {
			if ns.prefix == "xml" {
				hasXML = true
				break
			}
		}
		if !hasXML {
			scopes = append([]*domNS{{prefix: "xml", href: "http://www.w3.org/XML/1998/namespace", doc: el.doc}}, scopes...)
		}
		for _, ns := range scopes {
			if nsTarget != "*" && ns.prefix != nsTarget {
				continue
			}
			out = append(out, &domNode{
				kind:   NamespaceDecl,
				doc:    el.doc,
				parent: el,
				prefix: ns.prefix,
				nsURI:  ns.href,
			})
		}
	}
	return xpathResult{nodes: out, isSet: true}, true, nil
}

func normalizeXPathError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	const pfx = "not yet support this function "
	if strings.Contains(msg, pfx) {
		rest := msg[strings.Index(msg, pfx)+len(pfx):]
		fn := strings.TrimSuffix(strings.TrimSpace(rest), "()")
		return fmt.Errorf("xmlXPathCompOpEval: function %s not found", fn)
	}
	return err
}

func evalXPath(start *domNode, exprStr string, nsMap map[string]string, vars map[string]any) (xpathResult, error) {
	resolved := substituteXPathVars(exprStr, vars)
	if nsRes, handled, err := evalNamespaceAxis(start, resolved, nsMap, vars); handled {
		return nsRes, normalizeXPathError(err)
	}
	rewritten, rerr := rewriteNokogiriXPath(resolved)
	if rerr != nil {
		return xpathResult{}, rerr
	}
	compiled, err := xpath.Compile(rewritten)
	if err != nil {
		return xpathResult{}, normalizeXPathError(err)
	}
	nav := newDOMNavigator(start, nsMap)
	res := compiled.Evaluate(nav)
	switch v := res.(type) {
	case *xpath.NodeIterator:
		var out xpathResult
		out.isSet = true
		seen := map[*domNode]bool{}
		for v.MoveNext() {
			cn := v.Current().(*domNavigator)
			if cn.attr >= 0 {
				a := cn.cur.attrs[cn.attr]
				an := &domNode{
					kind:   AttributeNode,
					doc:    cn.cur.doc,
					parent: cn.cur,
					name:   a.name,
					prefix: a.prefix,
					nsURI:  a.nsURI,
					data:   a.value,
				}
				out.nodes = append(out.nodes, an)
			} else if !seen[cn.cur] {
				seen[cn.cur] = true
				out.nodes = append(out.nodes, cn.cur)
			}
		}
		return out, nil
	default:
		return xpathResult{val: v, isSet: false}, nil
	}
}

// ---------------------------------------------------------------------------
// CSS3/4 Selector Engine via github.com/andybalholm/cascadia
// ---------------------------------------------------------------------------

func mirrorForCSS(n *domNode, back map[*html.Node]*domNode, caseFold bool) *html.Node {
	var hn *html.Node
	switch n.kind {
	case DocumentNode, HTMLDocumentNode, DocumentFragNode:
		hn = &html.Node{Type: html.DocumentNode}
	case ElementNode:
		name := n.name
		if caseFold {
			name = strings.ToLower(name)
		}
		hn = &html.Node{
			Type:      html.ElementNode,
			Data:      name,
			Namespace: n.prefix,
		}
		if len(n.attrs) > 0 {
			hn.Attr = make([]html.Attribute, len(n.attrs))
			for i, a := range n.attrs {
				k := a.name
				if caseFold {
					k = strings.ToLower(k)
				}
				hn.Attr[i] = html.Attribute{
					Namespace: a.prefix,
					Key:       k,
					Val:       a.value,
				}
			}
		}
	case TextNode, CDATASectionNode:
		hn = &html.Node{Type: html.TextNode, Data: n.data}
	case CommentNode:
		hn = &html.Node{Type: html.CommentNode, Data: n.data}
	default:
		return nil
	}
	back[hn] = n
	for c := n.first; c != nil; c = c.next {
		if ch := mirrorForCSS(c, back, caseFold); ch != nil {
			hn.AppendChild(ch)
		}
	}
	return hn
}

func shouldUseXPathForCSS(sel string) bool {
	if strings.ContainsAny(sel, "/@|\\") {
		return true
	}
	lower := strings.ToLower(sel)
	for _, pat := range []string{
		":has(", ":nth(", ":nth-of-type(", ":nth-last-of-type(",
		":first-of-type", ":last-of-type", ":only-of-type",
		":first", ":last", ":eq(", ":gt(", ":lt(",
		":even", ":odd", ":parent", ":contains(",
		"text()", "comment()", "node()",
	} {
		if strings.Contains(lower, pat) {
			return true
		}
	}
	return false
}

func queryCSS(scope *domNode, selector string, nsMap map[string]string) ([]*domNode, error) {
	selStr := strings.TrimSpace(selector)
	if selStr == "" {
		return nil, nil
	}

	// Route Nokogiri CSS extensions & namespaced selectors through CSS->XPath
	if len(nsMap) > 0 || shouldUseXPathForCSS(selStr) {
		prefix := ".//"
		if scope.kind == DocumentNode || scope.kind == HTMLDocumentNode {
			prefix = "//"
		}
		xp, xerr := cssToXPath(selStr, prefix)
		if xerr == nil {
			res, eerr := evalXPath(scope, xp, nsMap, nil)
			if eerr == nil && res.isSet {
				return res.nodes, nil
			}
		}
	}

	caseFold := scope.isHTMLDoc()
	back := map[*html.Node]*domNode{}

	// Support Nokogiri leading child combinator e.g. "> li" or "> div > a"
	if strings.HasPrefix(selStr, ">") {
		wrapped := ":scope " + selStr
		rootMirror := mirrorForCSS(scope, back, caseFold)
		// cascadia does not treat DocumentNode as matching :scope directly; match on element or filter direct children
		subSel := strings.TrimSpace(selStr[1:])
		if scope.kind == ElementNode {
			// Match against children by checking direct child for first segment or using parent mirror
			parentWrap := &html.Node{Type: html.ElementNode, Data: "__oruby_scope__"}
			parentWrap.AppendChild(rootMirror)
			compiled, err := cascadia.Compile("__oruby_scope__ > * > " + subSel)
			if err != nil {
				return nil, fmt.Errorf("invalid CSS selector %q: %w", selector, err)
			}
			matches := compiled.MatchAll(parentWrap)
			out := make([]*domNode, 0, len(matches))
			for _, m := range matches {
				if dn, ok := back[m]; ok {
					out = append(out, dn)
				}
			}
			return out, nil
		}
		_ = wrapped
		selStr = subSel
	}

	compiled, err := cascadia.Compile(selStr)
	if err != nil {
		// Fallback to CSS->XPath for Nokogiri/jQuery extensions (:first, :last, :eq, [a!=b], etc.)
		prefix := ".//"
		if scope.kind == DocumentNode || scope.kind == HTMLDocumentNode {
			prefix = "//"
		}
		if xp, xerr := cssToXPath(selStr, prefix); xerr == nil {
			if res, eerr := evalXPath(scope, xp, nsMap, nil); eerr == nil && res.isSet {
				return res.nodes, nil
			}
		}
		return nil, fmt.Errorf("invalid CSS selector %q: %w", selector, err)
	}

	rootMirror := mirrorForCSS(scope, back, caseFold)
	matches := compiled.MatchAll(rootMirror)
	out := make([]*domNode, 0, len(matches))
	seen := map[*domNode]bool{}
	for _, m := range matches {
		dn, ok := back[m]
		if !ok || dn == nil || dn == scope && scope.kind == ElementNode {
			continue
		}
		if !seen[dn] {
			seen[dn] = true
			out = append(out, dn)
		}
	}
	return out, nil
}

func matchesCSS(node *domNode, selector string) (bool, error) {
	if node.kind != ElementNode {
		return false, nil
	}
	selStr := strings.TrimSpace(selector)
	root := node
	for root.parent != nil {
		root = root.parent
	}
	if shouldUseXPathForCSS(selStr) {
		if xp, xerr := cssToXPath(selStr, "//"); xerr == nil {
			if res, eerr := evalXPath(root, xp, nil, nil); eerr == nil && res.isSet {
				for _, cand := range res.nodes {
					if cand == node {
						return true, nil
					}
				}
				return false, nil
			}
		}
	}
	compiled, err := cascadia.Compile(selStr)
	if err != nil {
		if xp, xerr := cssToXPath(selStr, "//"); xerr == nil {
			if res, eerr := evalXPath(root, xp, nil, nil); eerr == nil && res.isSet {
				for _, cand := range res.nodes {
					if cand == node {
						return true, nil
					}
				}
				return false, nil
			}
		}
		return false, err
	}
	back := map[*html.Node]*domNode{}
	rootMirror := mirrorForCSS(root, back, node.isHTMLDoc())
	var target *html.Node
	for hn, dn := range back {
		if dn == node {
			target = hn
			break
		}
	}
	if target == nil {
		_ = rootMirror
		return false, nil
	}
	return compiled.Match(target), nil
}
