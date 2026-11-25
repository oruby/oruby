# mruby: `mrb_vm_run` checks the C stack outside `mrb_vm_exec`'s exception frame

Status: **not filed** (kept here until it can be a small, well-explained patch).
Found 2026-11-25. Applies to mruby `master` with `MRB_USE_C_STACK_CHECK`.

## Where

`src/vm.c`, `mrb_vm_run`:

```c
  if (!c->stbase) { stack_init(mrb); }
  c_stack_check(mrb);                 /* <- raises SystemStackError here */
  ...
  mrb_value result = mrb_vm_exec(mrb, proc, irep->iseq);   /* MRB_TRY lives in here */
```

`c_stack_check` raises `mrb->stack_err` through `mrb_exc_raise`. In
`mrb_vm_run` that happens after the caller has already done
`cipush(CINFO_DIRECT)` but before `mrb_vm_exec` has installed its own
`MRB_TRY`. The throw therefore goes to whatever `mrb->jmp` was before, not
to the Ruby frames' `rescue`/`ensure` handlers.

The other two call sites (`mrb_funcall_with_block`, `yield_with_attr`) check
before their `cipush`, and sit inside the caller's own protected region, so
they do not have this problem.

## Symptom (as seen in oruby)

With a machine-stack limit set (`mrb_c_stack_limit_set`), deep recursion that
re-enters the VM from C and overflows at a `mrb_vm_run` call:

- `rescue SystemStackError` / `ensure` blocks in the Ruby frames being
  unwound are skipped;
- in a `Thread`, the error ends the thread silently (no exception reaches
  `Thread#join`/`#value`).

## Minimal repro (to be confirmed against plain mruby before filing)

Needs a host that calls `mrb_c_stack_limit_set` with a small stack (e.g. a
thread with a 64 KB stack), then:

```ruby
def deep(n) = [1].each { deep(n + 1) }   # re-enters the VM through C each level
begin
  deep(0)
rescue SystemStackError
  puts "rescued"            # expected; not reached when overflow hits mrb_vm_run
ensure
  puts "ensure"             # expected; skipped
end
```

This repro has not been run in this form. Before filing, reproduce with a
small C driver against unmodified mruby (no oruby code).

## Possible fix

Do the check inside the protected region, or before the caller's `cipush`:
move `c_stack_check(mrb)` to the start of `mrb_vm_exec`'s body (after its
`MRB_TRY`), or have `mrb_vm_run` pop the frame it was handed before raising.
The first is a one-line move; check that `mrb_vm_exec` callers other than
`mrb_vm_run` do not double-check.

## oruby's workaround

`scripts/mruby_vm_compat.rb` (build-time patch of vm.c, mruby/ itself is
untouched) replaces the check with a per-thread machine-stack check and
drops the call in `mrb_vm_run`. Committed in `503eb42`.
