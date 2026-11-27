/* The IO functions of the C API (ruby/io.h), included at the end of capi.c.
   What needs Ruby asks ORubyCAPI (capi_io.rb, loaded when the API is bound).
   Waits run without the GVL (rb_nogvl), so other threads run and
   Thread#kill/#raise and signals interrupt them. */
#include "ruby/io.h"
#include <fcntl.h>
#include <poll.h>
#include <unistd.h>

VALUE rb_eIOTimeoutError;

/* called by Go once capi_io.rb is loaded */
void orb_capi_io_ready(void) { rb_eIOTimeoutError = named("IO::TimeoutError"); }

static mrb_value capi_mod(void) { return mrb_obj_value(mrb_module_get(M, "ORubyCAPI")); }

/* [fd, mode] of the open IO; IOError when it is closed or uninitialized */
static void capi_io_info(VALUE io, int *fd, int *mode) {
  mrb_value arg = MV(io);
  mrb_value r = call_id(capi_mod(), SYM("io_info"), 1, &arg);
  if (fd) *fd = (int)mrb_integer(mrb_ary_ref(M, r, 0));
  if (mode) *mode = (int)mrb_integer(mrb_ary_ref(M, r, 1));
}

VALUE rb_io_taint_check(VALUE io) { rb_check_frozen(io); return io; }

VALUE rb_io_check_io(VALUE obj) {
  if (!NIL_P(rb_cIO) && RTEST(rb_obj_is_kind_of(obj, rb_cIO))) return obj;
  if (!rb_respond_to(obj, rb_intern("to_io"))) return Qnil;
  VALUE r = rb_funcallv(obj, rb_intern("to_io"), 0, NULL);
  if (NIL_P(r)) return Qnil;
  if (!RTEST(rb_obj_is_kind_of(r, rb_cIO)))
    rb_raise(rb_eTypeError, "can't convert %s to IO (%s#to_io gives %s)",
             rb_obj_classname(obj), rb_obj_classname(obj), rb_obj_classname(r));
  return r;
}

VALUE rb_io_get_io(VALUE obj) {
  if (!NIL_P(rb_cIO) && RTEST(rb_obj_is_kind_of(obj, rb_cIO))) return obj;
  VALUE r = rb_io_check_io(obj);
  if (NIL_P(r)) rb_raise(rb_eTypeError, "no implicit conversion of %s into IO", rb_obj_classname(obj));
  return r;
}

VALUE rb_io_addstr(VALUE io, VALUE str) { call1(MV(io), "<<", MV(str)); return io; }
VALUE rb_io_printf(int argc, const VALUE *argv, VALUE out) { return rb_funcallv(out, SYM("printf"), argc, argv); }
VALUE rb_io_print(int argc, const VALUE *argv, VALUE out) { return rb_funcallv(out, SYM("print"), argc, argv); }
VALUE rb_io_close(VALUE io) { call0(MV(io), "close"); return Qnil; }
VALUE rb_io_binmode(VALUE io) { call0(MV(io), "binmode"); return io; }
VALUE rb_io_ascii8bit_binmode(VALUE io) { call0(MV(io), "binmode"); return io; }
VALUE rb_io_getbyte(VALUE io) { return V(call0(MV(io), "getbyte")); }
VALUE rb_io_gets(VALUE io) { return V(call0(MV(io), "gets")); }
VALUE rb_io_eof(VALUE io) { return V(call0(MV(io), "eof?")); }
VALUE rb_io_ungetc(VALUE io, VALUE c) { return V(call1(MV(io), "ungetc", MV(c))); }
VALUE rb_io_ungetbyte(VALUE io, VALUE b) { return V(call1(MV(io), "ungetbyte", MV(b))); }
VALUE rb_io_closed_p(VALUE io) { return V(call0(MV(io), "closed?")); }
VALUE rb_io_path(VALUE io) { return V(call0(MV(io), "path")); }
VALUE rb_io_timeout(VALUE io) { return V(call0(MV(io), "timeout")); }
VALUE rb_io_set_timeout(VALUE io, VALUE t) { call1(MV(io), "timeout=", MV(t)); return t; }

int rb_io_descriptor(VALUE io) { int fd; capi_io_info(io, &fd, NULL); return fd; }
int rb_io_mode(VALUE io) { int mode; capi_io_info(io, NULL, &mode); return mode; }

/* the rb_io_t of an IO, made on the first use and kept in an ivar */
rb_io_t *rb_capi_io_ptr(VALUE io) {
  Check_Type(io, T_FILE);
  ID iv = rb_intern("@__capi_fptr");
  VALUE box = rb_ivar_get(io, iv);
  rb_io_t *fp;
  if (NIL_P(box)) {
    fp = (rb_io_t *)calloc(1, sizeof(rb_io_t));
    if (!fp) rb_memerror();
    fp->self = io;
    fp->fd = -1;
    box = rb_data_object_wrap(rb_cObject, fp, NULL, free);
    rb_ivar_set(io, iv, box);
  } else {
    fp = (rb_io_t *)(*rb_capi_data_ptr_ref(box));
  }
  return fp;
}

/* the IO's state now: fd and mode, IOError when it is closed */
static void io_refresh(rb_io_t *fp) {
  if (!fp) rb_raise(rb_eIOError, "uninitialized stream");
  capi_io_info(fp->self, &fp->fd, &fp->mode);
}

rb_io_t *rb_io_check_closed(rb_io_t *fp) { io_refresh(fp); return fp; }
void rb_io_check_initialized(rb_io_t *fp) { if (!fp) rb_raise(rb_eIOError, "uninitialized stream"); }
void rb_io_check_readable(rb_io_t *fp) {
  io_refresh(fp);
  if (!(fp->mode & FMODE_READABLE)) rb_raise(rb_eIOError, "not opened for reading");
}
void rb_io_check_char_readable(rb_io_t *fp) { rb_io_check_readable(fp); }
void rb_io_check_byte_readable(rb_io_t *fp) { rb_io_check_readable(fp); }
void rb_io_check_writable(rb_io_t *fp) {
  io_refresh(fp);
  if (!(fp->mode & FMODE_WRITABLE)) rb_raise(rb_eIOError, "not opened for writing");
}

void rb_io_set_nonblock(rb_io_t *fp) {
  int fl = fcntl(fp->fd, F_GETFL);
  if (fl == -1) rb_sys_fail(0);
  if (!(fl & O_NONBLOCK) && fcntl(fp->fd, F_SETFL, fl | O_NONBLOCK) == -1) rb_sys_fail(0);
}

/* -- waiting -- */

VALUE rb_io_wait(VALUE io, VALUE events, VALUE timeout) {
  capi_io_info(io, NULL, NULL);
  mrb_value args[3] = { MV(io), MV(events), MV(timeout) };
  return V(call_id(capi_mod(), SYM("io_wait"), 3, args));
}

/* EINTR gives the events back, EAGAIN waits for them (false: the time
   passed), any other error is nil */
VALUE rb_io_maybe_wait(int error, VALUE io, VALUE events, VALUE timeout) {
  capi_io_info(io, NULL, NULL);
  if (error == EINTR) return events;
  if (error == EAGAIN || error == EWOULDBLOCK) return rb_io_wait(io, events, timeout);
  return Qnil;
}

static int io_maybe_wait(int error, VALUE io, int ev, VALUE timeout, const char *what) {
  VALUE r = rb_io_maybe_wait(error, io, INT2FIX(ev), timeout);
  if (RTEST(r)) return NUM2INT(r);
  if (r == Qfalse) rb_raise(rb_eIOTimeoutError, "Timed out waiting for IO to become %s!", what);
  return 0;
}
int rb_io_maybe_wait_readable(int error, VALUE io, VALUE timeout) {
  return io_maybe_wait(error, io, RUBY_IO_READABLE, timeout, "readable");
}
int rb_io_maybe_wait_writable(int error, VALUE io, VALUE timeout) {
  return io_maybe_wait(error, io, RUBY_IO_WRITABLE, timeout, "writable");
}

/* poll(2) without the GVL; EINTR (a kill, a signal) is taken before it goes on */
struct io_poll { struct pollfd *fds; nfds_t n; int ms; int err; };
static void *io_poll_fn(void *p) {
  struct io_poll *a = (struct io_poll *)p;
  int r = poll(a->fds, a->n, a->ms);
  a->err = errno;
  return (void *)(intptr_t)r;
}
static int io_poll(struct pollfd *fds, nfds_t n, int ms) {
  for (;;) {
    struct io_poll a = { fds, n, ms, 0 };
    int r = (int)(intptr_t)rb_nogvl(io_poll_fn, &a, RUBY_UBF_IO, NULL, 0);
    if (r >= 0 || a.err != EINTR) { errno = a.err; return r; }
    rb_thread_check_ints();
  }
}

static int fd_events_to_poll(int ev) {
  int p = 0;
  if (ev & RB_WAITFD_IN) p |= POLLIN;
  if (ev & RB_WAITFD_PRI) p |= POLLPRI;
  if (ev & RB_WAITFD_OUT) p |= POLLOUT;
  return p;
}
static int poll_to_fd_events(int p) {
  int ev = 0;
  if (p & (POLLIN | POLLHUP | POLLERR)) ev |= RB_WAITFD_IN;
  if (p & POLLPRI) ev |= RB_WAITFD_PRI;
  if (p & (POLLOUT | POLLERR)) ev |= RB_WAITFD_OUT;
  return ev;
}

int rb_wait_for_single_fd(int fd, int events, struct timeval *tv) {
  struct pollfd p = { fd, (short)fd_events_to_poll(events), 0 };
  int ms = tv ? (int)(tv->tv_sec * 1000 + tv->tv_usec / 1000) : -1;
  int r = io_poll(&p, 1, ms);
  if (r < 0) return -1;
  if (r == 0) return 0;
  return poll_to_fd_events(p.revents) & (events | RB_WAITFD_IN * ((p.revents & POLLHUP) != 0));
}

void rb_thread_wait_fd(int fd) { rb_wait_for_single_fd(fd, RB_WAITFD_IN, NULL); }
int rb_thread_fd_writable(int fd) { rb_wait_for_single_fd(fd, RB_WAITFD_OUT, NULL); return 1; }

/* the old API: errno says what happened */
int rb_io_wait_readable(int fd) {
  switch (errno) {
    case EINTR: rb_thread_check_ints(); return 1;
    case EAGAIN: rb_thread_wait_fd(fd); return 1;
    default: return 0;
  }
}
int rb_io_wait_writable(int fd) {
  switch (errno) {
    case EINTR: rb_thread_check_ints(); return 1;
    case EAGAIN: rb_thread_fd_writable(fd); return 1;
    default: return 0;
  }
}

/* -- fd sets -- */

static size_t fdset_bytes(int maxfd) {
  size_t n = (size_t)(maxfd > FD_SETSIZE ? maxfd : FD_SETSIZE);
  size_t bits = sizeof(fd_mask) * 8;
  return ((n + bits - 1) / bits) * sizeof(fd_mask);
}
void rb_fd_init(rb_fdset_t *f) {
  f->maxfd = 0;
  f->fdset = (fd_set *)calloc(1, fdset_bytes(0));
  if (!f->fdset) rb_memerror();
}
void rb_fd_term(rb_fdset_t *f) { free(f->fdset); f->fdset = NULL; f->maxfd = 0; }
void rb_fd_zero(rb_fdset_t *f) { if (f->fdset) memset(f->fdset, 0, fdset_bytes(f->maxfd)); }
static void fd_grow(rb_fdset_t *f, int fd) {
  if (fd < f->maxfd) return;
  size_t old = fdset_bytes(f->maxfd), now = fdset_bytes(fd + 1);
  if (now > old) {
    fd_set *p = (fd_set *)realloc(f->fdset, now);
    if (!p) rb_memerror();
    memset((char *)p + old, 0, now - old);
    f->fdset = p;
  }
  f->maxfd = fd + 1;
}
void rb_fd_set(int fd, rb_fdset_t *f) { fd_grow(f, fd); FD_SET(fd, f->fdset); }
void rb_fd_clr(int fd, rb_fdset_t *f) { if (fd < f->maxfd) FD_CLR(fd, f->fdset); }
int rb_fd_isset(int fd, const rb_fdset_t *f) { return fd < f->maxfd && FD_ISSET(fd, f->fdset); }

struct io_select { int max; fd_set *r, *w, *e; struct timeval *tv; int err; };
static void *io_select_fn(void *p) {
  struct io_select *a = (struct io_select *)p;
  int r = select(a->max, a->r, a->w, a->e, a->tv);
  a->err = errno;
  return (void *)(intptr_t)r;
}

/* select(2) without the GVL on copies of the sets, the ready ones copied back */
int rb_thread_fd_select(int max, rb_fdset_t *r, rb_fdset_t *w, rb_fdset_t *e, struct timeval *tv) {
  rb_fdset_t *sets[3] = { r, w, e };
  fd_set *copy[3] = { NULL, NULL, NULL };
  size_t bytes = fdset_bytes(max);
  for (int i = 0; i < 3; i++) {
    if (!sets[i] || !sets[i]->fdset) continue;
    copy[i] = (fd_set *)calloc(1, bytes);
    if (!copy[i]) rb_memerror();
    size_t have = fdset_bytes(sets[i]->maxfd);
    memcpy(copy[i], sets[i]->fdset, have < bytes ? have : bytes);
  }
  int res;
  struct timeval left, *ltv = NULL;
  if (tv) { left = *tv; ltv = &left; }
  for (;;) {
    struct io_select a = { max, copy[0], copy[1], copy[2], ltv, 0 };
    res = (int)(intptr_t)rb_nogvl(io_select_fn, &a, RUBY_UBF_IO, NULL, 0);
    if (res >= 0 || a.err != EINTR) { errno = a.err; break; }
    rb_thread_check_ints();
    for (int i = 0; i < 3; i++) { /* select cleared what was not ready: start again from the caller's sets */
      if (!copy[i]) continue;
      memset(copy[i], 0, bytes);
      size_t have = fdset_bytes(sets[i]->maxfd);
      memcpy(copy[i], sets[i]->fdset, have < bytes ? have : bytes);
    }
  }
  if (res >= 0) {
    for (int i = 0; i < 3; i++) {
      if (!copy[i]) continue;
      size_t have = fdset_bytes(sets[i]->maxfd);
      memcpy(sets[i]->fdset, copy[i], have < bytes ? have : bytes);
    }
  }
  for (int i = 0; i < 3; i++) free(copy[i]);
  if (res < 0) rb_sys_fail(0);
  return res;
}

/* -- descriptors -- */

int rb_cloexec_open(const char *path, int flags, mode_t mode) {
  int fd = open(path, flags | O_CLOEXEC, mode);
  if (fd > 2) rb_update_max_fd(fd);
  return fd;
}
int rb_cloexec_dup(int fd) { return rb_cloexec_fcntl_dupfd(fd, 3); }
int rb_cloexec_dup2(int oldfd, int newfd) {
  int r = dup2(oldfd, newfd);
  if (r >= 0) rb_fd_fix_cloexec(r);
  return r;
}
int rb_cloexec_pipe(int fds[2]) {
  int r = pipe(fds);
  if (r == 0) { rb_fd_fix_cloexec(fds[0]); rb_fd_fix_cloexec(fds[1]); }
  return r;
}
int rb_cloexec_fcntl_dupfd(int fd, int minfd) {
  int r = fcntl(fd, F_DUPFD_CLOEXEC, minfd);
  if (r >= 0) rb_update_max_fd(r);
  return r;
}
/* the standard streams are inherited, any other descriptor is not */
void rb_fd_fix_cloexec(int fd) {
  int fl = fcntl(fd, F_GETFD);
  if (fl == -1) return;
  int want = fd > 2 ? (fl | FD_CLOEXEC) : (fl & ~FD_CLOEXEC);
  if (want != fl) fcntl(fd, F_SETFD, want);
}
void rb_update_max_fd(int fd) { (void)fd; }

VALUE rb_io_open_descriptor(VALUE klass, int fd, int mode, VALUE path, VALUE timeout, struct rb_io_encoding *enc) {
  mrb_value args[7] = {
    MV(klass), mrb_int_value(M, fd), mrb_int_value(M, mode), MV(path), MV(timeout),
    mrb_nil_value(), mrb_nil_value()
  };
  if (enc) {
    if (enc->enc2) args[5] = MV(rb_enc_from_encoding(enc->enc2)); /* external */
    if (enc->enc) args[6] = MV(rb_enc_from_encoding(enc->enc));   /* internal */
  }
  if (!NIL_P(path)) args[3] = MV(rb_str_dup(rb_string_value(&path)));
  return V(call_id(capi_mod(), SYM("io_open_descriptor"), 7, args));
}
