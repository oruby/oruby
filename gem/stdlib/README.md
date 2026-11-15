# ORuby standard-library providers

ORuby targets **MRI Ruby 4.1 compatibility**. This is a compatibility direction, not a claim that the current provider set or any individual library is complete or passes the Ruby 4.1 specifications.

Blank-import `github.com/oruby/oruby/gem/stdlib` to register the standard-library and related provider gems currently shipped with ORuby. The former `gem/fullcore` package remains as a compatibility import and forwards to this package. The database gem remains opt-in.

This is a provider collection, not a complete MRI standard library. Some names have useful Go-backed or Ruby-source implementations, some are provided by the configured mruby build, and some registrations are compatibility placeholders. Availability through `require` does not mean complete MRI behavior or passing the corresponding `ruby/spec` suite; consult individual gem sources and tests for the current implementation level.

RubyGems and Bundler must use their Ruby implementations from the targeted MRI Ruby version, not Go reimplementations. They are not yet provided or claimed compatible; bringing them in requires matching their versions to the selected MRI release and addressing ORuby loader and runtime dependencies.

`optparse`, `prettyprint`, `pp`, and `matrix` currently implement limited subsets rather than the complete MRI APIs. `open-uri` provides HTTP(S) GET via Go's HTTP client, with a 30-second timeout and a 64 MiB response limit; request options, response metadata, and the complete MRI interface are not implemented. Redirects to non-HTTP schemes or from HTTPS to HTTP are rejected. `ERB` supports generated source, binding-based rendering, hash locals and `run`; non-default trim modes and method/class generation are not implemented.

## Go-backed Ruby classes and AOT

Where it suits the implementation, model a Ruby class or module with a Go struct whose methods implement the underlying behavior. Register a Ruby-facing adapter that preserves Ruby method names and uses ORuby's argument specifications to parse required, optional, rest, block, and keyword arguments and supply defaults before calling the Go method. This is a design preference, not a requirement; ordinary ORuby callbacks remain appropriate when a struct would add no value.

An AOT compiler may directly call such a Go method only when it can prove that the receiver and implementation are the expected ones and that Ruby-visible conversions, argument errors, blocks, exceptions, and method redefinitions have the same behavior. Otherwise it must use normal Ruby dispatch. State-owned Ruby values must not escape their `MrbState` or be shared across states by a direct-call optimization.

## YAML and OpenSSL backends

The opt-in-on-require `yaml`/`psych` provider uses the maintained Go module `go.yaml.in/yaml/v3`; YAML support is not part of Go's standard library. The current provider is deliberately a safe subset: it converts core scalars, sequences, and string-keyed mappings, supports bounded file loading, and rejects custom tags, multiple documents, unsupported Ruby values, cycles, and over-limit inputs. Aliases are disabled by default; `aliases: true` enables bounded expansion into separate values, so Ruby object identity is not preserved. It does not implement full `Psych` compatibility or arbitrary Ruby object loading.

The `openssl` provider uses Go's `crypto/tls`, `crypto/x509`, `crypto/hmac`, `crypto/rand`, `crypto/subtle`, and standard hash packages. `OpenSSL::Random.random_bytes` limits each request to 16 MiB. Go does **not** link to or use OpenSSL here; this is a small Ruby API compatibility layer backed by Go cryptography, not a native OpenSSL binding. OpenSSL-specific APIs, algorithms, cipher controls, certificate behavior, and error details outside the implemented subset remain unsupported or may differ from MRI.