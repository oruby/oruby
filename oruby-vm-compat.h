#ifndef ORUBY_VM_COMPAT_H
#define ORUBY_VM_COMPAT_H

/* A runtime flag for writable IREPs. Aliases and Proc copies share it. */
#define ORUBY_IREP_RUBY2_KEYWORDS 8
#define ORUBY_IREP_EVAL 16
#define ORUBY_IREP_RETAINED_SOURCE 32
#define ORUBY_IREP_BLOCK_WARNED 64

/* A VM argument accumulator made by a splat, including bare super's rest. */
#define ORUBY_ARRAY_SPLAT (1u << 13)
/* Bit 19 is the generic ivar flag even on strings. */
#define ORUBY_STRING_CHILLED (1u << 18)
#define ORUBY_STRING_TEMP_LOCK (1u << 17)
#define ORUBY_CHILLED_DEBUG_TAG 0xce

/* A spare class flag on the state's Kernel module. */
#define ORUBY_KERNEL_CHILLED_WARNING (1u << 15)
#define ORUBY_KERNEL_UNUSED_BLOCK_WARNING (1u << 12)
void oruby_chilled_warning_refresh(mrb_state *, mrb_value);
void oruby_chilled_warn(mrb_state *, struct RBasic *);

static inline void
oruby_chilled_mutation(mrb_state *mrb, struct RBasic *obj)
{
  obj->flags &= ~ORUBY_STRING_CHILLED;
  struct RBasic *kernel = (struct RBasic*)mrb->kernel_module;
  if (mrb_unlikely(kernel && (kernel->flags & ORUBY_KERNEL_CHILLED_WARNING))) {
    oruby_chilled_warn(mrb, obj);
  }
}

/* Unused high bits of mrb_callinfo.vis. Only method_missing frames use these. */
#define ORUBY_MISSING_PRIVATE (1u << 5)
#define ORUBY_MISSING_PROTECTED (1u << 6)
#define ORUBY_MISSING_VISIBILITY (ORUBY_MISSING_PRIVATE | ORUBY_MISSING_PROTECTED)

/* A method-table entry that changes the visibility of an inherited method. */
#define ORUBY_METHOD_VISIBILITY_ONLY (1u << 27)
void oruby_set_inherited_visibility(mrb_state *, struct RClass *, mrb_sym, uint32_t);
struct RClass *oruby_visibility_owner(mrb_state *, struct RClass *, struct RClass *, mrb_sym);
mrb_value mrb_mod_eqq(mrb_state *, mrb_value);

#endif
