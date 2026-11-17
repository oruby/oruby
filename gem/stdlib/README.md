# ORuby standard-library providers

ORuby targets **MRI Ruby 4.1 compatibility**. This is a compatibility direction, not a claim that the current provider set or any individual library is complete or passes the Ruby 4.1 specifications.

Blank-import `github.com/oruby/oruby/gem/stdlib` to register the standard-library and related provider gems currently shipped with ORuby. The former `gem/fullcore` package remains as a compatibility import and forwards to this package. The database gem remains opt-in.

This is a provider collection, not a complete MRI standard library. Some names have useful Go-backed or Ruby-source implementations, some are provided by the configured mruby build, and some registrations are compatibility placeholders. Availability through `require` does not mean complete MRI behavior or passing the corresponding `ruby/spec` suite; consult individual gem sources and tests for the current implementation level.

RubyGems and Bundler are their authentic Ruby implementations from the targeted MRI version, not Go reimplementations: RubyGems and Bundler 4.1.0.beta2, as shipped with MRI 4.1.0-preview1, are vendored unmodified in `lib/oruby/4.1.0`, with MRI's default gem specifications in `lib/oruby/gems/4.1.0/specifications/default` (see `lib/oruby/README.md`). ORuby finds that tree from its executable (`<prefix>/bin/oruby`), the way MRI does, and `oruby` and `orbi` load RubyGems at startup unless given `--disable-gems`.

Status as of 2026-11-16, with no MRI installation involved except a gem directory holding an installed `rake`:

- RubyGems is loaded at startup. `Gem::Version`, `Gem::Requirement`, `Gem::Specification.new`, installed-spec discovery (`Gem::Specification.all`, `find_by_name`) and `gem "rake"` activation work, and the installed pure-Ruby `rake` 13.4.2 loads. Gems with native extensions are skipped by RubyGems itself ("extensions are not built").
- `require "bundler"` loads Bundler 4.1.0.beta2, and the Gemfile DSL (`source`, `gem`) evaluates.
- `require "bundler/setup"` with a Gemfile and lockfile (`gem "rake"`) resolves, activates and loads the gem, with the same result as MRI (`Bundler.load.specs` = `rake-13.4.2`, `bundler-4.1.0.beta2`); with a Gemfile naming no gems it also writes the lockfile. `cmd/oruby/rubygems_test.go` checks this offline.

The runtime changes that got this far: Ruby-compatible regexps (`gem/regexp/full`), an MRI-style `RbConfig` install layout, `RUBY_PATCHLEVEL` and related constants, `Module#const_get(name, inherit)` and `method_defined?(name, inherit)`, `LoadError#path`, a complete `ENV`, eager `Pathname` (core in Ruby 4), `Thread::Queue` aliases and thread variables, an unlimited dynamic-symbol table (`MRB_SYMBOL_MAX=0`, which requires rebuilding libmruby), `Range#bsearch`, the FileUtils option tables, and `to_ary` conversion that ignores visibility in `Array#flatten`.

The mruby submodule tracks upstream `master`, which provides `$!` and re-raising bare `raise`, keyword arguments in bare `super`, `defined?(::X)`, `Exception#cause`, and native `autoload` (with `defined?` and lexical lookup), String names for `private` and friends, and `caller_locations`; ORuby uses those instead of its own emulation.

`Encoding` is MRI's class of Encoding objects (MRI 4.1's 103 encodings, aliases and constants); strings report them. Strings carry mruby's UTF-8 or binary tag, so any other encoding is kept in a hidden per-string tag, which `dup` and `clone` keep and most other string operations drop; `Encoding::Converter` and transcoding beyond `force_encoding` are not implemented.

Numbers are mruby's numeric tower, as in MRI: `Integer` with arbitrary precision (mruby-bigint; big literals, overflow promotion and the C core all use it), `Rational` (mruby-rational) and `Complex` with exact parts (mruby-complex). `BigDecimal` is Go-backed (`gem/bigdecimal`). The earlier Go `Bignum` class (`gem/bigint`) is kept for a future Go-backed bigint implementation and AOT use but is not loaded; Go code converts Integers with `MrbState.BigInt` / `BigIntValue`.

Known gaps that affect these libraries: Bundler's parallel workers need `Thread.new`, which is unsupported (they are only started for git sources). The sources are not yet vendored into ORuby's stdlib.

Regular expressions: `gem/regexp` runs every pattern Go's RE2 can express exactly, after translating Ruby syntax and semantics, and `gem/stdlib` also imports `gem/regexp/full`, a pure-Go backtracking fallback (`github.com/dlclark/regexp2/v2`) for Ruby-only syntax such as lookaround, atomic groups, possessive quantifiers and backreferences. Embedded users who import only `gem/regexp` do not link the fallback and get a `RegexpError` naming any unsupported construct. Subexpression calls (`\g<name>`), the absent operator and byte-wise matching of binary strings are not supported; see `gem/regexp/README.md`.

Ruby `Thread.new` is not supported (see `gem/thread`), so providers that MRI implements with helper threads differ: `Open3` returns a waiter object with `value`, `join` and `pid` rather than a `Thread`, and `Process.detach` is unavailable.

The `gem/marshal` package implements a bounded Marshal 4.8 subset; see its README for support limits and the critical warning that `Marshal.load` is unsafe for untrusted input.

`optparse`, `prettyprint`, `pp`, and `matrix` currently implement limited subsets rather than the complete MRI APIs. `open-uri` provides HTTP(S) GET via Go's HTTP client, with a 30-second timeout and a 64 MiB response limit; request options, response metadata, and the complete MRI interface are not implemented. Redirects to non-HTTP schemes or from HTTPS to HTTP are rejected. `ERB` supports generated source, binding-based rendering, hash locals and `run`; non-default trim modes and method/class generation are not implemented.

## Go-backed Ruby classes and AOT

Where it suits the implementation, model a Ruby class or module with a Go struct whose methods implement the underlying behavior. Register a Ruby-facing adapter that preserves Ruby method names and uses ORuby's argument specifications to parse required, optional, rest, block, and keyword arguments and supply defaults before calling the Go method. This is a design preference, not a requirement; ordinary ORuby callbacks remain appropriate when a struct would add no value.

An AOT compiler may directly call such a Go method only when it can prove that the receiver and implementation are the expected ones and that Ruby-visible conversions, argument errors, blocks, exceptions, and method redefinitions have the same behavior. Otherwise it must use normal Ruby dispatch. State-owned Ruby values must not escape their `MrbState` or be shared across states by a direct-call optimization.

## YAML and OpenSSL backends

The opt-in-on-require `yaml`/`psych` provider uses the maintained Go module `go.yaml.in/yaml/v3`; YAML support is not part of Go's standard library. The current provider is deliberately a safe subset: it converts core scalars, sequences, and string-keyed mappings, supports bounded file loading, and dumps `OpenStruct` fields and `File` objects as Ruby object tags. Loading still rejects custom tags, including those emitted for these classes; unsupported Ruby values, cycles, multiple documents, and over-limit inputs are rejected. Aliases are disabled by default; `aliases: true` enables bounded expansion into separate values, so Ruby object identity is not preserved. The Ruby 4.1 `YAML.dump` File spec's direct serialized output matches MRI, but its `match_yaml` helper falls back from `YAML.unsafe_load` to safe `YAML.load` and then compares the tagged document as a string, so that one example remains incompatible until a suitably restricted matcher or loading contract is available. It does not implement full `Psych` compatibility or arbitrary Ruby object loading.

The `openssl` provider uses Go's `crypto/tls`, `crypto/x509`, `crypto/hmac`, `crypto/rand`, `crypto/subtle`, and standard hash packages. `OpenSSL::Random.random_bytes` limits each request to 16 MiB. Go does **not** link to or use OpenSSL here; this is a small Ruby API compatibility layer backed by Go cryptography, not a native OpenSSL binding. OpenSSL-specific APIs, algorithms, cipher controls, certificate behavior, and error details outside the implemented subset remain unsupported or may differ from MRI.