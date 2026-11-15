# ORuby standard-library providers

ORuby targets **MRI Ruby 4.1 compatibility**. This is a compatibility direction, not a claim that the current provider set or any individual library is complete or passes the Ruby 4.1 specifications.

Blank-import `github.com/oruby/oruby/gem/stdlib` to register the standard-library and related provider gems currently shipped with ORuby. The former `gem/fullcore` package remains as a compatibility import and forwards to this package. The database gem remains opt-in.

This is a provider collection, not a complete MRI standard library. Some names have useful Go-backed or Ruby-source implementations, some are provided by the configured mruby build, and some registrations are compatibility placeholders. Availability through `require` does not mean complete MRI behavior or passing the corresponding `ruby/spec` suite; consult individual gem sources and tests for the current implementation level.

## Go-backed Ruby classes and AOT

Where it suits the implementation, model a Ruby class or module with a Go struct whose methods implement the underlying behavior. Register a Ruby-facing adapter that preserves Ruby method names and uses ORuby's argument specifications to parse required, optional, rest, block, and keyword arguments and supply defaults before calling the Go method. This is a design preference, not a requirement; ordinary ORuby callbacks remain appropriate when a struct would add no value.

An AOT compiler may directly call such a Go method only when it can prove that the receiver and implementation are the expected ones and that Ruby-visible conversions, argument errors, blocks, exceptions, and method redefinitions have the same behavior. Otherwise it must use normal Ruby dispatch. State-owned Ruby values must not escape their `MrbState` or be shared across states by a direct-call optimization.

## YAML and OpenSSL backends

The opt-in-on-require `yaml`/`psych` provider uses the maintained Go module `go.yaml.in/yaml/v3`; YAML support is not part of Go's standard library. The current provider is deliberately a safe subset: it converts core scalars, sequences, and string-keyed mappings, and rejects custom tags, aliases, multiple documents, unsupported Ruby values, cycles, and over-limit inputs. It does not implement full `Psych` compatibility or arbitrary Ruby object loading.

The `openssl` provider uses Go's `crypto/tls`, `crypto/x509`, `crypto/hmac`, `crypto/subtle`, and standard hash packages. Go does **not** link to or use OpenSSL here; this is a small Ruby API compatibility layer backed by Go cryptography, not a native OpenSSL binding. OpenSSL-specific APIs, algorithms, cipher controls, certificate behavior, and error details outside the implemented subset remain unsupported or may differ from MRI.