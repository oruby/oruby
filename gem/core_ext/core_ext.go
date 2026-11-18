package core_ext

import (
	"embed"
	"fmt"
	"io/fs"
	"math"
	"os"
	"runtime"
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
    'CROSS_COMPILING' => 'no'
  }
  def self.ruby
    CONFIG['bindir'] + '/' + CONFIG['ruby_install_name'] + CONFIG['EXEEXT']
  end
end
CROSS_COMPILING = nil unless Object.const_defined?(:CROSS_COMPILING)
`, prefix, runtime.GOOS, cpu, host, arch, runtime.GOOS, cpu, dlext, arch, arch, arch, arch))
		return nil
	})

	oruby.Gem("core_ext", func(mrb *oruby.MrbState) interface{} {
		// Ruby extensions below reopen classes defined by these gems
		for _, dep := range []string{"io", "io/file", "regexp", "time", "thread", "process"} {
			mrb.Resolve(dep)
		}
		initFormat(mrb)
		initPack(mrb)
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
