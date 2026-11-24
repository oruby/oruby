package stdlib

import (
	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/internal/rb"
)

func init() {
	// Built-in gem specifications for compiled-in ORuby gems. Only specs whose
	// backing Go feature is compiled into the binary (mrb.GemAvailable) are
	// exposed to RubyGems and Bundler via rubygems/defaults/oruby.
	specs := []oruby.GemSpec{
		// Bundled / stdlib-extracted gems (not in MRI 4.1 default specs)
		{Name: "base64", Version: "0.3.0", Feature: "base64", Summary: "ORuby built-in Base64", Files: []string{"lib/base64.rb"}},
		{Name: "bigdecimal", Version: "3.2.2", Feature: "bigdecimal", Summary: "ORuby built-in BigDecimal", Files: []string{"lib/bigdecimal.rb"}},
		{Name: "csv", Version: "3.3.2", Feature: "csv", Summary: "ORuby built-in CSV", Files: []string{"lib/csv.rb"}},
		{Name: "drb", Version: "2.2.1", Feature: "drb", Summary: "ORuby built-in dRuby", Files: []string{"lib/drb.rb"}},
		{Name: "logger", Version: "1.7.0", Summary: "ORuby built-in Logger (lib/oruby/4.1.0)", Files: []string{"lib/logger.rb"}},
		{Name: "matrix", Version: "0.4.2", Feature: "matrix", Summary: "ORuby built-in Matrix", Files: []string{"lib/matrix.rb"}},
		{Name: "observer", Version: "0.1.2", Feature: "observer", Summary: "ORuby built-in Observer", Files: []string{"lib/observer.rb"}},
		{Name: "ostruct", Version: "0.6.1", Feature: "ostruct", Summary: "ORuby built-in OpenStruct", Files: []string{"lib/ostruct.rb"}},
		{Name: "prime", Version: "0.1.3", Feature: "prime", Summary: "ORuby built-in Prime", Files: []string{"lib/prime.rb"}},
		{Name: "racc", Version: "1.8.1", Feature: "racc", Summary: "ORuby built-in Racc LALR parser runtime", Files: []string{"lib/racc.rb", "lib/racc/parser.rb"}},
		{Name: "syslog", Version: "0.2.0", Feature: "syslog", Summary: "ORuby built-in Syslog", Files: []string{"lib/syslog.rb"}},
		{Name: "tsort", Version: "0.2.0", Feature: "tsort", Summary: "ORuby built-in TSort", Files: []string{"lib/tsort.rb"}},
		// prism is MRI's C extension; oruby's is the Ruby::Prism library over
		// the parser in libmruby (lib/oruby/4.1.0/prism.rb, scripts/gen_prism.rb)
		{Name: "prism", Version: "1.9.0", Summary: "ORuby Prism (Ruby::Prism over libmruby's parser)", Files: []string{"lib/prism.rb"}},

		// Compiled-in pure-Go / native replacements for C-extension gems
		{Name: "nokogiri", Version: "1.19.7", Feature: "nokogiri", Summary: "ORuby pure-Go Nokogiri (XML/HTML5/XPath/CSS/XSD/XSLT/RelaxNG)", Files: []string{"lib/nokogiri.rb"}},
		{Name: "oruby-vips", Version: "2.3.0", Feature: "oruby-vips", Summary: "ORuby united libvips/pure-Go image processor", Files: []string{"lib/oruby-vips.rb", "lib/vips.rb", "lib/ruby-vips.rb"}},
		{Name: "ruby-vips", Version: "2.3.0", Feature: "oruby-vips", Summary: "ORuby ruby-vips compatibility provider", Files: []string{"lib/ruby-vips.rb", "lib/vips.rb"}},
		{Name: "oj", Version: "3.16.10", Feature: "oj", Summary: "ORuby pure-Go Oj JSON", Files: []string{"lib/oj.rb"}},
		{Name: "ffi", Version: "1.17.2", Feature: "ffi", Summary: "ORuby FFI", Files: []string{"lib/ffi.rb"}},
		{Name: "pg", Version: "1.6.3", Feature: "pg", Summary: "ORuby pure-Go PostgreSQL driver (pgx)", Files: []string{"lib/pg.rb"}},
		{Name: "mysql2", Version: "0.5.6", Feature: "mysql2", Summary: "ORuby pure-Go MySQL driver", Files: []string{"lib/mysql2.rb"}},
		{Name: "sqlite3", Version: "2.6.0", Feature: "sqlite3", Summary: "ORuby SQLite3 driver", Files: []string{"lib/sqlite3.rb"}},
		{Name: "hiredis", Version: "0.6.3", Feature: "hiredis", Summary: "ORuby pure-Go Hiredis RESP driver", Files: []string{"lib/hiredis.rb"}},
		{Name: "msgpack", Version: "1.8.0", Feature: "msgpack", Summary: "ORuby pure-Go MessagePack", Files: []string{"lib/msgpack.rb"}},
		{Name: "nio4r", Version: "2.7.4", Feature: "nio4r", Summary: "ORuby pure-Go NIO4r selector", Files: []string{"lib/nio.rb", "lib/nio4r.rb"}},
		{Name: "bcrypt", Version: "3.1.20", Feature: "bcrypt", Summary: "ORuby pure-Go bcrypt", Files: []string{"lib/bcrypt.rb"}},
		{Name: "bcrypt_pbkdf", Version: "1.1.1", Feature: "bcrypt_pbkdf", Summary: "ORuby pure-Go bcrypt_pbkdf", Files: []string{"lib/bcrypt_pbkdf.rb"}},
		{Name: "argon2", Version: "2.3.0", Feature: "argon2", Summary: "ORuby pure-Go Argon2", Files: []string{"lib/argon2.rb"}},
		{Name: "scrypt", Version: "3.0.7", Feature: "scrypt", Summary: "ORuby pure-Go scrypt", Files: []string{"lib/scrypt.rb"}},
		{Name: "rbnacl", Version: "7.1.2", Feature: "rbnacl", Summary: "ORuby pure-Go RbNaCl", Files: []string{"lib/rbnacl.rb"}},
		{Name: "ed25519", Version: "1.3.0", Feature: "ed25519", Summary: "ORuby pure-Go Ed25519", Files: []string{"lib/ed25519.rb"}},
		{Name: "brotli", Version: "0.6.0", Feature: "brotli", Summary: "ORuby pure-Go Brotli", Files: []string{"lib/brotli.rb"}},
		{Name: "zstd-ruby", Version: "1.5.6", Feature: "zstd-ruby", Summary: "ORuby pure-Go Zstandard", Files: []string{"lib/zstd-ruby.rb"}},
		{Name: "commonmarker", Version: "1.1.5", Feature: "commonmarker", Summary: "ORuby pure-Go CommonMarker (Goldmark)", Files: []string{"lib/commonmarker.rb"}},

		// Newly added oruby-* gems (registered under both oruby-* and replaced gem name)
		{Name: "oruby-redcarpet", Version: "3.6.0", Feature: "oruby-redcarpet", Summary: "ORuby pure-Go Redcarpet (Goldmark)", Files: []string{"lib/oruby-redcarpet.rb", "lib/redcarpet.rb"}},
		{Name: "redcarpet", Version: "3.6.0", Feature: "oruby-redcarpet", Summary: "ORuby Redcarpet compatibility provider", Files: []string{"lib/redcarpet.rb"}},
		{Name: "oruby-snappy", Version: "0.4.0", Feature: "oruby-snappy", Summary: "ORuby pure-Go Snappy", Files: []string{"lib/oruby-snappy.rb", "lib/snappy.rb"}},
		{Name: "snappy", Version: "0.4.0", Feature: "oruby-snappy", Summary: "ORuby Snappy compatibility provider", Files: []string{"lib/snappy.rb"}},
		{Name: "oruby-yajl", Version: "1.4.3", Feature: "oruby-yajl", Summary: "ORuby pure-Go Yajl (Go encoding/json/v2)", Files: []string{"lib/oruby-yajl.rb", "lib/yajl.rb"}},
		{Name: "yajl-ruby", Version: "1.4.3", Feature: "oruby-yajl", Summary: "ORuby yajl-ruby compatibility provider", Files: []string{"lib/yajl.rb"}},
		{Name: "oruby-escape_utils", Version: "1.3.0", Feature: "oruby-escape_utils", Summary: "ORuby pure-Go EscapeUtils", Files: []string{"lib/oruby-escape_utils.rb", "lib/escape_utils.rb"}},
		{Name: "escape_utils", Version: "1.3.0", Feature: "oruby-escape_utils", Summary: "ORuby escape_utils compatibility provider", Files: []string{"lib/escape_utils.rb"}},
		{Name: "oruby-fast_blank", Version: "1.0.1", Feature: "oruby-fast_blank", Summary: "ORuby pure-Go fast_blank", Files: []string{"lib/oruby-fast_blank.rb", "lib/fast_blank.rb"}},
		{Name: "fast_blank", Version: "1.0.1", Feature: "oruby-fast_blank", Summary: "ORuby fast_blank compatibility provider", Files: []string{"lib/fast_blank.rb"}},
		{Name: "oruby-ox", Version: "2.14.23", Feature: "oruby-ox", Summary: "ORuby pure-Go Ox XML", Files: []string{"lib/oruby-ox.rb", "lib/ox.rb"}},
		{Name: "ox", Version: "2.14.23", Feature: "oruby-ox", Summary: "ORuby Ox compatibility provider", Files: []string{"lib/ox.rb"}},
		{Name: "oruby-cbor", Version: "0.5.9.8", Feature: "oruby-cbor", Summary: "ORuby pure-Go CBOR", Files: []string{"lib/oruby-cbor.rb", "lib/cbor.rb"}},
		{Name: "cbor", Version: "0.5.9.8", Feature: "oruby-cbor", Summary: "ORuby CBOR compatibility provider", Files: []string{"lib/cbor.rb"}},
		{Name: "oruby-lz4", Version: "0.3.3", Feature: "oruby-lz4", Summary: "ORuby pure-Go LZ4", Files: []string{"lib/oruby-lz4.rb", "lib/lz4-ruby.rb"}},
		{Name: "lz4-ruby", Version: "0.3.3", Feature: "oruby-lz4", Summary: "ORuby lz4-ruby compatibility provider", Files: []string{"lib/lz4-ruby.rb"}},
		{Name: "oruby-digest-crc", Version: "0.7.0", Feature: "oruby-digest-crc", Summary: "ORuby pure-Go digest-crc", Files: []string{"lib/digest/crc.rb", "lib/digest/crc32.rb", "lib/digest/crc32c.rb"}},
		{Name: "digest-crc", Version: "0.7.0", Feature: "oruby-digest-crc", Summary: "ORuby digest-crc compatibility provider", Files: []string{"lib/digest/crc.rb", "lib/digest/crc32.rb", "lib/digest/crc32c.rb"}},
		{Name: "oruby-xxhash", Version: "0.6.0", Feature: "oruby-xxhash", Summary: "ORuby pure-Go XXhash", Files: []string{"lib/oruby-xxhash.rb", "lib/xxhash.rb"}},
		{Name: "xxhash", Version: "0.6.0", Feature: "oruby-xxhash", Summary: "ORuby xxhash compatibility provider", Files: []string{"lib/xxhash.rb"}},
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
      extensions.all? { |e| Gem.__oruby_native_extension?(File.basename(File.dirname(e))) })
    super
  end
end
Gem::Specification.prepend(ORubyNativeExtensions)
`))
	return nil
}
