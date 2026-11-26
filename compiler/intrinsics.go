package compiler

import "sort"

// intrinsicPkg describes the Go import alias and path used when emitting
// guarded direct Go calls to an AOTDirect Go extension package in goext/.
type intrinsicPkg struct {
	alias      string
	importPath string
}

var aotIntrinsicPkgs = map[string]intrinsicPkg{
	"fast_blank":   {alias: "goextfastblank", importPath: GoExtModule + "/oruby-fast_blank"},
	"escape_utils": {alias: "goextescapeutils", importPath: GoExtModule + "/oruby-escape_utils"},
	"xxhash":       {alias: "goextxxhash", importPath: GoExtModule + "/oruby-xxhash"},
	"zstd-ruby":    {alias: "goextzstd", importPath: GoExtModule + "/zstd"},
	"brotli":       {alias: "goextbrotli", importPath: GoExtModule + "/brotli"},
}

func (g *CodeGen) initIntrinsics() {
	g.extEnabled = make(map[string]bool, len(g.opts.GoExts))
	for _, sp := range g.opts.GoExts {
		if sp.AOTDirect {
			g.extEnabled[sp.Gem] = true
		}
	}
	g.usedIntrinsics = make(map[string]bool)
}

func (g *CodeGen) markIntrinsicUsed(gem string) {
	g.usedIntrinsics[gem] = true
	if g.modIntrinsics != nil {
		g.modIntrinsics[gem] = true
	}
}

func sortedIntrinsicPkgs(used map[string]bool) []intrinsicPkg {
	var keys []string
	for k := range used {
		if _, ok := aotIntrinsicPkgs[k]; ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	out := make([]intrinsicPkg, 0, len(keys))
	for _, k := range keys {
		out = append(out, aotIntrinsicPkgs[k])
	}
	return out
}

// emitGoExtIntrinsic emits a guarded direct Go-to-Go call to a pure-Go kernel
// in goext/ when an AOTDirect extension is enabled for this compilation.
// Every emitted fast path verifies mrb.AotIntrinsicOK (zero-cgo mruby method
// cache + class + refinement check) and falls back to normal Ruby dispatch on
// any mismatch, redefinition, wrong argument type, or runtime error.
func (g *CodeGen) emitGoExtIntrinsic(e *unitEmitter, idx int, a uint32, recv, symName string, list []string, n int) bool {
	if g.opts.Debug || len(g.extEnabled) == 0 {
		return false
	}

	switch symName {
	case "blank?", "blank_as?":
		if n != 0 || !g.extEnabled["fast_blank"] {
			return false
		}
		g.markIntrinsicUsed("fast_blank")
		slot := "oruby.AotIntrinsicFastBlank"
		if symName == "blank_as?" {
			slot = "oruby.AotIntrinsicFastBlankAs"
		}
		symVar := e.sym(symName)
		e.w("\tif mrb.AotIntrinsicOK(%s, %s) {\n", slot, recv)
		e.w("\t\tif _b, _ok := mrb.AotStringBytes(%s); _ok {\n", recv)
		e.w("\t\t\tr[%d] = mrb.BoolValue(goextfastblank.IsBlankBytes(_b))\n", a)
		e.w("\t\t} else {\n")
		e.w("\t\t\tr[%d], err = mrb.AotCall0(self, %s, %s, false)\n\t\t\t%s\n", a, recv, symVar, e.errChk(idx))
		e.w("\t\t}\n")
		e.w("\t} else {\n")
		e.w("\t\tr[%d], err = mrb.AotCall0(self, %s, %s, false)\n\t\t%s\n", a, recv, symVar, e.errChk(idx))
		e.w("\t}\n")
		return true

	case "escape_html":
		if n != 1 || !g.extEnabled["escape_utils"] {
			return false
		}
		g.markIntrinsicUsed("escape_utils")
		symVar := e.sym(symName)
		e.w("\tif _b, _ok := mrb.AotStringBytes(%s); _ok && mrb.AotIntrinsicOK(oruby.AotIntrinsicEscapeHTML, %s) {\n", list[0], recv)
		e.w("\t\tr[%d] = mrb.AotStrNew(goextescapeutils.EscapeHTMLBytes(_b, mrb.AotIntrinsicFlag(oruby.AotIntrinsicEscapeHTML)))\n", a)
		e.w("\t} else {\n")
		e.w("\t\tr[%d], err = mrb.AotCall1(self, %s, %s, %s, false)\n\t\t%s\n", a, recv, symVar, list[0], e.errChk(idx))
		e.w("\t}\n")
		return true

	case "escape_url":
		if n != 1 || !g.extEnabled["escape_utils"] {
			return false
		}
		g.markIntrinsicUsed("escape_utils")
		symVar := e.sym(symName)
		e.w("\tif _b, _ok := mrb.AotStringBytes(%s); _ok && mrb.AotIntrinsicOK(oruby.AotIntrinsicEscapeURL, %s) {\n", list[0], recv)
		e.w("\t\tr[%d] = mrb.AotStrNew(goextescapeutils.EscapeURLBytes(_b))\n", a)
		e.w("\t} else {\n")
		e.w("\t\tr[%d], err = mrb.AotCall1(self, %s, %s, %s, false)\n\t\t%s\n", a, recv, symVar, list[0], e.errChk(idx))
		e.w("\t}\n")
		return true

	case "xxh32":
		if (n != 1 && n != 2) || !g.extEnabled["xxhash"] {
			return false
		}
		g.markIntrinsicUsed("xxhash")
		symVar := e.sym(symName)
		if n == 1 {
			e.w("\tif _b, _ok := mrb.AotStringBytes(%s); _ok && mrb.AotIntrinsicOK(oruby.AotIntrinsicXXH32, %s) {\n", list[0], recv)
			e.w("\t\tr[%d] = oruby.AotFixnum(int64(goextxxhash.XXH32(_b, 0)))\n", a)
			e.w("\t} else {\n")
			e.w("\t\tr[%d], err = mrb.AotCall1(self, %s, %s, %s, false)\n\t\t%s\n", a, recv, symVar, list[0], e.errChk(idx))
			e.w("\t}\n")
		} else {
			e.w("\tif _b, _ok := mrb.AotStringBytes(%s); _ok && %s.Type() == oruby.MrbTTInteger && mrb.AotIntrinsicOK(oruby.AotIntrinsicXXH32, %s) {\n", list[0], list[1], recv)
			e.w("\t\tr[%d] = oruby.AotFixnum(int64(goextxxhash.XXH32(_b, uint32(aotInt(%s)))))\n", a, list[1])
			e.w("\t} else {\n")
			e.w("\t\tr[%d], err = mrb.AotCall(self, %s, %s, []oruby.Value{%s, %s}, mrb.NilValue(), false, false)\n\t\t%s\n", a, recv, symVar, list[0], list[1], e.errChk(idx))
			e.w("\t}\n")
		}
		return true

	case "xxh64":
		if (n != 1 && n != 2) || !g.extEnabled["xxhash"] {
			return false
		}
		g.markIntrinsicUsed("xxhash")
		symVar := e.sym(symName)
		if n == 1 {
			e.w("\tif _b, _ok := mrb.AotStringBytes(%s); _ok && mrb.AotIntrinsicOK(oruby.AotIntrinsicXXH64, %s) {\n", list[0], recv)
			e.w("\t\t_h := goextxxhash.XXH64(_b, 0)\n")
			e.w("\t\tif _h <= 1<<63-1 {\n\t\t\tr[%d] = oruby.AotFixnum(int64(_h))\n\t\t} else {\n\t\t\tr[%d] = mrb.Uint64Value(_h)\n\t\t}\n", a, a)
			e.w("\t} else {\n")
			e.w("\t\tr[%d], err = mrb.AotCall1(self, %s, %s, %s, false)\n\t\t%s\n", a, recv, symVar, list[0], e.errChk(idx))
			e.w("\t}\n")
		} else {
			e.w("\tif _b, _ok := mrb.AotStringBytes(%s); _ok && %s.Type() == oruby.MrbTTInteger && mrb.AotIntrinsicOK(oruby.AotIntrinsicXXH64, %s) {\n", list[0], list[1], recv)
			e.w("\t\t_h := goextxxhash.XXH64(_b, uint64(aotInt(%s)))\n", list[1])
			e.w("\t\tif _h <= 1<<63-1 {\n\t\t\tr[%d] = oruby.AotFixnum(int64(_h))\n\t\t} else {\n\t\t\tr[%d] = mrb.Uint64Value(_h)\n\t\t}\n", a, a)
			e.w("\t} else {\n")
			e.w("\t\tr[%d], err = mrb.AotCall(self, %s, %s, []oruby.Value{%s, %s}, mrb.NilValue(), false, false)\n\t\t%s\n", a, recv, symVar, list[0], list[1], e.errChk(idx))
			e.w("\t}\n")
		}
		return true

	case "compress":
		if n != 1 || !g.extEnabled["zstd-ruby"] {
			return false
		}
		g.markIntrinsicUsed("zstd-ruby")
		symVar := e.sym(symName)
		e.w("\t{\n\t\t_hit := false\n")
		e.w("\t\tif _b, _ok := mrb.AotStringBytes(%s); _ok && mrb.AotIntrinsicOK(oruby.AotIntrinsicZstdCompress, %s) {\n", list[0], recv)
		e.w("\t\t\tif _out, _err := goextzstd.Compress(_b, 3); _err == nil {\n")
		e.w("\t\t\t\tr[%d] = mrb.AotBytesBinary(_out)\n\t\t\t\t_hit = true\n\t\t\t}\n\t\t}\n", a)
		e.w("\t\tif !_hit {\n\t\t\tr[%d], err = mrb.AotCall1(self, %s, %s, %s, false)\n\t\t\t%s\n\t\t}\n\t}\n", a, recv, symVar, list[0], e.errChk(idx))
		return true

	case "decompress":
		if n != 1 || !g.extEnabled["zstd-ruby"] {
			return false
		}
		g.markIntrinsicUsed("zstd-ruby")
		symVar := e.sym(symName)
		e.w("\t{\n\t\t_hit := false\n")
		e.w("\t\tif _b, _ok := mrb.AotStringBytes(%s); _ok && mrb.AotIntrinsicOK(oruby.AotIntrinsicZstdDecompress, %s) {\n", list[0], recv)
		e.w("\t\t\tif _out, _err := goextzstd.Decompress(_b); _err == nil {\n")
		e.w("\t\t\t\tr[%d] = mrb.AotBytesBinary(_out)\n\t\t\t\t_hit = true\n\t\t\t}\n\t\t}\n", a)
		e.w("\t\tif !_hit {\n\t\t\tr[%d], err = mrb.AotCall1(self, %s, %s, %s, false)\n\t\t\t%s\n\t\t}\n\t}\n", a, recv, symVar, list[0], e.errChk(idx))
		return true

	case "deflate":
		if n != 1 || !g.extEnabled["brotli"] {
			return false
		}
		g.markIntrinsicUsed("brotli")
		symVar := e.sym(symName)
		e.w("\t{\n\t\t_hit := false\n")
		e.w("\t\tif _b, _ok := mrb.AotStringBytes(%s); _ok && mrb.AotIntrinsicOK(oruby.AotIntrinsicBrotliDeflate, %s) {\n", list[0], recv)
		e.w("\t\t\tif _out, _err := goextbrotli.Deflate(_b, -1, 22); _err == nil {\n")
		e.w("\t\t\t\tr[%d] = mrb.AotBytesBinary(_out)\n\t\t\t\t_hit = true\n\t\t\t}\n\t\t}\n", a)
		e.w("\t\tif !_hit {\n\t\t\tr[%d], err = mrb.AotCall1(self, %s, %s, %s, false)\n\t\t\t%s\n\t\t}\n\t}\n", a, recv, symVar, list[0], e.errChk(idx))
		return true

	case "inflate":
		if n != 1 || !g.extEnabled["brotli"] {
			return false
		}
		g.markIntrinsicUsed("brotli")
		symVar := e.sym(symName)
		e.w("\t{\n\t\t_hit := false\n")
		e.w("\t\tif _b, _ok := mrb.AotStringBytes(%s); _ok && mrb.AotIntrinsicOK(oruby.AotIntrinsicBrotliInflate, %s) {\n", list[0], recv)
		e.w("\t\t\tif _out, _err := goextbrotli.Inflate(_b); _err == nil {\n")
		e.w("\t\t\t\tr[%d] = mrb.AotBytesBinary(_out)\n\t\t\t\t_hit = true\n\t\t\t}\n\t\t}\n", a)
		e.w("\t\tif !_hit {\n\t\t\tr[%d], err = mrb.AotCall1(self, %s, %s, %s, false)\n\t\t\t%s\n\t\t}\n\t}\n", a, recv, symVar, list[0], e.errChk(idx))
		return true
	}

	return false
}

// emitCoreStdlibIntrinsic emits guarded zero-cgo fast paths for hot Ruby core
// methods (String, Array, Hash). Every fast path checks mrb.AotCoreOK (pure-Go
// mruby 2-way method cache + exact core class + refinement check) and falls
// back to normal Ruby dispatch on any subclass, monkey-patch, or mismatch.
func (g *CodeGen) emitCoreStdlibIntrinsic(e *unitEmitter, idx int, a uint32, recv, symName string, list []string, n int) bool {
	if g.opts.Debug {
		return false
	}
	symVar := e.sym(symName)
	switch symName {
	case "!":
		if n != 0 {
			return false
		}
		e.w("\tr[%d], err = aotOpNot(mrb, self, %s, %s)\n\t%s\n", a, recv, symVar, e.errChk(idx))
		return true

	case "bytesize":
		if n != 0 {
			return false
		}
		e.w("\tr[%d], err = aotOpBytesize(mrb, self, %s, %s)\n\t%s\n", a, recv, symVar, e.errChk(idx))
		return true

	case "empty?":
		if n != 0 {
			return false
		}
		e.w("\tr[%d], err = aotOpEmpty(mrb, self, %s, %s)\n\t%s\n", a, recv, symVar, e.errChk(idx))
		return true

	case "length", "size":
		if n != 0 {
			return false
		}
		e.w("\tr[%d], err = aotOpLength(mrb, self, %s, %s, %v)\n\t%s\n", a, recv, symVar, symName == "size", e.errChk(idx))
		return true

	case "<<":
		if n != 1 {
			return false
		}
		e.w("\tr[%d], err = aotOpConcat(mrb, self, %s, %s, %s)\n\t%s\n", a, recv, list[0], symVar, e.errChk(idx))
		return true

	case "&", "|", "^", ">>":
		// Integer bit operations on two Integers, as the method would: the core
		// method must be the original (AotCoreOK), the result needs no bignum
		if n != 1 {
			return false
		}
		helper := "aotAndII"
		switch symName {
		case "|":
			helper = "aotOrII"
		case "^":
			helper = "aotXorII"
		case ">>":
			helper = "aotShrII"
		}
		e.w("\tif _z, _ok := %s(mrb, %s, %s); _ok {\n\t\tr[%d] = _z\n", helper, recv, list[0], a)
		e.w("\t} else {\n\t\tr[%d], err = mrb.AotCall1(self, %s, %s, %s, false)\n\t\t%s\n\t}\n", a, recv, symVar, list[0], e.errChk(idx))
		return true

	case "strip", "downcase", "upcase":
		if n != 0 {
			return false
		}
		slot := "oruby.AotCoreStrStrip"
		op := 0
		if symName == "downcase" {
			slot = "oruby.AotCoreStrDowncase"
			op = 1
		} else if symName == "upcase" {
			slot = "oruby.AotCoreStrUpcase"
			op = 2
		}
		e.w("\tr[%d], err = aotOpStrASCII(mrb, self, %s, %s, %d, %s)\n\t%s\n", a, recv, slot, op, symVar, e.errChk(idx))
		return true

	case "gsub":
		if n != 2 || e.mayReadMatchData {
			return false
		}
		e.w("\tr[%d], err = aotOpGsub(mrb, self, %s, %s, %s, %s)\n\t%s\n", a, recv, list[0], list[1], symVar, e.errChk(idx))
		return true

	case "split":
		if n != 1 {
			return false
		}
		e.w("\tr[%d], err = aotOpSplit(mrb, self, %s, %s, %s)\n\t%s\n", a, recv, list[0], symVar, e.errChk(idx))
		return true

	case "first":
		if n != 0 {
			return false
		}
		e.w("\tr[%d], err = aotOpFirst(mrb, self, %s, %s)\n\t%s\n", a, recv, symVar, e.errChk(idx))
		return true

	case "last":
		if n != 0 {
			return false
		}
		e.w("\tr[%d], err = aotOpLast(mrb, self, %s, %s)\n\t%s\n", a, recv, symVar, e.errChk(idx))
		return true

	case "getbyte":
		if n != 1 {
			return false
		}
		e.w("\tr[%d], err = aotOpGetbyte(mrb, self, %s, %s, %s)\n\t%s\n", a, recv, list[0], symVar, e.errChk(idx))
		return true

	case "ord":
		if n != 0 {
			return false
		}
		e.w("\tr[%d], err = aotOpOrd(mrb, self, %s, %s)\n\t%s\n", a, recv, symVar, e.errChk(idx))
		return true

	case "start_with?":
		if n != 1 {
			return false
		}
		e.w("\tr[%d], err = aotOpStartWith(mrb, self, %s, %s, %s)\n\t%s\n", a, recv, list[0], symVar, e.errChk(idx))
		return true

	case "include?":
		if n != 1 {
			return false
		}
		e.w("\tr[%d], err = aotOpInclude(mrb, self, %s, %s, %s)\n\t%s\n", a, recv, list[0], symVar, e.errChk(idx))
		return true
	}
	return false
}
