# mruby: `Object#inspect` of a recursive object

Status: **not filed**. Found 2026-11-20. Repro below comes from oruby notes and is **unverified against plain mruby**: confirm with an unmodified mruby build before submitting.

Where: `src/kernel.c`, `mrb_obj_inspect`.

An object met again while inspecting its own ivars prints as `-<C:0x... ...>`: the placeholder `-` is never replaced on the recursive path. It also hides ivars when `#to_s` is overridden.

```ruby
class C; def initialize; @me = self; end; end
p C.new   # mruby: #<C:0x... @me=-<C:0x... ...>>   MRI: #<C:0x... @me=#<C:0x... ...>>
```

Fix: make the recursive branch emit `#<C:0x... ...>` without the leading `-`.

oruby workaround: go-mrb.c `CHK_OBJ_INSPECT` / `obj_inspect_ivars` with its own recursion guard.
