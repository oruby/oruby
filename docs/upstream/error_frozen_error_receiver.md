# mruby: `FrozenError#receiver` is not set

Status: **not filed**. Found 2026-11-21. Repro below comes from oruby notes and is **unverified against plain mruby**: confirm with an unmodified mruby build before submitting.

Where: `src/error.c`, `frozen_error`.

It raises without recording the frozen object, so `FrozenError#receiver` has nothing to return.

```ruby
begin; "a".freeze << "b"; rescue FrozenError => e; p e.receiver; end  # MRI: "a"
```

Fix: pass the object to `frozen_error` and set it as `@receiver` (as NameError does).

oruby workaround: `_orb_frozen_fix` finds the object again from the frame (self or the OP_DEF/OP_SDEF/OP_TDEF target).
