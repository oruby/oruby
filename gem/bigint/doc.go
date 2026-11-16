// Package bigint is a Go math/big implementation of arbitrary-precision
// integers, kept for future use and not loaded by gem/stdlib.
//
// ORuby's Integer is mruby's own: the bundled mruby is built with
// mruby-bigint, so integer literals, VM overflow promotion and the C core
// (pack, sprintf, Rational, ...) all produce and accept native Integer
// values (MRB_TT_BIGINT), as in MRI. Go code converts them with
// MrbState.BigInt and MrbState.BigIntValue.
//
// This package is the starting point for two planned uses:
//   - a Go-backed implementation of mruby's mrb_bint_* interface (see
//     mruby/examples/mrbgems/mruby-YOUR-bigint), keeping Integer native while
//     math/big does the arithmetic;
//   - AOT-compiled code that keeps big integers in Go and calls math/big
//     directly.
//
// Importing it still registers the old "bigint" gem (a separate Bignum class
// that replaces Integer's operators); that is not MRI behavior and turns off
// the VM's and the compiler's integer fast paths, so gem/stdlib does not.
package bigint
