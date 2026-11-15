# ORuby standard-library providers

Blank-import `github.com/oruby/oruby/gem/stdlib` to register the standard-library and related provider gems currently shipped with ORuby. The former `gem/fullcore` package remains as a compatibility import and forwards to this package. The database gem remains opt-in.

This is a provider collection, not a complete MRI standard library. Some names have useful Go-backed or Ruby-source implementations, some are provided by the configured mruby build, and some registrations are compatibility placeholders. Availability through `require` does not mean complete MRI behavior or passing the corresponding `ruby/spec` suite; consult individual gem sources and tests for the current implementation level.

## Go-backed Ruby classes and AOT

Where it suits the implementation, model a Ruby class or module with a Go struct whose methods implement the underlying behavior. Register a Ruby-facing adapter that preserves Ruby method names and uses ORuby's argument specifications to parse required, optional, rest, block, and keyword arguments and supply defaults before calling the Go method. This is a design preference, not a requirement; ordinary ORuby callbacks remain appropriate when a struct would add no value.

An AOT compiler may directly call such a Go method only when it can prove that the receiver and implementation are the expected ones and that Ruby-visible conversions, argument errors, blocks, exceptions, and method redefinitions have the same behavior. Otherwise it must use normal Ruby dispatch. State-owned Ruby values must not escape their `MrbState` or be shared across states by a direct-call optimization.