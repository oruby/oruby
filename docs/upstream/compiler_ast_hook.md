# mruby-compiler: AST inspection hook in `mrc_load_string_cxt`

Status: **not filed**. Recorded 2026-11-26.

Embedders that inspect the Prism AST before code generation (such as ORuby's
source rewriter in `go-prism.c` / `prism.go`) currently have to parse every
source buffer twice:

1. Once via `pm_parse()` to scan the AST for unsupported constructs.
2. A second time inside `mrc_load_string_cxt()`
   (`mrbgems/mruby-compiler/src/compile.c`), because `mrc_parse_string_cxt()`,
   `mrc_load_exec()`, and `mrc_prism_release_tree()` are all `static`, and
   `mrc_generate_code()` depends on `mrc_ccontext` state initialized by
   `mrc_pm_parser_init()`.

## Proposed minimal API (1 exported function)

In `mrbgems/mruby-compiler/include/mrc_compile.h` and `src/compile.c`:

```c
typedef mrc_bool (*mrc_ast_hook_f)(mrc_ccontext *c, mrc_node *root, void *user_data);

MRC_API mrc_irep *
mrc_load_string_cxt_hook(mrc_ccontext *c, const uint8_t **source, size_t length,
                         mrc_ast_hook_f hook, void *user_data)
{
  mrc_node *root = mrc_parse_string_cxt(c, source, length);
  if (hook && !hook(c, root, user_data)) {
    mrc_prism_release_tree(c, root);
    return NULL;
  }
  mrc_irep *irep = mrc_load_exec(c, root);
  mrc_prism_release_tree(c, root);
  return irep;
}

MRC_API mrc_irep *
mrc_load_string_cxt(mrc_ccontext *c, const uint8_t **source, size_t length)
{
  return mrc_load_string_cxt_hook(c, source, length, NULL, NULL);
}
```

`compile.c` retains full ownership of parse setup, diagnostics, codegen, and
tree cleanup.

## Intended use in ORuby

- Inside the hook, `go-prism.c` walks `root` (and reads `c->p` for encoding,
  warnings, and `frozen_string_literal`).
- If **0 rewrites** are needed, the hook returns `TRUE` and `mrc_load_exec()`
  compiles `root` directly into `mrc_irep*` (**1 Prism parse**).
- If **>= 1 rewrite** is needed, the hook records the rewrite spans in
  `user_data` and returns `FALSE`, `mrc_load_string_cxt_hook` frees `root` and
  returns `NULL`, and ORuby rewrites the source and compiles it.

Not patched locally in `mruby/`. Current ORuby mitigations are `.orb`
precompilation (`core_ext.orb`, `precompiled.orb`, `rubygems.orb`,
`stdlib.orb`) and the opt-in disk bytecode cache (`ORUBY_CACHE_DIR`).
