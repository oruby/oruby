// $0 = title and Process.setproctitle change what ps shows, as MRI's
// setproctitle does on Linux and macOS: the process's argument strings are
// overwritten in place. Their area is found by a constructor, which the
// C runtime calls with the original argc and argv before Go starts. Only
// the argv strings are used (Go has copied the environment by then; its
// os.Args point at them, read once at startup), so a title is cut to their
// length.
#include <stddef.h>
#include <string.h>
#if defined(__linux__)
#include <sys/prctl.h>
#endif

static char *orb_argv_start;
static size_t orb_argv_len; /* the bytes up to the last argument's NUL */

#if defined(__APPLE__) || defined(__linux__)
__attribute__((constructor)) static void orb_save_argv(int argc, char **argv, char **envp) {
  (void)envp;
  if (argc <= 0 || !argv || !argv[0]) return;
  char *end = argv[0] + strlen(argv[0]);
  for (int i = 1; i < argc; i++) {
    if (!argv[i] || argv[i] != end + 1) break; /* contiguous strings only */
    end = argv[i] + strlen(argv[i]);
  }
  orb_argv_start = argv[0];
  orb_argv_len = (size_t)(end - argv[0]);
}
#endif

void _orb_setproctitle(const char *s, size_t n) {
  if (!orb_argv_start || orb_argv_len == 0) return;
  if (n > orb_argv_len) n = orb_argv_len;
  memcpy(orb_argv_start, s, n);
  memset(orb_argv_start + n, 0, orb_argv_len - n + 1);
#if defined(__linux__)
  char comm[16];
  size_t m = n < 15 ? n : 15;
  memcpy(comm, s, m);
  comm[m] = 0;
  prctl(PR_SET_NAME, comm, 0, 0, 0);
#endif
}
