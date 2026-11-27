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

VALUE rb_enc_str_buf_cat(VALUE, const char *, long, rb_encoding *);
VALUE rb_str_conv_enc_opts(VALUE, rb_encoding *, rb_encoding *, int, VALUE);
VALUE rb_enc_interned_str_cstr(const char *, rb_encoding *);

/* -- more of MRI's encoding API: Onigmo's names over rb_encoding handles -- */
typedef unsigned char OnigUChar;
#define ONIGENC_CODE_TO_MBC_MAXLEN 7
#define ONIGENC_GET_CASE_FOLD_CODES_MAX_NUM 13
#define ONIGENC_CASE_FOLD (1 << 19)

/* what rb_enc_precise_mbclen returns: the length of a whole character, -1
   for an invalid one, -1 - n when n more bytes are needed */
#define ONIGENC_CONSTRUCT_MBCLEN_CHARFOUND(n) (n)
#define ONIGENC_CONSTRUCT_MBCLEN_INVALID() (-1)
#define ONIGENC_CONSTRUCT_MBCLEN_NEEDMORE(n) (-1 - (n))
#define ONIGENC_MBCLEN_CHARFOUND_P(r) (0 < (r))
#define ONIGENC_MBCLEN_CHARFOUND_LEN(r) (r)
#define ONIGENC_MBCLEN_INVALID_P(r) ((r) == -1)
#define ONIGENC_MBCLEN_NEEDMORE_P(r) ((r) < -1)
#define ONIGENC_MBCLEN_NEEDMORE_LEN(r) (-1 - (r))
#define MBCLEN_CHARFOUND_P(r) ONIGENC_MBCLEN_CHARFOUND_P(r)
#define MBCLEN_CHARFOUND_LEN(r) ONIGENC_MBCLEN_CHARFOUND_LEN(r)
#define MBCLEN_INVALID_P(r) ONIGENC_MBCLEN_INVALID_P(r)
#define MBCLEN_NEEDMORE_P(r) ONIGENC_MBCLEN_NEEDMORE_P(r)
#define MBCLEN_NEEDMORE_LEN(r) ONIGENC_MBCLEN_NEEDMORE_LEN(r)
#define MBCLEN_CHARFOUND(n) ONIGENC_CONSTRUCT_MBCLEN_CHARFOUND(n)
#define MBCLEN_INVALID() ONIGENC_CONSTRUCT_MBCLEN_INVALID()
#define MBCLEN_NEEDMORE(n) ONIGENC_CONSTRUCT_MBCLEN_NEEDMORE(n)

enum {
  ONIGENC_CTYPE_NEWLINE = 0, ONIGENC_CTYPE_ALPHA = 1, ONIGENC_CTYPE_BLANK = 2, ONIGENC_CTYPE_CNTRL = 3,
  ONIGENC_CTYPE_DIGIT = 4, ONIGENC_CTYPE_GRAPH = 5, ONIGENC_CTYPE_LOWER = 6, ONIGENC_CTYPE_PRINT = 7,
  ONIGENC_CTYPE_PUNCT = 8, ONIGENC_CTYPE_SPACE = 9, ONIGENC_CTYPE_UPPER = 10, ONIGENC_CTYPE_XDIGIT = 11,
  ONIGENC_CTYPE_WORD = 12, ONIGENC_CTYPE_ALNUM = 13, ONIGENC_CTYPE_ASCII = 14
};

int rb_locale_encindex(void);
int rb_filesystem_encindex(void);
int rb_to_encoding_index(VALUE);
VALUE rb_obj_encoding(VALUE);
void rb_enc_set_index(VALUE, int);
int rb_enc_alias(const char *, const char *);
char *rb_enc_nth(const char *, const char *, long, rb_encoding *);
char *rb_enc_left_char_head(const char *, const char *, const char *, rb_encoding *);
#define rb_enc_prev_char(s, p, e, enc) ((char *)(((p) > (s)) ? rb_enc_left_char_head((s), (p) - 1, (e), (enc)) : NULL))
int rb_enc_precise_mbclen(const char *, const char *, rb_encoding *);
int rb_enc_ascget(const char *, const char *, int *, rb_encoding *);
unsigned int rb_capi_enc_mbc_to_codepoint(const char *, const char *, rb_encoding *);
int rb_capi_enc_mbcput(unsigned int, char *, rb_encoding *);
int rb_capi_enc_mbc_case_fold(rb_encoding *, int, const OnigUChar **, const OnigUChar *, OnigUChar *);
int rb_capi_enc_unicode_p(rb_encoding *);
#define rb_enc_mbc_to_codepoint(p, e, enc) rb_capi_enc_mbc_to_codepoint((const char *)(p), (const char *)(e), (enc))
#define rb_enc_mbcput(c, buf, enc) rb_capi_enc_mbcput((unsigned int)(c), (char *)(buf), (enc))
#define ONIGENC_MBC_CASE_FOLD(enc, flag, pp, end, buf) \
  rb_capi_enc_mbc_case_fold((enc), (flag), (const OnigUChar **)(pp), (end), (buf))
#define ONIGENC_IS_UNICODE(enc) rb_capi_enc_unicode_p(enc)

int rb_enc_isctype(int, int, rb_encoding *);
#define rb_enc_isalnum(c, enc) rb_enc_isctype((c), ONIGENC_CTYPE_ALNUM, (enc))
#define rb_enc_isalpha(c, enc) rb_enc_isctype((c), ONIGENC_CTYPE_ALPHA, (enc))
#define rb_enc_isblank(c, enc) rb_enc_isctype((c), ONIGENC_CTYPE_BLANK, (enc))
#define rb_enc_iscntrl(c, enc) rb_enc_isctype((c), ONIGENC_CTYPE_CNTRL, (enc))
#define rb_enc_isdigit(c, enc) rb_enc_isctype((c), ONIGENC_CTYPE_DIGIT, (enc))
#define rb_enc_isgraph(c, enc) rb_enc_isctype((c), ONIGENC_CTYPE_GRAPH, (enc))
#define rb_enc_islower(c, enc) rb_enc_isctype((c), ONIGENC_CTYPE_LOWER, (enc))
#define rb_enc_isprint(c, enc) rb_enc_isctype((c), ONIGENC_CTYPE_PRINT, (enc))
#define rb_enc_ispunct(c, enc) rb_enc_isctype((c), ONIGENC_CTYPE_PUNCT, (enc))
#define rb_enc_isspace(c, enc) rb_enc_isctype((c), ONIGENC_CTYPE_SPACE, (enc))
#define rb_enc_isupper(c, enc) rb_enc_isctype((c), ONIGENC_CTYPE_UPPER, (enc))
#define rb_enc_isxdigit(c, enc) rb_enc_isctype((c), ONIGENC_CTYPE_XDIGIT, (enc))
#define rb_enc_isascii(c, enc) ((unsigned)(c) < 128)
int rb_enc_toupper(int, rb_encoding *);
int rb_enc_tolower(int, rb_encoding *);

NORETURN(void rb_enc_raise(rb_encoding *, VALUE, const char *, ...));
int rb_uv_to_utf8(char[6], unsigned long);
#define RB_ENC_CODERANGE_CLEAR(obj) ENC_CODERANGE_CLEAR(obj)
#define RB_ENC_CODERANGE_SET(obj, cr) ENC_CODERANGE_SET((obj), (cr))

#if defined(__cplusplus)
}
#endif

#endif
