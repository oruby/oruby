// Package oruby encapsulates mruby bindings for Go.
//
// This project implements rich mruby bindings for the Go language.
// High-level details about the project may be found at its web page:
//
//     https://oruby.github.io
//
// Usage of the bindings is as close as possible to mruby C API.
// To get started, obtain a state (oruby.MrbState) using the New() function:
//
//     mrb, err := oruby.New()
//
// This creates MRuby state, wrapped in MrbState struct and base for
// MRuby API calls. From then on, classes and modules can be defined,
// instance, global (etc) variables can be set and get, methods can be
// called, expressions evaluated... For example, this Go code:
//
//     m := mrb.DefineModule("MyModule")
//     c := mrb.DefineClassUnder(m, "MyClass", mrb.ObjectClass())
//
//     c.DefineMethod("hello_world", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
//        return mrb.Value("Hello World")
//     }, MRB_ARGS_NONE())
//
// is equvivalent to ruby:
//
//     module MyModule
//       class MyClass
//         def hello_world
//           "Hello World"
//         end
//       end
//     end
//
//
// MRuby encodes string, numeric, and all other values in Value struct, which is
// light-weight struct around mrb_value and is passed as value between various API calls.
//
// ORuby uses Go naming recommendation (CamelCase) for method names, while
// Ruby, mruby, and C api use underscore based names (snake_case). So this C API code:
//
//     mrb_define_module(mrb, "SomeModule")
//
//  translates to oruby Go binding like this:
//
//     mrb.DefineModule("SomeModule")
//
// The dot gives scope to the struct, next is mandatory uppercased letter, and the
// rest of method call is the same. Arguments are the same, without mrb_state pointer,
// which is encapsulated in oruby.MrbState struct.
//
// For more details - see the documentation for the types and methods.
// For mruby API usage - consult the MRuby docs and code at https://www.mruby.org
//
// NOTE: ORuby is preliminary release for internal team review.
//       Use with care - expect changes - enjoy.
//
package oruby
