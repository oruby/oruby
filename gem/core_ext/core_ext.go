package core_ext

import (
	"embed"
	"fmt"
	"io/fs"
	"math"
	"runtime"
	"strings"

	"github.com/oruby/oruby"
)

//go:embed ruby/*.rb
var rubyFiles embed.FS

func init() {
	oruby.Gem("rbconfig", func(mrb *oruby.MrbState) interface{} {
		mrb.Eval(fmt.Sprintf(`
module RbConfig
  CONFIG = {
    'host_os' => '%s',
    'EXEEXT' => '',
    'ruby_version' => (defined?(RUBY_VERSION) ? RUBY_VERSION : '3.0.0'),
    'bindir' => '',
    'ruby_install_name' => 'oruby'
  }
end
`, runtime.GOOS))
		return nil
	})

	oruby.Gem("core_ext", func(mrb *oruby.MrbState) interface{} {
		// Ruby extensions below reopen classes defined by these gems
		for _, dep := range []string{"io", "io/file", "regexp", "time", "thread", "process"} {
			mrb.Resolve(dep)
		}
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
