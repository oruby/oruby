#include "native.h"
#include <time.h>
#ifdef _WIN32
#include <windows.h>
#endif

static _Thread_local uint64_t deadline;
static _Thread_local unsigned polls;

static uint64_t
orb_regexp_now(void)
{
#ifdef _WIN32
  LARGE_INTEGER count, frequency;
  QueryPerformanceCounter(&count);
  QueryPerformanceFrequency(&frequency);
  return (uint64_t)((double)count.QuadPart * 1e9 / frequency.QuadPart);
#else
  struct timespec ts;
  clock_gettime(CLOCK_MONOTONIC, &ts);
  return (uint64_t)ts.tv_sec * 1000000000u + (uint64_t)ts.tv_nsec;
#endif
}

static inline mrb_bool
orb_regexp_poll(void)
{
  return deadline && (++polls & 255u) == 0 && orb_regexp_now() >= deadline;
}

#include "../../../mruby/build/host/oruby-core/regexp/re_exec.c"

int
orb_regexp_match(mrb_state *mrb, const mrb_regexp_pattern *pattern,
                 const char *subject, size_t len, mrb_int pos, int *captures, int size,
                 mrb_bool binary, uint64_t timeout_ns)
{
  memset(captures, -1, sizeof(int) * size);
  deadline = timeout_ns ? orb_regexp_now() + timeout_ns : 0;
  polls = 0;
  int result = mrb_re_exec(mrb, pattern, subject ? subject : "", len, pos, captures, size, binary);
  if (deadline && orb_regexp_now() >= deadline) result = ORB_RE_TIMEOUT;
  deadline = 0;
  return result;
}
