/* Go's side of the C API layer (capi.go) */
#ifndef ORUBY_CAPI_INTERNAL_H
#define ORUBY_CAPI_INTERNAL_H 1
#include <mruby.h>

int orb_capi_bind(mrb_state *mrb);
int orb_capi_bind_addr(uintptr_t mrb);
mrb_state *orb_capi_state(void);
uintptr_t orb_capi_value(mrb_value v);
mrb_value orb_capi_mrb(uintptr_t v);

#endif
