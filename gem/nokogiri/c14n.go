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
				if !first {
					b.WriteByte('\n')
				}
				c.writeC14N(&b, mode, inclSet, withComments, renderedNS)
				first = false
			case CommentNode:
				if withComments {
					if !first {
						b.WriteByte('\n')
					}
					b.WriteString("<!--")
					b.WriteString(c.data)
					b.WriteString("-->")
					first = false
				}
			case PINode:
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
		return b.String()
	}
	n.writeC14N(&b, mode, inclSet, withComments, renderedNS)
	return b.String()
}

func (n *domNode) writeC14N(b *strings.Builder, mode int, inclSet map[string]bool, withComments bool, parentRendered map[string]string) {
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
			c.writeC14N(b, mode, inclSet, withComments, nextRendered)
		}
		b.WriteString("</")
		b.WriteString(qname)
		b.WriteByte('>')
	}
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
			b.WriteString("/following-sibling::*[1]/self::")
		case '~':
			b.WriteString("/following-sibling::")
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
			} else if depthP == 0 && depthB == 0 && (c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '>' || c == '+' || c == '~') {
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

func parseCSSCompound(comp string) (string, []string, error) {
	tag := "*"
	var preds []string
	i := 0
	n := len(comp)

	// Optional leading tag name or *
	if i < n && (comp[i] == '*' || isCSSIdentStart(comp[i])) {
		start := i
		if comp[i] == '*' {
			i++
		} else {
			for i < n && (isCSSIdentChar(comp[i]) || comp[i] == '|') {
				i++
			}
		}
		tag = strings.ReplaceAll(comp[start:i], "|", ":")
	}

	for i < n {
		switch comp[i] {
		case '#':
			i++
			start := i
			for i < n && isCSSIdentChar(comp[i]) {
				i++
			}
			if i == start {
				return "", nil, fmt.Errorf("empty id selector in %q", comp)
			}
			preds = append(preds, fmt.Sprintf("@id = %s", xpathLiteral(comp[start:i])))
		case '.':
			i++
			start := i
			for i < n && isCSSIdentChar(comp[i]) {
				i++
			}
			if i == start {
				return "", nil, fmt.Errorf("empty class selector in %q", comp)
			}
			preds = append(preds, fmt.Sprintf("contains(concat(' ', normalize-space(@class), ' '), %s)", xpathLiteral(" "+comp[start:i]+" ")))
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
			p, err := cssPseudoToXPath(pseudo, arg, hasArg)
			if err != nil {
				return "", nil, err
			}
			if p != "" {
				preds = append(preds, p)
			}
		default:
			return "", nil, fmt.Errorf("unexpected %q in CSS selector %q", comp[i], comp)
		}
	}
	return tag, preds, nil
}

func parseCSSAttrPred(body string) (string, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return "", fmt.Errorf("empty attribute selector")
	}
	i := 0
	for i < len(body) && (isCSSIdentChar(body[i]) || body[i] == ':' || body[i] == '|') {
		i++
	}
	attr := strings.ReplaceAll(body[:i], "|", ":")
	rest := strings.TrimSpace(body[i:])
	if rest == "" {
		return "@" + attr, nil
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
		return fmt.Sprintf("@%s = %s", attr, lit), nil
	case "!=":
		return fmt.Sprintf("@%s != %s", attr, lit), nil
	case "~=":
		return fmt.Sprintf("contains(concat(' ', normalize-space(@%s), ' '), %s)", attr, xpathLiteral(" "+val+" ")), nil
	case "|=":
		return fmt.Sprintf("@%s = %s or starts-with(@%s, %s)", attr, lit, attr, xpathLiteral(val+"-")), nil
	case "^=":
		return fmt.Sprintf("starts-with(@%s, %s)", attr, lit), nil
	case "$=":
		return fmt.Sprintf("substring(@%s, string-length(@%s) - %d) = %s", attr, attr, len(val)-1, lit), nil
	case "*=":
		return fmt.Sprintf("contains(@%s, %s)", attr, lit), nil
	default:
		return "", fmt.Errorf("unsupported operator %q", op)
	}
}

func cssPseudoToXPath(pseudo, arg string, hasArg bool) (string, error) {
	switch pseudo {
	case "first-child":
		return "count(preceding-sibling::*) = 0", nil
	case "last-child":
		return "count(following-sibling::*) = 0", nil
	case "only-child":
		return "count(preceding-sibling::*) = 0 and count(following-sibling::*) = 0", nil
	case "empty":
		return "not(node())", nil
	case "parent":
		return "node()", nil
	case "contains":
		return fmt.Sprintf("contains(., %s)", xpathLiteral(unquoteCSS(arg))), nil
	case "checked":
		return "@checked", nil
	case "disabled":
		return "@disabled", nil
	case "enabled":
		return "not(@disabled)", nil
	case "selected":
		return "@selected", nil
	case "first":
		return "position() = 1", nil
	case "last":
		return "position() = last()", nil
	case "even":
		return "(position() mod 2) = 1", nil
	case "odd":
		return "(position() mod 2) = 0", nil
	case "eq":
		n, err := strconv.Atoi(strings.TrimSpace(arg))
		if err != nil {
			return "", fmt.Errorf("invalid :eq(%s)", arg)
		}
		return fmt.Sprintf("position() = %d", n+1), nil
	case "gt":
		n, err := strconv.Atoi(strings.TrimSpace(arg))
		if err != nil {
			return "", fmt.Errorf("invalid :gt(%s)", arg)
		}
		return fmt.Sprintf("position() > %d", n+1), nil
	case "lt":
		n, err := strconv.Atoi(strings.TrimSpace(arg))
		if err != nil {
			return "", fmt.Errorf("invalid :lt(%s)", arg)
		}
		return fmt.Sprintf("position() < %d", n+1), nil
	case "nth-child":
		a := strings.ToLower(strings.TrimSpace(arg))
		if a == "odd" {
			return "((count(preceding-sibling::*) + 1) mod 2) = 1", nil
		}
		if a == "even" {
			return "((count(preceding-sibling::*) + 1) mod 2) = 0", nil
		}
		if n, err := strconv.Atoi(a); err == nil {
			return fmt.Sprintf("count(preceding-sibling::*) = %d", n-1), nil
		}
		return "", nil
	case "not":
		if !hasArg {
			return "", fmt.Errorf(":not requires an argument")
		}
		tag, preds, err := parseCSSCompound(arg)
		if err != nil {
			return "", err
		}
		var parts []string
		if tag != "*" {
			parts = append(parts, "self::"+tag)
		}
		parts = append(parts, preds...)
		if len(parts) == 0 {
			return "false()", nil
		}
		return "not(" + strings.Join(parts, " and ") + ")", nil
	default:
		return "", nil
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
		return s[1 : len(s)-1]
	}
	return s
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
			out = append(out, `'"'`)
		}
	}
	return "concat(" + strings.Join(out, ", ") + ")"
}
