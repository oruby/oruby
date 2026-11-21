#include <mruby.h>
#include <mruby/presym.h>
#include <mruby/string.h>
#include <mruby/variable.h>

/* mruby copies mutable String keys before freezing them. Preserve ORuby's
   hidden encoding tag without copying user instance variables. */
static inline mrb_value oruby_hash_key_dup(mrb_state *mrb, mrb_value source)
{
  mrb_value key = mrb_str_dup(mrb, source);
  if (RSTR_BINARY_P(mrb_str_ptr(source))) {
    mrb_sym encoding = MRB_SYM(__encoding);
    mrb_value tag = mrb_iv_get(mrb, source, encoding);
    if (!mrb_nil_p(tag)) mrb_iv_set(mrb, key, encoding, tag);
  }
  return key;
}

#define mrb_str_dup oruby_hash_key_dup
#include "../../mruby/src/hash.c"
#undef mrb_str_dup
