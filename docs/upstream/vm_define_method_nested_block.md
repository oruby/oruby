# mruby: `return` in a block nested in a `define_method` body

Status: **not filed**. Found 2026-11-20; narrowed 2026-11-25 after mruby `f9b4ba50b` (#7871).
Verified on oruby (not on plain mruby): confirm with an unmodified mruby build before submitting.

`super` and `defined?(super)` in such a block were fixed upstream by `f9b4ba50b`
(`proc.c`: a block made in a define_method body keeps `ci->mid`). `return` is
what remains: in a block inside a `define_method` body it raises LocalJumpError,
where MRI returns from the defined method.

```ruby
class A; def f = 1; end
class C < A
  define_method(:f) { [1].each { return 5 }; 9 }
end
C.new.f   # MRI: 5     mruby: LocalJumpError
```

Cause not analyzed yet: likely the block's return target is the env of the
method that created the define_method proc, which has already returned. Needs a
root-cause note (where `OP_RETURN` in a block finds its home frame) before this
is a small patch. Not worked around in oruby.
