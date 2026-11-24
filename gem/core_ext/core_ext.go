package core_ext

import (
	"embed"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

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
		mrb.Eval(fmt.Sprintf(`
module RbConfig
  ruby_version = RUBY_VERSION
  prefix = %q
  join = lambda { |*parts| parts.join('/') } # File may not be loaded yet
  major, minor, teeny = ruby_version.split('.')
  rubylibprefix = join.call(prefix, 'lib', 'oruby')
  sitedir = join.call(rubylibprefix, 'site_ruby')
  vendordir = join.call(rubylibprefix, 'vendor_ruby')
  CONFIG = {
    'host_os' => %q,
    'host_cpu' => %q,
    'host' => %q,
    'arch' => %q,
    'target_os' => %q,
    'target_cpu' => %q,
    'EXEEXT' => '',
    'DLEXT' => %q,
    'MAJOR' => major, 'MINOR' => minor, 'TEENY' => teeny, 'PATCHLEVEL' => '0',
    'ruby_version' => ruby_version,
    'RUBY_PROGRAM_VERSION' => ruby_version,
    'RUBY_API_VERSION' => [major, minor].join('.'),
    'prefix' => prefix,
    'exec_prefix' => prefix,
    'bindir' => join.call(prefix, 'bin'),
    'libdir' => join.call(prefix, 'lib'),
    'datadir' => join.call(prefix, 'share'),
    'sysconfdir' => join.call(prefix, 'etc'),
    'rubylibprefix' => rubylibprefix,
    'rubylibdir' => join.call(rubylibprefix, ruby_version),
    'rubyarchdir' => join.call(rubylibprefix, ruby_version, %q),
    'archdir' => join.call(rubylibprefix, ruby_version, %q),
    'sitedir' => sitedir,
    'sitelibdir' => join.call(sitedir, ruby_version),
    'sitearchdir' => join.call(sitedir, ruby_version, %q),
    'vendordir' => vendordir,
    'vendorlibdir' => join.call(vendordir, ruby_version),
    'vendorarchdir' => join.call(vendordir, ruby_version, %q),
    'ruby_install_name' => 'oruby',
    'RUBY_INSTALL_NAME' => 'oruby',
    'RUBY_BASE_NAME' => 'oruby',
    'RUBY_SO_NAME' => 'oruby',
    'UNICODE_VERSION' => '17.0.0',
    'UNICODE_EMOJI_VERSION' => '17.0',
    'CROSS_COMPILING' => 'no',
    # oruby is one static executable: no libruby to link against
    'ENABLE_SHARED' => 'no',
    'LIBRUBY' => 'liboruby-static.a',
    'LIBRUBY_A' => 'liboruby-static.a',
    'LIBRUBY_SO' => 'liboruby.' + %q,
    'libdirname' => 'libdir',
    # every extension oruby has is built in, none a dynamically loaded file
    'EXTSTATIC' => 'static',
    'LIBPATHENV' => 'DYLD_LIBRARY_PATH'
  }
  # what mkmf compiles and links with: the C API's headers (gem/capi) and,
  # for its link tests, the C API and libmruby as static libraries
  CONFIG.update(%s)
  # no frozen values (MRI's CONFIG is mutable, under --enable-frozen-string-literal too)
  CONFIG.each { |k, v| CONFIG[k] = v.dup if String === v && v.frozen? }
  TOPDIR = prefix
  # MRI's unexpanded table; oruby's values hold no $(refs)
  MAKEFILE_CONFIG = {}
  CONFIG.each { |k, v| MAKEFILE_CONFIG[k] = String === v ? v.dup : v }
  def self.ruby
    CONFIG['bindir'] + '/' + CONFIG['ruby_install_name'] + CONFIG['EXEEXT']
  end
  # MRI's RbConfig.expand: the $(name) and ${name} references in val
  # replaced by config's values, recursively
  def self.expand(val, config = CONFIG)
    newval = val.gsub(/\$\$|\$\(([^()]+)\)|\$\{([^{}]+)\}/) do
      var = $&
      if !(v = $1 || $2)
        '$'
      elsif key = config[v = v[/\A[^:]+(?=(?::(.*?)=(.*))?\z)/]]
        pat, sub = $1, $2
        config[v] = false
        config[v] = expand(key, config)
        key = key.gsub(/#{Regexp.quote(pat)}(?=\s|\z)/n) { sub } if pat
        key
      else
        var
      end
    end
    val.replace(newval) unless newval == val
    val
  end
  # CONFIG is the expanded table, as MRI's
  CONFIG.each_value { |v| expand(v) if String === v }
end
CROSS_COMPILING = nil unless Object.const_defined?(:CROSS_COMPILING)
`, prefix, runtime.GOOS, cpu, host, arch, runtime.GOOS, cpu, dlext, arch, arch, arch, arch, dlext, buildConfig(prefix)))
		return nil
	})

	oruby.Gem("core_ext", func(mrb *oruby.MrbState) interface{} {
		// Ruby extensions below reopen classes defined by these gems
		for _, dep := range []string{"io", "io/file", "regexp", "time/core", "thread", "process"} {
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
		// IO::Buffer (ruby/32_io_buffer.rb) sizes its mappings by the page size
		mrb.Eval(fmt.Sprintf("class IO; class Buffer; PAGE_SIZE = %d; end; end", os.Getpagesize()))
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

		entries, err := fs.ReadDir(rubyFiles, "ruby")
		if err != nil {
			panic("core_ext read error: " + err.Error())
		}

		if mrb.ClassDefined("ObjectSpace") {
			mObjSpace := mrb.ModuleGet("ObjectSpace")
			mrb.DefineClassMethod(mObjSpace, "_caller_receiver", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
				return mrb.CallerReceiver()
			}, oruby.ArgsNone())
		}

		c := mrb.MrbCcontextNew()
		defer c.Free()

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
			// a core file, which backtraces show as MRI shows its own
			c.Filename("<internal:core_ext/" + entry.Name() + ">")
			_, err = mrb.LoadBytesCxt(data, c)
			if err != nil {
				panic(fmt.Sprintf("core_ext error loading %s: %s", path, err))
			}
		}
		// C front ends in place of the hot methods the Ruby above redefines
		// (go-fast.c): the common case answered in C, the rest in that Ruby
		mrb.InstallFastPaths()
		// MRI marks the default $LOAD_PATH entries, sitelibdir and after, with
		// @gem_prelude_index (the path itself): RubyGems puts activated gems'
		// paths before the first (Gem.load_path_insert_index). RUBYLIB's come
		// earlier, -I's later; neither is marked.
		mrb.Resolve("load") // $LOAD_PATH
		if def := oruby.DefaultLoadPath(); len(def) > 0 {
			mrb.Eval(fmt.Sprintf("if (i = $LOAD_PATH.index(%q)); $LOAD_PATH[i..].each { |pa| pa.instance_variable_set(:@gem_prelude_index, pa) unless pa.frozen? }; end", def[0]))
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
func buildConfig(prefix string) string {
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
		"sitearch": oruby.RubyArch(), "rubyarchprefix": filepath.Join(prefix, "lib", "oruby", oruby.RubyArch()),
	}
	if hdr != "" {
		for _, k := range []string{"rubyhdrdir", "rubyarchhdrdir", "sitehdrdir", "sitearchhdrdir", "vendorhdrdir", "vendorarchhdrdir", "topdir"} {
			kv[k] = hdr
		}
	}
	keys := make([]string, 0, len(kv))
	for k := range kv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("{")
	for i, k := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%q => %q", k, kv[k])
	}
	b.WriteString("}")
	return b.String()
}
