package core_ext

import (
	"embed"
	"fmt"
	"io/fs"
	"math"
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
		floatClass := mrb.FloatClass()
		mrb.DefineMethod(floatClass, "prev_float", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
			f := self.Float64()
			return mrb.FloatValue(math.Nextafter(f, math.Inf(-1)))
		}, oruby.ArgsNone())
		mrb.DefineMethod(floatClass, "next_float", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
			f := self.Float64()
			return mrb.FloatValue(math.Nextafter(f, math.Inf(1)))
		}, oruby.ArgsNone())

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
			c.Filename(path)
			_, err = mrb.LoadBytesCxt(data, c)
			if err != nil {
				panic(fmt.Sprintf("core_ext error loading %s: %s", path, err))
			}
		}
		return nil
	})
}
