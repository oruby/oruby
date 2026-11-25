package nokogiri

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Nokogiri::XML C14N mode constants
const (
	xmlC14N10          = 0
	xmlC14NExclusive10 = 1
	xmlC14N11          = 2
)

// canonicalize renders n (and its subtree) in W3C XML Canonicalization
// (C14N 1.0 / Exclusive C14N 1.0 / C14N 1.1).
func (n *domNode) canonicalize(mode int, inclusiveNS []string, withComments bool) string {
	s, _ := n.canonicalizeEx(mode, inclusiveNS, withComments, nil)
	return s
}

func (n *domNode) canonicalizeEx(mode int, inclusiveNS []string, withComments bool, includeFn func(node, parent *domNode) (bool, error)) (string, error) {
	if mode != xmlC14N10 && mode != xmlC14NExclusive10 && mode != xmlC14N11 {
		return "", fmt.Errorf("invalid C14N mode: %d", mode)
	}
	if len(inclusiveNS) > 0 && mode != xmlC14NExclusive10 {
		return "", fmt.Errorf("inclusive namespaces are only allowed in exclusive C14N mode")
	}
	// Check for relative namespace URIs (forbidden by W3C C14N)
	var checkRelNS func(cur *domNode) error
	checkRelNS = func(cur *domNode) error {
		for _, ns := range cur.nsDefs {
			if ns.href != "" && !strings.ContainsRune(ns.href, ':') {
				return fmt.Errorf("relative namespace URI %q is not allowed in C14N", ns.href)
			}
		}
		for c := cur.first; c != nil; c = c.next {
			if err := checkRelNS(c); err != nil {
				return err
			}
		}
		return nil
	}
	if err := checkRelNS(n); err != nil {
		return "", err
	}

	var b strings.Builder
	inclSet := map[string]bool{}
	for _, p := range inclusiveNS {
		inclSet[p] = true
	}
	renderedNS := map[string]string{} // prefix -> href currently rendered in ancestor output
	if n.kind == DocumentNode || n.kind == HTMLDocumentNode || n.kind == DocumentFragNode {
		first := true
		for c := n.first; c != nil; c = c.next {
			switch c.kind {
			case ElementNode:
				if includeFn != nil {
					inc, err := includeFn(c, n)
					if err != nil {
						return "", err
					}
					// libxml2 also queries (doc, nil) when processing the document element
					if _, err := includeFn(n, nil); err != nil {
						return "", err
					}
					if !inc {
						continue
					}
				}
				if !first {
					b.WriteByte('\n')
				}
				if err := c.writeC14NEx(&b, mode, inclSet, withComments, renderedNS, true, includeFn); err != nil {
					return "", err
				}
				if includeFn != nil {
					if _, err := includeFn(c, n); err != nil {
						return "", err
					}
				}
				first = false
			case CommentNode:
				if withComments {
					if includeFn != nil {
						inc, err := includeFn(c, n)
						if err != nil {
							return "", err
						}
						if !inc {
							continue
						}
					}
					if !first {
						b.WriteByte('\n')
					}
					b.WriteString("<!--")
					b.WriteString(c.data)
					b.WriteString("-->")
					first = false
				}
			case PINode:
				if includeFn != nil {
					inc, err := includeFn(c, n)
					if err != nil {
						return "", err
					}
					if !inc {
						continue
					}
				}
				if !first {
					b.WriteByte('\n')
				}
				b.WriteString("<?")
				b.WriteString(c.name)
				if c.data != "" {
					b.WriteByte(' ')
					b.WriteString(c.data)
				}
				b.WriteString("?>")
				first = false
			}
		}
		return b.String(), nil
	}
	if err := n.writeC14NEx(&b, mode, inclSet, withComments, renderedNS, true, includeFn); err != nil {
		return "", err
	}
	return b.String(), nil
}

func (n *domNode) writeC14NEx(b *strings.Builder, mode int, inclSet map[string]bool, withComments bool, parentRendered map[string]string, isSubtreeRoot bool, includeFn func(node, parent *domNode) (bool, error)) error {
	switch n.kind {
	case TextNode, CDATASectionNode:
		writeC14NText(b, n.data)
	case CommentNode:
		if withComments {
			b.WriteString("<!--")
			b.WriteString(n.data)
			b.WriteString("-->")
		}
	case PINode:
		b.WriteString("<?")
		b.WriteString(n.name)
		if n.data != "" {
			b.WriteByte(' ')
			b.WriteString(n.data)
		}
		b.WriteString("?>")
	case ElementNode:
		qname := n.name
		if n.prefix != "" {
			qname = n.prefix + ":" + n.name
		}
		b.WriteByte('<')
		b.WriteString(qname)

		// Collect in-scope namespaces for this element
		inScope := n.inScopeNamespaces()
		// Determine which namespaces to render on this element
		toRender := map[string]string{}
		if mode == xmlC14NExclusive10 {
			// Exclusive C14N: visibly utilized prefixes + inclusiveNS prefixes
			vis := map[string]bool{}
			vis[n.prefix] = true
			for _, a := range n.attrs {
				if a.prefix != "" {
					vis[a.prefix] = true
				}
			}
			for p := range inclSet {
				vis[p] = true
			}
			for p := range vis {
				href := inScope[p]
				prev, hasPrev := parentRendered[p]
				if p == "" {
					if href != "" && (!hasPrev || prev != href) {
						toRender[""] = href
					} else if href == "" && hasPrev && prev != "" {
						toRender[""] = ""
					}
				} else if href != "" && (!hasPrev || prev != href) {
					toRender[p] = href
				}
			}
		} else {
			// Inclusive C14N 1.0 / 1.1: render every in-scope NS whose value differs from parentRendered
			for p, href := range inScope {
				prev, hasPrev := parentRendered[p]
				if p == "" {
					if href != "" && (!hasPrev || prev != href) {
						toRender[""] = href
					} else if href == "" && hasPrev && prev != "" {
						toRender[""] = ""
					}
				} else if href != "" && (!hasPrev || prev != href) {
					toRender[p] = href
				}
			}
		}

		// Update rendered map for children
		nextRendered := make(map[string]string, len(parentRendered)+len(toRender))
		for k, v := range parentRendered {
			nextRendered[k] = v
		}
		for k, v := range toRender {
			nextRendered[k] = v
		}

		// Sort namespace declarations: default xmlns first, then lexicographically by prefix
		nsKeys := make([]string, 0, len(toRender))
		for k := range toRender {
			nsKeys = append(nsKeys, k)
		}
		sort.Strings(nsKeys)
		for _, p := range nsKeys {
			if p == "" {
				b.WriteString(` xmlns="`)
			} else {
				b.WriteString(` xmlns:`)
				b.WriteString(p)
				b.WriteString(`="`)
			}
			writeC14NAttr(b, toRender[p])
			b.WriteByte('"')
		}

		// Sort attributes: unprefixed first (empty nsURI), then by (nsURI, localName)
		attrs := append([]*domAttr(nil), n.attrs...)
		// In non-exclusive C14N 1.0 / 1.1, inherit xml:lang / xml:space (and xml:base in 1.0) from ancestors on subtree root
		if isSubtreeRoot && mode != xmlC14NExclusive10 {
			hasXMLAttr := map[string]bool{}
			for _, a := range attrs {
				if a.prefix == "xml" {
					hasXMLAttr[a.name] = true
				}
			}
			for anc := n.parent; anc != nil && anc.kind == ElementNode; anc = anc.parent {
				for _, a := range anc.attrs {
					if a.prefix == "xml" && !hasXMLAttr[a.name] {
						if a.name == "lang" || a.name == "space" || (mode == xmlC14N10 && a.name == "base") {
							hasXMLAttr[a.name] = true
							attrs = append(attrs, a)
						}
					}
				}
			}
		}
		sort.Slice(attrs, func(i, j int) bool {
			ui, uj := attrs[i].nsURI, attrs[j].nsURI
			if ui != uj {
				return ui < uj
			}
			return attrs[i].name < attrs[j].name
		})
		for _, a := range attrs {
			b.WriteByte(' ')
			if a.prefix != "" {
				b.WriteString(a.prefix)
				b.WriteByte(':')
			}
			b.WriteString(a.name)
			b.WriteString(`="`)
			writeC14NAttr(b, a.value)
			b.WriteByte('"')
		}

		b.WriteByte('>')
		for c := n.first; c != nil; c = c.next {
			if includeFn != nil {
				inc, err := includeFn(c, n)
				if err != nil {
					return err
				}
				if !inc {
					continue
				}
			}
			if err := c.writeC14NEx(b, mode, inclSet, withComments, nextRendered, false, includeFn); err != nil {
				return err
			}
		}
		b.WriteString("</")
		b.WriteString(qname)
		b.WriteByte('>')
	}
	return nil
}

func (n *domNode) inScopeNamespaces() map[string]string {
	var chain []*domNode
	for cur := n; cur != nil && cur.kind == ElementNode; cur = cur.parent {
		chain = append(chain, cur)
	}
	res := map[string]string{}
	for i := len(chain) - 1; i >= 0; i-- {
		for _, ns := range chain[i].nsDefs {
			res[ns.prefix] = ns.href
		}
	}
	return res
}

func writeC14NText(b *strings.Builder, s string) {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '\r':
			b.WriteString("&#xD;")
		default:
			b.WriteByte(s[i])
		}
	}
}

func writeC14NAttr(b *strings.Builder, s string) {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '"':
			b.WriteString("&quot;")
		case '\t':
			b.WriteString("&#x9;")
		case '\n':
			b.WriteString("&#xA;")
		case '\r':
			b.WriteString("&#xD;")
		default:
			b.WriteByte(s[i])
		}
	}
}

// ---------------------------------------------------------------------------
// CSS Selector -> XPath 1.0 translator (Nokogiri::CSS.xpath_for)
// ---------------------------------------------------------------------------

func cssToXPath(selector, prefix string) (string, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return "", fmt.Errorf("empty CSS selector")
	}
	if prefix == "" {
		prefix = "//"
	}
	groups := splitTopLevel(selector, ',')
	var out []string
	for _, g := range groups {
		xp, err := cssComplexToXPath(strings.TrimSpace(g), prefix)
		if err != nil {
			return "", err
		}
		out = append(out, xp)
	}
	return strings.Join(out, " | "), nil
}

func splitTopLevel(s string, sep byte) []string {
	var parts []string
	depthParen, depthBrack := 0, 0
	var quote byte
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == '\\' && i+1 < len(s) {
				i++
			} else if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '(':
			depthParen++
		case ')':
			if depthParen > 0 {
				depthParen--
			}
		case '[':
			depthBrack++
		case ']':
			if depthBrack > 0 {
				depthBrack--
			}
		default:
			if c == sep && depthParen == 0 && depthBrack == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
}

type cssStep struct {
	combinator byte // 0=start, ' '=descendant, '>'=child, '+'=adjacent, '~'=following-sibling
	compound   string
}

func cssComplexToXPath(sel, prefix string) (string, error) {
	steps, err := tokenizeCSSSteps(sel)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for i, st := range steps {
		tag, preds, err := parseCSSCompound(st.compound)
		if err != nil {
			return "", err
		}
		switch st.combinator {
		case 0:
			b.WriteString(prefix)
		case ' ':
			b.WriteString("//")
		case '>':
			if i == 0 {
				b.WriteString("./")
			} else {
				b.WriteByte('/')
			}
		case '+':
			if i == 0 {
				b.WriteString("./following-sibling::*[1]/self::")
			} else {
				b.WriteString("/following-sibling::*[1]/self::")
			}
		case '~':
			if i == 0 {
				b.WriteString("./following-sibling::")
			} else {
				b.WriteString("/following-sibling::")
			}
		}
		b.WriteString(tag)
		for _, p := range preds {
			b.WriteByte('[')
			b.WriteString(p)
			b.WriteByte(']')
		}
	}
	return b.String(), nil
}

func tokenizeCSSSteps(sel string) ([]cssStep, error) {
	var steps []cssStep
	i := 0
	n := len(sel)
	var comb byte = 0
	for i < n {
		for i < n && (sel[i] == ' ' || sel[i] == '\t' || sel[i] == '\n' || sel[i] == '\r') {
			i++
		}
		if i >= n {
			break
		}
		if sel[i] == '>' || sel[i] == '+' || sel[i] == '~' {
			comb = sel[i]
			i++
			for i < n && (sel[i] == ' ' || sel[i] == '\t' || sel[i] == '\n' || sel[i] == '\r') {
				i++
			}
			if i >= n {
				return nil, fmt.Errorf("dangling combinator %q in %q", comb, sel)
			}
		} else if sel[i] == '/' {
			if i+1 < n && sel[i+1] == '/' {
				comb = ' '
				i += 2
			} else {
				comb = '>'
				i++
			}
			for i < n && (sel[i] == ' ' || sel[i] == '\t' || sel[i] == '\n' || sel[i] == '\r') {
				i++
			}
			if i >= n {
				return nil, fmt.Errorf("dangling combinator '/' in %q", sel)
			}
		}
		start := i
		depthP, depthB := 0, 0
		var quote byte
		for i < n {
			c := sel[i]
			if quote != 0 {
				if c == '\\' && i+1 < n {
					i += 2
					continue
				}
				if c == quote {
					quote = 0
				}
				i++
				continue
			}
			if c == '\\' && i+1 < n {
				i += 2
				continue
			}
			if c == '\'' || c == '"' {
				quote = c
				i++
				continue
			}
			if c == '(' {
				depthP++
			} else if c == ')' {
				depthP--
			} else if c == '[' {
				depthB++
			} else if c == ']' {
				depthB--
			} else if depthP == 0 && depthB == 0 && (c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '>' || c == '+' || c == '~' || c == '/') {
				break
			}
			i++
		}
		if depthP != 0 || depthB != 0 || quote != 0 {
			return nil, fmt.Errorf("unbalanced CSS selector: %q", sel)
		}
		steps = append(steps, cssStep{combinator: comb, compound: sel[start:i]})
		comb = ' '
	}
	if len(steps) == 0 {
		return nil, fmt.Errorf("empty CSS selector")
	}
	return steps, nil
}

func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func scanCSSIdent(s string, i int, allowPipe bool) (string, int) {
	var sb strings.Builder
	n := len(s)
	for i < n {
		c := s[i]
		if c == '\\' && i+1 < n {
			i++
			if isHexDigit(s[i]) {
				hexStart := i
				for i < n && i-hexStart < 6 && isHexDigit(s[i]) {
					i++
				}
				if cp, err := strconv.ParseInt(s[hexStart:i], 16, 32); err == nil && cp > 0 {
					sb.WriteRune(rune(cp))
				}
				// CSS allows a single optional trailing space after hex escape
				if i < n && (s[i] == ' ' || s[i] == '\t') {
					i++
				}
			} else {
				sb.WriteByte(s[i])
				i++
			}
		} else if isCSSIdentChar(c) || (allowPipe && c == '|') {
			sb.WriteByte(c)
			i++
		} else {
			break
		}
	}
	return sb.String(), i
}

func unescapeCSSString(s string) string {
	var sb strings.Builder
	n := len(s)
	for i := 0; i < n; {
		if s[i] == '\\' && i+1 < n {
			i++
			if isHexDigit(s[i]) {
				hexStart := i
				for i < n && i-hexStart < 6 && isHexDigit(s[i]) {
					i++
				}
				if cp, err := strconv.ParseInt(s[hexStart:i], 16, 32); err == nil && cp > 0 {
					sb.WriteRune(rune(cp))
				}
				if i < n && (s[i] == ' ' || s[i] == '\t') {
					i++
				}
			} else {
				sb.WriteByte(s[i])
				i++
			}
		} else {
			sb.WriteByte(s[i])
			i++
		}
	}
	return sb.String()
}

func parseCSSCompound(comp string) (string, []string, error) {
	tag := "*"
	var preds []string
	i := 0
	n := len(comp)

	// XPath-in-CSS node tests: text(), comment(), node(), @attr
	if comp == "text()" || comp == "comment()" || comp == "node()" {
		return comp, nil, nil
	}
	if i < n && comp[i] == '@' {
		attr, nextI := scanCSSIdent(comp, i+1, true)
		if attr == "" {
			return "", nil, fmt.Errorf("empty attribute step in %q", comp)
		}
		tag = "@" + strings.ReplaceAll(attr, "|", ":")
		i = nextI
	} else if i < n && comp[i] == '|' {
		i++
		if i < n && comp[i] == '*' {
			i++
			tag = "*"
			preds = append(preds, "namespace-uri() = ''")
		} else {
			local, nextI := scanCSSIdent(comp, i, false)
			if local == "" {
				return "", nil, fmt.Errorf("empty element after | in %q", comp)
			}
			i = nextI
			tag = "*"
			preds = append(preds, fmt.Sprintf("local-name() = %s and namespace-uri() = ''", xpathLiteral(local)))
		}
	} else if i < n && (comp[i] == '*' || isCSSIdentStart(comp[i]) || comp[i] == '\\') {
		if comp[i] == '*' {
			i++
			if i < n && comp[i] == '|' {
				i++
				if i < n && comp[i] == '*' {
					i++
					tag = "*"
				} else {
					local, nextI := scanCSSIdent(comp, i, false)
					i = nextI
					tag = "*"
					if local != "" {
						preds = append(preds, fmt.Sprintf("local-name() = %s", xpathLiteral(local)))
					}
				}
			}
		} else {
			ident, nextI := scanCSSIdent(comp, i, true)
			i = nextI
			if i < n && comp[i] == '(' && (ident == "text" || ident == "comment" || ident == "node") {
				end := findMatching(comp, i, '(', ')')
				if end >= 0 {
					tag = ident + comp[i:end+1]
					i = end + 1
				}
			} else {
				tag = strings.ReplaceAll(ident, "|", ":")
			}
		}
	}

	var posPreds []string
	for i < n {
		switch comp[i] {
		case '#':
			i++
			idVal, nextI := scanCSSIdent(comp, i, false)
			if nextI == i || idVal == "" {
				return "", nil, fmt.Errorf("empty id selector in %q", comp)
			}
			i = nextI
			preds = append(preds, fmt.Sprintf("@id = %s", xpathLiteral(idVal)))
		case '.':
			i++
			clsVal, nextI := scanCSSIdent(comp, i, false)
			if nextI == i || clsVal == "" {
				return "", nil, fmt.Errorf("empty class selector in %q", comp)
			}
			i = nextI
			preds = append(preds, fmt.Sprintf("contains(concat(' ', translate(string(@class), '\t\n\r', '   '), ' '), %s)", xpathLiteral(" "+clsVal+" ")))
		case '[':
			end := findMatching(comp, i, '[', ']')
			if end < 0 {
				return "", nil, fmt.Errorf("unterminated [ in %q", comp)
			}
			p, err := parseCSSAttrPred(comp[i+1 : end])
			if err != nil {
				return "", nil, err
			}
			preds = append(preds, p)
			i = end + 1
		case ':':
			i++
			if i < n && comp[i] == ':' {
				i++
			}
			start := i
			for i < n && isCSSIdentChar(comp[i]) {
				i++
			}
			pseudo := strings.ToLower(comp[start:i])
			arg := ""
			hasArg := false
			if i < n && comp[i] == '(' {
				end := findMatching(comp, i, '(', ')')
				if end < 0 {
					return "", nil, fmt.Errorf("unterminated ( in %q", comp)
				}
				arg = strings.TrimSpace(comp[i+1 : end])
				hasArg = true
				i = end + 1
			}
			p, isPos, err := cssPseudoToXPath(pseudo, arg, hasArg, tag, preds)
			if err != nil {
				return "", nil, err
			}
			if p != "" {
				if isPos {
					posPreds = append(posPreds, p)
				} else {
					preds = append(preds, p)
				}
			}
		default:
			return "", nil, fmt.Errorf("unexpected %q in CSS selector %q", comp[i], comp)
		}
	}
	preds = append(preds, posPreds...)
	return tag, preds, nil
}

func parseCSSAttrPred(body string) (string, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return "", fmt.Errorf("empty attribute selector")
	}
	i := 0
	if body[i] == '@' {
		i++
	}
	noNS := false
	if i < len(body) && body[i] == '|' {
		noNS = true
		i++
	}
	nameStart := i
	attrRaw, nextI := scanCSSIdent(body, i, true)
	i = nextI
	// Allow colon inside attribute names like xml:lang
	for i < len(body) && body[i] == ':' {
		i++
		part, ni := scanCSSIdent(body, i, false)
		attrRaw += ":" + part
		i = ni
	}
	rawSlice := body[nameStart:i]
	hasPipe := strings.ContainsRune(rawSlice, '|')
	hasBackslash := strings.ContainsRune(rawSlice, '\\')
	var attrExpr string
	if noNS || (hasBackslash && !hasPipe && strings.ContainsRune(attrRaw, ':')) {
		attrExpr = fmt.Sprintf("@*[local-name() = %s and namespace-uri() = '']", xpathLiteral(attrRaw))
	} else {
		attrExpr = "@" + strings.ReplaceAll(attrRaw, "|", ":")
	}
	rest := strings.TrimSpace(body[i:])
	if rest == "" {
		return attrExpr, nil
	}
	op := ""
	if len(rest) >= 2 && rest[1] == '=' && strings.ContainsRune("~|^$*!", rune(rest[0])) {
		op = rest[:2]
		rest = strings.TrimSpace(rest[2:])
	} else if rest[0] == '=' {
		op = "="
		rest = strings.TrimSpace(rest[1:])
	} else {
		return "", fmt.Errorf("invalid attribute operator in [%s]", body)
	}
	val := unquoteCSS(rest)
	lit := xpathLiteral(val)
	switch op {
	case "=":
		return fmt.Sprintf("%s = %s", attrExpr, lit), nil
	case "!=":
		return fmt.Sprintf("%s != %s", attrExpr, lit), nil
	case "~=":
		return fmt.Sprintf("contains(concat(' ', translate(string(%s), '\t\n\r', '   '), ' '), %s)", attrExpr, xpathLiteral(" "+val+" ")), nil
	case "|=":
		return fmt.Sprintf("%s = %s or starts-with(%s, %s)", attrExpr, lit, attrExpr, xpathLiteral(val+"-")), nil
	case "^=":
		return fmt.Sprintf("starts-with(%s, %s)", attrExpr, lit), nil
	case "$=":
		return fmt.Sprintf("substring(%s, string-length(%s) - %d) = %s", attrExpr, attrExpr, len(val)-1, lit), nil
	case "*=":
		return fmt.Sprintf("contains(%s, %s)", attrExpr, lit), nil
	default:
		return "", fmt.Errorf("unsupported operator %q", op)
	}
}

func parseAnPlusB(expr string) (a, b int, ok bool) {
	s := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(expr), " ", ""))
	if s == "" {
		return 0, 0, false
	}
	switch s {
	case "odd":
		return 2, 1, true
	case "even":
		return 2, 0, true
	}
	nIdx := strings.IndexByte(s, 'n')
	if nIdx < 0 {
		v, err := strconv.Atoi(s)
		if err != nil {
			return 0, 0, false
		}
		return 0, v, true
	}
	aStr := s[:nIdx]
	switch aStr {
	case "", "+":
		a = 1
	case "-":
		a = -1
	default:
		v, err := strconv.Atoi(aStr)
		if err != nil {
			return 0, 0, false
		}
		a = v
	}
	bStr := s[nIdx+1:]
	if bStr == "" {
		return a, 0, true
	}
	v, err := strconv.Atoi(bStr)
	if err != nil {
		return 0, 0, false
	}
	return a, v, true
}

func anPlusBXPath(posExpr string, a, b int) string {
	if a == 0 {
		if b <= 0 {
			return "false()"
		}
		return fmt.Sprintf("%s = %d", posExpr, b)
	}
	if a > 0 {
		if b <= 0 {
			// Shift b to smallest positive integer >= 1 with same residue mod a
			rem := ((b % a) + a) % a
			if rem == 0 {
				rem = a
			}
			b = rem
		}
		if a == 1 {
			if b <= 1 {
				return "true()"
			}
			return fmt.Sprintf("%s >= %d", posExpr, b)
		}
		return fmt.Sprintf("(%s >= %d and ((%s - %d) mod %d) = 0)", posExpr, b, posExpr, b, a)
	}
	// a < 0
	if b <= 0 {
		return "false()"
	}
	step := -a
	if step == 1 {
		return fmt.Sprintf("%s <= %d", posExpr, b)
	}
	return fmt.Sprintf("(%s <= %d and ((%d - %s) mod %d) = 0)", posExpr, b, b, posExpr, step)
}

func siblingOfTypeTest(axis, tag string, preds []string) string {
	var b strings.Builder
	b.WriteString(axis)
	b.WriteString("::")
	b.WriteString(tag)
	for _, p := range preds {
		b.WriteByte('[')
		b.WriteString(p)
		b.WriteByte(']')
	}
	return b.String()
}

func cssPseudoToXPath(pseudo, arg string, hasArg bool, tag string, priorPreds []string) (string, bool, error) {
	switch pseudo {
	case "first-child":
		return "count(preceding-sibling::*) = 0", false, nil
	case "last-child":
		return "count(following-sibling::*) = 0", false, nil
	case "only-child":
		return "count(preceding-sibling::*) = 0 and count(following-sibling::*) = 0", false, nil
	case "first-of-type":
		return fmt.Sprintf("count(%s) = 0", siblingOfTypeTest("preceding-sibling", tag, priorPreds)), false, nil
	case "last-of-type":
		return fmt.Sprintf("count(%s) = 0", siblingOfTypeTest("following-sibling", tag, priorPreds)), false, nil
	case "only-of-type":
		return fmt.Sprintf("count(%s) = 0 and count(%s) = 0", siblingOfTypeTest("preceding-sibling", tag, priorPreds), siblingOfTypeTest("following-sibling", tag, priorPreds)), false, nil
	case "empty":
		return "not(node())", false, nil
	case "parent":
		return "node()", false, nil
	case "contains":
		return fmt.Sprintf("contains(., %s)", xpathLiteral(unquoteCSS(arg))), false, nil
	case "checked":
		return "@checked", false, nil
	case "disabled":
		return "@disabled", false, nil
	case "enabled":
		return "not(@disabled)", false, nil
	case "selected":
		return "@selected", false, nil
	case "first":
		return "position() = 1", true, nil
	case "last":
		return "position() = last()", true, nil
	case "even":
		return "(position() mod 2) = 0", true, nil
	case "odd":
		return "(position() mod 2) = 1", true, nil
	case "eq":
		n, err := strconv.Atoi(strings.TrimSpace(arg))
		if err != nil {
			return "", false, fmt.Errorf("invalid :eq(%s)", arg)
		}
		return fmt.Sprintf("position() = %d", n+1), true, nil
	case "gt":
		n, err := strconv.Atoi(strings.TrimSpace(arg))
		if err != nil {
			return "", false, fmt.Errorf("invalid :gt(%s)", arg)
		}
		return fmt.Sprintf("position() > %d", n+1), true, nil
	case "lt":
		n, err := strconv.Atoi(strings.TrimSpace(arg))
		if err != nil {
			return "", false, fmt.Errorf("invalid :lt(%s)", arg)
		}
		return fmt.Sprintf("position() < %d", n+1), true, nil
	case "nth":
		a, b, ok := parseAnPlusB(arg)
		if !ok {
			return "", false, fmt.Errorf("invalid :nth(%s)", arg)
		}
		return anPlusBXPath("position()", a, b), true, nil
	case "nth-of-type":
		a, b, ok := parseAnPlusB(arg)
		if !ok {
			return "", false, fmt.Errorf("invalid :nth-of-type(%s)", arg)
		}
		posExpr := fmt.Sprintf("(count(%s) + 1)", siblingOfTypeTest("preceding-sibling", tag, priorPreds))
		return anPlusBXPath(posExpr, a, b), false, nil
	case "nth-last-of-type":
		a, b, ok := parseAnPlusB(arg)
		if !ok {
			return "", false, fmt.Errorf("invalid :nth-last-of-type(%s)", arg)
		}
		posExpr := fmt.Sprintf("(count(%s) + 1)", siblingOfTypeTest("following-sibling", tag, priorPreds))
		return anPlusBXPath(posExpr, a, b), false, nil
	case "nth-child":
		a, b, ok := parseAnPlusB(arg)
		if !ok {
			return "", false, fmt.Errorf("invalid :nth-child(%s)", arg)
		}
		return anPlusBXPath("(count(preceding-sibling::*) + 1)", a, b), false, nil
	case "nth-last-child":
		a, b, ok := parseAnPlusB(arg)
		if !ok {
			return "", false, fmt.Errorf("invalid :nth-last-child(%s)", arg)
		}
		return anPlusBXPath("(count(following-sibling::*) + 1)", a, b), false, nil
	case "has":
		if !hasArg || strings.TrimSpace(arg) == "" {
			return "", false, fmt.Errorf(":has requires an argument")
		}
		sub := strings.TrimSpace(arg)
		subGroups := splitTopLevel(sub, ',')
		var xpParts []string
		for _, sg := range subGroups {
			sg = strings.TrimSpace(sg)
			prefix := ".//"
			if strings.HasPrefix(sg, ">") || strings.HasPrefix(sg, "+") || strings.HasPrefix(sg, "~") {
				prefix = ""
			}
			xp, err := cssComplexToXPath(sg, prefix)
			if err != nil {
				return "", false, err
			}
			xpParts = append(xpParts, xp)
		}
		return strings.Join(xpParts, " | "), false, nil
	case "not":
		if !hasArg {
			return "", false, fmt.Errorf(":not requires an argument")
		}
		tag, preds, err := parseCSSCompound(arg)
		if err != nil {
			return "", false, err
		}
		var parts []string
		if tag != "*" {
			parts = append(parts, "self::"+tag)
		}
		parts = append(parts, preds...)
		if len(parts) == 0 {
			return "false()", false, nil
		}
		return "not(" + strings.Join(parts, " and ") + ")", false, nil
	default:
		// Nokogiri custom CSS pseudo-class handler -> nokogiri:pseudo(., args...)
		if hasArg {
			if arg == "" {
				return fmt.Sprintf("nokogiri:%s(.)", pseudo), false, nil
			}
			return fmt.Sprintf("nokogiri:%s(., %s)", pseudo, arg), false, nil
		}
		return fmt.Sprintf("nokogiri:%s(.)", pseudo), false, nil
	}
}

func findMatching(s string, openIdx int, openCh, closeCh byte) int {
	depth := 0
	var quote byte
	for i := openIdx; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == '\\' && i+1 < len(s) {
				i++
			} else if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		if c == openCh {
			depth++
		} else if c == closeCh {
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func isCSSIdentStart(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_' || c == '-' || c >= 0x80
}

func isCSSIdentChar(c byte) bool {
	return isCSSIdentStart(c) || (c >= '0' && c <= '9')
}

func unquoteCSS(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && ((s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'')) {
		return unescapeCSSString(s[1 : len(s)-1])
	}
	return unescapeCSSString(s)
}

func xpathLiteral(s string) string {
	if !strings.ContainsRune(s, '\'') {
		return "'" + s + "'"
	}
	if !strings.ContainsRune(s, '"') {
		return `"` + s + `"`
	}
	parts := strings.Split(s, "'")
	var out []string
	for i, p := range parts {
		if p != "" {
			out = append(out, "'"+p+"'")
		}
		if i < len(parts)-1 {
			out = append(out, `"'"`)
		}
	}
	return "concat(" + strings.Join(out, ", ") + ")"
}
