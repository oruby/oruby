/* More of the encoding API (ruby/encoding.h), included at the end of capi.c.
   An rb_encoding is a handle, not Onigmo's table of functions: what MRI does
   through it (character lengths, code points, ctypes, case folding) is done
   here, in C for UTF-8 and by asking Ruby for the other encodings. */

int rb_locale_encindex(void) { return rb_locale_encoding()->index; }
int rb_filesystem_encindex(void) { return rb_filesystem_encoding()->index; }

int rb_to_encoding_index(VALUE v) {
  mrb_value m = MV(v);
  if (mrb_string_p(m)) return rb_enc_find_index(mrb_string_cstr(M, m));
  if (!SPECIAL_CONST_P(v) && mrb_obj_is_kind_of(M, m, mrb_class_ptr(encoding_class()))) return enc_of_encoding(m)->index;
  if (mrb_respond_to(M, m, SYM("to_str"))) {
    mrb_value s = call0(m, "to_str");
    if (mrb_string_p(s)) return rb_enc_find_index(mrb_string_cstr(M, s));
  }
  return -1;
}

VALUE rb_obj_encoding(VALUE obj) {
  rb_encoding *e = rb_enc_get(obj);
  if (!e) rb_raise(rb_eTypeError, "unknown encoding");
  return rb_enc_from_encoding(e);
}

void rb_enc_set_index(VALUE obj, int idx) { rb_enc_associate_index(obj, idx); }

int rb_enc_alias(const char *alias, const char *orig) {
  mrb_value args[2] = { mrb_str_new_cstr(M, alias), mrb_str_new_cstr(M, orig) };
  mrb_value enc = call(encoding_class(), "__capi_alias", 2, args);
  return mrb_nil_p(enc) ? -1 : enc_of_encoding(enc)->index;
}

/* -- character lengths -- */

static int utf8_need(unsigned char c) {
  if (c < 0x80) return 1;
  if (c < 0xC2) return 0;
  if (c < 0xE0) return 2;
  if (c < 0xF0) return 3;
  if (c < 0xF5) return 4;
  return 0;
}

/* the length of the character at p: positive, -1 invalid, -1 - n for n more
   bytes needed (the end comes first) */
int rb_enc_precise_mbclen(const char *p, const char *e, rb_encoding *enc) {
  if (!enc) enc = rb_ascii8bit_encoding();
  if (p >= e) return ONIGENC_CONSTRUCT_MBCLEN_NEEDMORE(1);
  long avail = e - p;
  if (enc == rb_utf8_encoding()) {
    unsigned char c = (unsigned char)*p;
    int need = utf8_need(c);
    if (!need) return ONIGENC_CONSTRUCT_MBCLEN_INVALID();
    int have = avail < need ? (int)avail : need;
    for (int i = 1; i < have; i++) {
      unsigned char k = (unsigned char)p[i];
      if ((k & 0xC0) != 0x80) return ONIGENC_CONSTRUCT_MBCLEN_INVALID();
      if (i == 1 && ((c == 0xE0 && k < 0xA0) || (c == 0xED && k > 0x9F) || (c == 0xF0 && k < 0x90) || (c == 0xF4 && k > 0x8F)))
        return ONIGENC_CONSTRUCT_MBCLEN_INVALID();
    }
    return avail < need ? ONIGENC_CONSTRUCT_MBCLEN_NEEDMORE(need - (int)avail) : need;
  }
  if (enc->max_enc_len == 1) return 1;
  /* any other encoding: the first prefix that is a valid character */
  int top = enc->max_enc_len < avail ? enc->max_enc_len : (int)avail;
  for (int k = enc->min_enc_len > 0 ? enc->min_enc_len : 1; k <= top; k++) {
    if (mrb_test(call0(MV(rb_enc_str_new(p, k, enc)), "valid_encoding?"))) return k;
  }
  return avail < enc->max_enc_len ? ONIGENC_CONSTRUCT_MBCLEN_NEEDMORE(1) : ONIGENC_CONSTRUCT_MBCLEN_INVALID();
}

int rb_enc_ascget(const char *p, const char *e, int *len, rb_encoding *enc) {
  if (p >= e) return -1;
  unsigned char c = (unsigned char)*p;
  if (c >= 0x80 || !enc || !enc->ascii_compatible) return -1;
  if (len) *len = 1;
  return c;
}

char *rb_enc_nth(const char *p, const char *e, long nth, rb_encoding *enc) {
  if (!enc || enc->max_enc_len == 1) {
    p += nth;
    return (char *)(p > e ? e : p);
  }
  while (p < e && nth-- > 0) {
    int n = rb_enc_mbclen(p, e, enc);
    p += n > 0 ? n : 1;
  }
  return (char *)(p > e ? e : p);
}

/* the start of the character p is in (MRI: onigenc_get_left_adjust_char_head) */
char *rb_enc_left_char_head(const char *s, const char *p, const char *e, rb_encoding *enc) {
  if (!enc || enc->max_enc_len == 1 || p <= s) return (char *)p;
  if (enc == rb_utf8_encoding()) {
    if (p >= e) return (char *)p;
    int back = 0;
    while (p > s && back < 3 && ((unsigned char)*p & 0xC0) == 0x80) { p--; back++; }
    return (char *)p;
  }
  const char *head = s;
  while (head < p && head < e) {
    int n = rb_enc_mbclen(head, e, enc);
    if (n < 1) n = 1;
    if (head + n > p) break;
    head += n;
  }
  return (char *)head;
}

/* the code of the character at p; a broken or cut one gives its first byte */
unsigned int rb_capi_enc_mbc_to_codepoint(const char *p, const char *e, rb_encoding *enc) {
  if (p >= e) return 0;
  int n = rb_enc_precise_mbclen(p, e, enc);
  if (n <= 0) return (unsigned char)*p;
  if (n == 1 && (!enc || enc->min_enc_len == 1)) return (unsigned char)*p;
  mrb_value s = MV(rb_enc_str_new(p, n, enc));
  return (unsigned int)mrb_integer(call0(s, "ord"));
}

/* the bytes of the character code in buf, nothing past them; their number */
int rb_capi_enc_mbcput(unsigned int c, char *buf, rb_encoding *enc) {
  if (!enc || enc == rb_utf8_encoding()) {
    return rb_uv_to_utf8(buf, c);
  }
  mrb_value s = call1(mrb_int_value(M, (mrb_int)c), "chr", MV(rb_enc_from_encoding(enc)));
  mrb_int n = RSTRING_LEN(s);
  memcpy(buf, RSTRING_PTR(s), (size_t)n);
  return (int)n;
}

int rb_uv_to_utf8(char buf[6], unsigned long uv) {
  if (uv <= 0x7f) { buf[0] = (char)uv; return 1; }
  if (uv <= 0x7ff) { buf[0] = (char)(((uv >> 6) & 0xff) | 0xc0); buf[1] = (char)((uv & 0x3f) | 0x80); return 2; }
  if (uv <= 0xffff) {
    buf[0] = (char)(((uv >> 12) & 0xff) | 0xe0); buf[1] = (char)(((uv >> 6) & 0x3f) | 0x80); buf[2] = (char)((uv & 0x3f) | 0x80);
    return 3;
  }
  if (uv <= 0x1fffff) {
    buf[0] = (char)(((uv >> 18) & 0xff) | 0xf0); buf[1] = (char)(((uv >> 12) & 0x3f) | 0x80);
    buf[2] = (char)(((uv >> 6) & 0x3f) | 0x80); buf[3] = (char)((uv & 0x3f) | 0x80);
    return 4;
  }
  if (uv <= 0x3ffffff) {
    buf[0] = (char)(((uv >> 24) & 0xff) | 0xf8); buf[1] = (char)(((uv >> 18) & 0x3f) | 0x80);
    buf[2] = (char)(((uv >> 12) & 0x3f) | 0x80); buf[3] = (char)(((uv >> 6) & 0x3f) | 0x80); buf[4] = (char)((uv & 0x3f) | 0x80);
    return 5;
  }
  if (uv <= 0x7fffffff) {
    buf[0] = (char)(((uv >> 30) & 0xff) | 0xfc); buf[1] = (char)(((uv >> 24) & 0x3f) | 0x80);
    buf[2] = (char)(((uv >> 18) & 0x3f) | 0x80); buf[3] = (char)(((uv >> 12) & 0x3f) | 0x80);
    buf[4] = (char)(((uv >> 6) & 0x3f) | 0x80); buf[5] = (char)((uv & 0x3f) | 0x80);
    return 6;
  }
  rb_raise(rb_eRangeError, "pack(U): value out of range");
  return 0;
}

/* -- case folding and ctypes -- */

/* the fold of the character at *pp into to; its length; *pp moves past it */
int rb_capi_enc_mbc_case_fold(rb_encoding *enc, int flag, const OnigUChar **pp, const OnigUChar *end, OnigUChar *to) {
  (void)flag;
  const char *p = (const char *)*pp;
  int n = rb_enc_precise_mbclen(p, (const char *)end, enc);
  if (n < 1) n = enc && enc->min_enc_len > 0 ? enc->min_enc_len : 1;
  mrb_value s = MV(rb_enc_str_new(p, n, enc));
  mrb_value fold = call1(s, "downcase", mrb_symbol_value(SYM("fold")));
  mrb_int len = RSTRING_LEN(fold);
  memcpy(to, RSTRING_PTR(fold), (size_t)len);
  *pp += n;
  return (int)len;
}

int rb_capi_enc_unicode_p(rb_encoding *enc) {
  static const char *const names[] = {
    "UTF-8", "UTF8-MAC", "UTF8-DoCoMo", "UTF8-KDDI", "UTF8-SoftBank", "CESU-8",
    "UTF-16LE", "UTF-16BE", "UTF-32LE", "UTF-32BE", NULL
  };
  if (!enc) return 0;
  for (int i = 0; names[i]; i++) if (strcmp(enc->name, names[i]) == 0) return 1;
  return 0;
}

int rb_enc_isctype(int c, int type, rb_encoding *enc) {
  if (c < 0) return 0;
  if (type == ONIGENC_CTYPE_ASCII) return c < 128;
  mrb_value args[3] = { mrb_int_value(M, c), mrb_int_value(M, type), MV(rb_enc_from_encoding(enc)) };
  return mrb_test(call(mrb_obj_value(mrb_module_get(M, "ORubyCAPI")), "enc_ctype", 3, args));
}

/* ASCII letters only, as MRI's */
int rb_enc_toupper(int c, rb_encoding *enc) { (void)enc; return (c >= 'a' && c <= 'z') ? c - 32 : c; }
int rb_enc_tolower(int c, rb_encoding *enc) { (void)enc; return (c >= 'A' && c <= 'Z') ? c + 32 : c; }

void rb_enc_raise(rb_encoding *enc, VALUE exc, const char *fmt, ...) {
  va_list ap;
  va_start(ap, fmt);
  VALUE msg = rb_enc_associate(rb_vsprintf(fmt, ap), enc);
  va_end(ap);
  rb_exc_raise(rb_exc_new_str(exc, msg));
}
