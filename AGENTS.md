# AGENTS.md

Guidance for AI agents working in this repository.

## Project

ORuby (`github.com/oruby/oruby`) is a Go package that embeds [mruby](https://github.com/mruby/mruby) via cgo and exposes Go types/functions to mruby scripts. Status: pre-release, API still changing.

## Setup & build

mruby is a git submodule (`mruby/`, tracking upstream `master` until mruby 4.1.0 is released — it carries fixes not on `stable`, such as bare `raise`/`$!`, keyword arguments in bare `super`, native `autoload` and refinements) and must be built before any Go code compiles. mruby vendors Prism as a nested submodule, so update recursively.

```sh
git submodule update --init --recursive      # populate mruby/ and its Prism submodule
make                                          # builds mruby/build/host/lib/libmruby.a, runs check_api.rb, go build
```

- Requires: Go (see `go.mod`), a C toolchain (clang on macOS), Ruby + rake (mruby build), readline/ncurses.
- `MRUBY_CONFIG` is selected by the Makefile: `scripts/build_config_{darwin,unix,win}.rb`. Gems compiled into libmruby are listed in `scripts/oruby.gembox`.
- `make bin` — builds `bin/oruby`, `bin/orbi`, `bin/orubyc`; next to `lib/oruby` they run in-tree. `make install PREFIX=…` installs tools + `lib/oruby`. `oruby`/`orbi` load RubyGems at startup (`--disable-gems` skips it).
- `make check` — `scripts/check_api.rb` lists `MRB_API` functions from mruby headers that oruby does not use (no `C.name(` in a root `.go` file, no call in the `go-*.c`/`go-*.h` shims); its `IGNORED` table holds the ones left out on purpose, with the reason. mruby calls that can raise go through a protected shim in `go-mrb.h` (`_mrb_protected_op` & co.) and come back to Go as an `error`, never as a longjmp across Go frames.
- `make update` — clean, pull latest submodule, rebuild libmruby.
- Symbol names: in C use `MRB_SYM(name)` (and `MRB_SYM_Q`, `MRB_IVSYM`, …), in Go the `oruby.Sym_name` constants, rather than interning a literal at run time; a new name becomes a constant after `make` (tools/gensym). A name missing from the table is a compile error, not a wrong symbol.
- Go's build cache does not track mruby's headers or `libmruby.a`: after rebuilding libmruby against a different mruby revision, run `go clean -cache` (or build/test with `-a`), otherwise cached cgo objects with the old struct layout crash at `mrb_open` ("allocation failure").
- Windows (`mingw32-make`, `mruby.dll`) is unsupported/WIP.

## Testing

```sh
make test          # root package + each gem/* package (see Makefile list)
go test ./...      # alternative; gem/signal is excluded in Makefile
go test -run TestName .            # single test, root package
(cd gem/json && go test -run X)    # single gem
```

Tests need the built `libmruby.a`; if you see cgo/link errors about `mruby.h` or `-lmruby`, the submodule isn't built.

Test helpers (root package): `Expect`, `ExpectEql`, `ExpectNilError`, `ExpectErr`, `Pending` in `helpers_test.go`. Typical test pattern:

```go
mrb := oruby.MrbOpen()
defer mrb.Close()
v, err := mrb.Eval(`ruby code`)
```

## Layout

| Path | Purpose |
|------|---------|
| `*.go` (root) | Bindings. Files mirror mruby headers: `array.go` ↔ `mruby/array.h`, `string.go`, `hash.go`, `class.go`, `proc.go`, `variable.go`, `compile.go`, `irep.go`, `gc.go`, … `mruby.go` ↔ `mruby.h` (largest; core `MrbState`). |
| `go*.go` (root) | Go-side conveniences on top of the raw API: `goclass.go`, `goobject.go`, `goarray.go`, `gohash.go`, `gostring.go`, `goerror.go`, `goprint.go`. |
| `reflections.go`, `value.go`, `args.go` | Go ↔ mruby value conversion (reflection), `Value`/`MrbValue`, argument parsing (`GetArgs*`, `Args*` aspecs). |
| `go-mrb.h`, `go-mrb.c` | C shims: inline wrappers for mruby macros (`_MRB_...`) and Go↔C callback trampolines. |
| `prism.go`, `go-prism.c` | Source rewriter: parses Ruby with Prism (inside libmruby) and rewrites, by exact node span, the constructs mruby's codegen rejects (flip-flops, `A::B \|\|=`, gvar aliases, dynamic alias/undef, …) into code it compiles; helpers are `Kernel.__oruby_*__` in `mruby.go`. |
| `tools/gensym`, `presym_gen.go` | Presyms for oruby: gensym collects the symbol names the Go and cgo C code look up by literal (`Intern("x")`, `mrb_intern_lit(mrb, "x")`, …) into the `scripts/mruby-oruby-presym` gem, so libmruby preallocates them, then writes `presym_gen.go`, a Go constant per name (`Sym_to_s`, `SymQ_nil`, `IvSym_name`, `GvSym_stdout`, `OpSym_add`). `make` runs both steps around the libmruby build; both outputs are generated, do not edit. |
| `signal.go`, `go-signal.c` | Asynchronous requests into Ruby: OS signals (`signal.Notify`) and `Timeout.timeout` deadlines (a Go `context.WithTimeoutCause` per block, derived from the state's current one: `mrb.GoContext()`). A goroutine marks the request and arms a one-shot `code_fetch_hook` (or the injector polls it), which raises at the VM's next instruction (`Signal.__oruby_signal__`, `Timeout.__oruby_expire`). `gem/signal` and `gem/stdlib/ruby/timeout.rb` hold the Ruby side; `cmd/oruby` installs MRI's default signal handlers (`signal.InstallDefaults`). |
| `gems.go` | Go-gem registry: `oruby.Gem(name, initFn)`, `Resolve`, `Require`. |
| `gem/<name>/` | Stdlib-like gems implemented in Go (base64, json, io, regexp, time, process, zlib, database (sqlite3), erubi, …). `gem/fullcore` blank-imports a standard set. |
| `cmd/orbi` | REPL (mirb equivalent). |
| `cmd/oruby` | Script runner (mruby equivalent). |
| `cmd/orubyc` | Compiler (mrbc equivalent). |
| `scripts/` | mruby build configs, gembox, API-coverage and `.def` scripts. |
| `lib/oruby/` | Install layout of the Ruby libraries ORuby ships as files: vendored RubyGems/Bundler 4.1.0.beta2 (`4.1.0/`) and MRI default gem specs (`gems/4.1.0/specifications/default`). Unmodified upstream copies — do not edit. `golayout.go` finds it from the executable (`<prefix>/bin/oruby`). |
| `mruby/` | Submodule — **do not edit**. |

## Conventions

- **Naming:** mruby C `mrb_foo_bar(mrb, ...)` → Go `mrb.FooBar(...)` on `*MrbState` (or on `RClass`, etc.). Keep argument order, drop the `mrb_state*`. Each wrapper should call `C.mrb_foo_bar(` literally, or a `go-mrb.h` shim that does, so `check_api.rb` detects it.
- Put a new binding in the root file named after the mruby header that declares it (`mruby.h` APIs may also live in `class.go`).
- mruby macros can't be called from cgo — add a `static inline` `_MACRO_NAME` wrapper in `go-mrb.h`.
- Method callbacks have signature `func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue` (`MrbFuncT`). Raise errors with `return mrb.Raise(mrb.EArgumentError(), msg)`.
- **New gem:** create `gem/<name>/` package, register in `init()` via `oruby.Gem("<name>", func(mrb *oruby.MrbState) interface{} {...})`, add a `_test.go`, add it to the `test` target in `Makefile`, and optionally to `gem/fullcore`.
- Platform-specific code uses `_unix.go` / `_windows.go` / `_darwin.go` / `_linux.go` suffixes.
- cgo flags are in `mruby.go`. Note the `MRB_INT64 … MRB_NO_BOXING` CFLAGS apply to 64-bit platforms (`amd64` and `arm64`); they must match how libmruby was built (`scripts/build_config_*.rb` + gems).
- Go sources use LF; `Makefile`, `README.mkd` and `scripts/*` use CRLF. Preserve existing line endings when editing.
- Format with `gofmt`; run `go vet ./...` before finishing.

## Gotchas

- `MrbState` is not goroutine-safe; it carries a `sync.Mutex`, and code from other goroutines/signal handlers should go through `mrb.Inject` / `mrb.InjectFunc` rather than calling the state directly.
- Values returned to mruby must stay reachable; the state keeps Go callbacks/objects in maps (`mrbProcs`, `classmap`, `funcs`) to keep them from being GC'd. Follow existing patterns rather than passing Go pointers into C directly.
- Always `defer mrb.Close()` in tests and examples.
- A signal or a `Timeout.timeout` deadline reaches Ruby only when the VM runs its next instruction. A Go method that blocks for long should wait on `mrb.GoContext()` (pass it to `net/http`, a `net.Conn` deadline, a `select`) and on `mrb.SignalWake()`; when either fires it returns `mrb.RaiseError(err)` for a non-nil `mrb.ContextErr(ctx)` / `mrb.CheckSignals()`. Otherwise the timeout or Ctrl-C waits until the call returns (a second SIGINT still ends the process).
