// Command gensym makes the symbol names oruby looks up by name into mruby
// presyms (preallocated symbols), and gives them to Go as constants.
//
// mruby builds its presym table by scanning the C sources of libmruby and
// its gems; oruby's own C (go-*.c, compiled by cgo) and its Go code are not
// among them, so every name they look up is interned at run time, which
// with many symbols is slow (mruby's symbol hash has 256 buckets). gensym
// collects those names:
//
//   - Go: string literals given to the oruby functions that take a name
//     (Intern, Call, GetGV, DefineMethod, ...), and the Sym* constants this
//     command generates, so a call site converted to a constant keeps its
//     name in the list;
//   - C (go-*.c, go-*.h): string literals given to mrb_intern_lit and the
//     other name-taking mruby functions, names in `const char *` tables, and
//     MRB_SYM() and its variants.
//
// -c writes them as the C source of the mruby-oruby-presym gem, which the
// build configs include, so the libmruby build makes them presyms. -go,
// after libmruby is built, writes presym_gen.go: a constant for each name
// the presym table has a macro for (MRB_SYM__to_s is Sym_to_s, MRB_SYM_Q__nil
// SymQ_nil, MRB_IVSYM__name IvSym_name, MRB_GVSYM__stdout GvSym_stdout,
// MRB_OPSYM__add OpSym_add), taken from the header through cgo, so it is
// always the number libmruby was built with. The file also carries a hash
// of the presym table: when the table changes, the file changes, and Go
// rebuilds the package's C code, which the build cache would otherwise keep
// with the old numbers.
//
// Without flags, gensym does both (-go only when libmruby is built).
// Files are only written when their content changes.
package main

import (
	"bytes"
	"crypto/sha256"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	gemSource = "scripts/mruby-oruby-presym/src/oruby_presym.c"
	goOutput  = "presym_gen.go"
	idHeader  = "mruby/build/host/include/mruby/presym/id.h"
)

// The Go functions and methods (root package API) whose string arguments
// are symbol names: methods, classes, constants, variables
var goNameFuncs = map[string]bool{
	"AttrAccessor": true, "AttrReader": true, "AttrWriter": true,
	"Call": true, "CheckIntern": true, "Class": true,
	"ClassDefined": true, "ClassDefinedUnder": true, "ClassGet": true, "ClassGetUnder": true,
	"Const": true, "ConstDefined": true, "ConstDefinedAt": true, "ConstGet": true,
	"ConvertType": true, "CheckConvertType": true,
	"DefineAlias": true, "DefineClass": true, "DefineClassFunc": true, "DefineClassMethod": true,
	"DefineClassUnder": true, "DefineConst": true, "DefineGlobalConst": true,
	"DefineGlobalFunction": true, "DefineGoClass": true, "DefineGoClassUnder": true,
	"DefineGoModule": true, "DefineGoModuleUnder": true, "DefineMethod": true,
	"DefineMethodFunc": true, "DefineModule": true, "DefineModuleFunc": true,
	"DefineModuleFunction": true, "DefineModuleUnder": true, "DefineSingletonFunc": true,
	"DefineSingletonMethod": true, "Funcall": true, "GetGV": true, "GetIV": true,
	"GetObjGV": true, "Intern": true, "InternCheck": true, "InternLit": true,
	"InternStatic": true, "ModuleGet": true, "ModuleGetUnder": true, "NewInstance": true,
	"SetGV": true, "SetIV": true, "Sym": true, "UndefClassMethod": true, "UndefMethod": true,
	"RespondTo": true,
}

// The C functions whose string arguments are symbol names
var cNameFuncs = map[string]bool{
	"mrb_intern_lit": true, "mrb_intern_cstr": true, "mrb_intern_static": true, "mrb_intern": true,
	"mrb_define_method": true, "mrb_define_private_method": true, "mrb_define_class_method": true,
	"mrb_define_singleton_method": true, "mrb_define_module_function": true,
	"mrb_define_class": true, "mrb_define_class_under": true, "mrb_define_module": true,
	"mrb_define_module_under": true, "mrb_define_const": true, "mrb_define_global_const": true,
	"mrb_define_alias": true, "mrb_class_get": true, "mrb_class_get_under": true,
	"mrb_module_get": true, "mrb_module_get_under": true, "mrb_funcall": true,
	"mrb_undef_method": true, "mrb_undef_class_method": true,
	"wrap_cfunc": true, "bt_replace": true, "_mrb_gv_hook": true,
}

// lib/mruby/presym.rb: OPERATORS
var operators = map[string]string{
	"!": "not", "%": "mod", "&": "and", "*": "mul", "+": "add", "-": "sub", "/": "div",
	"<": "lt", ">": "gt", "^": "xor", "`": "tick", "|": "or", "~": "neg", "!=": "neq",
	"!~": "nmatch", "&&": "andand", "**": "pow", "+@": "plus", "-@": "minus", "<<": "lshift",
	"<=": "le", "==": "eq", "=~": "match", ">=": "ge", ">>": "rshift", "[]": "aref",
	"||": "oror", "<=>": "cmp", "===": "eqq", "[]=": "aset",
}

var (
	wordSym  = regexp.MustCompile(`^(\$|@@|@)?([A-Za-z_][A-Za-z0-9_]*)([?!=])?$`)
	punctGv  = regexp.MustCompile("^\\$([!@~=/\\\\,;.<>_*$?:\"&`'+0]|[1-9][0-9]*|-[0-9A-Za-z])$")
	constRef = regexp.MustCompile(`^(Sym|SymQ|SymB|SymE|IvSym|GvSym|CvSym|OpSym)_(\w+)$`)
	cMacro   = regexp.MustCompile(`\bMRB_(SYM|SYM_Q|SYM_B|SYM_E|IVSYM|GVSYM|CVSYM|OPSYM)(?:_2)?\(\s*(?:mrb\s*,\s*)?(\w+)\s*\)`)
	cTable   = regexp.MustCompile(`(?s)const\s+char\s*\*\s*(?:const\s+)?\w+\s*(?:\[[^\]]*\])+\s*=\s*\{(.*?)\};`)
	cString  = regexp.MustCompile(`"((?:[^"\\]|\\.)*)"`)
)

// validName: a name mruby can hold as a symbol that is worth a presym
func validName(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	if m := wordSym.FindStringSubmatch(s); m != nil {
		return m[1] == "" || m[3] == "" // no prefix with a suffix
	}
	if _, ok := operators[s]; ok {
		return true
	}
	return punctGv.MatchString(s)
}

// macro is the name of the presym macro for name, as lib/mruby/presym.rb
// write_id_header() makes it; "" when there is none
func macro(name string) string {
	if m := wordSym.FindStringSubmatch(name); m != nil {
		prefix := map[string]string{"": "", "$": "GV", "@@": "CV", "@": "IV"}[m[1]]
		suffix := map[string]string{"": "", "!": "_B", "?": "_Q", "=": "_E"}[m[3]]
		if m[1] != "" && m[3] != "" {
			return ""
		}
		return "MRB_" + prefix + "SYM" + suffix + "__" + m[2]
	}
	if op, ok := operators[name]; ok {
		return "MRB_OPSYM__" + op
	}
	return ""
}

// goConst is the Go constant for a macro: MRB_SYM_Q__nil -> SymQ_nil
func goConst(m string) string {
	head, core, _ := strings.Cut(strings.TrimPrefix(m, "MRB_"), "__")
	switch head {
	case "SYM":
		return "Sym_" + core
	case "SYM_Q":
		return "SymQ_" + core
	case "SYM_B":
		return "SymB_" + core
	case "SYM_E":
		return "SymE_" + core
	case "IVSYM":
		return "IvSym_" + core
	case "GVSYM":
		return "GvSym_" + core
	case "CVSYM":
		return "CvSym_" + core
	case "OPSYM":
		return "OpSym_" + core
	}
	return ""
}

// nameOfRef is the name a constant (Sym_x) or macro kind and core
// (SYM_Q, x) stands for; "" when unknown
func nameOfRef(kind, core string) string {
	switch kind {
	case "Sym", "SYM":
		return core
	case "SymQ", "SYM_Q":
		return core + "?"
	case "SymB", "SYM_B":
		return core + "!"
	case "SymE", "SYM_E":
		return core + "="
	case "IvSym", "IVSYM":
		return "@" + core
	case "GvSym", "GVSYM":
		return "$" + core
	case "CvSym", "CVSYM":
		return "@@" + core
	case "OpSym", "OPSYM":
		for op, n := range operators {
			if n == core {
				return op
			}
		}
	}
	return ""
}

type names map[string]bool

func (n names) add(s string) {
	if validName(s) {
		n[s] = true
	}
}

func (n names) sorted() []string {
	out := make([]string, 0, len(n))
	for s := range n {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func skipDir(path string) bool {
	switch filepath.Base(path) {
	case "mruby", "lib", "tmp", "bin", "node_modules", "testdata":
		return true
	}
	return strings.HasPrefix(filepath.Base(path), ".") && path != "."
}

func scanGo(root string, n names) error {
	fset := token.NewFileSet()
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && skipDir(path) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || filepath.Base(path) == goOutput {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		ast.Inspect(f, func(node ast.Node) bool {
			switch x := node.(type) {
			case *ast.CallExpr:
				var fn string
				switch f := x.Fun.(type) {
				case *ast.SelectorExpr:
					fn = f.Sel.Name
				case *ast.Ident:
					fn = f.Name
				}
				if !goNameFuncs[fn] {
					return true
				}
				for _, a := range x.Args {
					if lit, ok := a.(*ast.BasicLit); ok && lit.Kind == token.STRING {
						if s, err := strconv.Unquote(lit.Value); err == nil {
							n.add(s)
						}
					}
				}
			case *ast.Ident:
				if m := constRef.FindStringSubmatch(x.Name); m != nil {
					n.add(nameOfRef(m[1], m[2]))
				}
			}
			return true
		})
		return nil
	})
}

func unescapeC(s string) string {
	if u, err := strconv.Unquote(`"` + s + `"`); err == nil {
		return u
	}
	return s
}

// cCalls calls fn with the name and the argument text of each call in src
// (comments and strings outside the arguments skipped)
func cCalls(src string, fn func(name, args string)) {
	isIdent := func(c byte) bool {
		return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
	}
	for i := 0; i < len(src); i++ {
		switch {
		case strings.HasPrefix(src[i:], "//"):
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case strings.HasPrefix(src[i:], "/*"):
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				return
			}
			i += end + 3
		case src[i] == '"' || src[i] == '\'':
			q := src[i]
			for i++; i < len(src) && src[i] != q; i++ {
				if src[i] == '\\' {
					i++
				}
			}
		case isIdent(src[i]) && (i == 0 || !isIdent(src[i-1])):
			j := i
			for j < len(src) && isIdent(src[j]) {
				j++
			}
			name := src[i:j]
			k := j
			for k < len(src) && (src[k] == ' ' || src[k] == '\t') {
				k++
			}
			if k < len(src) && src[k] == '(' && cNameFuncs[name] {
				depth, e := 0, k
			args:
				for ; e < len(src); e++ {
					switch src[e] {
					case '(':
						depth++
					case ')':
						depth--
						if depth == 0 {
							break args
						}
					case '"':
						for e++; e < len(src) && src[e] != '"'; e++ {
							if src[e] == '\\' {
								e++
							}
						}
					}
				}
				if e < len(src) {
					fn(name, src[k+1:e])
				}
			}
			i = j - 1
		}
	}
}

func scanC(root string, n names) error {
	files, err := filepath.Glob(filepath.Join(root, "go-*.[ch]"))
	if err != nil {
		return err
	}
	for _, path := range files {
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		src := string(b)
		cCalls(src, func(_, args string) {
			for _, m := range cString.FindAllStringSubmatch(args, -1) {
				n.add(unescapeC(m[1]))
			}
		})
		for _, t := range cTable.FindAllStringSubmatch(src, -1) {
			for _, m := range cString.FindAllStringSubmatch(t[1], -1) {
				n.add(unescapeC(m[1]))
			}
		}
		for _, m := range cMacro.FindAllStringSubmatch(src, -1) {
			n.add(nameOfRef(m[1], m[2]))
		}
	}
	return nil
}

func cQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func writeIfChanged(path string, data []byte) (bool, error) {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, data) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, data, 0o644)
}

func genC(list []string) []byte {
	var b bytes.Buffer
	b.WriteString(`/* Code generated by tools/gensym; DO NOT EDIT. */

/* The symbol names oruby's C and Go code look up by name. mruby's presym
   scan finds them here, so the libmruby build makes them preallocated
   symbols; the interns never run. */

#include <mruby.h>

void
mrb_mruby_oruby_presym_gem_init(mrb_state *mrb)
{
  if (mrb) return;
`)
	for _, s := range list {
		fmt.Fprintf(&b, "  (void)mrb_intern_lit(mrb, %s);\n", cQuote(s))
	}
	b.WriteString(`}

void
mrb_mruby_oruby_presym_gem_final(mrb_state *mrb)
{
}
`)
	return b.Bytes()
}

func genGo(list []string, header []byte) ([]byte, []string, error) {
	have := map[string]bool{}
	for _, line := range strings.Split(string(header), "\n") {
		if rest, ok := strings.CutPrefix(line, "#define "); ok {
			if name, _, ok := strings.Cut(rest, " "); ok {
				have[name] = true
			}
		}
	}
	var missing []string
	type entry struct{ goName, macro, name string }
	var entries []entry
	for _, s := range list {
		m := macro(s)
		if m == "" {
			continue
		}
		if !have[m] {
			missing = append(missing, s)
			continue
		}
		entries = append(entries, entry{goConst(m), m, s})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].goName < entries[j].goName })
	sum := sha256.Sum256(header)
	var b bytes.Buffer
	fmt.Fprintf(&b, `// Code generated by tools/gensym; DO NOT EDIT.

package oruby

// #include "go-mrb.h"
import "C"

// presymTable is the hash of the presym table these constants were
// generated against (%s); it changes with the table, so this
// file does, and Go rebuilds the package's C code with the new numbers.
const presymTable = "%x"

// Preallocated symbols of the names oruby looks up, as constants: the same
// symbol in every state, with no lookup. MRB_SYM__to_s is Sym_to_s,
// MRB_SYM_Q__nil SymQ_nil, MRB_SYM_B__x SymB_x, MRB_SYM_E__x SymE_x,
// MRB_IVSYM__x IvSym_x, MRB_GVSYM__x GvSym_x, MRB_CVSYM__x CvSym_x and
// MRB_OPSYM__add OpSym_add. Regenerated by `+"`make`"+` (tools/gensym).
const (
`, idHeader, sum[:8])
	for _, e := range entries {
		fmt.Fprintf(&b, "\t%s MrbSym = C.%s // %s\n", e.goName, e.macro, strconv.Quote(e.name))
	}
	b.WriteString(")\n")
	out, err := format.Source(b.Bytes())
	return out, missing, err
}

func main() {
	doC := flag.Bool("c", false, "write the presym gem's C list")
	doGo := flag.Bool("go", false, "write "+goOutput+" from the built presym table")
	root := flag.String("root", ".", "oruby source directory")
	flag.Parse()
	both := !*doC && !*doGo

	n := names{}
	if err := scanGo(*root, n); err != nil {
		fail(err)
	}
	if err := scanC(*root, n); err != nil {
		fail(err)
	}
	list := n.sorted()

	if *doC || both {
		changed, err := writeIfChanged(filepath.Join(*root, gemSource), genC(list))
		if err != nil {
			fail(err)
		}
		if changed {
			fmt.Printf("gensym: %s: %d names\n", gemSource, len(list))
		}
	}
	if *doGo || both {
		header, err := os.ReadFile(filepath.Join(*root, idHeader))
		if err != nil {
			if both {
				fmt.Println("gensym: libmruby is not built; run make for " + goOutput)
				return
			}
			fail(err)
		}
		out, missing, err := genGo(list, header)
		if err != nil {
			fail(err)
		}
		if len(missing) > 0 {
			fail(fmt.Errorf("not presyms in %s (rebuild libmruby: make): %s", idHeader, strings.Join(missing, " ")))
		}
		changed, err := writeIfChanged(filepath.Join(*root, goOutput), out)
		if err != nil {
			fail(err)
		}
		if changed {
			fmt.Printf("gensym: %s written\n", goOutput)
		}
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gensym:", err)
	os.Exit(1)
}
