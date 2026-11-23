#include "native.h"
#include <mruby/error.h>
#include <mruby/string.h>
#include <stdio.h>

/* The regexp compiler uses this formatter in its diagnostics. */
void
mrb_re_flags_cat(mrb_state *mrb, mrb_value str, uint32_t flags)
{
  if (flags & RE_FLAG_DOTALL) mrb_str_cat_lit(mrb, str, "m");
  if (flags & RE_FLAG_IGNORECASE) mrb_str_cat_lit(mrb, str, "i");
  if (flags & RE_FLAG_EXTENDED) mrb_str_cat_lit(mrb, str, "x");
}
mrb_state *
orb_regexp_state(void)
{
  mrb_state *mrb = mrb_open_core();
  if (mrb) mrb_define_class_id(mrb, MRB_SYM(RegexpError), E_STANDARD_ERROR);
  return mrb;
}

struct orb_compile_request {
  mrb_regexp_pattern *pattern;
  const char *source;
  size_t len;
  uint32_t flags;
  mrb_bool binary;
};

static mrb_value
compile_pattern(mrb_state *mrb, void *data)
{
  struct orb_compile_request *request = data;
  request->pattern = mrb_calloc(mrb, 1, sizeof(*request->pattern));
  mrb_re_compile(mrb, request->pattern, request->source, request->len,
                 request->flags, request->binary);
  return mrb_nil_value();
}

mrb_regexp_pattern *
orb_regexp_compile(mrb_state *mrb, const char *source, size_t len, uint32_t flags,
                   mrb_bool binary, char *msg, size_t msglen)
{
  struct orb_compile_request request = {NULL, source ? source : "", len, flags, binary};
  int arena = mrb_gc_arena_save(mrb);
  mrb_bool raised;
  mrb_value error = mrb_protect_error(mrb, compile_pattern, &request, &raised);
  if (raised) {
    mrb_value message = mrb_exc_mesg_get(mrb, mrb_exc_ptr(error));
    if (mrb_string_p(message)) {
      snprintf(msg, msglen, "%.*s", (int)RSTRING_LEN(message), RSTRING_PTR(message));
    }
    mrb_re_free(mrb, request.pattern);
    request.pattern = NULL;
  }
  mrb_gc_arena_restore(mrb, arena);
  return request.pattern;
}
