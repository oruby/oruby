package file

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Dir.glob as MRI's dir.c does it (glob_make_pattern, glob_helper): a
// pattern is a list of components. A component without wildcards is joined
// to the path as it is (the file system checks the result at the end); a
// directory is read only for a component with wildcards or a recursive one
// ("**/", and Ruby 4.1's "***/" which follows symlinks), and then every
// active component is matched against its entries:
//
//   - ".." is never an entry; "." is one only for a directory not itself
//     reached through an entry, and not while a recursive component is
//     active (unless FNM_DOTMATCH)
//   - a leading "." is matched only by a "." in the component (unless
//     FNM_DOTMATCH); recursion skips dot directories the same way
//   - "**/" does not descend into symlinks; "***/" does, and stops at a
//     directory already on its way (matching the link, not entering it)
//   - a trailing "/" matches directories only and is kept
//   - braces are expanded where they are reached, into the rest of the
//     pattern (each expansion's results in turn); one in the last component
//     while a recursive one is active is matched per entry instead
//
// Each directory's entries are taken in sorted order (sort: true): the
// results of a pattern come sorted, depth first.

const (
	segPlain = iota
	segMagic
	segBrace     // the rest of the pattern, from a component with '{'
	segRecursive // "**/"
	segFollow    // "***/"
)

func isRecursive(kind int) bool { return kind == segRecursive || kind == segFollow }

type globSeg struct {
	text  string // as written (escapes included)
	plain string // a plain component without its escapes
	kind  int
	sep   string // the slashes after it
}

type globPattern struct {
	root  string // leading slashes of an absolute pattern
	segs  []globSeg
	trail string // slashes after the last component: directories only
}

func hasMagic(s string, flags int) bool {
	escape := flags&fnmNoescape == 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '*', '?', '[':
			return true
		case '\\':
			if escape {
				i++
			}
		}
	}
	return false
}

func hasBrace(s string, flags int) bool {
	escape := flags&fnmNoescape == 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '{':
			return true
		case '\\':
			if escape {
				i++
			}
		}
	}
	return false
}

func unescapeGlob(s string, flags int) string {
	if flags&fnmNoescape != 0 || !strings.Contains(s, "\\") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func parseGlob(pat string, flags int) globPattern {
	var gp globPattern
	i := 0
	for i < len(pat) && pat[i] == '/' {
		i++
	}
	gp.root = pat[:i]
	for i < len(pat) {
		j := i
		for j < len(pat) && pat[j] != '/' {
			j++
		}
		k := j
		for k < len(pat) && pat[k] == '/' {
			k++
		}
		seg := globSeg{text: pat[i:j], sep: pat[j:k]}
		last := k >= len(pat)
		switch {
		case hasBrace(seg.text, flags):
			// the rest is expanded when it is reached
			gp.segs = append(gp.segs, globSeg{text: pat[i:], kind: segBrace})
			return gp
		case (seg.text == "**" || seg.text == "***") && seg.sep != "":
			seg.kind = segRecursive
			if seg.text == "***" {
				seg.kind = segFollow
			}
			// consecutive recursive components are one ("***" if any is)
			if n := len(gp.segs); n > 0 && isRecursive(gp.segs[n-1].kind) {
				if seg.kind == segFollow {
					gp.segs[n-1].kind = segFollow
				}
				gp.segs[n-1].sep = seg.sep
				if last {
					gp.trail = seg.sep
				}
				i = k
				continue
			}
		case hasMagic(seg.text, flags):
			seg.kind = segMagic
		default:
			seg.kind = segPlain
			seg.plain = unescapeGlob(seg.text, flags)
		}
		gp.segs = append(gp.segs, seg)
		if last && seg.sep != "" {
			gp.trail = seg.sep
		}
		i = k
	}
	return gp
}

// textFrom is the pattern from component i on, as written
func (gp globPattern) textFrom(i int) string {
	var b strings.Builder
	for _, s := range gp.segs[i:] {
		b.WriteString(s.text)
		b.WriteString(s.sep)
	}
	return b.String()
}

type globber struct {
	flags   int
	base    string // the directory relative patterns start in ("" the cwd)
	hasBase bool
	sorted  bool
	out     []string
}

// fsPath is where path is on the file system
func (g *globber) fsPath(path string) string {
	if path == "" {
		if g.base == "" {
			return "."
		}
		return g.base
	}
	if g.base == "" || filepath.IsAbs(path) {
		return path
	}
	return g.base + "/" + path
}

func join(path, sep, name string) string {
	if path == "" {
		return name
	}
	return path + sep + name
}

type globState struct {
	path      string
	sep       string // the separator before the next component
	pos       []int  // components to match next (len(segs): the end)
	fromEntry bool   // path's last component is a directory entry
	endOnly   bool   // a "***/" cycle: the link matches, nothing in it
	ancestors []fileID
}

func (g *globber) run(pat string) {
	gp := parseGlob(pat, g.flags)
	if len(gp.segs) == 0 && gp.root == "" {
		return // the empty pattern
	}
	g.helper(gp, globState{path: gp.root, pos: []int{0}})
}

// expand adds the components after a recursive one (it matching no
// directory)
func expand(gp globPattern, pos []int) []int {
	var out []int
	seen := map[int]bool{}
	var add func(int)
	add = func(i int) {
		if seen[i] {
			return
		}
		seen[i] = true
		out = append(out, i)
		if i < len(gp.segs) && isRecursive(gp.segs[i].kind) {
			add(i + 1)
		}
	}
	for _, i := range pos {
		add(i)
	}
	return out
}

func (g *globber) helper(gp globPattern, st globState) {
	pos := expand(gp, st.pos)
	recursive := false
	for _, i := range pos {
		if i < len(gp.segs) && isRecursive(gp.segs[i].kind) {
			recursive = true
		}
	}
	// a brace reached here: the rest of the pattern, from the first
	// component active, once per expansion
	if !st.endOnly {
		for _, i := range pos {
			if i < len(gp.segs) && gp.segs[i].kind == segBrace && (!recursive || strings.Contains(gp.segs[i].text, "/")) {
				first := st.pos[0]
				for _, p := range st.pos {
					if p < first {
						first = p
					}
				}
				rest := gp.textFrom(first)
				braceExpand(rest, g.flags, func(e string) bool {
					sub := parseGlob(e, g.flags)
					path := st.path
					if path == "" {
						path = sub.root
					}
					if len(sub.segs) > 0 || sub.root != "" {
						g.helper(sub, globState{path: path, sep: st.sep, pos: []int{0}, fromEntry: st.fromEntry,
							ancestors: st.ancestors})
					}
					return true
				})
				return
			}
		}
	}

	needRead := false
	for _, i := range pos {
		if i == len(gp.segs) {
			g.matchEnd(gp, st)
			continue
		}
		if st.endOnly {
			continue
		}
		if gp.segs[i].kind != segPlain {
			needRead = true
		}
	}
	if st.endOnly {
		return
	}
	if !needRead {
		// plain components only: the names as written, no directory read
		for _, i := range pos {
			if i == len(gp.segs) {
				continue
			}
			s := gp.segs[i]
			g.helper(gp, globState{path: join(st.path, st.sep, s.plain), sep: s.sep, pos: []int{i + 1},
				ancestors: st.ancestors})
		}
		return
	}

	f, err := os.Open(g.fsPath(st.path))
	if err != nil {
		return
	}
	entries, err := f.ReadDir(-1)
	_ = f.Close()
	if err != nil && len(entries) == 0 {
		return
	}
	dotmatch := g.flags&fnmDotmatch != 0
	names := make([]string, 0, len(entries)+1)
	types := make(map[string]os.FileMode, len(entries)+1)
	if !st.fromEntry && (!recursive || dotmatch) {
		names = append(names, ".")
		types["."] = os.ModeDir
	}
	for _, e := range entries {
		names = append(names, e.Name())
		types[e.Name()] = e.Type()
	}
	if g.sorted {
		sort.Strings(names)
	}

	// what an entry continues with; one group per separator (an end needs
	// none, so it goes with the first: the match is yielded before what is
	// found under it)
	type next struct {
		sep     string
		pos     []int
		endOnly bool
		allEnd  bool
	}
	for _, name := range names {
		var nexts []next
		addPos := func(sep string, i int, endOnly bool) {
			isEnd := i == len(gp.segs)
			for k := range nexts {
				n := &nexts[k]
				if n.endOnly != endOnly {
					continue
				}
				if isEnd || n.allEnd || n.sep == sep {
					if !isEnd && n.allEnd {
						n.sep, n.allEnd = sep, false
					}
					n.pos = append(n.pos, i)
					return
				}
			}
			nexts = append(nexts, next{sep: sep, pos: []int{i}, endOnly: endOnly, allEnd: isEnd})
		}
		childPath := join(st.path, st.sep, name)
		childAnc := st.ancestors
		for _, i := range pos {
			if i == len(gp.segs) {
				continue
			}
			s := gp.segs[i]
			switch s.kind {
			case segRecursive, segFollow:
				if name == "." || (!dotmatch && name[0] == '.') {
					continue
				}
				mode := types[name]
				if mode&os.ModeSymlink != 0 {
					if s.kind != segFollow {
						continue // "**/" stays out of symlinks
					}
					fi, err := os.Stat(g.fsPath(childPath))
					if err != nil || !fi.IsDir() {
						continue
					}
				} else if !mode.IsDir() {
					continue
				}
				if s.kind == segFollow {
					anc := st.ancestors
					if anc == nil {
						if id, ok := statID(g.fsPath(st.path)); ok {
							anc = []fileID{id}
						}
					}
					if id, ok := statID(g.fsPath(childPath)); ok {
						cycle := false
						for _, a := range anc {
							if a == id {
								cycle = true
								break
							}
						}
						if cycle {
							addPos(s.sep, i+1, true)
							continue
						}
						childAnc = append(append([]fileID(nil), anc...), id)
					}
				}
				addPos(s.sep, i, false)
			case segBrace:
				// the last component, matched per entry
				if fnmatchBraceSeg(s.text, name, g.flags) {
					addPos("", len(gp.segs), false)
				}
			default:
				if fnmatchSeg(s.text, name, g.flags) {
					addPos(s.sep, i+1, false)
				}
			}
		}
		for _, n := range nexts {
			g.helper(gp, globState{path: childPath, sep: n.sep, pos: n.pos, fromEntry: true,
				endOnly: n.endOnly, ancestors: childAnc})
		}
	}
}

// matchEnd: the whole pattern matched path (if it is there)
func (g *globber) matchEnd(gp globPattern, st globState) {
	if gp.trail == "" {
		if st.path == "" {
			return
		}
		if !st.fromEntry {
			if _, err := os.Lstat(g.fsPath(st.path)); err != nil {
				return
			}
		}
		g.out = append(g.out, st.path)
		return
	}
	if fi, err := os.Stat(g.fsPath(st.path)); err != nil || !fi.IsDir() {
		return
	}
	if st.path == "" {
		if g.hasBase {
			g.out = append(g.out, gp.trail) // MRI: "**/" with base: yields "/"
		}
		return
	}
	g.out = append(g.out, st.path+gp.trail)
}

// fnmatchSeg matches one directory entry against one component
func fnmatchSeg(pattern, name string, flags int) bool {
	ok, _, _ := fnmatchHelper(pattern, 0, name, 0, flags&^fnmPathname)
	return ok
}

func fnmatchBraceSeg(pattern, name string, flags int) bool {
	found := false
	braceExpand(pattern, flags, func(p string) bool {
		if fnmatchSeg(p, name, flags) {
			found = true
			return false
		}
		return true
	})
	return found
}

// globPatterns runs each pattern in turn; the results are appended
func globPatterns(patterns []string, flags int, base string, hasBase, sorted bool) []string {
	g := &globber{flags: flags, base: base, hasBase: hasBase, sorted: sorted}
	for _, p := range patterns {
		g.run(p)
	}
	return g.out
}
