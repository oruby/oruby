#include "re_internal.h"

#define ORB_RE_TIMEOUT (-4)
mrb_state *orb_regexp_state(void);
mrb_regexp_pattern *orb_regexp_compile(mrb_state *mrb, const char *source, size_t len,
                                      uint32_t flags, mrb_bool binary, char *msg, size_t msglen);
int orb_regexp_match(mrb_state *mrb, const mrb_regexp_pattern *pattern,
                     const char *subject, size_t len, mrb_int pos, int *captures, int size,
                     mrb_bool binary, uint64_t timeout_ns);
