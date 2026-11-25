Status: **not filed**. Found 2026-11-25. Written up in `tmp/regexp_patch`; the patch (`regex_empty_iteration.diff`) is **not yet re-run** after the move: apply it to a clean mruby-regexp and re-run `language/regexp/empty_checks_spec.rb` before sending it upstream.

### mruby-regexp: a loop stops on an empty iteration even when that iteration changed a capture

**Summary**

`(...)*` stops as soon as one iteration ends where it began. Onigmo (CRuby) only treats an iteration as empty if no capture state changed during it (`NULL_CHECK_MEMST`). When an empty iteration changes a capture's state (e.g. unmatched → empty, or non-empty → empty), CRuby keeps looping. `mruby-regexp` stops, so patterns whose later iterations read that capture through a backreference match less than in CRuby.

**Minimal example**

```ruby
/(a|\2b|())*/.match("aaabbb").to_a
# CRuby:        ["aaabbb", "", ""]
# mruby-regexp: ["aaa", "", ""]

/(a|\2b|()|)*/.match("aaabbb").to_a
# CRuby:        ["aaabbb", "", ""]
# mruby-regexp: ["aaa", "", ""]

/(a|\2b|(c?))*/.match("acbbb").to_a
# CRuby:        ["acbbb", "", ""]
# mruby-regexp: ["ac", "", ""]
```

Why CRuby gets `"aaabbb"`:
1. Three iterations match `a`.
2. A fourth iteration takes `()`, consuming nothing but changing group 2 from unmatched to empty `""`.
3. Because capture state changed, the loop continues. `\2` now matches `""`, so `\2b` matches the `b`s.
4. Once an iteration ends at the same position with unchanged capture states, the loop stops.

These come from ruby/spec `language/regexp/empty_checks_spec.rb`.

**Where**

`mrbgems/mruby-regexp/src/re_exec.c`:
- `ITER_EMPTY(m, key, sp)` / `bt_iter_begin()` in `backtrack_exec()` / `bt_match()`.
- `loop_head_seen()` / `re_loop_back()` in `pike_vm()` (for the non-backref greedy edge case `/^(()|()|a)*$/` on `"aaa"`).

**Proposed fix**

See `regex_empty_iteration.diff` in this directory: record a 3-state capture signature (`0 = unmatched`, `1 = non-empty`, `2 = empty`) per loop entry (`entered_sig`) via `bt_log()` and require `(m)->entered_sig[key] == bt_caps_sig(m)` in `ITER_EMPTY`.

Hashing the 3-valued state rather than raw `(start, end)` offsets avoids oscillation when a lookahead writes different non-empty spans into the same group at a fixed position (e.g. `/(?:(?=(?<a>a))|(?=.\g<a>))*b/` on `"aa"`), while still passing all 86/86 assertions of `language/regexp/empty_checks_spec.rb`, `e+`/`e+?` variants, and `(a|\2b|(c?))*`.

**Note for ORuby**

Once upstream `mruby` takes this fix, ORuby's `gem/regexp` (`engine.go`) and `gem/regexp/full` (`full.go`) will also need to route loops whose body can match empty and write captures to `compileNative` instead of RE2 / `dlclark/regexp2`.
