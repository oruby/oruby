/* ORuby C API: ruby/io.h. An IO has no rb_io_t of its own in oruby: GetOpenFile
   gives a small one made from the IO each time (self, fd, mode, pathv). */
#ifndef ORUBY_CAPI_IO_H
#define ORUBY_CAPI_IO_H 1
#include "ruby/ruby.h"
#include "ruby/encoding.h"
#include <stdio.h>
#include <sys/types.h>
#include <sys/time.h>
#include <sys/select.h>

#ifdef __cplusplus
extern "C" {
#endif

/* what IO#wait and rb_io_wait take and give (IO::READABLE, IO::PRIORITY, IO::WRITABLE) */
enum ruby_io_event {
  RUBY_IO_READABLE = 0x01,
  RUBY_IO_PRIORITY = 0x02,
  RUBY_IO_WRITABLE = 0x04
};
#define RB_WAITFD_IN  0x001
#define RB_WAITFD_PRI 0x002
#define RB_WAITFD_OUT 0x004

#define FMODE_READABLE      0x00000001
#define FMODE_WRITABLE      0x00000002
#define FMODE_READWRITE     (FMODE_READABLE | FMODE_WRITABLE)
#define FMODE_BINMODE       0x00000004
#define FMODE_SYNC          0x00000008
#define FMODE_TTY           0x00000010
#define FMODE_DUPLEX        0x00000020
#define FMODE_APPEND        0x00000040
#define FMODE_CREATE        0x00000080
#define FMODE_EXCL          0x00000400
#define FMODE_TRUNC         0x00000800
#define FMODE_TEXTMODE      0x00001000
#define FMODE_SETENC_BY_BOM 0x00100000

/* the encoding conversion flags rb_io_encoding carries (ruby/encoding.h in MRI) */
#ifndef ECONV_UNIVERSAL_NEWLINE_DECORATOR
#define ECONV_ERROR_HANDLER_MASK 0x000000ff
#define ECONV_INVALID_MASK 0x0000000f
#define ECONV_INVALID_REPLACE 0x00000002
#define ECONV_UNDEF_MASK 0x000000f0
#define ECONV_UNDEF_REPLACE 0x00000020
#define ECONV_UNDEF_HEX_CHARREF 0x00000030
#define ECONV_UNIVERSAL_NEWLINE_DECORATOR 0x00000100
#define ECONV_CRLF_NEWLINE_DECORATOR 0x00001000
#define ECONV_CR_NEWLINE_DECORATOR 0x00002000
#endif

typedef struct rb_io {
  VALUE self;
  FILE *stdio_file;
  int fd;
  int mode;
  pid_t pid;
  int lineno;
  VALUE pathv;
  void (*finalize)(struct rb_io *, int);
  VALUE tied_io_for_writing;
  VALUE timeout;
} rb_io_t;

struct rb_io_encoding {
  rb_encoding *enc;
  rb_encoding *enc2;
  int ecflags;
  VALUE ecopts;
};

extern VALUE rb_eIOTimeoutError;

rb_io_t *rb_capi_io_ptr(VALUE io);
#define RB_IO_POINTER(obj, fp) rb_io_check_closed((fp) = rb_capi_io_ptr(rb_io_taint_check(obj)))
#define GetOpenFile(obj, fp) RB_IO_POINTER((obj), (fp))
#define GetReadFile(fptr) (fptr)->stdio_file
#define GetWriteFile(fptr) (fptr)->stdio_file

VALUE rb_io_taint_check(VALUE);
VALUE rb_io_get_io(VALUE);
VALUE rb_io_check_io(VALUE);
VALUE rb_io_addstr(VALUE, VALUE);
VALUE rb_io_printf(int, const VALUE *, VALUE);
VALUE rb_io_print(int, const VALUE *, VALUE);
VALUE rb_io_puts(int, const VALUE *, VALUE);
VALUE rb_io_write(VALUE, VALUE);
VALUE rb_io_flush(VALUE);
VALUE rb_io_close(VALUE);
VALUE rb_io_binmode(VALUE);
VALUE rb_io_ascii8bit_binmode(VALUE);
VALUE rb_io_getbyte(VALUE);
VALUE rb_io_gets(VALUE);
VALUE rb_io_eof(VALUE);
VALUE rb_io_ungetc(VALUE, VALUE);
VALUE rb_io_ungetbyte(VALUE, VALUE);
VALUE rb_io_closed_p(VALUE);
VALUE rb_io_path(VALUE);
VALUE rb_io_timeout(VALUE);
VALUE rb_io_set_timeout(VALUE, VALUE);
int rb_io_descriptor(VALUE);
int rb_io_mode(VALUE);
VALUE rb_io_open_descriptor(VALUE, int, int, VALUE, VALUE, struct rb_io_encoding *);

rb_io_t *rb_io_check_closed(rb_io_t *);
void rb_io_check_initialized(rb_io_t *);
void rb_io_check_readable(rb_io_t *);
void rb_io_check_writable(rb_io_t *);
void rb_io_check_char_readable(rb_io_t *);
void rb_io_check_byte_readable(rb_io_t *);
void rb_io_set_nonblock(rb_io_t *);

VALUE rb_io_wait(VALUE io, VALUE events, VALUE timeout);
VALUE rb_io_maybe_wait(int error, VALUE io, VALUE events, VALUE timeout);
int rb_io_maybe_wait_readable(int error, VALUE io, VALUE timeout);
int rb_io_maybe_wait_writable(int error, VALUE io, VALUE timeout);
int rb_io_wait_readable(int fd);
int rb_io_wait_writable(int fd);
int rb_wait_for_single_fd(int fd, int events, struct timeval *tv);
void rb_thread_wait_fd(int fd);
int rb_thread_fd_writable(int fd);

typedef struct {
  int maxfd;
  fd_set *fdset;
} rb_fdset_t;
void rb_fd_init(rb_fdset_t *);
void rb_fd_term(rb_fdset_t *);
void rb_fd_zero(rb_fdset_t *);
void rb_fd_set(int, rb_fdset_t *);
void rb_fd_clr(int, rb_fdset_t *);
int rb_fd_isset(int, const rb_fdset_t *);
#define rb_fd_ptr(f) ((f)->fdset)
#define rb_fd_max(f) ((f)->maxfd)
int rb_thread_fd_select(int, rb_fdset_t *, rb_fdset_t *, rb_fdset_t *, struct timeval *);

int rb_cloexec_open(const char *path, int flags, mode_t mode);
int rb_cloexec_dup(int fd);
int rb_cloexec_dup2(int oldfd, int newfd);
int rb_cloexec_pipe(int fildes[2]);
int rb_cloexec_fcntl_dupfd(int fd, int minfd);
void rb_fd_fix_cloexec(int fd);
void rb_update_max_fd(int fd);

#ifdef __cplusplus
}
#endif
#endif
