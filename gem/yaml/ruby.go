package yaml

// YAML <-> Ruby values as Psych has them, over go.yaml.in/yaml/v3's node
// tree: Hashes keep their order and any keys, aliases are the same object,
// merge keys (<<) merge, timestamps are Time or Date, !binary is a BINARY
// String, and the !ruby/... tags make objects. What a load may make is
// Psych's: safe_load only the plain types (and permitted_classes:), load
// Symbols too, unsafe_load everything. The size limits stay for every mode.

import (
	"encoding/base64"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/rb"
	yamllib "go.yaml.in/yaml/v3"
)

const (
	modeSafe = iota
	modeLoad
	modeUnsafe
)

type loadOpts struct {
	mode           int
	aliases        bool
	symbolizeNames bool
	freeze         bool
	permitted      map[string]bool // class names a safe load may make
	permittedSyms  map[string]bool // nil: any Symbol (when Symbol is permitted)
	fallback       oruby.Value
	hasFallback    bool
}

// errors raised as Psych's
type disallowedError struct{ class string }

func (e disallowedError) Error() string { return "Tried to load unspecified class: " + e.class }

type aliasError struct{}

func (aliasError) Error() string {
	return "Alias parsing was not enabled. To enable it, pass `aliases: true` to `Psych::load` or `Psych::safe_load`."
}

// rubyError: an exception Ruby raised while a value was made (pending in mrb)
type rubyError struct{}

func (rubyError) Error() string { return "ruby exception" }

// parseLoadOpts reads a load method's keywords as Psych's
func parseLoadOpts(mrb *oruby.MrbState, mode int) (*loadOpts, oruby.MrbValue) {
	o := &loadOpts{mode: mode, permitted: map[string]bool{}}
	if mode == modeUnsafe {
		o.aliases = true
	}
	if mode == modeLoad {
		o.permitted["Symbol"] = true
	}
	kw := mrb.KeywordArgs()
	if kw.Value().IsNil() || kw.EmptyP() {
		return o, nil
	}
	keys := kw.Keys()
	for i := 0; i < keys.Len(); i++ {
		k := keys.Item(i)
		v := kw.Get(k)
		name := ""
		if k.Type() == oruby.MrbTTSymbol {
			name = mrb.SymName(k.Symbol())
		}
		switch name {
		case "aliases":
			o.aliases = v.Bool()
		case "symbolize_names":
			o.symbolizeNames = v.Bool()
		case "freeze":
			o.freeze = v.Bool()
		case "fallback":
			o.fallback, o.hasFallback = v, true
		case "filename", "strict_integer", "parse_symbols":
		case "permitted_classes":
			if mode == modeUnsafe {
				return nil, mrb.Raise(mrb.EArgumentError(), "unknown keyword: :permitted_classes")
			}
			o.permitted = map[string]bool{}
			if v.IsArray() {
				for _, c := range oruby.RArraySlice(v) {
					if c.IsString() {
						o.permitted[mrb.String(c)] = true
					} else if n := mrb.Call(c, "name"); n.IsString() {
						o.permitted[mrb.String(n)] = true
					}
				}
			}
		case "permitted_symbols":
			if v.IsArray() && len(oruby.RArraySlice(v)) > 0 {
				o.permittedSyms = map[string]bool{}
				for _, s := range oruby.RArraySlice(v) {
					if s.Type() == oruby.MrbTTSymbol {
						o.permittedSyms[mrb.SymName(s.Symbol())] = true
					} else if s.IsString() {
						o.permittedSyms[mrb.String(s)] = true
					}
				}
			}
		default:
			return nil, mrb.Raise(mrb.EArgumentError(), "unknown keyword: "+mrb.Call(k, "inspect").String())
		}
	}
	return o, nil
}

// raiseYAML raises err as Psych does
func raiseYAML(mrb *oruby.MrbState, err error) oruby.MrbValue {
	switch e := err.(type) {
	case rubyError:
		if exc := mrb.Exc(); exc != nil {
			return exc.Value()
		}
		return mrb.Raise(mrb.ERuntimeError(), "YAML load failed")
	case disallowedError:
		return mrb.Raise(rb.ClassPath(mrb, "Psych::DisallowedClass"), e.Error())
	case aliasError:
		return mrb.Raise(rb.ClassPath(mrb, "Psych::AliasesNotEnabled"), e.Error())
	case yamlSyntaxError:
		return mrb.Raise(rb.ClassPath(mrb, "Psych::SyntaxError"), e.Error())
	}
	return mrb.Raise(mrb.EArgumentError(), err.Error())
}

type rdec struct {
	mrb     *oruby.MrbState
	o       *loadOpts
	anchors map[*yamllib.Node]oruby.Value
	nodes   int
	bytes   int
}

func newDecoder(mrb *oruby.MrbState, o *loadOpts) *rdec {
	return &rdec{mrb: mrb, o: o, anchors: map[*yamllib.Node]oruby.Value{}}
}

func (d *rdec) allow(class string) error {
	if d.o.mode == modeUnsafe || d.o.permitted[class] {
		return nil
	}
	return disallowedError{class}
}

// call runs a Ruby method, a pending exception becoming rubyError
func (d *rdec) call(recv oruby.MrbValue, name string, args ...any) (oruby.Value, error) {
	v := d.mrb.Call(recv, name, args...)
	if d.mrb.Exc() != nil {
		return v, rubyError{}
	}
	return v, nil
}

func (d *rdec) constant(path string) (oruby.Value, error) {
	v, err := d.call(d.mrb.ObjectClass().Value(), "const_get", path)
	return v, err
}

func (d *rdec) symbol(name string) (oruby.Value, error) {
	if err := d.allow("Symbol"); err != nil {
		return oruby.Nil, err
	}
	if d.o.mode != modeUnsafe && d.o.permittedSyms != nil && !d.o.permittedSyms[name] {
		return oruby.Nil, disallowedError{"Symbol(" + strconv.Quote(name) + ")"}
	}
	return d.mrb.SymbolValue(d.mrb.Intern(name)), nil
}

func (d *rdec) str(s string) oruby.Value {
	v := d.mrb.StrNew(s)
	if d.o.freeze {
		d.mrb.Call(v, "freeze")
	}
	return v
}

func (d *rdec) done(v oruby.Value) oruby.Value {
	if d.o.freeze {
		d.mrb.Call(v, "freeze")
	}
	return v
}

func (d *rdec) decode(node *yamllib.Node, depth int) (oruby.Value, error) {
	d.nodes++
	if depth > maxYAMLDepth || d.nodes > maxYAMLNodes {
		return oruby.Nil, unsafeYAMLError{message: "YAML structure exceeds safety limits"}
	}
	switch node.Kind {
	case yamllib.DocumentNode:
		if len(node.Content) == 0 {
			return oruby.Nil, nil
		}
		return d.decode(node.Content[0], depth)
	case yamllib.AliasNode:
		if !d.o.aliases {
			return oruby.Nil, aliasError{}
		}
		if v, ok := d.anchors[node.Alias]; ok {
			return v, nil
		}
		if node.Alias == nil {
			return oruby.Nil, yamlSyntaxError{err: fmt.Errorf("YAML alias has no target")}
		}
		return d.decode(node.Alias, depth+1)
	case yamllib.ScalarNode:
		d.bytes += len(node.Value)
		if d.bytes > maxYAMLBytes {
			return oruby.Nil, unsafeYAMLError{message: "expanded YAML exceeds size limit"}
		}
		v, err := d.scalar(node)
		if err == nil && node.Anchor != "" {
			d.anchors[node] = v
		}
		return v, err
	case yamllib.SequenceNode:
		return d.sequence(node, depth)
	case yamllib.MappingNode:
		return d.mapping(node, depth)
	}
	return oruby.Nil, unsafeYAMLError{message: fmt.Sprintf("YAML node kind %d is not supported", node.Kind)}
}

func (d *rdec) scalar(node *yamllib.Node) (oruby.Value, error) {
	mrb := d.mrb
	tag := node.ShortTag()
	val := node.Value
	switch tag {
	case "!!null":
		return oruby.Nil, nil
	case "!!bool":
		var b bool
		if err := node.Decode(&b); err != nil {
			return oruby.Nil, yamlSyntaxError{err: err}
		}
		return mrb.BoolValue(b), nil
	case "!!int":
		var i int64
		if err := node.Decode(&i); err == nil {
			return mrb.Int64Value(i), nil
		}
		z, ok := new(big.Int).SetString(strings.ReplaceAll(val, "_", ""), 0)
		if !ok {
			return d.str(val), nil
		}
		return mrb.BigIntValue(z), nil
	case "!!float":
		var f float64
		if err := node.Decode(&f); err != nil {
			return d.str(val), nil
		}
		return mrb.FloatValue(f), nil
	case "!!binary", "!binary":
		b, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(val), ""))
		if err != nil {
			return oruby.Nil, yamlSyntaxError{err: err}
		}
		return d.done(mrb.BytesValue(b)), nil
	case "!!timestamp":
		cls := "Time"
		if !strings.ContainsAny(val, " tT") {
			cls = "Date"
		}
		if err := d.allow(cls); err != nil {
			return oruby.Nil, err
		}
		return d.call(mrb.ModuleGet("Psych").Value(), "__oruby_timestamp", val)
	case "!!str", "!str", "!", "tag:yaml.org,2002:str":
		// a plain :name is a Symbol (Psych's ScalarScanner)
		if tag == "!!str" && node.Style == 0 && len(val) > 1 && val[0] == ':' {
			name := val[1:]
			if len(name) >= 2 && (name[0] == '"' && name[len(name)-1] == '"' || name[0] == '\'' && name[len(name)-1] == '\'') {
				name = name[1 : len(name)-1]
			}
			return d.symbol(name)
		}
		return d.str(val), nil
	case "!ruby/sym", "!ruby/symbol":
		return d.symbol(val)
	case "!ruby/regexp":
		if err := d.allow("Regexp"); err != nil {
			return oruby.Nil, err
		}
		src, opts := val, ""
		if strings.HasPrefix(val, "/") {
			if i := strings.LastIndexByte(val, '/'); i > 0 {
				src, opts = val[1:i], val[i+1:]
			}
		}
		flags := 0
		for _, c := range opts {
			switch c {
			case 'i':
				flags |= 1
			case 'x':
				flags |= 2
			case 'm':
				flags |= 4
			}
		}
		return d.call(mrb.ClassGet("Regexp").Value(), "new", src, flags)
	case "!ruby/class", "!ruby/module":
		if err := d.allow(map[string]string{"!ruby/class": "Class", "!ruby/module": "Module"}[tag]); err != nil {
			return oruby.Nil, err
		}
		return d.constant(val)
	case "!ruby/range":
		if err := d.allow("Range"); err != nil {
			return oruby.Nil, err
		}
		excl := strings.Contains(val, "...")
		sep := ".."
		if excl {
			sep = "..."
		}
		parts := strings.SplitN(val, sep, 2)
		if len(parts) != 2 {
			return d.str(val), nil
		}
		b, _ := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
		e, _ := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
		return d.call(mrb.ClassGet("Range").Value(), "new", b, e, excl)
	}
	if strings.HasPrefix(tag, "!ruby/object:") || strings.HasPrefix(tag, "!ruby/struct") {
		// an object written as a scalar: its class, allocated
		cls := strings.TrimPrefix(strings.TrimPrefix(tag, "!ruby/object:"), "!ruby/struct:")
		if err := d.allow(cls); err != nil {
			return oruby.Nil, err
		}
		k, err := d.constant(cls)
		if err != nil {
			return oruby.Nil, err
		}
		return d.call(k, "allocate")
	}
	// an unknown tag: the value as a String (Psych's)
	return d.str(val), nil
}

func (d *rdec) sequence(node *yamllib.Node, depth int) (oruby.Value, error) {
	ary := d.mrb.AryNew()
	if node.Anchor != "" {
		d.anchors[node] = ary.Value()
	}
	for _, child := range node.Content {
		v, err := d.decode(child, depth+1)
		if err != nil {
			return oruby.Nil, err
		}
		ary.Push(v)
	}
	return d.done(ary.Value()), nil
}

// pairs decodes a mapping's keys and values in order, merge keys applied
func (d *rdec) pairs(node *yamllib.Node, depth int, h oruby.Value) error {
	if len(node.Content)%2 != 0 {
		return yamlSyntaxError{err: fmt.Errorf("mapping has an unmatched key")}
	}
	for i := 0; i < len(node.Content); i += 2 {
		kn, vn := node.Content[i], node.Content[i+1]
		if kn.Kind == yamllib.ScalarNode && kn.ShortTag() == "!!merge" {
			v, err := d.decode(vn, depth+1)
			if err != nil {
				return err
			}
			merge := []oruby.Value{v}
			if v.IsArray() {
				merge = oruby.RArraySlice(v)
			}
			for _, m := range merge {
				if m.Type() == oruby.MrbTTHash {
					d.mrb.Call(h, "update", m)
				}
			}
			continue
		}
		k, err := d.decode(kn, depth+1)
		if err != nil {
			return err
		}
		if d.o.symbolizeNames && k.IsString() {
			k = d.mrb.SymbolValue(d.mrb.Intern(d.mrb.String(k)))
		}
		v, err := d.decode(vn, depth+1)
		if err != nil {
			return err
		}
		d.mrb.HashSet(h, k, v)
	}
	return nil
}

func (d *rdec) mapping(node *yamllib.Node, depth int) (oruby.Value, error) {
	mrb := d.mrb
	tag := node.ShortTag()
	switch {
	case tag == "!!map" || !strings.HasPrefix(tag, "!ruby/"):
		h := mrb.HashNew().Value()
		if node.Anchor != "" {
			d.anchors[node] = h
		}
		if err := d.pairs(node, depth, h); err != nil {
			return oruby.Nil, err
		}
		return d.done(h), nil
	case tag == "!ruby/range":
		if err := d.allow("Range"); err != nil {
			return oruby.Nil, err
		}
		h, err := d.fields(node, depth)
		if err != nil {
			return oruby.Nil, err
		}
		return d.call(mrb.ClassGet("Range").Value(), "new", mrb.HashGet(h, mrb.StrNew("begin")),
			mrb.HashGet(h, mrb.StrNew("end")), mrb.HashGet(h, mrb.StrNew("excl")).Bool())
	case strings.HasPrefix(tag, "!ruby/struct"):
		return d.structValue(node, depth, strings.TrimPrefix(strings.TrimPrefix(tag, "!ruby/struct"), ":"))
	case strings.HasPrefix(tag, "!ruby/exception"):
		return d.exception(node, depth, strings.TrimPrefix(strings.TrimPrefix(tag, "!ruby/exception"), ":"))
	case tag == "!ruby/object":
		return d.object(node, depth, "Object")
	case strings.HasPrefix(tag, "!ruby/object:"):
		return d.object(node, depth, strings.TrimPrefix(tag, "!ruby/object:"))
	case strings.HasPrefix(tag, "!ruby/hash:"):
		cls := strings.TrimPrefix(tag, "!ruby/hash:")
		if err := d.allow(cls); err != nil {
			return oruby.Nil, err
		}
		k, err := d.constant(cls)
		if err != nil {
			return oruby.Nil, err
		}
		h, err := d.call(k, "new")
		if err != nil {
			return oruby.Nil, err
		}
		if node.Anchor != "" {
			d.anchors[node] = h
		}
		return h, d.pairs(node, depth, h)
	}
	h := mrb.HashNew().Value()
	return h, d.pairs(node, depth, h)
}

// fields: a tagged mapping's entries as a plain Hash
func (d *rdec) fields(node *yamllib.Node, depth int) (oruby.Value, error) {
	h := d.mrb.HashNew().Value()
	saved := d.o.symbolizeNames
	d.o.symbolizeNames = false
	err := d.pairs(node, depth, h)
	d.o.symbolizeNames = saved
	return h, err
}

func (d *rdec) object(node *yamllib.Node, depth int, cls string) (oruby.Value, error) {
	mrb := d.mrb
	if err := d.allow(cls); err != nil {
		return oruby.Nil, err
	}
	k, err := d.constant(cls)
	if err != nil {
		return oruby.Nil, err
	}
	if cls == "OpenStruct" {
		h, err := d.fields(node, depth)
		if err != nil {
			return oruby.Nil, err
		}
		if t := mrb.HashGet(h, mrb.StrNew("table")); t.Type() == oruby.MrbTTHash && mrb.HashSize(h) == 1 {
			h = t // the old form: table: {:name => value}
		}
		return d.call(k, "new", h)
	}
	obj, err := d.call(k, "allocate")
	if err != nil {
		return oruby.Nil, err
	}
	if node.Anchor != "" {
		d.anchors[node] = obj
	}
	h, err := d.fields(node, depth)
	if err != nil {
		return oruby.Nil, err
	}
	if mrb.RespondTo(obj, mrb.Intern("init_with")) {
		// Psych's coder protocol, with the map only
		_, err := d.call(obj, "init_with", h)
		return obj, err
	}
	keys := mrb.HashKeys(h)
	for i := 0; i < keys.Len(); i++ {
		key := keys.Item(i)
		if _, err := d.call(obj, "instance_variable_set", "@"+mrb.Call(key, "to_s").String(), mrb.HashGet(h, key)); err != nil {
			return oruby.Nil, err
		}
	}
	return obj, nil
}

func (d *rdec) structValue(node *yamllib.Node, depth int, cls string) (oruby.Value, error) {
	mrb := d.mrb
	allowName := cls
	if allowName == "" {
		allowName = "Struct"
	}
	if err := d.allow(allowName); err != nil {
		return oruby.Nil, err
	}
	h, err := d.fields(node, depth)
	if err != nil {
		return oruby.Nil, err
	}
	keys := mrb.HashKeys(h)
	var k oruby.Value
	if cls == "" {
		syms := make([]any, 0, keys.Len())
		for i := 0; i < keys.Len(); i++ {
			syms = append(syms, mrb.Call(keys.Item(i), "to_sym"))
		}
		if k, err = d.call(mrb.ClassGet("Struct").Value(), "new", syms...); err != nil {
			return oruby.Nil, err
		}
	} else if k, err = d.constant(cls); err != nil {
		return oruby.Nil, err
	}
	obj, err := d.call(k, "new") // members nil (mruby's allocate has no slots)
	if err != nil {
		return oruby.Nil, err
	}
	for i := 0; i < keys.Len(); i++ {
		key := keys.Item(i)
		if _, err := d.call(obj, "[]=", mrb.Call(key, "to_sym"), mrb.HashGet(h, key)); err != nil {
			return oruby.Nil, err
		}
	}
	return obj, nil
}

func (d *rdec) exception(node *yamllib.Node, depth int, cls string) (oruby.Value, error) {
	mrb := d.mrb
	if cls == "" {
		cls = "Exception"
	}
	if err := d.allow(cls); err != nil {
		return oruby.Nil, err
	}
	k, err := d.constant(cls)
	if err != nil {
		return oruby.Nil, err
	}
	h, err := d.fields(node, depth)
	if err != nil {
		return oruby.Nil, err
	}
	msg := mrb.HashGet(h, mrb.StrNew("message"))
	var e oruby.Value
	if msg.IsNil() {
		e, err = d.call(k, "new")
	} else {
		e, err = d.call(k, "new", msg)
	}
	if err != nil {
		return oruby.Nil, err
	}
	if bt := mrb.HashGet(h, mrb.StrNew("backtrace")); !bt.IsNil() {
		if _, err := d.call(e, "set_backtrace", bt); err != nil {
			return oruby.Nil, err
		}
	}
	return e, nil
}

// ---- dump ----

type renc struct {
	mrb    *oruby.MrbState
	nodes  map[uintptr]*yamllib.Node
	anchor int
}

func scalar(tag, value string) *yamllib.Node {
	return &yamllib.Node{Kind: yamllib.ScalarNode, Tag: tag, Value: value}
}

// composite: the node a repeated object becomes, an anchor on its first
// occurrence and aliases after (cycles too); register before the children
func (e *renc) composite(v oruby.Value, n *yamllib.Node) (*yamllib.Node, bool) {
	p := v.Pointer()
	if first, ok := e.nodes[p]; ok {
		if first.Anchor == "" {
			e.anchor++
			first.Anchor = strconv.Itoa(e.anchor)
		}
		return &yamllib.Node{Kind: yamllib.AliasNode, Alias: first, Value: first.Anchor}, true
	}
	e.nodes[p] = n
	return n, false
}

func (e *renc) className(v oruby.Value) (string, error) {
	name := e.mrb.Call(e.mrb.Call(v, "class"), "name")
	if !name.IsString() {
		return "", fmt.Errorf("can't dump anonymous class: %s", e.mrb.Call(e.mrb.Call(v, "class"), "inspect").String())
	}
	return name.String(), nil
}

func (e *renc) mapNode(tag string, v oruby.Value) (*yamllib.Node, bool) {
	return e.composite(v, &yamllib.Node{Kind: yamllib.MappingNode, Tag: tag})
}

func (e *renc) addPair(n *yamllib.Node, key string, v oruby.Value, depth int) error {
	vn, err := e.encode(v, depth+1)
	if err != nil {
		return err
	}
	n.Content = append(n.Content, scalar("!!str", key), vn)
	return nil
}

func (e *renc) ivars(n *yamllib.Node, v oruby.Value, depth int) error {
	names := e.mrb.Call(v, "instance_variables")
	for _, s := range oruby.RArraySlice(names) {
		name := e.mrb.SymName(s.Symbol())
		if strings.HasPrefix(name, "@__") {
			continue // oruby's own
		}
		if err := e.addPair(n, strings.TrimPrefix(name, "@"), e.mrb.Call(v, "instance_variable_get", s), depth); err != nil {
			return err
		}
	}
	return nil
}

func (e *renc) is(v oruby.Value, cls string) bool {
	return e.mrb.ClassDefined(cls) && e.mrb.ObjIsKindOf(v, e.mrb.ClassGet(cls))
}

func (e *renc) encode(v oruby.Value, depth int) (*yamllib.Node, error) {
	mrb := e.mrb
	if depth > maxYAMLDepth {
		return nil, fmt.Errorf("Ruby value exceeds YAML nesting limit")
	}
	switch v.Type() {
	case oruby.MrbTTFalse:
		if v.IsNil() {
			return scalar("!!null", ""), nil // empty, as Psych writes it
		}
		return scalar("!!bool", "false"), nil
	case oruby.MrbTTTrue:
		return scalar("!!bool", "true"), nil
	case oruby.MrbTTInteger:
		return scalar("!!int", strconv.FormatInt(v.Int64(), 10)), nil
	case oruby.MrbTTFloat:
		f := v.Float64()
		switch {
		case math.IsNaN(f):
			return scalar("!!float", ".nan"), nil
		case math.IsInf(f, 1):
			return scalar("!!float", ".inf"), nil
		case math.IsInf(f, -1):
			return scalar("!!float", "-.inf"), nil
		}
		return scalar("!!float", mrb.Call(v, "to_s").String()), nil
	case oruby.MrbTTSymbol:
		return scalar("!!str", ":"+mrb.SymName(v.Symbol())), nil
	case oruby.MrbTTString:
		s := mrb.String(v)
		if oruby.StrBinary(v) && !utf8.ValidString(s) {
			n := scalar("!binary", base64.StdEncoding.EncodeToString([]byte(s)))
			n.Style = yamllib.LiteralStyle
			return n, nil
		}
		n := scalar("!!str", s)
		// quoted where a plain scalar would read back as something else
		// (Psych's single quotes; double where it needs escapes)
		if strings.HasPrefix(s, ":") || (&yamllib.Node{Kind: yamllib.ScalarNode, Value: s}).ShortTag() != "!!str" {
			n.Style = yamllib.DoubleQuotedStyle
			if !strings.HasPrefix(s, ":") && !strings.ContainsAny(s, "\n\r\t\\\"") && utf8.ValidString(s) {
				n.Style = yamllib.SingleQuotedStyle
			}
		}
		return n, nil
	case oruby.MrbTTArray:
		n, alias := e.composite(v, &yamllib.Node{Kind: yamllib.SequenceNode, Tag: "!!seq"})
		if alias {
			return n, nil
		}
		for _, item := range oruby.RArraySlice(v) {
			c, err := e.encode(item, depth+1)
			if err != nil {
				return nil, err
			}
			n.Content = append(n.Content, c)
		}
		return n, nil
	case oruby.MrbTTHash:
		n, alias := e.composite(v, &yamllib.Node{Kind: yamllib.MappingNode, Tag: "!!map"})
		if alias {
			return n, nil
		}
		for _, pair := range oruby.RHashEntries(v) {
			if pair[0].Type() == oruby.MrbTTUndef {
				continue
			}
			kn, err := e.encode(pair[0], depth+1)
			if err != nil {
				return nil, err
			}
			vn, err := e.encode(pair[1], depth+1)
			if err != nil {
				return nil, err
			}
			n.Content = append(n.Content, kn, vn)
		}
		return n, nil
	case oruby.MrbTTClass, oruby.MrbTTModule:
		name := mrb.Call(v, "name")
		if !name.IsString() {
			return nil, fmt.Errorf("can't dump anonymous class: %s", mrb.Call(v, "inspect").String())
		}
		tag := "!ruby/class"
		if v.Type() == oruby.MrbTTModule {
			tag = "!ruby/module"
		}
		n := scalar(tag, name.String())
		n.Style = yamllib.SingleQuotedStyle
		return n, nil
	}
	switch {
	case e.is(v, "Time"):
		f := "%Y-%m-%d %H:%M:%S.%9N %:z"
		if mrb.Call(v, "utc?").Bool() {
			f = "%Y-%m-%d %H:%M:%S.%9N Z"
		}
		// untagged: a plain timestamp, as Psych writes it
		return scalar("", mrb.Call(v, "strftime", f).String()), nil
	case e.is(v, "DateTime"):
		return scalar("!ruby/object:DateTime", mrb.Call(v, "strftime", "%Y-%m-%d %H:%M:%S.%9N %:z").String()), nil
	case e.is(v, "Date"):
		return scalar("", mrb.Call(v, "to_s").String()), nil
	case e.is(v, "Integer"):
		return scalar("", mrb.Call(v, "to_s").String()), nil // a Bignum
	case e.is(v, "Regexp"):
		return scalar("!ruby/regexp", mrb.Call(v, "inspect").String()), nil
	case e.is(v, "Range"):
		n, alias := e.mapNode("!ruby/range", v)
		if alias {
			return n, nil
		}
		for _, k := range []string{"begin", "end"} {
			if err := e.addPair(n, k, mrb.Call(v, k), depth); err != nil {
				return nil, err
			}
		}
		return n, e.addPair(n, "excl", mrb.Call(v, "exclude_end?"), depth)
	case e.is(v, "Struct"):
		tag := "!ruby/struct"
		if name := mrb.Call(mrb.Call(v, "class"), "name"); name.IsString() {
			tag += ":" + name.String()
		}
		n, alias := e.mapNode(tag, v)
		if alias {
			return n, nil
		}
		for _, m := range oruby.RArraySlice(mrb.Call(v, "members")) {
			if err := e.addPair(n, mrb.SymName(m.Symbol()), mrb.Call(v, "[]", m), depth); err != nil {
				return nil, err
			}
		}
		return n, nil
	case e.is(v, "Exception"):
		cls, err := e.className(v)
		if err != nil {
			return nil, err
		}
		n, alias := e.mapNode("!ruby/exception:"+cls, v)
		if alias {
			return n, nil
		}
		if err := e.addPair(n, "message", mrb.Call(v, "message"), depth); err != nil {
			return nil, err
		}
		if err := e.addPair(n, "backtrace", mrb.Call(v, "backtrace"), depth); err != nil {
			return nil, err
		}
		return n, nil
	case e.is(v, "OpenStruct"):
		n, alias := e.mapNode("!ruby/object:OpenStruct", v)
		if alias {
			return n, nil
		}
		h := mrb.Call(v, "to_h")
		for _, pair := range oruby.RHashEntries(h) {
			if err := e.addPair(n, mrb.Call(pair[0], "to_s").String(), pair[1], depth); err != nil {
				return nil, err
			}
		}
		return n, nil
	case e.is(v, "IO"):
		cls, err := e.className(v)
		if err != nil {
			return nil, err
		}
		n := &yamllib.Node{Kind: yamllib.MappingNode, Tag: "!ruby/object:" + cls, Style: yamllib.FlowStyle}
		return n, nil
	}
	if v.Type() == oruby.MrbTTObject {
		cls, err := e.className(v)
		if err != nil {
			return nil, err
		}
		tag := "!ruby/object:" + cls
		if cls == "Object" {
			tag = "!ruby/object"
		}
		n, alias := e.mapNode(tag, v)
		if alias {
			return n, nil
		}
		if mrb.RespondTo(v, mrb.Intern("encode_with")) {
			// Psych's coder protocol, as a Hash
			coder := mrb.HashNew().Value()
			if mrb.Call(v, "encode_with", coder); mrb.Exc() != nil {
				return nil, rubyError{}
			}
			for _, pair := range oruby.RHashEntries(coder) {
				if err := e.addPair(n, mrb.Call(pair[0], "to_s").String(), pair[1], depth); err != nil {
					return nil, err
				}
			}
			return n, nil
		}
		if err := e.ivars(n, v, depth); err != nil {
			return nil, err
		}
		return n, nil
	}
	return nil, fmt.Errorf("Ruby value of type %s is not supported", mrb.TypeName(v))
}

// psychTimestampRb is Psych's ScalarScanner#parse_time and the Date case,
// for !!timestamp scalars
const psychTimestampRb = `
module Psych
  @load_tags = {}
  @dump_tags = {}
  @domain_types = {}

  class << self
    attr_accessor :load_tags, :dump_tags, :domain_types

    def add_builtin_type(type_tag, &block)
      @domain_types["yaml.org,2002:#{type_tag}"] = ["yaml.org,2002:#{type_tag}", block]
    end

    def add_domain_type(domain, type_tag, &block)
      key = "#{domain}:#{type_tag}"
      @domain_types[key] = [key, block]
    end

    def remove_type(type_tag)
      @domain_types.delete(type_tag)
    end

    def libyaml_version
      [0, 2, 5]
    end
  end

  def self.__oruby_timestamp(string)
    unless string.match?(/[ tT]/)
      require "date"
      y, m, d = string.split("-").map { |x| Integer(x, 10) }
      return Date.new(y, m, d, Date::GREGORIAN)
    end
    date, time = string.split(/[ tT]/, 2)
    yy, m, dd = date.match(/^(-?\d{4})-(\d{1,2})-(\d{1,2})/).captures.map { |x| x.to_i }
    md = time.match(/(\d+:\d+:\d+)(?:\.(\d*))?\s*(Z|[-+]\d+(:\d\d)?)?/)
    hh, mm, ss = md[1].split(":").map { |x| x.to_i }
    us = (md[2] ? Rational("0.#{md[2]}") : 0) * 1000000
    time = Time.utc(yy, m, dd, hh, mm, ss, us.to_i)
    return time if md[3] == "Z"
    return Time.at(time.to_i, us.to_i) unless md[3]
    tz = md[3].match(/^([+\-]?\d{1,2})\:?(\d{1,2})?$/)[1..-1].compact.map { |digit| Integer(digit, 10) }
    offset = tz.first * 3600
    if offset < 0
      offset -= ((tz[1] || 0) * 60)
    else
      offset += ((tz[1] || 0) * 60)
    end
    Time.new(yy, m, dd, hh, mm, ss + us.to_i / 1_000_000r, offset)
  end
end
`
