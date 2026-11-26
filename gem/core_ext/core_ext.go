package core_ext

import (
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/oruby/oruby"
	// gems the Ruby extensions build on, always linked with core_ext
	_ "github.com/oruby/oruby/gem/io"
	_ "github.com/oruby/oruby/gem/io/file"
	_ "github.com/oruby/oruby/gem/process"
	_ "github.com/oruby/oruby/gem/regexp"
	_ "github.com/oruby/oruby/gem/thread"
	_ "github.com/oruby/oruby/gem/time"
)

//go:embed ruby/*.rb
var rubyFiles embed.FS

//go:embed core_ext.orb
var coreExtOrbBytes []byte

type coreExtEntry struct {
	name     string
	mrbBytes []byte
	rbBytes  []byte
}

var (
	coreExtOrbOnce sync.Once
	coreExtEntries []coreExtEntry
)

func getCoreExtEntries() []coreExtEntry {
	coreExtOrbOnce.Do(func() {
		entries, err := fs.ReadDir(rubyFiles, "ruby")
		if err != nil {
			panic("core_ext read error: " + err.Error())
		}
		var orb *oruby.OrbFile
		var orbHashes map[string]string
		if len(coreExtOrbBytes) > 0 {
			if parsed, perr := oruby.ParseOrb("<internal:core_ext.orb>", coreExtOrbBytes); perr == nil {
				if e, ok := parsed.Lookup("_manifest.json"); ok {
					var hashes map[string]string
					if json.Unmarshal(parsed.EntryData(e), &hashes) == nil {
						orb = parsed
						orbHashes = hashes
					}
				}
			}
		}
		out := make([]coreExtEntry, 0, len(entries))
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".rb") {
				continue
			}
			path := "ruby/" + entry.Name()
			data, err := rubyFiles.ReadFile(path)
			if err != nil {
				panic(fmt.Sprintf("core_ext read error for %s: %s", path, err))
			}
			if len(data) == 0 {
				continue
			}
			ent := coreExtEntry{name: entry.Name(), rbBytes: data}
			if orb != nil {
				sum := fmt.Sprintf("%x", sha256.Sum256(data))
				if orbHashes[entry.Name()] == sum {
					if e, ok := orb.Lookup(entry.Name()); ok {
						ent.mrbBytes = orb.EntryData(e)
					}
				}
			}
			out = append(out, ent)
		}
		coreExtEntries = out
	})
	return coreExtEntries
}

func init() {
	oruby.Gem("rbconfig", func(mrb *oruby.MrbState) interface{} {
		arch := oruby.RubyArch()
		cpu := arch[:strings.IndexByte(arch, '-')]
		host := cpu + "-unknown-" + runtime.GOOS
		switch runtime.GOOS {
		case "darwin":
			host = cpu + "-apple-darwin"
		case "linux":
			host = cpu + "-unknown-linux-gnu"
		case "windows":
			host = cpu + "-w64-mingw32"
		}
		// Install layout follows MRI's, relative to the oruby executable:
		// <prefix>/bin/oruby, <prefix>/lib/oruby/<ruby_version>, ...
		prefix := oruby.InstallPrefix()
		dlext := "so"
		if runtime.GOOS == "darwin" {
			dlext = "bundle"
		}
		rbVer := oruby.RubyVersion
		parts := strings.Split(rbVer, ".")
		apiVer := rbVer
		if len(parts) >= 2 {
			apiVer = parts[0] + "." + parts[1]
		}
		rubylibprefix := prefix + "/lib/oruby"
		sitedir := rubylibprefix + "/site_ruby"
		vendordir := rubylibprefix + "/vendor_ruby"

		rbConfigMod := mrb.DefineModule("RbConfig")
		cfgMap := buildConfigMap(prefix)
		extraCfg := map[string]string{
			"host_os":               runtime.GOOS,
			"host_cpu":              cpu,
			"host":                  host,
			"arch":                  arch,
			"target_os":             runtime.GOOS,
			"target_cpu":            cpu,
			"EXEEXT":                "",
			"DLEXT":                 dlext,
			"MAJOR":                 parts[0],
			"MINOR":                 parts[1],
			"TEENY":                 parts[2],
			"PATCHLEVEL":            "0",
			"ruby_version":          rbVer,
			"RUBY_PROGRAM_VERSION":  rbVer,
			"RUBY_API_VERSION":      apiVer,
			"prefix":                prefix,
			"exec_prefix":           prefix,
			"bindir":                prefix + "/bin",
			"libdir":                prefix + "/lib",
			"datadir":               prefix + "/share",
			"sysconfdir":            prefix + "/etc",
			"rubylibprefix":         rubylibprefix,
			"rubylibdir":            rubylibprefix + "/" + rbVer,
			"rubyarchdir":           rubylibprefix + "/" + rbVer + "/" + arch,
			"archdir":               rubylibprefix + "/" + rbVer + "/" + arch,
			"sitedir":               sitedir,
			"sitelibdir":            sitedir + "/" + rbVer,
			"sitearchdir":           sitedir + "/" + rbVer + "/" + arch,
			"vendordir":             vendordir,
			"vendorlibdir":          vendordir + "/" + rbVer,
			"vendorarchdir":         vendordir + "/" + rbVer + "/" + arch,
			"ruby_install_name":     "oruby",
			"RUBY_INSTALL_NAME":     "oruby",
			"RUBY_BASE_NAME":        "oruby",
			"RUBY_SO_NAME":          "oruby",
			"UNICODE_VERSION":       "17.0.0",
			"UNICODE_EMOJI_VERSION": "17.0",
			"CROSS_COMPILING":       "no",
			"ENABLE_SHARED":         "no",
			"LIBRUBY":               "liboruby-static.a",
			"LIBRUBY_A":             "liboruby-static.a",
			"LIBRUBY_SO":            "liboruby." + dlext,
			"libdirname":            "libdir",
			"EXTSTATIC":             "static",
			"LIBPATHENV":            "DYLD_LIBRARY_PATH",
		}
		for k, v := range extraCfg {
			cfgMap[k] = v
		}
		cfgHash := mrb.HashNewCapa(len(cfgMap))
		mkHash := mrb.HashNewCapa(len(cfgMap))
		for k, v := range cfgMap {
			kv := mrb.StrNew(k)
			cfgHash.Set(kv, mrb.StrNew(v))
			mkHash.Set(kv, mrb.StrNew(v))
		}
		mrb.DefineConst(rbConfigMod, "CONFIG", cfgHash.Value())
		mrb.DefineConst(rbConfigMod, "MAKEFILE_CONFIG", mkHash.Value())
		mrb.DefineConst(rbConfigMod, "TOPDIR", mrb.StrNew(prefix))
		if !mrb.ConstDefinedAt(mrb.ObjectClass(), mrb.Intern("CROSS_COMPILING")) {
			mrb.DefineGlobalConst("CROSS_COMPILING", mrb.NilValue())
		}
		mrb.DefineModuleFunction(rbConfigMod, "ruby", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
			cfg := mrb.ConstGet(mrb.ClassPtr(self), mrb.Intern("CONFIG"))
			b := mrb.String(mrb.HashGet(cfg, mrb.StrNew("bindir")))
			n := mrb.String(mrb.HashGet(cfg, mrb.Intern("ruby_install_name")))
			if n == "" {
				n = mrb.String(mrb.HashGet(cfg, mrb.StrNew("ruby_install_name")))
			}
			e := mrb.String(mrb.HashGet(cfg, mrb.StrNew("EXEEXT")))
			return mrb.StrNew(b + "/" + n + e)
		}, oruby.ArgsNone())
		return nil
	})

	oruby.Gem("core_ext", func(mrb *oruby.MrbState) interface{} {
		// Ruby extensions below reopen classes defined by these gems
		for _, dep := range []string{"io", "io/file", "regexp", "time/core", "thread", "process", "rbconfig"} {
			mrb.Resolve(dep)
		}
		initFormat(mrb)
		initPack(mrb)
		initRandom(mrb)
		initBigPow(mrb)
		// before the Ruby files: 21_implicit.rb wraps String#+ in Ruby and
		// calls the original, which must be the encoding-checking one
		mrb.WrapStringConcat()
		initCrypt(mrb)
		initISeq(mrb)
		// IO::Buffer (ruby/32_io_buffer.rb) sizes its mappings by the page size
		ioCls := mrb.ClassGet("IO")
		bufCls := mrb.DefineClassUnder(ioCls, "Buffer", mrb.ObjectClass())
		mrb.DefineConst(bufCls, "PAGE_SIZE", mrb.FixnumValue(os.Getpagesize()))
		floatClass := mrb.FloatClass()
		mrb.DefineMethod(floatClass, "prev_float", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
			f := self.Float64()
			return mrb.FloatValue(math.Nextafter(f, math.Inf(-1)))
		}, oruby.ArgsNone())
		mrb.DefineMethod(floatClass, "next_float", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
			f := self.Float64()
			return mrb.FloatValue(math.Nextafter(f, math.Inf(1)))
		}, oruby.ArgsNone())
		// obj.__oruby_singleton_class_or_nil: the singleton class obj has,
		// without making one (Kernel#singleton_methods, 05_kernel.rb)
		mrb.DefineMethod(mrb.KernelModule(), "__oruby_singleton_class_or_nil", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
			if c := mrb.ClassOf(self); !c.IsNil() && c.Type() == oruby.MrbTTSClass {
				return c.Value()
			}
			return mrb.NilValue()
		}, oruby.ArgsNone())

		initMathGamma(mrb)
		initNumconv(mrb)
		initFloatRound(mrb)

		if mrb.ClassDefined("ObjectSpace") {
			mObjSpace := mrb.ModuleGet("ObjectSpace")
			mrb.DefineClassMethod(mObjSpace, "_caller_receiver", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
				return mrb.CallerReceiver()
			}, oruby.ArgsNone())
		}

		c := mrb.MrbCcontextNew()
		defer c.Free()

		for _, ent := range getCoreExtEntries() {
			// a core file, which backtraces show as MRI shows its own
			c.InternalFilename("<internal:core_ext/" + ent.name + ">")
			var err error
			if ent.mrbBytes != nil {
				_, err = mrb.LoadStaticIrepCxt(ent.mrbBytes, c)
			} else {
				_, err = mrb.LoadBytesCxt(ent.rbBytes, c)
			}
			if err != nil {
				panic(fmt.Sprintf("core_ext error loading ruby/%s: %s", ent.name, err))
			}
		}
		// C front ends in place of the hot methods the Ruby above redefines
		// (go-fast.c): the common case answered in C, the rest in that Ruby
		mrb.InstallFastPaths()
		// MRI marks the default $LOAD_PATH entries, sitelibdir and after, with
		// @gem_prelude_index (the path itself): RubyGems puts activated gems'
		// paths before the first (Gem.load_path_insert_index). RUBYLIB's come
		// earlier, -I's later; neither is marked.
		// (only with gem/load linked: it makes $LOAD_PATH)
		if _, err := mrb.Resolve("load"); err != nil {
			return nil
		}
		if def := oruby.DefaultLoadPath(); len(def) > 0 {
			if lpv := mrb.GVGet(oruby.GvSym_LOAD_PATH); lpv.IsArray() {
				if elems, ok := mrb.AotArySlice(lpv); ok {
					found := false
					ivSym := mrb.Intern("@gem_prelude_index")
					for _, pa := range elems {
						if !found {
							if b, sok := mrb.AotStringBytes(pa); sok && string(b) == def[0] {
								found = true
							}
						}
						if found && !pa.Frozen() {
							_ = mrb.IVSet(pa, ivSym, pa)
						}
					}
				}
			}
		}
		return nil
	})
}

// Math.gamma and Math.lgamma as MRI's math.c (mruby's math gem has
// neither): an integer argument up to 23 gives the exact factorial, a
// negative integer and -Infinity are out of the domain
func initMathGamma(mrb *oruby.MrbState) {
	mathMod := mrb.ModuleGet("Math")
	if mathMod.IsNil() {
		return
	}
	toF := func(mrb *oruby.MrbState, v oruby.Value) (float64, error) {
		switch {
		case v.Type() == oruby.MrbTTFloat:
			return v.Float64(), nil
		case v.IsInteger():
			return float64(v.Int64()), nil
		case v.IsNil():
			return 0, oruby.ETypeError("can't convert nil into Float")
		case v.IsString():
			return 0, oruby.ETypeError("can't convert String into Float")
		}
		if numeric := mrb.ClassGet("Numeric"); !numeric.IsNil() && mrb.ObjIsKindOf(v, numeric) && mrb.RespondTo(v, mrb.Intern("to_f")) {
			f, err := mrb.Funcall(v, mrb.Intern("to_f"))
			if err != nil {
				return 0, err
			}
			if f.Type() == oruby.MrbTTFloat {
				return f.Float64(), nil
			}
		}
		return 0, oruby.ETypeError("can't convert %s into Float", mrb.ClassName(mrb.ObjClass(v)))
	}
	domain := func(mrb *oruby.MrbState, name string) oruby.MrbValue {
		return mrb.Raise(mrb.ClassGetUnder(mathMod, "DomainError"), `Numerical argument is out of domain - "`+name+`"`)
	}
	mrb.DefineModuleFunction(mathMod, "gamma", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		d, err := toF(mrb, mrb.GetArgsFirst())
		if err != nil {
			return mrb.RaiseError(err)
		}
		if math.IsInf(d, -1) {
			return domain(mrb, "gamma")
		}
		if d == math.Floor(d) && !math.IsInf(d, 0) {
			if d < 0 {
				return domain(mrb, "gamma")
			}
			if d >= 1 && d <= 23 {
				fact := 1.0
				for k := 2.0; k < d; k++ {
					fact *= k
				}
				return mrb.FloatValue(fact)
			}
		}
		return mrb.FloatValue(math.Gamma(d))
	}, oruby.ArgsReq(1))
	mrb.DefineModuleFunction(mathMod, "lgamma", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		d, err := toF(mrb, mrb.GetArgsFirst())
		if err != nil {
			return mrb.RaiseError(err)
		}
		if math.IsInf(d, 0) {
			if d < 0 {
				return domain(mrb, "lgamma")
			}
			return mrb.AryNewFromValues(mrb.FloatValue(math.Inf(1)), oruby.Int(1).Value())
		}
		if d == 0 {
			sign := 1
			if math.Signbit(d) {
				sign = -1
			}
			return mrb.AryNewFromValues(mrb.FloatValue(math.Inf(1)), oruby.Int(sign).Value())
		}
		v, sign := math.Lgamma(d)
		return mrb.AryNewFromValues(mrb.FloatValue(v), oruby.Int(sign).Value())
	}, oruby.ArgsReq(1))
}

// buildConfig is the part of RbConfig::CONFIG that mkmf compiles and links
// with, as a Ruby Hash literal. The headers are the C API's (gem/capi):
// <prefix>/include/oruby-<version> where ORuby is installed, gem/capi/include
// in its source tree; without them mkmf stops, as MRI's does without
// ruby.h. mkmf's link tests (have_func) link with the C API and libmruby as
// static libraries in <prefix>/lib (make bin), so they find what the C API
// has. Extensions are compiled into an oruby binary (oruby build), never
// loaded as files.
func buildConfigMap(prefix string) map[string]string {
	hdr := ""
	for _, d := range []string{
		filepath.Join(prefix, "include", "oruby-"+oruby.RubyVersion),
		filepath.Join(prefix, "gem", "capi", "include"),
	} {
		if _, err := os.Stat(filepath.Join(d, "ruby", "ruby.h")); err == nil {
			hdr = d
			break
		}
	}
	libdir := filepath.Join(prefix, "lib")
	cc, cxx := "cc", "c++"
	ccdl, cppflags, ldshared, ldsharedxx, dldflags, libs := "-fPIC", "", "$(CC) -shared", "$(CXX) -shared", "", "-lm -lpthread"
	if runtime.GOOS == "darwin" {
		ccdl = "-fno-common"
		cppflags = "-D_XOPEN_SOURCE -D_DARWIN_C_SOURCE -D_DARWIN_UNLIMITED_SELECT -D_REENTRANT"
		ldshared, ldsharedxx = "$(CC) -dynamic -bundle", "$(CXX) -dynamic -bundle"
		dldflags = "-Wl,-undefined,dynamic_lookup"
		libs = "-lpthread"
	}
	librubyarg := "-loruby-capi -lmruby -lm"
	kv := map[string]string{
		"CC": cc, "CXX": cxx, "CPP": cc + " -E", "GCC": "yes", "LDSHARED": ldshared, "LDSHAREDXX": ldsharedxx,
		"optflags": "-O3 -fno-fast-math", "debugflags": "-ggdb3", "warnflags": "-Wall -Wno-unused-parameter",
		"cflags": "-O3 -fno-fast-math -ggdb3 -Wall -Wno-unused-parameter", "cxxflags": "-O3 -ggdb3",
		"CCDLFLAGS": ccdl, "CFLAGS": ccdl + " -O3 -fno-fast-math -ggdb3 -Wall -Wno-unused-parameter -pipe",
		"CXXFLAGS": "-O3 -ggdb3", "CPPFLAGS": cppflags, "cppflags": "", "DEFS": "", "ARCH_FLAG": "",
		"LDFLAGS": "-L. -L" + libdir, "DLDFLAGS": dldflags, "EXTDLDFLAGS": "", "EXTLDFLAGS": "", "DLDLIBS": "",
		"LIBS": libs, "MAINLIBS": "", "SOLIBS": "", "LOCAL_LIBS": "",
		"LIBRUBYARG": librubyarg, "LIBRUBYARG_STATIC": librubyarg, "LIBRUBYARG_SHARED": "",
		"LIBPATHFLAG": " -L%s", "RPATHFLAG": "", "LIBARG": "-l%s", "LINK_SO": "", "TRY_LINK": "",
		"OBJEXT": "o", "LIBEXT": "a", "ASMEXT": "S", "COUTFLAG": "-o ", "OUTFLAG": "-o ", "CSRCFLAG": "",
		"CPPOUTFILE": "-o conftest.i", "CC_WRAPPER": "", "STRIP": "strip -S -x",
		"INSTALL": "/usr/bin/install -c", "INSTALL_PROGRAM": "/usr/bin/install -c",
		"INSTALL_SCRIPT": "/usr/bin/install -c", "INSTALL_DATA": "/usr/bin/install -c -m 644",
		"MKDIR_P": "mkdir -p", "MAKEDIRS": "mkdir -p", "RM": "rm -f", "RMDIR": "rmdir", "RMDIRS": "rmdir -p",
		"RMALL": "rm -fr", "CP": "cp", "AR": "ar", "ARFLAGS": "rcu", "RANLIB": "ranlib", "NULLCMD": ":",
		"THREAD_MODEL": "pthread", "configure_args": "", "PLATFORM_DIR": "", "ENCOBJS": "",
		"COMMON_HEADERS": "", "COMMON_MACROS": "", "COMMON_LIBS": "", "includedir": filepath.Join(prefix, "include"),
		"PATH_SEPARATOR": string(os.PathListSeparator),
		"sitearch":       oruby.RubyArch(),
		"rubyarchprefix": filepath.Join(prefix, "lib", "oruby", oruby.RubyArch()),
	}
	if hdr != "" {
		for _, k := range []string{"rubyhdrdir", "rubyarchhdrdir", "sitehdrdir", "sitearchhdrdir", "vendorhdrdir", "vendorarchhdrdir", "topdir"} {
			kv[k] = hdr
		}
	}
	return kv
}
