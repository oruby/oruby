# mruby: class name of a class nested in an anonymous module

Status: **not filed**. Found 2026-11-20. Repro below comes from oruby notes and is **unverified against plain mruby**: confirm with an unmodified mruby build before submitting.

Where: `src/class.c`, `mrb_class_name_class`.

A nested path under an anonymous module (`#<Module:0x..>::N::Inner`) is stored in `__classname__` as if permanent. It should set `__outer__` instead when the outer path starts with `#`, so the name becomes real once the outer module is named.

```ruby
m = Module.new
m::Inner = Class.new
M = m
p M::Inner.name   # MRI: "M::Inner"
```

oruby workaround: go-mrb.c `mod_name_assigned` (in `Module#const_added`) drops such strings.
