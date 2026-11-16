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
- `make check` — `scripts/check_api.rb` lists `MRB_API` functions from mruby headers not yet wrapped in the matching `<header>.go` file.
- `make update` — clean, pull latest submodule, rebuild libmruby.
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
| `gems.go` | Go-gem registry: `oruby.Gem(name, initFn)`, `Resolve`, `Require`. |
| `gem/<name>/` | Stdlib-like gems implemented in Go (base64, json, io, regexp, time, process, zlib, database (sqlite3), erubi, …). `gem/fullcore` blank-imports a standard set. |
| `cmd/orbi` | REPL (mirb equivalent). |
| `cmd/oruby` | Script runner (mruby equivalent). |
| `cmd/orubyc` | Compiler (mrbc equivalent). |
| `scripts/` | mruby build configs, gembox, API-coverage and `.def` scripts. |
| `mruby/` | Submodule — **do not edit**. |

## Conventions

- **Naming:** mruby C `mrb_foo_bar(mrb, ...)` → Go `mrb.FooBar(...)` on `*MrbState` (or on `RClass`, etc.). Keep argument order, drop the `mrb_state*`. Each wrapper should call `C.mrb_foo_bar(` literally so `check_api.rb` detects it.
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
