#ifndef ORUBY_VM_COMPAT_H
#define ORUBY_VM_COMPAT_H

/* A runtime flag for writable IREPs. Aliases and Proc copies share it. */
#define ORUBY_IREP_RUBY2_KEYWORDS 8
#define ORUBY_IREP_EVAL 16
#define ORUBY_IREP_RETAINED_SOURCE 32

/* A VM argument accumulator made by a splat, including bare super's rest. */
#define ORUBY_ARRAY_SPLAT (1u << 13)

/* Unused high bits of mrb_callinfo.vis. Only method_missing frames use these. */
#define ORUBY_MISSING_PRIVATE (1u << 5)
#define ORUBY_MISSING_PROTECTED (1u << 6)
#define ORUBY_MISSING_VISIBILITY (ORUBY_MISSING_PRIVATE | ORUBY_MISSING_PROTECTED)

/* A method-table entry that changes the visibility of an inherited method. */
#define ORUBY_METHOD_VISIBILITY_ONLY (1u << 27)
void oruby_set_inherited_visibility(mrb_state *, struct RClass *, mrb_sym, uint32_t);
struct RClass *oruby_visibility_owner(mrb_state *, struct RClass *, struct RClass *, mrb_sym);

#endif
