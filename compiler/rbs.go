package compiler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/oruby/oruby"
)

// PrimType is a primitive Ruby type that the AOT compiler can represent as an
// unboxed Go value inside guarded specialized functions.
type PrimType uint8

const (
	TypeUnknown      PrimType = iota
	TypeInt64                 // RBS: Integer
	TypeFloat64               // RBS: Float
	TypeBool                  // RBS: bool
	TypeString                // RBS: String
	TypeArrayInt64            // RBS: Array[Integer]
	TypeArrayFloat64          // RBS: Array[Float]
)

func (t PrimType) String() string {
	switch t {
	case TypeInt64:
		return "Integer"
	case TypeFloat64:
		return "Float"
	case TypeBool:
		return "bool"
	case TypeString:
		return "String"
	case TypeArrayInt64:
		return "Array[Integer]"
	case TypeArrayFloat64:
		return "Array[Float]"
	default:
		return "untyped"
	}
}

// GoType returns the Go type name for an unboxed PrimType.
func (t PrimType) GoType() string {
	switch t {
	case TypeInt64:
		return "int64"
	case TypeFloat64:
		return "float64"
	case TypeBool:
		return "bool"
	case TypeString:
		return "string"
	case TypeArrayInt64, TypeArrayFloat64:
		return "oruby.Value"
	default:
		return "oruby.Value"
	}
}

// IsNumeric reports whether t is Integer or Float.
func (t PrimType) IsNumeric() bool {
	return t == TypeInt64 || t == TypeFloat64
}

// IsSpecializable reports whether t can be returned unboxed in typed Go codegen.
func (t PrimType) IsSpecializable() bool {
	return t == TypeInt64 || t == TypeFloat64 || t == TypeBool
}

// IsSpecializableParam reports whether t can be accepted as a parameter in typed Go codegen.
func (t PrimType) IsSpecializableParam() bool {
	return t.IsSpecializable() || t == TypeArrayInt64 || t == TypeArrayFloat64
}

// ParseRBSPrimType maps an RBS type expression to a PrimType (TypeUnknown for
// unions, optionals, user classes, or complex types).
func ParseRBSPrimType(expr string) PrimType {
	s := strings.TrimSpace(expr)
	s = strings.TrimPrefix(s, "::")
	switch s {
	case "Integer", "int":
		return TypeInt64
	case "Float":
		return TypeFloat64
	case "bool":
		return TypeBool
	case "String", "string":
		return TypeString
	}
	if strings.HasPrefix(s, "Array[") && strings.HasSuffix(s, "]") {
		inner := ParseRBSPrimType(s[len("Array[") : len(s)-1])
		switch inner {
		case TypeInt64:
			return TypeArrayInt64
		case TypeFloat64:
			return TypeArrayFloat64
		}
	}
	return TypeUnknown
}

type rbsMethodKey struct {
	className string // "" for top-level/Object, or "Foo" / "Foo::Bar"
	method    string
	singleton bool
}

// RBSMethodSig holds a parsed RBS method signature.
type RBSMethodSig struct {
	ClassName string
	Method    string
	Singleton bool
	Params    []PrimType
	Return    PrimType
	Source    string // file:line or origin description
}

// RBSEnv stores RBS method and instance-variable signatures loaded from .rbs
// files and inline RBS comments in .rb sources.
type RBSEnv struct {
	Methods map[rbsMethodKey]*RBSMethodSig
	Ivars   map[string]map[string]PrimType
}

// NewRBSEnv creates an empty RBSEnv.
func NewRBSEnv() *RBSEnv {
	return &RBSEnv{
		Methods: make(map[rbsMethodKey]*RBSMethodSig),
		Ivars:   make(map[string]map[string]PrimType),
	}
}

func normalizeRBSClass(name string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimPrefix(name, "::")
	if name == "Object" || name == "Kernel" {
		return ""
	}
	return name
}

// AddMethod registers a parsed RBS method signature.
func (env *RBSEnv) AddMethod(sig *RBSMethodSig) {
	if env == nil || sig == nil || sig.Method == "" {
		return
	}
	sig.ClassName = normalizeRBSClass(sig.ClassName)
	k := rbsMethodKey{
		className: sig.ClassName,
		method:    sig.Method,
		singleton: sig.Singleton,
	}
	env.Methods[k] = sig
}

// LookupMethod finds an RBS signature by class name, method name, and singleton flag.
func (env *RBSEnv) LookupMethod(className, method string, singleton bool) *RBSMethodSig {
	if env == nil {
		return nil
	}
	cn := normalizeRBSClass(className)
	if sig, ok := env.Methods[rbsMethodKey{className: cn, method: method, singleton: singleton}]; ok {
		return sig
	}
	return nil
}

// AddIvar registers a declared instance variable type on className.
func (env *RBSEnv) AddIvar(className, ivar string, pt PrimType) {
	if env == nil || ivar == "" || pt == TypeUnknown {
		return
	}
	cn := normalizeRBSClass(className)
	if env.Ivars[cn] == nil {
		env.Ivars[cn] = make(map[string]PrimType)
	}
	env.Ivars[cn][ivar] = pt
}

// LookupIvar returns the declared RBS type for ivar on className, or TypeUnknown.
func (env *RBSEnv) LookupIvar(className, ivar string) PrimType {
	if env == nil || ivar == "" {
		return TypeUnknown
	}
	cn := normalizeRBSClass(className)
	if m := env.Ivars[cn]; m != nil {
		return m[ivar]
	}
	return TypeUnknown
}

// Merge copies all signatures from other into env (other wins on collision).
func (env *RBSEnv) Merge(other *RBSEnv) {
	if env == nil || other == nil {
		return
	}
	for k, v := range other.Methods {
		env.Methods[k] = v
	}
	for cls, ivs := range other.Ivars {
		if env.Ivars[cls] == nil {
			env.Ivars[cls] = make(map[string]PrimType)
		}
		for k, v := range ivs {
			env.Ivars[cls][k] = v
		}
	}
}

// LoadRBSPaths loads all .rbs files named directly or found recursively under
// directories in paths. When mrb has the bundled rbs gem available (gem/rbs),
// it uses RBS::Parser; otherwise it falls back to the built-in Go RBS parser.
func LoadRBSPaths(mrb *oruby.MrbState, paths []string) (*RBSEnv, error) {
	env := NewRBSEnv()
	var files []string
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		fi, err := os.Stat(p)
		if err != nil {
			return nil, fmt.Errorf("reading RBS path %s: %w", p, err)
		}
		if !fi.IsDir() {
			files = append(files, p)
			continue
		}
		err = filepath.WalkDir(p, func(path string, d os.DirEntry, wErr error) error {
			if wErr != nil {
				return wErr
			}
			if !d.IsDir() && strings.HasSuffix(path, ".rbs") {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", f, err)
		}
		parsed, ok := parseRBSWithGem(mrb, string(data), f)
		if !ok {
			parsed = ParseRBSSource(string(data), f)
		}
		env.Merge(parsed)
	}
	return env, nil
}

// parseRBSWithGem parses an .rbs source string using ORuby's bundled RBS gem
// (gem/rbs C extension) when available in mrb. Returns (_, false) if the rbs
// gem is not loadable in mrb (e.g. minimal unit-test states without lib/oruby).
func parseRBSWithGem(mrb *oruby.MrbState, src, filename string) (*RBSEnv, bool) {
	if mrb == nil || !mrb.GemAvailable("rbs_extension.so") {
		return nil, false
	}
	mrb.SetGV("$__oruby_rbs_src__", src)
	mrb.SetGV("$__oruby_rbs_file__", filename)
	const extractor = `
begin
  require "rbs"
  require "json"
  _, _, decls = RBS::Parser.parse_signature($__oruby_rbs_src__)
  out = { "methods" => [], "ivars" => {} }
  walk = lambda do |list, prefix|
    list.each do |d|
      case d
      when RBS::AST::Declarations::Class, RBS::AST::Declarations::Module
        cname = d.name.name.to_s
        full = prefix.empty? ? cname : "#{prefix}::#{cname}"
        d.members.each do |m|
          case m
          when RBS::AST::Members::MethodDefinition
            ov = m.overloads.first
            next unless ov && ov.method_type.type.is_a?(RBS::Types::Function)
            fn = ov.method_type.type
            next unless fn.optional_positionals.empty? && fn.rest_positionals.nil? &&
                        fn.trailing_positionals.empty? && fn.required_keywords.empty? &&
                        fn.optional_keywords.empty? && fn.rest_keywords.nil?
            params = fn.required_positionals.map { |p| p.type.to_s }
            out["methods"] << {
              "class" => full,
              "method" => m.name.to_s,
              "singleton" => m.singleton?,
              "params" => params,
              "return" => fn.return_type.to_s
            }
          when RBS::AST::Members::InstanceVariable
            out["ivars"][full] ||= {}
            out["ivars"][full][m.name.to_s] = m.type.to_s
          end
        end
        walk.call(d.members, full)
      end
    end
  end
  walk.call(decls, "")
  JSON.generate(out)
rescue LoadError
  nil
end
`
	v, err := mrb.Eval(extractor)
	if err != nil || v.IsNil() || v.Type() != oruby.MrbTTString {
		return nil, false
	}
	var payload struct {
		Methods []struct {
			Class     string   `json:"class"`
			Method    string   `json:"method"`
			Singleton bool     `json:"singleton"`
			Params    []string `json:"params"`
			Return    string   `json:"return"`
		} `json:"methods"`
		Ivars map[string]map[string]string `json:"ivars"`
	}
	if err := json.Unmarshal([]byte(v.String()), &payload); err != nil {
		return nil, false
	}
	env := NewRBSEnv()
	for _, m := range payload.Methods {
		params := make([]PrimType, len(m.Params))
		for i, p := range m.Params {
			params[i] = ParseRBSPrimType(p)
		}
		env.AddMethod(&RBSMethodSig{
			ClassName: m.Class,
			Method:    m.Method,
			Singleton: m.Singleton,
			Params:    params,
			Return:    ParseRBSPrimType(m.Return),
			Source:    filename,
		})
	}
	for cls, ivs := range payload.Ivars {
		nc := normalizeRBSClass(cls)
		if env.Ivars[nc] == nil {
			env.Ivars[nc] = make(map[string]PrimType)
		}
		for iv, ts := range ivs {
			env.Ivars[nc][iv] = ParseRBSPrimType(ts)
		}
	}
	return env, true
}

var (
	reRBSClassDecl   = regexp.MustCompile(`^\s*(class|module)\s+([A-Z][A-Za-z0-9_:]*)`)
	reRBSDefDecl     = regexp.MustCompile(`^\s*def\s+(self\.)?([A-Za-z_][A-Za-z0-9_]*[?!=]?)\s*:\s*(.+)$`)
	reRBSIvarDecl    = regexp.MustCompile(`^\s*(@[A-Za-z_][A-Za-z0-9_]*)\s*:\s*([^#\s]+)`)
	reRBSFuncType    = regexp.MustCompile(`^\(([^)]*)\)\s*->\s*([^#|]+?)(?:\s*$|\s*\|)`)
	reRBSNullaryType = regexp.MustCompile(`^->\s*([^#|]+?)(?:\s*$|\s*\|)`)
	reRBInlineSig    = regexp.MustCompile(`^\s*#(?::|\s*@rbs)\s*(.+)$`)
	reRubyDefLine    = regexp.MustCompile(`^\s*def\s+(self\.)?([A-Za-z_][A-Za-z0-9_]*[?!=]?)(?:\s|\(|$|;)`)
	reRubyEndLine    = regexp.MustCompile(`^\s*end\b`)
	reRBSInlineParam = regexp.MustCompile(`^\s*#\s*@rbs\s+([A-Za-z_][A-Za-z0-9_]*)\s*:\s*(.+)$`)
	reRBSInlineRet   = regexp.MustCompile(`^\s*#\s*@rbs\s+return\s*:\s*(.+)$`)
)

// parseMethodTypeExpr parses `(Integer x, Float y) -> Float` or `() -> Integer`
// or `-> Integer` into positional parameter types and return type.
func parseMethodTypeExpr(expr string) ([]PrimType, PrimType, bool) {
	s := strings.TrimSpace(expr)
	if idx := strings.Index(s, "#"); idx >= 0 {
		s = strings.TrimSpace(s[:idx])
	}
	// Reject block / keyword / optional / rest signatures for now so we never
	// mis-specialize complex signatures.
	if strings.Contains(s, "{") {
		return nil, TypeUnknown, false
	}
	if m := reRBSNullaryType.FindStringSubmatch(s); m != nil {
		return nil, ParseRBSPrimType(m[1]), true
	}
	m := reRBSFuncType.FindStringSubmatch(s)
	if m == nil {
		return nil, TypeUnknown, false
	}
	paramStr := strings.TrimSpace(m[1])
	retType := ParseRBSPrimType(m[2])
	if paramStr == "" {
		return nil, retType, true
	}
	parts := strings.Split(paramStr, ",")
	params := make([]PrimType, 0, len(parts))
	for _, raw := range parts {
		p := strings.TrimSpace(raw)
		if p == "" {
			continue
		}
		if strings.HasPrefix(p, "?") || strings.HasPrefix(p, "*") || strings.Contains(p, ":") {
			return nil, TypeUnknown, false
		}
		// RBS allows `Type name` (e.g. `Integer n`) or bare `Type` (`Integer`)
		fields := strings.Fields(p)
		if len(fields) == 0 {
			return nil, TypeUnknown, false
		}
		params = append(params, ParseRBSPrimType(fields[0]))
	}
	return params, retType, true
}

// ParseRBSSource parses standard .rbs declarations in pure Go (used as fallback
// when gem/rbs is not present in the compiler's mrb_state).
func ParseRBSSource(src, filename string) *RBSEnv {
	env := NewRBSEnv()
	var scopeStack []string
	var blockDepth []bool // true = class/module scope pushed

	lines := strings.Split(src, "\n")
	for i, raw := range lines {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if m := reRBSClassDecl.FindStringSubmatch(line); m != nil {
			scopeStack = append(scopeStack, m[2])
			blockDepth = append(blockDepth, true)
			continue
		}
		if reRubyEndLine.MatchString(line) {
			if len(blockDepth) > 0 {
				if blockDepth[len(blockDepth)-1] && len(scopeStack) > 0 {
					scopeStack = scopeStack[:len(scopeStack)-1]
				}
				blockDepth = blockDepth[:len(blockDepth)-1]
			}
			continue
		}
		curClass := strings.Join(scopeStack, "::")
		if m := reRBSDefDecl.FindStringSubmatch(line); m != nil {
			params, ret, ok := parseMethodTypeExpr(m[3])
			if !ok {
				continue
			}
			env.AddMethod(&RBSMethodSig{
				ClassName: curClass,
				Method:    m[2],
				Singleton: m[1] != "",
				Params:    params,
				Return:    ret,
				Source:    fmt.Sprintf("%s:%d", filename, i+1),
			})
			continue
		}
		if m := reRBSIvarDecl.FindStringSubmatch(line); m != nil && curClass != "" {
			nc := normalizeRBSClass(curClass)
			if env.Ivars[nc] == nil {
				env.Ivars[nc] = make(map[string]PrimType)
			}
			env.Ivars[nc][m[1]] = ParseRBSPrimType(m[2])
		}
	}
	return env
}

// ExtractInlineRBS scans a Ruby source file for inline RBS annotations directly
// preceding `def` statements:
//
//	#: (Integer, Float) -> Float
//	def compound(n, rate)
//
// or rbs-inline parameter/return comments:
//
//	# @rbs n: Integer
//	# @rbs rate: Float
//	# @rbs return: Float
//	def compound(n, rate)
func ExtractInlineRBS(src, filename string) *RBSEnv {
	env := NewRBSEnv()
	var scopeStack []string
	var blockStack []bool // true when the block opened a class/module

	type pendingSig struct {
		directExpr string
		paramTypes []PrimType
		retType    PrimType
		hasReturn  bool
		line       int
	}
	var pend *pendingSig

	lines := strings.Split(src, "\n")
	for i, raw := range lines {
		trimmed := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if trimmed == "" {
			pend = nil
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			if addInlineIvarIfMatch(env, scopeStack, trimmed) {
				continue
			}
			if m := reRBSInlineRet.FindStringSubmatch(trimmed); m != nil {
				if pend == nil {
					pend = &pendingSig{line: i + 1}
				}
				pend.retType = ParseRBSPrimType(m[1])
				pend.hasReturn = true
				continue
			}
			if m := reRBSInlineParam.FindStringSubmatch(trimmed); m != nil {
				if pend == nil {
					pend = &pendingSig{line: i + 1}
				}
				pend.paramTypes = append(pend.paramTypes, ParseRBSPrimType(m[2]))
				continue
			}
			if m := reRBInlineSig.FindStringSubmatch(trimmed); m != nil {
				pend = &pendingSig{directExpr: m[1], line: i + 1}
				continue
			}
			continue
		}

		// Track class/module nesting conservatively for enclosing scope names
		if m := reRBSClassDecl.FindStringSubmatch(trimmed); m != nil {
			pend = nil
			if !strings.Contains(trimmed, "; end") {
				scopeStack = append(scopeStack, m[2])
				blockStack = append(blockStack, true)
			}
			continue
		}
		if m := reRubyDefLine.FindStringSubmatch(trimmed); m != nil {
			curClass := strings.Join(scopeStack, "::")
			if pend != nil {
				var params []PrimType
				var ret PrimType
				ok := false
				if pend.directExpr != "" {
					params, ret, ok = parseMethodTypeExpr(pend.directExpr)
				} else if pend.hasReturn || len(pend.paramTypes) > 0 {
					params, ret, ok = pend.paramTypes, pend.retType, true
				}
				if ok {
					env.AddMethod(&RBSMethodSig{
						ClassName: curClass,
						Method:    m[2],
						Singleton: m[1] != "",
						Params:    params,
						Return:    ret,
						Source:    fmt.Sprintf("%s:%d", filename, pend.line),
					})
				}
				pend = nil
			}
			// Single-line endless def or def...end doesn't push onto blockStack
			if !strings.Contains(trimmed, "=") || strings.Contains(trimmed, "(") && !strings.Contains(trimmed, ") =") {
				if !strings.HasSuffix(trimmed, " end") && !strings.Contains(trimmed, "; end") {
					blockStack = append(blockStack, false)
				}
			}
			continue
		}
		pend = nil
		if startsBlock(trimmed) {
			blockStack = append(blockStack, false)
		} else if reRubyEndLine.MatchString(trimmed) && len(blockStack) > 0 {
			if blockStack[len(blockStack)-1] && len(scopeStack) > 0 {
				scopeStack = scopeStack[:len(scopeStack)-1]
			}
			blockStack = blockStack[:len(blockStack)-1]
		}
	}
	return env
}

var (
	reRBSInlineIvar = regexp.MustCompile(`^#\s*@rbs\s+(@[A-Za-z_][A-Za-z0-9_]*)\s*:\s*(.+)$`)
)

func addInlineIvarIfMatch(env *RBSEnv, scopeStack []string, trimmed string) bool {
	if m := reRBSInlineIvar.FindStringSubmatch(trimmed); m != nil {
		if pt := ParseRBSPrimType(m[2]); pt != TypeUnknown {
			env.AddIvar(strings.Join(scopeStack, "::"), m[1], pt)
		}
		return true
	}
	return false
}

func startsBlock(trimmed string) bool {
	for _, kw := range []string{"if ", "unless ", "while ", "until ", "case ", "begin", "for "} {
		if strings.HasPrefix(trimmed, kw) && !strings.Contains(trimmed, "; end") && !strings.HasSuffix(trimmed, " end") {
			return true
		}
	}
	return strings.HasSuffix(trimmed, " do") || strings.Contains(trimmed, " do |")
}
