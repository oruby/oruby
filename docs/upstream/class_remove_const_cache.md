# mruby: `remove_const` does not clear the constant cache

Status: **not filed**. Found 2026-11-20. Repro below comes from oruby notes and is **unverified against plain mruby**: confirm with an unmodified mruby build before submitting.

Where: `src/class.c`, `mrb_mod_remove_const`.

It does not call `mrb_const_cache_clear`, so `OP_GETCONST` can return the removed value. Flaky: it depends on cache hash collisions.

```ruby
class A; X = 1; end
def get = A::X   # fill the cache
get
A.send(:remove_const, :X)
get              # expected NameError; may still return 1
```

Fix: one line, call `mrb_const_cache_clear(mrb)` after removing (as `const_set` paths do).

oruby workaround: go-mrb.c `remove_const_w` clears the cache.
