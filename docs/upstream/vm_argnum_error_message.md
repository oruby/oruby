# mruby: `argnum_error` reports only the required count

Status: **not filed**. Found 2026-11-20. Repro below comes from oruby notes and is **unverified against plain mruby**: confirm with an unmodified mruby build before submitting.

Where: `src/vm.c`, `argnum_error`.

For a method with optional or rest parameters the message is "wrong number of arguments (given 0, expected 2)", naming only the required count. MRI says `expected 2..3` / `expected 2+`.

```ruby
def m(a, b, c = 1); end
m  # mruby: expected 2    MRI: expected 2..3
def r(a, *s); end
r  # mruby: expected 1    MRI: expected 1+
```

Fix idea: `argnum_error` already has the OP_ENTER aspec; format `req`, `req..req+opt`, or `req+` from it. Small and self-contained.

oruby workaround: go-mrb.c `argnum_fix` rewrites the message in the raise hook.
