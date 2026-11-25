# mruby: `super`/`return` in a block nested in a `define_method` body

Status: **not filed**. Found 2026-11-20. Repro below comes from oruby notes and is **unverified against plain mruby**: confirm with an unmodified mruby build before submitting.

Where: VM (`OP_SUPER`, `OP_RETURN` from a block whose home is a `define_method` proc).

In plain mruby, inside a block within a `define_method` body, `super` fails ("super called outside of method"), `defined?(super)` is nil, and `return` raises LocalJumpError.

```ruby
class A; def f = 1; end
class B < A
  define_method(:f) { [1].each { return super() } }
end
B.new.f   # expected 1
```

Not worked around in oruby. Cause not yet analyzed: likely the block's env points at the define_method proc, which has no method callinfo of its own. Needs a root-cause note before this is a fixable patch.
