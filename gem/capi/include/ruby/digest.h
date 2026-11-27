/* ORuby C API: MRI's ruby/digest.h, what a digest plugin includes */
#ifndef ORUBY_CAPI_DIGEST_H
#define ORUBY_CAPI_DIGEST_H 1
#include "ruby/ruby.h"

#ifdef __cplusplus
extern "C" {
#endif

#define RUBY_DIGEST_API_VERSION 3

typedef int (*rb_digest_hash_init_func_t)(void *);
typedef void (*rb_digest_hash_update_func_t)(void *, unsigned char *, size_t);
typedef int (*rb_digest_hash_finish_func_t)(void *, unsigned char *);

typedef struct {
  int api_version;
  size_t digest_len;
  size_t block_len;
  size_t ctx_size;
  rb_digest_hash_init_func_t init_func;
  rb_digest_hash_update_func_t update_func;
  rb_digest_hash_finish_func_t finish_func;
} rb_digest_metadata_t;

VALUE rb_digest_make_metadata(const rb_digest_metadata_t *);
VALUE rb_digest_namespace(void);

#ifdef __cplusplus
}
#endif

#endif
