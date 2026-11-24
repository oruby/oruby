/* Encodings in ORuby's C API (gem/capi): an rb_encoding is a handle for an
 * encoding name, one per Encoding, made on first use. */
#ifndef ORUBY_CAPI_ENCODING_H
#define ORUBY_CAPI_ENCODING_H 1

#include "ruby/ruby.h"

#if defined(__cplusplus)
extern "C" {
#endif

typedef struct rb_capi_encoding {
  const char *name;
  int index;
  int ascii_compatible;
  int dummy;
  int max_enc_len; /* MRI's OnigEncodingType fields of the same name */
  int min_enc_len;
} rb_encoding;

rb_encoding *rb_enc_get(VALUE);
int rb_enc_get_index(VALUE);
rb_encoding *rb_enc_from_index(int);
int rb_enc_to_index(rb_encoding *);
rb_encoding *rb_enc_find(const char *);
int rb_enc_find_index(const char *);
int rb_define_dummy_encoding(const char *);
rb_encoding *rb_to_encoding(VALUE);
VALUE rb_enc_from_encoding(rb_encoding *);
rb_encoding *rb_utf8_encoding(void);
rb_encoding *rb_ascii8bit_encoding(void);
rb_encoding *rb_usascii_encoding(void);
rb_encoding *rb_locale_encoding(void);
rb_encoding *rb_filesystem_encoding(void);
rb_encoding *rb_default_external_encoding(void);
rb_encoding *rb_default_internal_encoding(void);
int rb_utf8_encindex(void);
int rb_ascii8bit_encindex(void);
int rb_usascii_encindex(void);
VALUE rb_enc_associate(VALUE, rb_encoding *);
VALUE rb_enc_associate_index(VALUE, int);
void rb_enc_copy(VALUE, VALUE);
VALUE rb_enc_str_new(const char *, long, rb_encoding *);
VALUE rb_enc_str_new_cstr(const char *, rb_encoding *);
VALUE rb_enc_str_new_static(const char *, long, rb_encoding *);
VALUE rb_enc_interned_str(const char *, long, rb_encoding *);
VALUE rb_external_str_new_with_enc(const char *, long, rb_encoding *);
VALUE rb_str_conv_enc(VALUE, rb_encoding *, rb_encoding *);
VALUE rb_str_export_to_enc(VALUE, rb_encoding *);
VALUE rb_str_encode(VALUE, VALUE, int, VALUE);
int rb_enc_str_asciionly_p(VALUE);
int rb_enc_str_coderange(VALUE);
VALUE rb_enc_sprintf(rb_encoding *, const char *, ...);
ID rb_intern3(const char *, long, rb_encoding *);
VALUE rb_enc_reg_new(const char *, long, rb_encoding *, int);
rb_encoding *rb_enc_compatible(VALUE, VALUE);
rb_encoding *rb_enc_check(VALUE, VALUE);
long rb_enc_strlen(const char *, const char *, rb_encoding *);
int rb_enc_mbclen(const char *, const char *, rb_encoding *);
int rb_enc_codelen(int, rb_encoding *);
unsigned int rb_enc_codepoint_len(const char *, const char *, int *, rb_encoding *);
int rb_enc_dummy_p(rb_encoding *);
#define rb_enc_name(enc) ((enc)->name)
#define rb_enc_asciicompat(enc) ((enc)->ascii_compatible)
#define rb_enc_str_asciicompat_p(str) rb_enc_asciicompat(rb_enc_get(str))
#define ENC_CODERANGE_UNKNOWN 0
#define ENC_CODERANGE_7BIT 0x100000
#define ENC_CODERANGE_VALID 0x200000
#define ENC_CODERANGE_BROKEN 0x300000
#define RUBY_ENC_CODERANGE_7BIT ENC_CODERANGE_7BIT
#define RUBY_ENC_CODERANGE_VALID ENC_CODERANGE_VALID
#define RUBY_ENC_CODERANGE_BROKEN ENC_CODERANGE_BROKEN
#define ENC_CODERANGE_MASK 0x300000
/* MRI caches a String's code range in its flags (UNKNOWN until scanned);
   here it is scanned when asked, and there is no cache to set or clear */
#define ENC_CODERANGE(obj) rb_enc_str_coderange((VALUE)(obj))
#define ENC_CODERANGE_ASCIIONLY(obj) (ENC_CODERANGE(obj) == ENC_CODERANGE_7BIT)
#define ENC_CODERANGE_SET(obj, cr) ((void)(obj), (void)(cr))
#define ENC_CODERANGE_CLEAR(obj) ((void)(obj))
#define RB_ENC_CODERANGE(obj) ENC_CODERANGE(obj)
#define RB_ENC_CODERANGE_ASCIIONLY(obj) ENC_CODERANGE_ASCIIONLY(obj)
#define RUBY_ENC_CODERANGE_UNKNOWN ENC_CODERANGE_UNKNOWN
#define RUBY_ENC_CODERANGE_MASK ENC_CODERANGE_MASK
#define rb_enc_mbmaxlen(enc) ((enc)->max_enc_len)
#define rb_enc_mbminlen(enc) ((enc)->min_enc_len)
#define ENCODING_GET(obj) rb_enc_get_index((VALUE)(obj))
#define ENCODING_GET_INLINED(obj) ENCODING_GET(obj)
#define RB_ENCODING_GET(obj) ENCODING_GET(obj)
#define RB_ENCODING_GET_INLINED(obj) ENCODING_GET(obj)
#define RB_ENCODING_SET(obj, i) ENCODING_SET(obj, i)
#define ENCODING_SET(obj, i) rb_enc_associate_index((VALUE)(obj), (i))

#if defined(__cplusplus)
}
#endif

#endif
