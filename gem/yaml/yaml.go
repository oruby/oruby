package yaml

import (
	"bytes"
	"errors"
	"fmt"
	"io"
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
	psych.Const("VERSION", "5.2.0")
	psychErr := mrb.DefineClassUnder(psych, "Exception", mrb.ClassGet("RuntimeError"))
	mrb.DefineClassUnder(psych, "SyntaxError", psychErr)
	mrb.DefineClassUnder(psych, "DisallowedClass", psychErr)
	badAlias := mrb.DefineClassUnder(psych, "BadAlias", psychErr)
	mrb.DefineClassUnder(psych, "AliasesNotEnabled", badAlias)
	psych.DefineClassMethod("load", yamlLoadMode(modeLoad), mrb.ArgsReq(1))
	psych.DefineClassMethod("safe_load", yamlLoadMode(modeSafe), mrb.ArgsReq(1))
	psych.DefineClassMethod("unsafe_load", yamlLoadMode(modeUnsafe), mrb.ArgsReq(1))
	psych.DefineClassMethod("load_file", yamlLoadFileMode(modeLoad), mrb.ArgsReq(1))
	psych.DefineClassMethod("safe_load_file", yamlLoadFileMode(modeSafe), mrb.ArgsReq(1))
	psych.DefineClassMethod("unsafe_load_file", yamlLoadFileMode(modeUnsafe), mrb.ArgsReq(1))
	psych.DefineClassMethod("load_stream", yamlLoadStream, mrb.ArgsReq(1)|mrb.ArgsBlock())
	psych.DefineClassMethod("parse", yamlParse, mrb.ArgsReq(1))
	psych.DefineClassMethod("parse_file", yamlParseFile, mrb.ArgsReq(1))
	psych.DefineClassMethod("dump", yamlDump, mrb.ArgsArg(1, 1))
	psych.DefineClassMethod("safe_dump", yamlDump, mrb.ArgsArg(1, 1))
	psych.DefineClassMethod("dump_stream", yamlDumpStream, mrb.ArgsRest())
	nodesMod := mrb.DefineModuleUnder(psych, "Nodes")
	docCls := mrb.DefineClassUnder(nodesMod, "Document", mrb.ObjectClass())
	docCls.DefineMethod("to_ruby", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.GetIV(self, "@__value")
	}, mrb.ArgsNone())
	docCls.DefineMethod("root", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return self
	}, mrb.ArgsNone())
	mrb.ObjectClass().DefineMethod("to_yaml", yamlToYAML, mrb.ArgsOpt(1))
	mrb.ConstSet(mrb.ObjectClass(), oruby.Sym_YAML, psych)
	if _, err := mrb.LoadString(psychTimestampRb); err != nil {
		return err
	}
	return nil
}

func yamlLoadMode(mode int) oruby.MrbFuncT {
	return func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		o, exc := parseLoadOpts(mrb, mode)
		if exc != nil {
			return exc
		}
		text, exc := yamlReadSource(mrb, mrb.GetArgsFirst())
		if exc != nil {
			return exc
		}
		return loadYAMLText(mrb, text, o)
	}
}

func yamlLoadFileMode(mode int) oruby.MrbFuncT {
	return func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		o, exc := parseLoadOpts(mrb, mode)
		if exc != nil {
			return exc
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
		return loadYAMLText(mrb, string(text), o)
	}
}

// loadYAMLText: the first document of text, as o's load makes it
func loadYAMLText(mrb *oruby.MrbState, text string, o *loadOpts) oruby.MrbValue {
	if len(text) > maxYAMLBytes {
		return mrb.Raise(mrb.EArgumentError(), "YAML input exceeds size limit")
	}
	var document yamllib.Node
	if err := yamllib.NewDecoder(strings.NewReader(text)).Decode(&document); err != nil {
		if !errors.Is(err, io.EOF) {
			return raiseYAML(mrb, yamlSyntaxError{err: err})
		}
	}
	if len(document.Content) == 0 {
		switch {
		case o.hasFallback:
			return o.fallback
		case o.mode == modeUnsafe:
			return mrb.FalseValue()
		}
		return mrb.NilValue()
	}
	v, err := newDecoder(mrb, o).decode(&document, 0)
	if err != nil {
		return raiseYAML(mrb, err)
	}
	return v
}

func yamlReadSource(mrb *oruby.MrbState, source oruby.Value) (string, oruby.MrbValue) {
	if source.IsString() {
		return mrb.String(source), nil
	}
	if mrb.RespondTo(source, oruby.Sym_read) {
		v, err := mrb.Funcall(source, oruby.Sym_read)
		if err != nil {
			return "", mrb.RaiseError(err)
		}
		if v.IsNil() {
			return "", nil
		}
		return mrb.String(v), nil
	}
	return "", mrb.Raise(mrb.ExcGet("TypeError"), fmt.Sprintf("YAML source must be a String, got %s", rb.TypeName(mrb, source)))
}

func yamlLoadStream(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	o, exc := parseLoadOpts(mrb, modeUnsafe)
	if exc != nil {
		return exc
	}
	text, exc := yamlReadSource(mrb, mrb.GetArgsFirst())
	if exc != nil {
		return exc
	}
	if len(text) > maxYAMLBytes {
		return mrb.Raise(mrb.EArgumentError(), "YAML input exceeds size limit")
	}
	blk := mrb.GetArgsBlock()
	decoder := yamllib.NewDecoder(strings.NewReader(text))
	out := mrb.AryNew()
	for {
		var document yamllib.Node
		if err := decoder.Decode(&document); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return raiseYAML(mrb, yamlSyntaxError{err: err})
		}
		rv, err := newDecoder(mrb, o).decode(&document, 0)
		if err != nil {
			return raiseYAML(mrb, err)
		}
		if !blk.IsNil() {
			mrb.Yield(blk, rv)
			if e := mrb.Exc(); e != nil {
				return e.Value()
			}
		} else {
			out.Push(rv)
		}
	}
	if !blk.IsNil() {
		return mrb.NilValue()
	}
	return out
}

func yamlParseText(mrb *oruby.MrbState, text string) oruby.MrbValue {
	if strings.TrimSpace(text) == "" {
		return mrb.FalseValue()
	}
	o, _ := parseLoadOpts(mrb, modeUnsafe)
	val := loadYAMLText(mrb, text, o)
	if mrb.Exc() != nil {
		return val
	}
	docCls := rb.ClassPath(mrb, "Psych::Nodes::Document")
	doc, err := mrb.ObjNew(docCls)
	if err != nil {
		return mrb.RaiseError(err)
	}
	mrb.SetIV(doc.Value(), "@__value", val.Value())
	return doc
}

func yamlParse(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	text, exc := yamlReadSource(mrb, mrb.GetArgsFirst())
	if exc != nil {
		return exc
	}
	return yamlParseText(mrb, text)
}

func yamlParseFile(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	path := mrb.GetArgsFirst()
	if !path.IsString() {
		return mrb.Raise(mrb.ExcGet("TypeError"), "YAML filename must be a String")
	}
	data, err := os.ReadFile(mrb.String(path))
	if err != nil {
		return mrb.Raise(rb.ClassPath(mrb, "Psych::Exception"), err.Error())
	}
	return yamlParseText(mrb, string(data))
}

func yamlDumpStream(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	items := mrb.GetArgs().Slice()
	var buf bytes.Buffer
	psych := mrb.ModuleGet("Psych")
	for _, it := range items {
		d := mrb.Call(psych, "dump", it)
		if e := mrb.Exc(); e != nil {
			return e.Value()
		}
		buf.Write(d.Bytes())
	}
	return mrb.BytesValue(buf.Bytes())
}

func yamlDump(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	node, err := (&renc{mrb: mrb, nodes: map[uintptr]*yamllib.Node{}}).encode(args.Item(0), 0)
	if err != nil {
		if e := mrb.Exc(); e != nil {
			return e.Value()
		}
		return mrb.Raise(mrb.ExcGet("TypeError"), err.Error())
	}
	var buf bytes.Buffer
	enc := yamllib.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&yamllib.Node{Kind: yamllib.DocumentNode, Content: []*yamllib.Node{node}}); err != nil {
		return mrb.Raise(mrb.EArgumentError(), err.Error())
	}
	enc.Close()
	encoded := buf.Bytes()
	// "--- value" unless the document starts with a block collection
	prefix := "--- "
	if node.Kind == yamllib.ScalarNode && node.Value == "" && node.Tag == "!!null" {
		prefix, encoded = "---\n", nil
	} else if (node.Kind == yamllib.MappingNode || node.Kind == yamllib.SequenceNode) &&
		len(node.Content) > 0 && node.Anchor == "" && !strings.HasPrefix(node.Tag, "!ruby/") {
		prefix = "---\n"
	}
	document := mrb.BytesValue(append([]byte(prefix), encoded...))
	if output := args.Item(1); !output.IsNil() && output.Type() != oruby.MrbTTHash {
		mrb.Call(output, "write", document)
		if exc := mrb.Exc(); exc != nil {
			return exc.Value()
		}
		return output
	}
	return document
}

func yamlToYAML(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	if args.Len() == 0 {
		return mrb.Call(mrb.ModuleGet("Psych"), "dump", self)
	}
	return mrb.Call(mrb.ModuleGet("Psych"), "dump", self, args.Item(0))
}
