package yaml

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strings"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/internal/rb"
	yamllib "go.yaml.in/yaml/v3"
)

const (
	maxYAMLBytes = 8 << 20
	maxYAMLDepth = 100
	maxYAMLNodes = 100000
)

type unsafeYAMLError struct{ message string }

func (e unsafeYAMLError) Error() string { return e.message }

type yamlSyntaxError struct{ err error }

func (e yamlSyntaxError) Error() string { return e.err.Error() }

func init() {
	oruby.LazyGem("yaml", initYAML)
	oruby.GemAlias("psych", "yaml")
}

func initYAML(mrb *oruby.MrbState) interface{} {
	psych := mrb.DefineModule("Psych")
	psychErr := mrb.DefineClassUnder(psych, "Exception", mrb.ClassGet("StandardError"))
	mrb.DefineClassUnder(psych, "SyntaxError", psychErr)
	psych.DefineClassMethod("load", yamlLoad, mrb.ArgsReq(1))
	psych.DefineClassMethod("safe_load", yamlLoad, mrb.ArgsReq(1))
	psych.DefineClassMethod("load_file", yamlLoadFile, mrb.ArgsReq(1))
	psych.DefineClassMethod("safe_load_file", yamlLoadFile, mrb.ArgsReq(1))
	psych.DefineClassMethod("dump", yamlDump, mrb.ArgsArg(1, 1))
	psych.DefineClassMethod("safe_dump", yamlDump, mrb.ArgsArg(1, 1))
	mrb.ObjectClass().DefineMethod("to_yaml", yamlToYAML, mrb.ArgsOpt(1))
	mrb.ConstSet(mrb.ObjectClass(), mrb.Intern("YAML"), psych)
	return nil
}

func yamlLoad(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	allowAliases, err := yamlAliasesEnabled(mrb)
	if err != nil {
		return mrb.Raise(mrb.EArgumentError(), err.Error())
	}
	source := mrb.GetArgsFirst()
	if !source.IsString() {
		return mrb.Raise(mrb.ExcGet("TypeError"), fmt.Sprintf("YAML source must be a String, got %s", rb.TypeName(mrb, source)))
	}
	text := mrb.String(source)
	if len(text) > maxYAMLBytes {
		return mrb.Raise(mrb.EArgumentError(), "YAML input exceeds size limit")
	}
	return loadYAMLText(mrb, text, allowAliases)
}

func yamlLoadFile(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	allowAliases, err := yamlAliasesEnabled(mrb)
	if err != nil {
		return mrb.Raise(mrb.EArgumentError(), err.Error())
	}
	path := mrb.GetArgsFirst()
	if !path.IsString() {
		return mrb.Raise(mrb.ExcGet("TypeError"), "YAML filename must be a String")
	}
	file, err := os.Open(mrb.String(path))
	if err != nil {
		return mrb.Raise(rb.ClassPath(mrb, "Psych::Exception"), err.Error())
	}
	defer file.Close()
	text, err := io.ReadAll(io.LimitReader(file, maxYAMLBytes+1))
	if err != nil {
		return mrb.Raise(rb.ClassPath(mrb, "Psych::Exception"), err.Error())
	}
	return loadYAMLText(mrb, string(text), allowAliases)
}

func yamlAliasesEnabled(mrb *oruby.MrbState) (bool, error) {
	kwargs := mrb.KeywordArgs()
	key := mrb.SymbolValue(mrb.Intern("aliases"))
	known := 0
	if kwargs.KeyP(key) {
		known = 1
	}
	if kwargs.Size(kwargs.Value()) != known {
		return false, errors.New("unsupported YAML keyword option")
	}
	return kwargs.Get(key).Bool(), nil
}

func loadYAMLText(mrb *oruby.MrbState, text string, allowAliases bool) oruby.MrbValue {
	if len(text) > maxYAMLBytes {
		return mrb.Raise(mrb.EArgumentError(), "YAML input exceeds size limit")
	}
	value, err := decodeYAML(text, allowAliases)
	if err != nil {
		var unsafeErr unsafeYAMLError
		if errors.As(err, &unsafeErr) {
			return mrb.Raise(mrb.EArgumentError(), err.Error())
		}
		var syntaxErr yamlSyntaxError
		if errors.As(err, &syntaxErr) {
			return mrb.Raise(rb.ClassPath(mrb, "Psych::SyntaxError"), err.Error())
		}
		return mrb.Raise(mrb.EArgumentError(), err.Error())
	}
	return mrb.Value(value)
}

func decodeYAML(source string, allowAliases bool) (interface{}, error) {
	decoder := yamllib.NewDecoder(strings.NewReader(source))
	var document yamllib.Node
	if err := decoder.Decode(&document); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, yamlSyntaxError{err: err}
	}
	if len(document.Content) == 0 {
		return nil, nil
	}
	var extra yamllib.Node
	if err := decoder.Decode(&extra); err == nil {
		return nil, unsafeYAMLError{message: "multiple YAML documents are not supported"}
	} else if !errors.Is(err, io.EOF) {
		return nil, yamlSyntaxError{err: err}
	}
	limits := yamlNodeLimits{allowAliases: allowAliases, active: make(map[*yamllib.Node]bool)}
	return decodeYAMLNode(document.Content[0], 0, &limits)
}

type yamlNodeLimits struct {
	nodes        int
	bytes        int
	allowAliases bool
	active       map[*yamllib.Node]bool
}

func decodeYAMLNode(node *yamllib.Node, depth int, limits *yamlNodeLimits) (interface{}, error) {
	limits.nodes++
	if depth > maxYAMLDepth || limits.nodes > maxYAMLNodes {
		return nil, unsafeYAMLError{message: "YAML structure exceeds safety limits"}
	}
	if node.Kind == yamllib.AliasNode {
		if !limits.allowAliases {
			return nil, unsafeYAMLError{message: "YAML aliases are disabled; pass aliases: true to enable them"}
		}
		if node.Alias == nil {
			return nil, yamlSyntaxError{err: errors.New("YAML alias has no target")}
		}
		if limits.active[node.Alias] {
			return nil, unsafeYAMLError{message: "cyclic YAML aliases are not supported"}
		}
		return decodeYAMLNode(node.Alias, depth+1, limits)
	}
	if node.Anchor != "" {
		limits.active[node] = true
		defer delete(limits.active, node)
	}
	if node.Kind == yamllib.SequenceNode && node.Tag != "!!seq" || node.Kind == yamllib.MappingNode && node.Tag != "!!map" {
		return nil, unsafeYAMLError{message: fmt.Sprintf("YAML tag %q is not supported", node.Tag)}
	}
	switch node.Kind {
	case yamllib.ScalarNode:
		limits.bytes += len(node.Value)
		if limits.bytes > maxYAMLBytes {
			return nil, unsafeYAMLError{message: "expanded YAML exceeds size limit"}
		}
		switch node.Tag {
		case "!!null":
			return nil, nil
		case "!!bool":
			var value bool
			if err := node.Decode(&value); err != nil {
				return nil, yamlSyntaxError{err: err}
			}
			return value, nil
		case "!!int":
			var value int64
			if err := node.Decode(&value); err != nil {
				return nil, unsafeYAMLError{message: "YAML integer is outside the supported range"}
			}
			return value, nil
		case "!!float":
			var value float64
			if err := node.Decode(&value); err != nil {
				return nil, yamlSyntaxError{err: err}
			}
			return value, nil
		case "!!str":
			return node.Value, nil
		default:
			return nil, unsafeYAMLError{message: fmt.Sprintf("YAML tag %q is not supported", node.Tag)}
		}
	case yamllib.SequenceNode:
		values := make([]interface{}, len(node.Content))
		for i, child := range node.Content {
			value, err := decodeYAMLNode(child, depth+1, limits)
			if err != nil {
				return nil, err
			}
			values[i] = value
		}
		return values, nil
	case yamllib.MappingNode:
		if len(node.Content)%2 != 0 {
			return nil, yamlSyntaxError{err: errors.New("mapping has an unmatched key")}
		}
		values := make(map[string]interface{}, len(node.Content)/2)
		for i := 0; i < len(node.Content); i += 2 {
			keyNode := node.Content[i]
			limits.nodes++
			limits.bytes += len(keyNode.Value)
			if limits.nodes > maxYAMLNodes || limits.bytes > maxYAMLBytes {
				return nil, unsafeYAMLError{message: "expanded YAML exceeds safety limits"}
			}
			if keyNode.Kind != yamllib.ScalarNode || keyNode.Tag != "!!str" || keyNode.Anchor != "" {
				return nil, unsafeYAMLError{message: "YAML mapping keys must be untagged strings"}
			}
			if _, exists := values[keyNode.Value]; exists {
				return nil, yamlSyntaxError{err: fmt.Errorf("duplicate mapping key %q", keyNode.Value)}
			}
			value, err := decodeYAMLNode(node.Content[i+1], depth+1, limits)
			if err != nil {
				return nil, err
			}
			values[keyNode.Value] = value
		}
		return values, nil
	default:
		return nil, unsafeYAMLError{message: fmt.Sprintf("YAML node kind %d is not supported", node.Kind)}
	}
}

func yamlDump(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	if !mrb.KeywordArgs().EmptyP() {
		return mrb.Raise(mrb.EArgumentError(), "unsupported YAML dump keyword option")
	}
	args := mrb.GetArgs()
	value, err := rubyYAMLValue(mrb, args.Item(0), make(map[uintptr]bool), 0)
	if err != nil {
		return mrb.Raise(mrb.EArgumentError(), err.Error())
	}
	encoded, err := yamllib.Marshal(value)
	if err != nil {
		return mrb.Raise(mrb.EArgumentError(), err.Error())
	}
	prefix := "--- "
	switch v := value.(type) {
	case nil:
		encoded = nil
		prefix = "---\n"
	case []interface{}:
		if len(v) != 0 {
			prefix = "---\n"
		}
	case map[string]interface{}:
		if len(v) != 0 {
			prefix = "---\n"
		}
	}
	document := mrb.BytesValue(append([]byte(prefix), encoded...))
	if output := args.Item(1); !output.IsNil() {
		mrb.Call(output, "write", document)
		if exc := mrb.Exc(); exc != nil {
			return exc.Value()
		}
		return output
	}
	return document
}

func yamlToYAML(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if !mrb.KeywordArgs().EmptyP() {
		return mrb.Raise(mrb.EArgumentError(), "unsupported YAML dump keyword option")
	}
	args := mrb.GetArgs()
	if args.Len() == 0 {
		return mrb.Call(mrb.ModuleGet("Psych"), "dump", self)
	}
	return mrb.Call(mrb.ModuleGet("Psych"), "dump", self, args.Item(0))
}

func rubyYAMLValue(mrb *oruby.MrbState, value oruby.Value, seen map[uintptr]bool, depth int) (interface{}, error) {
	if depth > maxYAMLDepth {
		return nil, errors.New("Ruby value exceeds YAML nesting limit")
	}
	if value.IsNil() {
		return nil, nil
	}
	switch value.Type() {
	case oruby.MrbTTFalse:
		return false, nil
	case oruby.MrbTTTrue:
		return true, nil
	case oruby.MrbTTInteger:
		return value.Int64(), nil
	case oruby.MrbTTFloat:
		f := value.Float64()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, errors.New("non-finite floats are not supported")
		}
		return f, nil
	case oruby.MrbTTString:
		return mrb.String(value), nil
	case oruby.MrbTTArray:
		if err := markYAMLComposite(value, seen); err != nil {
			return nil, err
		}
		items := oruby.RArraySlice(value)
		result := make([]interface{}, len(items))
		for i, item := range items {
			converted, err := rubyYAMLValue(mrb, item, seen, depth+1)
			if err != nil {
				return nil, err
			}
			result[i] = converted
		}
		return result, nil
	case oruby.MrbTTHash:
		if err := markYAMLComposite(value, seen); err != nil {
			return nil, err
		}
		entries := oruby.RHashEntries(value)
		result := make(map[string]interface{}, len(entries))
		for _, pair := range entries {
			if pair[0].Type() == oruby.MrbTTUndef {
				continue
			}
			if !pair[0].IsString() {
				return nil, errors.New("YAML only supports String hash keys")
			}
			key := mrb.String(pair[0])
			converted, err := rubyYAMLValue(mrb, pair[1], seen, depth+1)
			if err != nil {
				return nil, err
			}
			result[key] = converted
		}
		return result, nil
	default:
		return nil, fmt.Errorf("Ruby value of type %s is not supported", mrb.TypeName(value))
	}
}

func markYAMLComposite(value oruby.Value, seen map[uintptr]bool) error {
	pointer := value.Pointer()
	if seen[pointer] {
		return errors.New("YAML aliases and cyclic Ruby values are not supported")
	}
	seen[pointer] = true
	return nil
}
