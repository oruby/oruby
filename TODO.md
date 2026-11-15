# ORuby & AOT Compiler — Ruby Spec Status & TODO

This document tracks the compatibility of **ORuby Direct (interpreted)** and the **ORuby AOT Compiler** against the 47 core Ruby specification files in `mruby/test/t/*.rb` (tested via `gem/assert` and `gem/fullcore`), along with the concrete tasks required to reach 100% spec compliance.

---

## Summary Matrix

| Mode | Compiles Cleanly | Passes 100% | Files with Failures / Crashes |
| :--- | :---: | :---: | :---: |
| **ORuby Direct (VM)** | 47 / 47 | **32 / 47** | 15 (5 missing C test fixtures, 10 runtime/gem differences) |
| **ORuby AOT Compiler** | 38 / 47 | **15 / 47** | 32 (9 Go build issues, 14 unhandled opcodes, catch-table & splat runtime handling) |

### Spec Files Passing 100% in Both Direct & AOT Compiled Modes (15)
- `bs_literal.rb` (9 specs)
- `enumerable.rb` (22 specs)
- `false.rb` (6 specs)
- `indexerror.rb` (1 spec)
- `iterations.rb` (4 specs)
- `lang.rb` (2 specs)
- `localjumperror.rb` (1 spec)
- `platform.rb` (2 specs)
- `rangeerror.rb` (1 spec)
- `runtimeerror.rb` (1 spec)
- `standarderror.rb` (1 spec)
- `true.rb` (6 specs)
- `typeerror.rb` (1 spec)
- `unicode.rb` (3 specs)
- `version.rb` (1 spec)

### Additional Spec Files Passing 100% in Direct Mode Only (17)
- `argumenterror.rb`
- `bs_block.rb`
- `class.rb`
- `comparable.rb`
- `ensure.rb`
- `gc.rb`
- `hash.rb`
- `literals.rb`
- `methods.rb`
- `nameerror.rb`
- `nil.rb`
- `nomethoderror.rb`
- `numeric.rb`
- `proc.rb`
- `regexperror.rb`
- `superclass.rb`
- `symbol.rb`

---

## Part 1: AOT Compiler TODO (`compiler/`)

### 1.1 Fix Go Codegen Build Errors (9 Spec Files Affected)
- [ ] **Conditional `"fmt"` import (`compiler/codegen.go`)**
  - `emitHeader` currently imports `"fmt"` unconditionally, causing `"fmt" imported and not used` when compiling packages with `Standalone: false`. Only import `"fmt"` when `Standalone: true`.
- [ ] **Empty Hash literal `{}` (`OpHASH` with `ins.B == 0`) (`compiler/codegen.go`)**
  - Affects: `argumenterror.rb`, `codegen.rb`, `gc.rb`, `hash.rb`, `kernel.rb`, `vformat.rb`.
  - When `ins.B == 0`, `hv, fh := fastHashAlloc()` declares `fh` without using it (`declared and not used: fh`). Emit `r[A], _ = fastHashAlloc()` when `ins.B == 0`.
- [ ] **Implement `OpJMPNIL` to fix orphan jump labels (`compiler/codegen.go`)**
  - Affects: `codegen.rb`, `env.rb`, `kernel.rb`, `nil.rb`, `syntax.rb`.
  - `ins.IsJump()` returns `true` for `OpJMPNIL` (emitting `L_xxxx:` labels), but `OpJMPNIL` is missing from `emitUnit`'s `switch ins.Op`, causing `label L_xxxx defined and not used`.
  - Emit: `if r[A].IsNil() { goto L_xxxx }` (with `mrb.GCArenaRestore(_ai)` on backward jumps).
- [ ] **Support `CALL_MAXARGS` (`ins.C & 0xf == 15`) Splat/Varargs in `OpSEND` / `OpSSEND` / `OpSENDB` / `OpSSENDB` (`compiler/codegen.go`)**
  - Affects: `codegen.rb`, `syntax.rb`.
  - In mruby bytecode, `n == 15` (`CALL_MAXARGS`) means arguments are packed as an `MrbTTArray` in `r[A+1]` rather than 15 individual registers `r[A+1]..r[A+15]` (which causes Go compile-time array bounds errors `invalid argument: index 7 out of bounds`).
  - Unpack `r[A+1]` slice at runtime and pass to `fastCall` / `FuncallWithBlock`.

### 1.2 Implement 14 Remaining Bytecode Opcodes (`compiler/codegen.go`)
Currently falling through to `// Unhandled opcode: <OP>`:

- [ ] **`OP_JMPNIL`** (44 occurrences in `codegen.rb`, `env.rb`, `kernel.rb`, `nil.rb`, `syntax.rb`)
  - Jump to `ins.Target` if `r[A].IsNil()`.
- [ ] **`OP_BLKCALL`** (42 occurrences in `bs_block.rb`, `codegen.rb`, `ensure.rb`, `exception.rb`, `gc.rb`, `kernel.rb`, `module.rb`, `syntax.rb`)
  - `yield` / block call inside a method (`mrb.YieldArgv(r[A], args...)`).
- [ ] **`OP_UNDEF`** (137 occurrences in `codegen.rb`, `kernel.rb`, `methods.rb`)
  - `undef method_name` on target class (`mrb.UndefMethodID(mrb.ClassPtr(self), sym)`).
- [ ] **`OP_ALIAS`** (12 occurrences in `codegen.rb`, `hash.rb`, `methods.rb`, `nameerror.rb`, `syntax.rb`)
  - `alias new_mid old_mid` (`mrb.AliasMethod(mrb.ClassPtr(self), symA, symB)`).
- [ ] **`OP_INTERN`** (9 occurrences in `literals.rb`, `symbol.rb`, `syntax.rb`)
  - Dynamic symbol creation `:"foo_#{bar}"` (`r[A] = mrb.StrIntern(r[A])`).
- [ ] **`OP_GETCV` & `OP_SETCV`** (26 occurrences in `class.rb`, `ensure.rb`, `module.rb`, `syntax.rb`)
  - Class variable `@@var` lookup and assignment (`mrb.CVGet(self, sym)` / `mrb.CVSet(self, sym, r[A])`).
- [ ] **`OP_APOST`** (16 occurrences in `bs_block.rb`, `codegen.rb`, `hash.rb`, `proc.rb`, `syntax.rb`)
  - Post-splat array destructuring `a, *b, c = ary` (slice pre/post elements and middle splat array from `r[A]`).
- [ ] **`OP_KARG`, `OP_KEY_P`, `OP_KEYEND`** (73 total occurrences in `hash.rb`, `numeric.rb`, `syntax.rb`)
  - Keyword argument extraction, presence check, and unknown-keyword validation from keyword hash register.
- [ ] **`OP_SUPER`** (18 occurrences in `hash.rb`, `kernel.rb`, `module.rb`, `syntax.rb`)
  - Resolve superclass method for the current class/method and invoke it with `self` and arguments.
- [ ] **`OP_BREAK`** (15 occurrences in `bs_block.rb`, `codegen.rb`, `ensure.rb`, `exception.rb`, `kernel.rb`, `numeric.rb`, `proc.rb`, `range.rb`, `syntax.rb`)
  - Non-local break return from block/closure back to the enclosing method loop/call.
- [ ] **`OP_MATCHERR`** (37 occurrences in `syntax.rb`)
  - Raise `NoMatchingPatternError` in Ruby 3+ `case ... in` pattern matching when no branch matches.

### 1.3 Fix Runtime Exception Handling (`begin / rescue / ensure`) & Splat Edge Cases
- [ ] **Decode `mrb_irep_catch_handler` table in `compiler/decoder.go` and emit `rescue`/`ensure` dispatch in `compiler/codegen.go`**
  - Affects: `exception.rb`, `ensure.rb`, `nomethoderror.rb`, `nameerror.rb`, `sysfail.rb`, `methods.rb`, `float.rb`.
  - mruby uses an IRep catch table (`MRB_CATCH_FILTER_RESCUE` and `MRB_CATCH_FILTER_ENSURE`) covering `[begin, end) -> target` bytecode ranges rather than explicit jump instructions when an exception is raised.
  - Check `mrb.Exc()` after calls inside covered `[begin, end)` ranges and jump to the matching handler label `L_target`.
- [ ] **Avoid C `longjmp` across Go stack frames in `OpRAISEIF` (`compiler/codegen.go`)**
  - Affects: `exception.rb`, `sysfail.rb` (`fatal error: exitsyscall: syscall frame is no longer valid`).
  - `OpRAISEIF` currently calls `mrb.Raise(...)` directly (which invokes `mrb_exc_raise` / `longjmp` across the Go frame). Instead, set `mrb->exc` and return `(mrb.NilValue(), mrb.Err())` or jump to the enclosing catch handler.
- [ ] **Safe `OpARYCAT` and `OpARYSPLAT` handling (`compiler/codegen.go`, `array.go`)**
  - Affects: `array.rb`, `comparable.rb`, `string.rb`, `superclass.rb`, `class.rb` (`SIGSEGV` in `mrb_ary_concat`).
  - In mruby's VM, `OP_ARYCAT` splats `r[A+1]` first (`mrb_ary_splat`) and handles `r[A]` being `nil` by initializing an empty array before concatenating.

---

## Part 2: ORuby Direct (VM & Gems) TODO

### 2.1 Fix Bugs in ORuby Gems & Core Wrappers
- [ ] **Fix `NaN` panic in `gem/bigint` (`integer.rb`)**
  - In `gem/bigint/rbigint.go:69`, `toBigInt` calls `math/big.NewFloat(f)` without checking `math.IsNaN(f)` or `math.IsInf(f, 0)`, which panics when comparing a BigInt against `Float::NAN` (`RBigInt.Cmp`).
- [ ] **Fix `String#gsub`, `String#sub`, `String#index`, `String#slice`, `String#bytesplice` in `gem/regexp` (`string.rb`)**
  - `gem/regexp` overrides `String` methods using Go's `regexp` package without full Ruby backslash replacement semantics (`\&`, `\1`, `\'`, `` \` ``) or byte/character offset parity.
- [ ] **Fix `BasicObject.superclass` and `Object.superclass` (`basicobject.rb`, `object.rb`)**
  - ORuby attaches `GO` class as superclass of `BasicObject`, causing `BasicObject.superclass` to return `GO` instead of `nil`.
- [ ] **Fix `Module#remove_const`, `Module#dup`, `Module#to_s` (`module.rb`)**
  - Align `Module` visibility and naming edge cases in `mruby.go` / `class.go`.
- [ ] **Fix `Exception` in rescue (`exception.rb`)**
  - 2 failing assertions in `exception.rb`: `GC in rescue` and `Method call in rescue`.
- [ ] **Fix `Float::NAN == Float::NAN` (`float.rb`)**
  - 1 failing assertion: `a NaN is equal to nothing at all, itself included`.
- [ ] **Fix `Range#last` on endless range (`range.rb`)**
  - 1 failing assertion: `Range#last [15.2.14.4.10]` (`cannot get the last element of endless range`).
- [ ] **Fix `__FILE__` and backtick command execution (`syntax.rb`, `codegen.rb`)**
  - Populate filename in `MrbCompileContext` when loading files, and fix `Kernel#` `` ` `` command output handling in `gem/process` / `gem/io/popen`.
- [ ] **Fix deep recursion guard in `Array#join` (`array.rb`)**
  - `Array#join deeply nested array does not overflow the C stack`.

### 2.2 Optional: Port `mruby-test` C Test Fixtures (5 Spec Files)
These failures are only due to C test fixtures from `mruby/mrbgems/mruby-test/` not being compiled into `libmruby.a` (not missing Ruby features):
- [ ] `AryShared` (`array.rb`)
- [ ] `__env_*` test helpers (`env.rb`)
- [ ] `TestNotImplement` (`kernel.rb`)
- [ ] `nofree_cstr?` (`string.rb`)
- [ ] `TestSysFail` (`sysfail.rb`)
- [ ] `TestVFormat` (`vformat.rb`)
