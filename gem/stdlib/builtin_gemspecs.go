package stdlib

import (
	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/rb"
)

func init() {
	// Built-in gem specifications for compiled-in ORuby gems. Only specs whose
	// backing Go feature is compiled into the binary (mrb.GemAvailable) are
	// exposed to RubyGems and Bundler via rubygems/defaults/oruby.
	specs := []oruby.GemSpec{
		// Bundled / stdlib-extracted gems (not in MRI 4.1 default specs)
		{Name: "base64", Version: "0.3.0", Feature: "base64", Summary: "ORuby built-in Base64", Files: []string{"lib/base64.rb"}},
		{Name: "bigdecimal", Version: "4.1.3", Feature: "bigdecimal", Summary: "ORuby built-in BigDecimal", Files: []string{"lib/bigdecimal.rb"}},
		{Name: "csv", Version: "3.3.2", Feature: "csv", Summary: "ORuby built-in CSV", Files: []string{"lib/csv.rb"}},
		{Name: "drb", Version: "2.2.3", Feature: "drb", Summary: "ORuby built-in dRuby", Files: []string{"lib/drb.rb"}},
		{Name: "logger", Version: "1.7.0", Summary: "ORuby built-in Logger (lib/oruby/4.1.0)", Files: []string{"lib/logger.rb"}},
		{Name: "matrix", Version: "0.4.3", Feature: "matrix", Summary: "ORuby built-in Matrix", Files: []string{"lib/matrix.rb"}},
		{Name: "observer", Version: "0.1.2", Feature: "observer", Summary: "ORuby built-in Observer", Files: []string{"lib/observer.rb"}},
		{Name: "ostruct", Version: "0.6.1", Feature: "ostruct", Summary: "ORuby built-in OpenStruct", Files: []string{"lib/ostruct.rb"}},
		{Name: "prime", Version: "0.1.4", Feature: "prime", Summary: "ORuby built-in Prime", Files: []string{"lib/prime.rb"}},
		{Name: "racc", Version: "1.8.1", Feature: "racc", Summary: "ORuby built-in Racc LALR parser runtime", Files: []string{"lib/racc.rb", "lib/racc/parser.rb"}},
		{Name: "syslog", Version: "0.2.0", Feature: "syslog", Summary: "ORuby built-in Syslog", Files: []string{"lib/syslog.rb"}},
		{Name: "tsort", Version: "0.2.0", Feature: "tsort", Summary: "ORuby built-in TSort", Files: []string{"lib/tsort.rb"}},
		// prism is MRI's C extension; oruby's is the Ruby::Prism library over
		// the parser in libmruby (lib/oruby/4.1.0/prism.rb, scripts/gen_prism.rb)
		{Name: "prism", Version: "2.1.0", Summary: "ORuby Prism (Ruby::Prism over libmruby's parser)", Files: []string{"lib/prism.rb"}},
		{Name: "ffi", Version: "1.17.4", Feature: "ffi", Summary: "ORuby FFI", Files: []string{"lib/ffi.rb"}},
	}
	for _, s := range specs {
		oruby.RegisterGemSpec(s)
	}

	// Official RubyGems hook: lib/oruby/4.1.0/rubygems.rb calls
	// `require "rubygems/defaults/#{RUBY_ENGINE}"` right before
	// `Gem::Specification.load_defaults`.
	oruby.LazyGem("rubygems/defaults/oruby", initRubyGemsDefaults)
}

func initRubyGemsDefaults(mrb *oruby.MrbState) any {
	gemMod := mrb.DefineModule("Gem")
	gemMod.DefineClassMethod("__oruby_builtin_specs_data", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		all := oruby.BuiltinGemSpecs()
		ary := mrb.AryNewCapa(len(all))
		for _, sp := range all {
			if sp.Feature != "" && !mrb.GemAvailable(sp.Feature) {
				continue
			}
			entry := mrb.AryNewCapa(4)
			mrb.AryPush(entry, mrb.StrNew(sp.Name))
			mrb.AryPush(entry, mrb.StrNew(sp.Version))
			mrb.AryPush(entry, mrb.StrNew(sp.Summary))
			files := mrb.AryNewCapa(len(sp.Files))
			for _, f := range sp.Files {
				mrb.AryPush(files, mrb.StrNew(f))
			}
			mrb.AryPush(entry, files)
			mrb.AryPush(ary, entry)
		}
		return ary
	}, mrb.ArgsNone())

	gemMod.DefineClassMethod("__oruby_native_gem?", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		return mrb.BoolValue(oruby.NativeExtensionGem(mrb.String(mrb.GetArgsFirst())))
	}, mrb.ArgsReq(1))
	gemMod.DefineClassMethod("__oruby_native_extension?", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		// a compiled-in C extension is the Go gem named as its library (gem/capi)
		return mrb.BoolValue(mrb.GemAvailable(mrb.String(mrb.GetArgsFirst()) + ".so"))
	}, mrb.ArgsReq(1))

	rb.LoadRuby(mrb, "<internal:rubygems/defaults/oruby>", []byte(`
module ORubyDefaultGemSpecs
  def __oruby_builtin_specs
    @__oruby_builtin_specs ||= begin
      dir = Gem.default_specifications_dir
      Gem.__oruby_builtin_specs_data.map do |name, version, summary, files|
        Gem::Specification.new do |s|
          s.name = name
          s.version = version
          s.summary = summary
          s.authors = ["ORuby"]
          s.require_paths = ["lib"]
          s.files = files
          s.loaded_from = File.join(dir, "#{name}-#{version}.gemspec")
        end
      end
    end
  end

  def default_stubs(pattern = "*.gemspec")
    disk = super
    seen = {}
    disk.each { |s| seen[s.name] = true }
    extra = __oruby_builtin_specs.select do |s|
      !seen[s.name] && File.fnmatch?(pattern, "#{s.full_name}.gemspec")
    end
    disk + extra
  end

  def load_defaults
    super
    __oruby_builtin_specs.each do |spec|
      Gem.register_default_spec(spec)
    end
  end
end

Gem::Specification.singleton_class.prepend(ORubyDefaultGemSpecs)

# A gem's C extension that oruby compiles in (a Go gem registered under the
# extension's name, through the Ruby C API layer gem/capi: rbs_extension)
# is built: RubyGems neither ignores the gem nor asks for a build.
module ORubyNativeExtensions
  def missing_extensions?
    return false if !extensions.empty? && (Gem.__oruby_native_gem?(name) ||
      Gem.__oruby_embedded_covers?(name, version) ||
      extensions.all? { |e| Gem.__oruby_native_extension?(File.basename(File.dirname(e))) })
    super
  end
end
Gem::Specification.prepend(ORubyNativeExtensions)

# The embedded gems (oruby's Go gems, default specs of their version) stand
# in for every version of the gem up to their own: one of those is not
# built (it would not run), and Bundler resolves to the embedded version
# when the Gemfile allows it. A newer version the Gemfile or lockfile asks
# for is installed and built as in MRI, and require loads it.
module Gem
  def self.__oruby_embedded_spec(name)
    @__oruby_embedded ||= Gem::Specification.__oruby_builtin_specs.to_h { |s| [s.name, s] }
    @__oruby_embedded[name]
  end

  # the embedded gem stands in for name at version: it is not older
  def self.__oruby_embedded_covers?(name, version)
    e = __oruby_embedded_spec(name)
    !e.nil? && e.version >= Gem::Version.new(version)
  end
end
`))
	return nil
}

// After RubyGems' and Bundler's files load: their steps for an embedded
// gem (Gem.__oruby_embedded_covers?).
func init() {
	oruby.AfterLoad("rubygems/ext/builder.rb", `
module ORubyEmbeddedBuild
  def build_extensions
    return super if @spec.extensions.empty? || !Gem.__oruby_embedded_covers?(@spec.name, @spec.version)
    say "#{@spec.full_name}: not building its extension, oruby's #{Gem.__oruby_embedded_spec(@spec.name).full_name} stands in for it"
    require "fileutils"
    FileUtils.mkdir_p @spec.extension_dir
    FileUtils.touch @spec.gem_build_complete_path
  end
end
Gem::Ext::Builder.prepend(ORubyEmbeddedBuild)
`)
	oruby.AfterLoad("bundler/source/rubygems.rb", `
module Bundler
  class Source
    class Rubygems
      # an embedded gem's default spec is a candidate even where the remote
      # index has the gem, and takes the place of the remote spec of its
      # version (whose dependencies are the C extension's)
      module ORubyEmbeddedSpecs
        def specs
          super.tap do |index|
            next unless @allow_local
            default_specs.each { |s| index << s if Gem.__oruby_embedded_spec(s.name) }
          end
        end

        # MRI's Bundler fetches and installs a default gem's .gem to cache
        # it; an embedded gem's files would not run
        def download(spec, options = {})
          return super unless spec.default_gem? && Gem.__oruby_embedded_spec(spec.name)
          true
        end

        def install(spec, options = {})
          return super unless spec.default_gem? && Gem.__oruby_embedded_spec(spec.name)
          print_using_message "Using #{version_message(spec, options[:previous_spec])}"
          nil
        end
      end
      prepend ORubyEmbeddedSpecs
    end
  end
end
`)
	oruby.AfterLoad("bundler/resolver.rb", `
module Bundler
  class Resolver
    # an embedded gem's package prefers the local versions, the embedded
    # one first: remote versions only when the requirement excludes it
    module ORubyEmbeddedPackage
      def initialize(name, *args, **opts)
        super
        @prefer_local = true if Gem.__oruby_embedded_spec(name)
      end
    end
    Package.prepend(ORubyEmbeddedPackage)

    module ORubyEmbeddedFilter
      def filter_remote_specs(specs, package)
        if package.prefer_local? && Gem.__oruby_embedded_spec(package.name)
          embedded = specs.select { |s| s.is_a?(StubSpecification) && s.default_gem? }
          return embedded unless embedded.empty?
        end
        super
      end
    end
    prepend ORubyEmbeddedFilter
  end
end
`)
}
