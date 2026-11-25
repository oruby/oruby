# mruby: svar lost after a bare `private`/`public`/`module_function`

Status: **not filed**. Found 2026-11-21. Repro below comes from oruby notes and is **unverified against plain mruby**: confirm with an unmodified mruby build before submitting.

Where: `src/class.c` `vis_scope_persist`, `src/vm.c` `svar_own_scope_p` / `svar_owner_from`.

`vis_scope_persist` gives the frame's scope proc its own frame's env, so `svar_own_scope_p` is false for it and `svar_owner_from` scans from `ci - 1`, missing the frame itself.

```ruby
class X; private; "a" =~ /a/; p $~; end   # mruby: nil   MRI: #<MatchData "a">
```

Fix idea (what oruby applies): a scope proc whose env is `CI_ENV(ci)` owns the frame's slot (`svar_scope_frame` in `scripts/mruby_vm_compat.rb`). Worth turning into a clean patch against vm.c.
