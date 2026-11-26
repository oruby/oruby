package compiler

import (
	"bytes"
	"fmt"
	"go/format"
	"regexp"
	"strconv"
	"strings"
)

// A grouped package build puts the modules of a bundle into several Go
// packages of about GroupSize modules each, instead of one package. `go build`
// compiles packages in parallel and caches each one, so a bundle of thousands
// of files builds on all cores and only the packages whose modules changed
// are built again.
//
// Modules that refer to each other's compiled functions (`Mod_N_...`, a direct
// call to a unique top-level method) stay in one package, since a call across
// packages would need exported names; modules in different packages find each
// other through the require registry in the main package, as `require` does.
//
// Every group package holds a copy of the (stateless) helpers, its own symbol
// and string tables numbered from zero, an exported Init that fills them for a
// state, and RequireHook, which main points at its registry.

type rawModule struct {
	index int
	text  string // the whole file, not yet formatted, with global sym_N/str_N names
}

type groupPackage struct {
	name       string // "g000", also the directory name
	importPath string
	modules    []int // indexes into the bundle's modules
}

var (
	modRefRe  = regexp.MustCompile(`\bMod_(\d+)_`)
	symUseRe  = regexp.MustCompile(`\bsym_(\d+)\b`)
	strUseRe  = regexp.MustCompile(`\bstr_(\d+)\b`)
	pkgLineRe = regexp.MustCompile(`(?m)^package \w+$`)
)

// planGroups packs the modules into packages of about size modules, keeping
// modules that refer to each other together (a larger group when they must).
func planGroups(raws []rawModule, size int, modulePath string) []groupPackage {
	n := len(raws)
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	for _, r := range raws {
		for _, m := range modRefRe.FindAllStringSubmatch(r.text, -1) {
			j, err := strconv.Atoi(m[1])
			if err == nil && j != r.index && j >= 0 && j < n {
				a, b := find(r.index), find(j)
				if a != b {
					if a < b {
						parent[b] = a
					} else {
						parent[a] = b
					}
				}
			}
		}
	}
	comps := make(map[int][]int)
	var order []int
	for i := 0; i < n; i++ {
		root := find(i)
		if _, ok := comps[root]; !ok {
			order = append(order, root)
		}
		comps[root] = append(comps[root], i)
	}
	var groups []groupPackage
	var cur []int
	flush := func() {
		if len(cur) == 0 {
			return
		}
		name := fmt.Sprintf("g%03d", len(groups))
		groups = append(groups, groupPackage{name: name, importPath: modulePath + "/" + name, modules: cur})
		cur = nil
	}
	for _, root := range order {
		cur = append(cur, comps[root]...)
		if len(cur) >= size {
			flush()
		}
	}
	flush()
	return groups
}

// localizeTables renames the global sym_N/str_N names in texts to numbers
// local to the group (in order of first use). It returns the rewritten texts,
// the var block declaring the tables and the statements that fill them for a
// state.
func (g *CodeGen) localizeTables(texts []string) (out []string, vars, initBody string) {
	symLocal := map[int]int{}
	strLocal := map[int]int{}
	var symOrder, strOrder []int
	note := func(re *regexp.Regexp, local map[int]int, order *[]int, s string) {
		for _, m := range re.FindAllStringSubmatch(s, -1) {
			k, err := strconv.Atoi(m[1])
			if err != nil {
				continue
			}
			if _, ok := local[k]; !ok {
				local[k] = len(*order)
				*order = append(*order, k)
			}
		}
	}
	for _, t := range texts {
		note(symUseRe, symLocal, &symOrder, t)
		note(strUseRe, strLocal, &strOrder, t)
	}
	out = make([]string, len(texts))
	for i, t := range texts {
		t = symUseRe.ReplaceAllStringFunc(t, func(s string) string {
			k, _ := strconv.Atoi(s[4:])
			return "sym_" + strconv.Itoa(symLocal[k])
		})
		t = strUseRe.ReplaceAllStringFunc(t, func(s string) string {
			k, _ := strconv.Atoi(s[4:])
			return "str_" + strconv.Itoa(strLocal[k])
		})
		out[i] = t
	}

	var v bytes.Buffer
	if len(symOrder) > 0 || len(strOrder) > 0 {
		v.WriteString("var (\n")
		for i, k := range symOrder {
			fmt.Fprintf(&v, "\tsym_%d oruby.MrbSym // %q\n", i, g.symList[k])
		}
		for i, k := range strOrder {
			fmt.Fprintf(&v, "\tstr_%d oruby.Value // %q\n", i, g.strList[k])
		}
		v.WriteString(")\n\n")
	}
	var b bytes.Buffer
	if len(symOrder) > 0 {
		var names, ptrs []string
		for i, k := range symOrder {
			names = append(names, fmt.Sprintf("%q", g.symList[k]))
			ptrs = append(ptrs, fmt.Sprintf("&sym_%d", i))
		}
		fmt.Fprintf(&b, "\tmrb.InternSymbols([]string{%s}, []*oruby.MrbSym{%s})\n", strings.Join(names, ", "), strings.Join(ptrs, ", "))
	}
	for i, k := range strOrder {
		fmt.Fprintf(&b, "\tstr_%d = mrb.StrNewStatic(%q)\n\tmrb.GCRegister(str_%d)\n", i, g.strList[k], i)
	}
	return out, v.String(), b.String()
}

// symTableMarker stands where a grouped build's main package gets its symbol
// table, which can be written only once the whole runtime text is known.
const symTableMarker = "/*ORUBY_SYMTABLE*/"

// pruneMainTable fills in the symbol table of the main package of a grouped
// build with the symbols and strings its own text uses; the group packages
// carry the rest.
func (g *CodeGen) pruneMainTable(src string) string {
	out, vars, initBody := g.localizeTables([]string{src})
	table := vars + "func initSymbols(mrb *oruby.MrbState) {\n" + initBody + "}\n\n"
	return strings.Replace(out[0], symTableMarker, table, 1)
}

// emitGroupPackages writes every group's files into files (keyed by relative
// path) and records the package of each module's root function in g.fnPkg.
func (g *CodeGen) emitGroupPackages(files map[string]string, modules []*ModuleUnit, raws []rawModule, groups []groupPackage) error {
	var helpers bytes.Buffer
	g.emitHelpers(&helpers)
	for gi, grp := range groups {
		g.opts.Progress.report("package", gi+1, len(groups), grp.name)
		texts := []string{"package " + grp.name + "\n\nimport (\n\t\"math\"\n\t\"unsafe\"\n\n\t\"github.com/oruby/oruby\"\n)\n" + helpers.String()}
		for _, mi := range grp.modules {
			texts = append(texts, pkgLineRe.ReplaceAllString(raws[mi].text, "package "+grp.name))
		}
		local, vars, initBody := g.localizeTables(texts)
		tables := "// Code generated by oruby-compiler. DO NOT EDIT.\n\npackage " + grp.name + "\n\nimport \"github.com/oruby/oruby\"\n\n" +
			vars + "// Init interns the group's symbols and strings in mrb.\nfunc Init(mrb *oruby.MrbState) {\n" + initBody + "}\n\n" + groupHook
		put := func(name, src string) error {
			if g.opts.NoFormat {
				files[grp.name+"/"+name] = src
				return nil
			}
			formatted, err := format.Source([]byte(src))
			if err != nil {
				return fmt.Errorf("formatting %s/%s: %w\nSource:\n%s", grp.name, name, err, src)
			}
			files[grp.name+"/"+name] = string(formatted)
			return nil
		}
		if err := put("aot_helpers.go", local[0]); err != nil {
			return err
		}
		if err := put("aot_tables.go", tables); err != nil {
			return err
		}
		for k, mi := range grp.modules {
			if err := put(modFileName(mi), local[k+1]); err != nil {
				return err
			}
			g.fnPkg[g.info[modules[mi].Root].fn] = grp.name
		}
	}
	return nil
}

// qualFn is fn as the main package names it: with its group package's name
// when the module was put in one.
func (g *CodeGen) qualFn(fn string) string {
	if pkg, ok := g.fnPkg[fn]; ok {
		return pkg + "." + fn
	}
	return fn
}

// modFileName is the file of module i. A file name that ends in _GOARCH (or
// _GOOS_GOARCH) is built only for that architecture, and "386" is one: the
// plain name of module 386 would be silently left out of the package on every
// other platform.
func modFileName(i int) string {
	if i == 386 {
		return "aot_mod_386_.go"
	}
	return fmt.Sprintf("aot_mod_%03d.go", i)
}

const groupHook = `// RequireHook is set by the main package to its require registry.
var RequireHook func(mrb *oruby.MrbState, self, feature oruby.Value, dir string, kind int) (oruby.Value, error)

func doCompiledRequire(mrb *oruby.MrbState, self, feature oruby.Value, dir string, kind int) (oruby.Value, error) {
	return RequireHook(mrb, self, feature, dir, kind)
}
`
