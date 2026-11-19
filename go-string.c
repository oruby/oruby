// Native C implementations for MRI-compliant String & Math methods:
//   String#count, delete/delete!, squeeze/squeeze!, tr/tr!, tr_s/tr_s!,
//   lstrip/lstrip!, rstrip/rstrip!, strip/strip!, concat/<<,
//   byteindex/byterindex; Math.expm1, log1p, log2, ldexp.
//
// All hot paths run in C without Ruby wrapper frames, preserving @__encoding
// tags and binary flags directly.
#include <string.h>
#include <stdlib.h>
#include <stdint.h>
#include <math.h>
#include "go-mrb.h"

/* ---- UTF-8 & encoding helpers ---- */

static inline int utf8_decode_one(const uint8_t *p, mrb_int len, uint32_t *cp) {
  if (len <= 0) return 0;
  uint8_t b0 = p[0];
  if (b0 < 0x80) {
    *cp = b0;
    return 1;
  }
  if ((b0 & 0xE0) == 0xC0) {
    if (len < 2 || (p[1] & 0xC0) != 0x80) return -1;
    uint32_t c = ((uint32_t)(b0 & 0x1F) << 6) | (uint32_t)(p[1] & 0x3F);
    if (c < 0x80) return -1;
    *cp = c;
    return 2;
  }
  if ((b0 & 0xF0) == 0xE0) {
    if (len < 3 || (p[1] & 0xC0) != 0x80 || (p[2] & 0xC0) != 0x80) return -1;
    uint32_t c = ((uint32_t)(b0 & 0x0F) << 12) | ((uint32_t)(p[1] & 0x3F) << 6) | (uint32_t)(p[2] & 0x3F);
    if (c < 0x800 || (c >= 0xD800 && c <= 0xDFFF)) return -1;
    *cp = c;
    return 3;
  }
  if ((b0 & 0xF8) == 0xF0) {
    if (len < 4 || (p[1] & 0xC0) != 0x80 || (p[2] & 0xC0) != 0x80 || (p[3] & 0xC0) != 0x80) return -1;
    uint32_t c = ((uint32_t)(b0 & 0x07) << 18) | ((uint32_t)(p[1] & 0x3F) << 12) |
                 ((uint32_t)(p[2] & 0x3F) << 6) | (uint32_t)(p[3] & 0x3F);
    if (c < 0x10000 || c > 0x10FFFF) return -1;
    *cp = c;
    return 4;
  }
  return -1;
}

static inline int utf8_encode_one(uint32_t cp, char out[4]) {
  if (cp < 0x80) {
    out[0] = (char)cp;
    return 1;
  }
  if (cp < 0x800) {
    out[0] = (char)(0xC0 | (cp >> 6));
    out[1] = (char)(0x80 | (cp & 0x3F));
    return 2;
  }
  if (cp < 0x10000) {
    out[0] = (char)(0xE0 | (cp >> 12));
    out[1] = (char)(0x80 | ((cp >> 6) & 0x3F));
    out[2] = (char)(0x80 | (cp & 0x3F));
    return 3;
  }
  out[0] = (char)(0xF0 | (cp >> 18));
  out[1] = (char)(0x80 | ((cp >> 12) & 0x3F));
  out[2] = (char)(0x80 | ((cp >> 6) & 0x3F));
  out[3] = (char)(0x80 | (cp & 0x3F));
  return 4;
}

static mrb_bool str_is_ascii_only(const uint8_t *p, mrb_int len) {
  for (mrb_int i = 0; i < len; i++) {
    if (p[i] >= 0x80) return FALSE;
  }
  return TRUE;
}

static mrb_bool str_is_binary_enc(mrb_state *mrb, mrb_value s) {
  if (RSTR_BINARY_P(mrb_str_ptr(s))) return TRUE;
  return FALSE;
}

static mrb_value str_enc_obj(mrb_state *mrb, mrb_value s) {
  mrb_value tag = mrb_iv_get(mrb, s, MRB_SYM(__encoding));
  if (!mrb_nil_p(tag)) return tag;
  return mrb_funcall_id(mrb, s, MRB_SYM(encoding), 0);
}

static mrb_bool str_enc_fast_same(mrb_state *mrb, mrb_value a, mrb_value b) {
  if (RSTR_BINARY_P(mrb_str_ptr(a)) != RSTR_BINARY_P(mrb_str_ptr(b))) return FALSE;
  mrb_value ta = mrb_iv_get(mrb, a, MRB_SYM(__encoding));
  mrb_value tb = mrb_iv_get(mrb, b, MRB_SYM(__encoding));
  return mrb_obj_eq(mrb, ta, tb);
}

static void str_raise_compat_err(mrb_state *mrb, mrb_value a, mrb_value b) {
  mrb_value enc_cls = mrb_obj_value(mrb_class_get(mrb, "Encoding"));
  struct RClass *compat_err = mrb_class_get_under(mrb, mrb_class_ptr(enc_cls), "CompatibilityError");
  mrb_value ea = mrb_funcall_id(mrb, a, MRB_SYM(encoding), 0);
  mrb_value eb = mrb_funcall_id(mrb, b, MRB_SYM(encoding), 0);
  mrb_raisef(mrb, compat_err, "incompatible character encodings: %v and %v", ea, eb);
}

/* Check Encoding.compatible?(a, b), returning combined Encoding or raising CompatibilityError.
   Fast path: same internal binary flag & @__encoding tag -> returns nil (no change needed). */
static mrb_value str_check_compat(mrb_state *mrb, mrb_value a, mrb_value b) {
  if (str_enc_fast_same(mrb, a, b)) return mrb_nil_value();
  mrb_value enc_cls = mrb_obj_value(mrb_class_get(mrb, "Encoding"));
  mrb_value c = mrb_funcall_id(mrb, enc_cls, mrb_intern_lit(mrb, "compatible?"), 2, a, b);
  if (mrb_nil_p(c)) {
    str_raise_compat_err(mrb, a, b);
  }
  return c;
}

static void str_copy_enc(mrb_state *mrb, mrb_value from, mrb_value to) {
  struct RString *sf = mrb_str_ptr(from);
  struct RString *st = mrb_str_ptr(to);
  st->flags = (st->flags & ~(MRB_STR_ENCODING_MASK | MRB_STR_CODERANGE_MASK)) |
              (sf->flags & MRB_STR_ENCODING_MASK);
  mrb_value tag = mrb_iv_get(mrb, from, MRB_SYM(__encoding));
  if (!mrb_nil_p(tag)) {
    mrb_iv_set(mrb, to, MRB_SYM(__encoding), tag);
  }
}

static mrb_value str_implicit_to_str(mrb_state *mrb, mrb_value v) {
  if (mrb_string_p(v)) return v;
  mrb_value cv[2] = {v, mrb_obj_value(mrb->string_class)};
  return mrb_funcall_argv(mrb, mrb_obj_value(mrb->kernel_module), MRB_SYM(__implicit), 2, cv);
}

static void str_check_frozen(mrb_state *mrb, mrb_value self) {
  if (mrb_frozen_p(mrb_basic_ptr(self))) {
    mrb_check_frozen(mrb, mrb_basic_ptr(self));
  }
}

/* ---- Character selector parsing (for count / delete / squeeze / strip / tr) ---- */

typedef struct {
  uint32_t lo;
  uint32_t hi;
} cp_range_t;

#define MAX_INLINE_RANGES 32

typedef struct {
  mrb_bool neg;
  uint8_t ascii[256]; /* membership BEFORE negation for bytes 0..255 */
  cp_range_t ranges[MAX_INLINE_RANGES];
  cp_range_t *extra;
  int nrange;
  int cap_extra;
} cset_t;

static void cset_init(cset_t *cs) {
  memset(cs, 0, sizeof(*cs));
}

static void cset_free(mrb_state *mrb, cset_t *cs) {
  if (cs->extra) {
    mrb_free(mrb, cs->extra);
    cs->extra = NULL;
  }
}

static void cset_add_range(mrb_state *mrb, cset_t *cs, uint32_t lo, uint32_t hi, mrb_bool single_byte) {
  uint32_t lim = single_byte ? 255 : 127;
  if (lo <= lim) {
    uint32_t end = hi < lim ? hi : lim;
    for (uint32_t c = lo; c <= end; c++) cs->ascii[c] = 1;
  }
  if (hi > lim) {
    uint32_t start = lo > lim ? lo : (lim + 1);
    if (cs->nrange < MAX_INLINE_RANGES) {
      cs->ranges[cs->nrange].lo = start;
      cs->ranges[cs->nrange].hi = hi;
      cs->nrange++;
    } else {
      int idx = cs->nrange - MAX_INLINE_RANGES;
      if (idx >= cs->cap_extra) {
        int ncap = cs->cap_extra ? cs->cap_extra * 2 : 32;
        cs->extra = (cp_range_t *)mrb_realloc(mrb, cs->extra, sizeof(cp_range_t) * ncap);
        cs->cap_extra = ncap;
      }
      cs->extra[idx].lo = start;
      cs->extra[idx].hi = hi;
      cs->nrange++;
    }
  }
}

static inline mrb_bool cset_match(const cset_t *cs, uint32_t cp, mrb_bool single_byte) {
  mrb_bool hit = FALSE;
  uint32_t lim = single_byte ? 255 : 127;
  if (cp <= lim) {
    hit = cs->ascii[cp] != 0;
  } else {
    int n = cs->nrange < MAX_INLINE_RANGES ? cs->nrange : MAX_INLINE_RANGES;
    for (int i = 0; i < n; i++) {
      if (cp >= cs->ranges[i].lo && cp <= cs->ranges[i].hi) {
        hit = TRUE;
        break;
      }
    }
    if (!hit && cs->nrange > MAX_INLINE_RANGES) {
      int m = cs->nrange - MAX_INLINE_RANGES;
      for (int i = 0; i < m; i++) {
        if (cp >= cs->extra[i].lo && cp <= cs->extra[i].hi) {
          hit = TRUE;
          break;
        }
      }
    }
  }
  return cs->neg ? !hit : hit;
}

static inline int next_cp(const uint8_t *p, mrb_int len, mrb_bool single_byte, uint32_t *cp) {
  if (len <= 0) return 0;
  if (single_byte) {
    *cp = p[0];
    return 1;
  }
  int n = utf8_decode_one(p, len, cp);
  if (n < 0) {
    *cp = 0x80000000u | p[0];
    return 1;
  }
  return n;
}

/* Parse a selector string into cs. Returns FALSE (and sets err_lo/err_hi) on invalid range. */
static mrb_bool cset_parse(mrb_state *mrb, cset_t *cs, const uint8_t *p, mrb_int len, mrb_bool single_byte,
                           uint32_t *err_lo, uint32_t *err_hi) {
  cset_init(cs);
  mrb_int i = 0;
  if (len > 1 && p[0] == '^') {
    cs->neg = TRUE;
    i = 1;
  }
  while (i < len) {
    uint32_t c1;
    int n1;
    if (p[i] == '\\' && i + 1 < len) {
      i++;
    }
    n1 = next_cp(p + i, len - i, single_byte, &c1);
    if (c1 & 0x80000000u) return FALSE;
    i += n1;
    if (i < len && p[i] == '-' && i + 1 < len) {
      i++; /* skip '-' */
      if (p[i] == '\\' && i + 1 < len) {
        i++;
      }
      uint32_t c2;
      int n2 = next_cp(p + i, len - i, single_byte, &c2);
      if (c2 & 0x80000000u) return FALSE;
      i += n2;
      if (c1 > c2) {
        *err_lo = c1;
        *err_hi = c2;
        return FALSE;
      }
      cset_add_range(mrb, cs, c1, c2, single_byte);
    } else {
      cset_add_range(mrb, cs, c1, c1, single_byte);
    }
  }
  return TRUE;
}

/* Build intersection of k selector arguments; checks #to_str & encoding compatibility.
   Sets ascii_mask[0..255] = 1 iff ALL sets match that byte. */
#define MAX_INLINE_SETS 8

typedef struct {
  cset_t inline_sets[MAX_INLINE_SETS];
  cset_t *sets;
  int count;
  uint8_t ascii_mask[256];
  mrb_bool single_byte;
} cset_group_t;

static void cset_group_free(mrb_state *mrb, cset_group_t *g) {
  for (int i = 0; i < g->count; i++) cset_free(mrb, &g->sets[i]);
  if (g->sets && g->sets != g->inline_sets) mrb_free(mrb, g->sets);
  g->sets = NULL;
  g->count = 0;
}

static void cset_group_build(mrb_state *mrb, mrb_value self, mrb_int argc, const mrb_value *argv, cset_group_t *g) {
  memset(g, 0, sizeof(*g));
  g->single_byte = str_is_binary_enc(mrb, self);
  /* First convert all args and check encoding compatibility BEFORE allocating C memory */
  mrb_value coerced_buf[MAX_INLINE_SETS];
  mrb_value ary_coerced = mrb_nil_value();
  mrb_value *coerced = coerced_buf;
  if (argc > MAX_INLINE_SETS) {
    ary_coerced = mrb_ary_new_capa(mrb, argc);
    for (mrb_int i = 0; i < argc; i++) mrb_ary_push(mrb, ary_coerced, mrb_nil_value());
    coerced = RARRAY_PTR(ary_coerced);
  }
  for (mrb_int i = 0; i < argc; i++) {
    mrb_value s = str_implicit_to_str(mrb, argv[i]);
    str_check_compat(mrb, self, s);
    coerced[i] = s;
  }
  if (argc <= MAX_INLINE_SETS) {
    g->sets = g->inline_sets;
  } else {
    g->sets = (cset_t *)mrb_malloc(mrb, sizeof(cset_t) * argc);
  }
  g->count = (int)argc;
  for (int i = 0; i < g->count; i++) cset_init(&g->sets[i]);
  for (int i = 0; i < g->count; i++) {
    uint32_t elo = 0, ehi = 0;
    if (!cset_parse(mrb, &g->sets[i], (const uint8_t *)RSTRING_PTR(coerced[i]), RSTRING_LEN(coerced[i]),
                    g->single_byte, &elo, &ehi)) {
      cset_group_free(mrb, g);
      mrb_raise(mrb, E_ARGUMENT_ERROR, "invalid range in string transliteration");
    }
  }
  uint32_t lim = g->single_byte ? 256 : 128;
  for (uint32_t b = 0; b < lim; b++) {
    uint8_t ok = 1;
    for (int i = 0; i < g->count; i++) {
      if (!cset_match(&g->sets[i], b, g->single_byte)) {
        ok = 0;
        break;
      }
    }
    g->ascii_mask[b] = ok;
  }
}

static inline mrb_bool cset_group_match(const cset_group_t *g, uint32_t cp) {
  uint32_t lim = g->single_byte ? 255 : 127;
  if (cp <= lim) return g->ascii_mask[cp] != 0;
  for (int i = 0; i < g->count; i++) {
    if (!cset_match(&g->sets[i], cp, g->single_byte)) return FALSE;
  }
  return TRUE;
}

/* ---- String#count ---- */

static mrb_value str_count_m(mrb_state *mrb, mrb_value self) {
  mrb_int argc = mrb_get_argc(mrb);
  const mrb_value *argv = mrb_get_argv(mrb);
  if (argc < 1) {
    mrb_raisef(mrb, E_ARGUMENT_ERROR, "wrong number of arguments (given %i, expected 1+)", argc);
  }
  cset_group_t g;
  cset_group_build(mrb, self, argc, argv, &g);

  const uint8_t *p = (const uint8_t *)RSTRING_PTR(self);
  mrb_int len = RSTRING_LEN(self);
  mrb_int cnt = 0;
  if (g.single_byte || str_is_ascii_only(p, len)) {
    for (mrb_int i = 0; i < len; i++) {
      cnt += g.ascii_mask[p[i]];
    }
  } else {
    mrb_int i = 0;
    while (i < len) {
      uint32_t cp;
      int n = next_cp(p + i, len - i, FALSE, &cp);
      if (cset_group_match(&g, cp)) cnt++;
      i += n;
    }
  }
  cset_group_free(mrb, &g);
  return mrb_fixnum_value(cnt);
}

/* ---- String#delete / #delete! ---- */

static mrb_value str_delete_impl(mrb_state *mrb, mrb_value self, mrb_bool bang) {
  mrb_int argc = mrb_get_argc(mrb);
  const mrb_value *argv = mrb_get_argv(mrb);
  if (argc < 1) {
    mrb_raisef(mrb, E_ARGUMENT_ERROR, "wrong number of arguments (given %i, expected 1+)", argc);
  }
  if (bang) str_check_frozen(mrb, self);
  cset_group_t g;
  cset_group_build(mrb, self, argc, argv, &g);

  const uint8_t *p = (const uint8_t *)RSTRING_PTR(self);
  mrb_int len = RSTRING_LEN(self);
  mrb_value out = mrb_str_new_capa(mrb, len);
  mrb_bool modified = FALSE;
  if (g.single_byte || str_is_ascii_only(p, len)) {
    mrb_int seg = 0;
    for (mrb_int i = 0; i < len; i++) {
      if (g.ascii_mask[p[i]]) {
        if (i > seg) mrb_str_cat(mrb, out, (const char *)(p + seg), i - seg);
        seg = i + 1;
        modified = TRUE;
      }
    }
    if (len > seg) mrb_str_cat(mrb, out, (const char *)(p + seg), len - seg);
  } else {
    mrb_int i = 0, seg = 0;
    while (i < len) {
      uint32_t cp;
      int n = next_cp(p + i, len - i, FALSE, &cp);
      if (cset_group_match(&g, cp)) {
        if (i > seg) mrb_str_cat(mrb, out, (const char *)(p + seg), i - seg);
        seg = i + n;
        modified = TRUE;
      }
      i += n;
    }
    if (len > seg) mrb_str_cat(mrb, out, (const char *)(p + seg), len - seg);
  }
  cset_group_free(mrb, &g);
  if (bang) {
    if (!modified) return mrb_nil_value();
    mrb_str_modify(mrb, mrb_str_ptr(self));
    mrb_str_resize(mrb, self, RSTRING_LEN(out));
    memcpy(RSTRING_PTR(self), RSTRING_PTR(out), RSTRING_LEN(out));
    return self;
  }
  str_copy_enc(mrb, self, out);
  return out;
}

static mrb_value str_delete_m(mrb_state *mrb, mrb_value self) { return str_delete_impl(mrb, self, FALSE); }
static mrb_value str_delete_bang_m(mrb_state *mrb, mrb_value self) { return str_delete_impl(mrb, self, TRUE); }

/* ---- String#squeeze / #squeeze! ---- */

static mrb_value str_squeeze_impl(mrb_state *mrb, mrb_value self, mrb_bool bang) {
  mrb_int argc = mrb_get_argc(mrb);
  const mrb_value *argv = mrb_get_argv(mrb);
  if (bang) str_check_frozen(mrb, self);

  cset_group_t g;
  mrb_bool has_set = argc > 0;
  mrb_bool single_byte = str_is_binary_enc(mrb, self);
  if (has_set) {
    cset_group_build(mrb, self, argc, argv, &g);
    single_byte = g.single_byte;
  }

  const uint8_t *p = (const uint8_t *)RSTRING_PTR(self);
  mrb_int len = RSTRING_LEN(self);
  mrb_value out = mrb_str_new_capa(mrb, len);
  mrb_bool modified = FALSE;
  uint32_t prev = 0xFFFFFFFFu;
  if (single_byte || str_is_ascii_only(p, len)) {
    for (mrb_int i = 0; i < len; i++) {
      uint32_t c = p[i];
      if (c == prev && (!has_set || g.ascii_mask[c])) {
        modified = TRUE;
        continue;
      }
      char ch = (char)c;
      mrb_str_cat(mrb, out, &ch, 1);
      prev = c;
    }
  } else {
    mrb_int i = 0;
    while (i < len) {
      uint32_t cp;
      int n = next_cp(p + i, len - i, FALSE, &cp);
      if (cp == prev && (!has_set || cset_group_match(&g, cp))) {
        modified = TRUE;
        i += n;
        continue;
      }
      mrb_str_cat(mrb, out, (const char *)(p + i), n);
      prev = cp;
      i += n;
    }
  }
  if (has_set) cset_group_free(mrb, &g);
  if (bang) {
    if (!modified) return mrb_nil_value();
    mrb_str_modify(mrb, mrb_str_ptr(self));
    mrb_str_resize(mrb, self, RSTRING_LEN(out));
    memcpy(RSTRING_PTR(self), RSTRING_PTR(out), RSTRING_LEN(out));
    return self;
  }
  str_copy_enc(mrb, self, out);
  return out;
}

static mrb_value str_squeeze_m(mrb_state *mrb, mrb_value self) { return str_squeeze_impl(mrb, self, FALSE); }
static mrb_value str_squeeze_bang_m(mrb_state *mrb, mrb_value self) { return str_squeeze_impl(mrb, self, TRUE); }

/* ---- String#tr / #tr! / #tr_s / #tr_s! ---- */

/* Expand a tr spec string into a flat codepoint sequence (for small/medium specs).
   Uses a GC-managed mrb_str_new_capa as backing byte buffer of uint32_t so exceptions never leak. */
static mrb_value tr_expand_spec(mrb_state *mrb, const uint8_t *p, mrb_int len, mrb_bool single_byte,
                                mrb_bool *neg_out, mrb_int *count_out) {
  mrb_int i = 0;
  *neg_out = FALSE;
  if (len > 1 && p[0] == '^') {
    *neg_out = TRUE;
    i = 1;
  }
  mrb_value buf = mrb_str_new_capa(mrb, (len + 4) * (mrb_int)sizeof(uint32_t));
  mrb_int cnt = 0;
  while (i < len) {
    uint32_t c1;
    if (p[i] == '\\' && i + 1 < len) i++;
    i += next_cp(p + i, len - i, single_byte, &c1);
    if (c1 & 0x80000000u) mrb_raise(mrb, E_ARGUMENT_ERROR, "invalid byte sequence in UTF-8");
    if (i < len && p[i] == '-' && i + 1 < len) {
      i++;
      if (p[i] == '\\' && i + 1 < len) i++;
      uint32_t c2;
      i += next_cp(p + i, len - i, single_byte, &c2);
      if (c2 & 0x80000000u) mrb_raise(mrb, E_ARGUMENT_ERROR, "invalid byte sequence in UTF-8");
      if (c1 > c2) {
        mrb_raise(mrb, E_ARGUMENT_ERROR, "invalid range in string transliteration");
      }
      for (uint32_t c = c1; c <= c2; c++) {
        mrb_str_cat(mrb, buf, (const char *)&c, sizeof(uint32_t));
        cnt++;
      }
    } else {
      mrb_str_cat(mrb, buf, (const char *)&c1, sizeof(uint32_t));
      cnt++;
    }
  }
  *count_out = cnt;
  return buf;
}

/* Fixed code unit width for UTF-16/UTF-32 in hash-form tr; 0 means UTF-8 / variable / 1-byte */
static int enc_fixed_width(mrb_state *mrb, mrb_value s) {
  mrb_value tag = mrb_iv_get(mrb, s, MRB_SYM(__encoding));
  if (mrb_nil_p(tag)) return 0;
  mrb_value name_v = mrb_funcall_id(mrb, tag, MRB_SYM(name), 0);
  if (!mrb_string_p(name_v)) return 0;
  const char *n = RSTRING_PTR(name_v);
  mrb_int nl = RSTRING_LEN(name_v);
  if ((nl == 8 && (memcmp(n, "UTF-16LE", 8) == 0 || memcmp(n, "UTF-16BE", 8) == 0)) ||
      (nl == 6 && memcmp(n, "UTF-16", 6) == 0)) return 2;
  if ((nl == 8 && (memcmp(n, "UTF-32LE", 8) == 0 || memcmp(n, "UTF-32BE", 8) == 0)) ||
      (nl == 6 && memcmp(n, "UTF-32", 6) == 0)) return 4;
  return 0;
}

/* Byte length of 1 codepoint at p[0..len) for UTF-8 / binary / UTF-16 / UTF-32. Returns -1 if invalid. */
static int str_unit_len(const uint8_t *p, mrb_int len, mrb_bool single_byte, int fw, mrb_bool utf16_be) {
  if (len <= 0) return 0;
  if (fw == 4) return len >= 4 ? 4 : -1;
  if (fw == 2) {
    if (len < 2) return -1;
    uint16_t u = utf16_be ? ((uint16_t)p[0] << 8 | p[1]) : ((uint16_t)p[1] << 8 | p[0]);
    if (u >= 0xD800 && u <= 0xDBFF) {
      if (len < 4) return -1;
      uint16_t u2 = utf16_be ? ((uint16_t)p[2] << 8 | p[3]) : ((uint16_t)p[3] << 8 | p[2]);
      if (u2 >= 0xDC00 && u2 <= 0xDFFF) return 4;
      return -1;
    }
    if (u >= 0xDC00 && u <= 0xDFFF) return -1;
    return 2;
  }
  if (single_byte) return 1;
  uint32_t cp;
  return utf8_decode_one(p, len, &cp);
}

static mrb_value str_tr_hash(mrb_state *mrb, mrb_value self, mrb_value h, mrb_bool bang) {
  if (bang) str_check_frozen(mrb, self);
  if (!mrb_hash_p(h)) {
    mrb_value cv[2] = {h, mrb_obj_value(mrb->hash_class)};
    h = mrb_funcall_argv(mrb, mrb_obj_value(mrb->kernel_module), MRB_SYM(__implicit), 2, cv);
  }
  /* Check self valid encoding */
  mrb_value valid = mrb_funcall_id(mrb, self, mrb_intern_lit(mrb, "valid_encoding?"), 0);
  if (!mrb_test(valid)) {
    mrb_raise(mrb, E_ARGUMENT_ERROR, "invalid byte sequence in string");
  }

  int fw = enc_fixed_width(mrb, self);
  mrb_bool utf16_be = FALSE;
  if (fw == 2) {
    mrb_value tag = mrb_iv_get(mrb, self, MRB_SYM(__encoding));
    mrb_value nv = mrb_funcall_id(mrb, tag, MRB_SYM(name), 0);
    if (mrb_string_p(nv) && RSTRING_LEN(nv) >= 8 && memcmp(RSTRING_PTR(nv), "UTF-16BE", 8) == 0) utf16_be = TRUE;
  }
  mrb_bool single_byte = str_is_binary_enc(mrb, self) && fw == 0;
  mrb_value keys = mrb_hash_keys(mrb, h);
  mrb_int nk = RARRAY_LEN(keys);
  mrb_value res_enc = str_enc_obj(mrb, self);
  mrb_value enc_probe = mrb_str_dup(mrb, self);
  str_copy_enc(mrb, self, enc_probe);

  /* Validate all keys & values: #to_str, 1 codepoint keys, compatible encodings */
  mrb_value k_arr = mrb_ary_new_capa(mrb, nk);
  mrb_value v_arr = mrb_ary_new_capa(mrb, nk);
  for (mrb_int i = 0; i < nk; i++) {
    mrb_value k = str_implicit_to_str(mrb, RARRAY_PTR(keys)[i]);
    mrb_value v = str_implicit_to_str(mrb, mrb_hash_get(mrb, h, RARRAY_PTR(keys)[i]));
    str_check_compat(mrb, self, k);
    mrb_value ce = str_check_compat(mrb, enc_probe, v);
    if (!mrb_nil_p(ce)) {
      res_enc = ce;
      enc_probe = mrb_funcall_id(mrb, mrb_str_dup(mrb, v), MRB_SYM(force_encoding), 1, ce);
    }
    /* Key must be exactly 1 codepoint */
    const uint8_t *kp = (const uint8_t *)RSTRING_PTR(k);
    mrb_int kl = RSTRING_LEN(k);
    int kfw = enc_fixed_width(mrb, k);
    mrb_bool kbe = FALSE;
    if (kfw == 2) {
      mrb_value ktag = mrb_iv_get(mrb, k, MRB_SYM(__encoding));
      mrb_value knv = mrb_funcall_id(mrb, ktag, MRB_SYM(name), 0);
      if (mrb_string_p(knv) && RSTRING_LEN(knv) >= 8 && memcmp(RSTRING_PTR(knv), "UTF-16BE", 8) == 0) kbe = TRUE;
    }
    int ulen = str_unit_len(kp, kl, str_is_binary_enc(mrb, k) && kfw == 0, kfw, kbe);
    if (ulen <= 0 || ulen != kl) {
      mrb_raise(mrb, E_ARGUMENT_ERROR, "key must be a single character");
    }
    mrb_ary_push(mrb, k_arr, k);
    mrb_ary_push(mrb, v_arr, v);
  }

  if (nk == 0) {
    if (bang) return mrb_nil_value();
    mrb_value dup = mrb_str_dup(mrb, self);
    str_copy_enc(mrb, self, dup);
    return dup;
  }

  const uint8_t *p = (const uint8_t *)RSTRING_PTR(self);
  mrb_int len = RSTRING_LEN(self);
  mrb_value out = mrb_str_new_capa(mrb, len);
  mrb_bool modified = FALSE;
  mrb_int i = 0;
  while (i < len) {
    int n = str_unit_len(p + i, len - i, single_byte, fw, utf16_be);
    if (n <= 0) n = 1;
    mrb_int found = -1;
    for (mrb_int j = 0; j < nk; j++) {
      mrb_value k = RARRAY_PTR(k_arr)[j];
      if (RSTRING_LEN(k) == n && memcmp(RSTRING_PTR(k), p + i, n) == 0) {
        found = j;
        break;
      }
    }
    if (found >= 0) {
      mrb_value v = RARRAY_PTR(v_arr)[found];
      mrb_str_cat(mrb, out, RSTRING_PTR(v), RSTRING_LEN(v));
      modified = TRUE;
    } else {
      mrb_str_cat(mrb, out, (const char *)(p + i), n);
    }
    i += n;
  }

  mrb_funcall_id(mrb, out, MRB_SYM(force_encoding), 1, res_enc);
  if (bang) {
    if (!modified) return mrb_nil_value();
    mrb_str_modify(mrb, mrb_str_ptr(self));
    mrb_str_resize(mrb, self, RSTRING_LEN(out));
    memcpy(RSTRING_PTR(self), RSTRING_PTR(out), RSTRING_LEN(out));
    mrb_funcall_id(mrb, self, MRB_SYM(force_encoding), 1, res_enc);
    return self;
  }
  return out;
}

static mrb_value str_tr_impl(mrb_state *mrb, mrb_value self, mrb_bool squeeze, mrb_bool bang) {
  const mrb_value *argv = NULL;
  mrb_int argc = 0;
  mrb_get_args(mrb, "*", &argv, &argc);
  if (!squeeze && argc == 1) {
    return str_tr_hash(mrb, self, argv[0], bang);
  }
  if (argc != 2) {
    mrb_raisef(mrb, E_ARGUMENT_ERROR, "wrong number of arguments (given %i, expected 2)", argc);
  }
  if (bang) str_check_frozen(mrb, self);

  mrb_value from_v = str_implicit_to_str(mrb, argv[0]);
  mrb_value to_v = str_implicit_to_str(mrb, argv[1]);
  str_check_compat(mrb, self, from_v);
  mrb_value res_enc = str_check_compat(mrb, self, to_v);

  mrb_bool single_byte = str_is_binary_enc(mrb, self);
  mrb_bool neg = FALSE;
  mrb_int fcnt = 0, tcnt = 0;
  mrb_value fbuf = tr_expand_spec(mrb, (const uint8_t *)RSTRING_PTR(from_v), RSTRING_LEN(from_v), single_byte, &neg, &fcnt);
  mrb_bool tneg_unused = FALSE;
  /* to_str does NOT treat leading '^' as negation */
  mrb_value tbuf = mrb_str_new_capa(mrb, (RSTRING_LEN(to_v) + 4) * (mrb_int)sizeof(uint32_t));
  {
    const uint8_t *tp = (const uint8_t *)RSTRING_PTR(to_v);
    mrb_int tlen = RSTRING_LEN(to_v);
    mrb_int ti = 0;
    while (ti < tlen) {
      uint32_t c1;
      if (tp[ti] == '\\' && ti + 1 < tlen) ti++;
      ti += next_cp(tp + ti, tlen - ti, single_byte, &c1);
      if (c1 & 0x80000000u) mrb_raise(mrb, E_ARGUMENT_ERROR, "invalid byte sequence in UTF-8");
      if (ti < tlen && tp[ti] == '-' && ti + 1 < tlen) {
        ti++;
        if (tp[ti] == '\\' && ti + 1 < tlen) ti++;
        uint32_t c2;
        ti += next_cp(tp + ti, tlen - ti, single_byte, &c2);
        if (c2 & 0x80000000u) mrb_raise(mrb, E_ARGUMENT_ERROR, "invalid byte sequence in UTF-8");
        if (c1 > c2) mrb_raise(mrb, E_ARGUMENT_ERROR, "invalid range in string transliteration");
        for (uint32_t c = c1; c <= c2; c++) {
          mrb_str_cat(mrb, tbuf, (const char *)&c, sizeof(uint32_t));
          tcnt++;
        }
      } else {
        mrb_str_cat(mrb, tbuf, (const char *)&c1, sizeof(uint32_t));
        tcnt++;
      }
    }
    (void)tneg_unused;
  }

  const uint8_t *p = (const uint8_t *)RSTRING_PTR(self);
  mrb_int len = RSTRING_LEN(self);
  if (fcnt == 0 && !neg) {
    if (!single_byte && !str_is_ascii_only(p, len)) {
      mrb_int vi = 0;
      while (vi < len) {
        uint32_t vcp;
        vi += next_cp(p + vi, len - vi, FALSE, &vcp);
        if (vcp & 0x80000000u) mrb_raise(mrb, E_ARGUMENT_ERROR, "invalid byte sequence in UTF-8");
      }
    }
    if (bang) return mrb_nil_value();
    mrb_value dup = mrb_str_dup(mrb, self);
    str_copy_enc(mrb, self, dup);
    return dup;
  }

  const uint32_t *fcps = (const uint32_t *)RSTRING_PTR(fbuf);
  const uint32_t *tcps = (const uint32_t *)RSTRING_PTR(tbuf);
  mrb_value out = mrb_str_new_capa(mrb, len);
  mrb_bool modified = FALSE;
  mrb_bool last_tr = FALSE;
  uint32_t last_cp = 0xFFFFFFFFu;

  mrb_int i = 0;
  while (i < len) {
    uint32_t cp;
    int n = next_cp(p + i, len - i, single_byte, &cp);
    if (cp & 0x80000000u) mrb_raise(mrb, E_ARGUMENT_ERROR, "invalid byte sequence in UTF-8");
    mrb_int match_idx = -1;
    for (mrb_int j = fcnt - 1; j >= 0; j--) {
      if (fcps[j] == cp) {
        match_idx = j;
        break;
      }
    }
    mrb_bool matched = neg ? (match_idx < 0) : (match_idx >= 0);
    if (matched) {
      if (tcnt == 0) {
        modified = TRUE;
        i += n;
        continue;
      }
      uint32_t repl;
      if (neg) {
        repl = tcps[tcnt - 1];
      } else {
        repl = match_idx < tcnt ? tcps[match_idx] : tcps[tcnt - 1];
      }
      if (repl != cp) modified = TRUE;
      if (squeeze && last_tr && repl == last_cp) {
        modified = TRUE;
        i += n;
        continue;
      }
      if (single_byte) {
        char ch = (char)(repl & 0xFF);
        mrb_str_cat(mrb, out, &ch, 1);
      } else {
        char enc_ch[4];
        int en = utf8_encode_one(repl, enc_ch);
        mrb_str_cat(mrb, out, enc_ch, en);
      }
      last_tr = TRUE;
      last_cp = repl;
    } else {
      mrb_str_cat(mrb, out, (const char *)(p + i), n);
      last_tr = FALSE;
      last_cp = cp;
    }
    i += n;
  }

  if (bang) {
    if (!modified) return mrb_nil_value();
    mrb_str_modify(mrb, mrb_str_ptr(self));
    mrb_str_resize(mrb, self, RSTRING_LEN(out));
    memcpy(RSTRING_PTR(self), RSTRING_PTR(out), RSTRING_LEN(out));
    if (!mrb_nil_p(res_enc)) mrb_funcall_id(mrb, self, MRB_SYM(force_encoding), 1, res_enc);
    return self;
  }
  if (!mrb_nil_p(res_enc)) {
    mrb_funcall_id(mrb, out, MRB_SYM(force_encoding), 1, res_enc);
  } else {
    str_copy_enc(mrb, self, out);
  }
  return out;
}

static mrb_value str_tr_m(mrb_state *mrb, mrb_value self) { return str_tr_impl(mrb, self, FALSE, FALSE); }
static mrb_value str_tr_bang_m(mrb_state *mrb, mrb_value self) { return str_tr_impl(mrb, self, FALSE, TRUE); }
static mrb_value str_tr_s_m(mrb_state *mrb, mrb_value self) { return str_tr_impl(mrb, self, TRUE, FALSE); }
static mrb_value str_tr_s_bang_m(mrb_state *mrb, mrb_value self) { return str_tr_impl(mrb, self, TRUE, TRUE); }

/* ---- String#lstrip / #rstrip / #strip (with 0 or >=1 selector args) ---- */

static inline mrb_bool is_ascii_ws(uint8_t c, mrb_bool allow_nul) {
  return c == ' ' || (c >= '\t' && c <= '\r') || (allow_nul && c == '\0');
}

static mrb_value str_strip_impl(mrb_state *mrb, mrb_value self, mrb_bool left, mrb_bool right, mrb_bool bang) {
  mrb_int argc = mrb_get_argc(mrb);
  const mrb_value *argv = mrb_get_argv(mrb);
  if (bang) str_check_frozen(mrb, self);

  const uint8_t *p = (const uint8_t *)RSTRING_PTR(self);
  mrb_int len = RSTRING_LEN(self);
  mrb_int beg = 0, end = len;
  mrb_bool single_byte = str_is_binary_enc(mrb, self);

  if (argc == 0) {
    if (left) {
      while (beg < end && is_ascii_ws(p[beg], TRUE)) beg++;
      /* MRI: lstrip! raises ArgumentError if first non-space byte is invalid UTF-8 */
      if (bang && !right && !single_byte && beg < end && p[beg] >= 0x80) {
        uint32_t cp;
        if (utf8_decode_one(p + beg, end - beg, &cp) < 0) {
          mrb_raise(mrb, E_ARGUMENT_ERROR, "invalid byte sequence in UTF-8");
        }
      }
    }
    if (right) {
      while (end > beg && is_ascii_ws(p[end - 1], TRUE)) end--;
      /* MRI: rstrip! raises Encoding::CompatibilityError if last non-space byte ends an invalid UTF-8 char */
      if (bang && !left && !single_byte && end > beg && p[end - 1] >= 0x80) {
        mrb_int start = end - 1;
        while (start > beg && (p[start] & 0xC0) == 0x80 && (end - start) < 4) start--;
        uint32_t cp;
        int n = utf8_decode_one(p + start, end - start, &cp);
        if (n < 0 || start + n != end) {
          mrb_value enc_cls = mrb_obj_value(mrb_class_get(mrb, "Encoding"));
          struct RClass *compat_err = mrb_class_get_under(mrb, mrb_class_ptr(enc_cls), "CompatibilityError");
          mrb_raise(mrb, compat_err, "invalid byte sequence in UTF-8");
        }
      }
    }
  } else {
    cset_group_t g;
    cset_group_build(mrb, self, argc, argv, &g);
    if (left) {
      while (beg < end) {
        uint32_t cp;
        int n = next_cp(p + beg, end - beg, g.single_byte, &cp);
        if (!cset_group_match(&g, cp)) break;
        beg += n;
      }
    }
    if (right) {
      if (g.single_byte || str_is_ascii_only(p + beg, end - beg)) {
        while (end > beg && g.ascii_mask[p[end - 1]]) end--;
      } else {
        /* Scan forward from beg to track last non-matching boundary */
        mrb_int i = beg, last_keep = beg;
        while (i < end) {
          uint32_t cp;
          int n = next_cp(p + i, end - i, FALSE, &cp);
          i += n;
          if (!cset_group_match(&g, cp)) last_keep = i;
        }
        end = last_keep;
      }
    }
    cset_group_free(mrb, &g);
  }

  if (bang) {
    if (beg == 0 && end == len) return mrb_nil_value();
    mrb_str_modify(mrb, mrb_str_ptr(self));
    mrb_int nlen = end - beg;
    if (beg > 0 && nlen > 0) memmove(RSTRING_PTR(self), RSTRING_PTR(self) + beg, nlen);
    mrb_str_resize(mrb, self, nlen);
    return self;
  }
  mrb_value res = mrb_str_new(mrb, (const char *)(p + beg), end - beg);
  str_copy_enc(mrb, self, res);
  return res;
}

static mrb_value str_lstrip_m(mrb_state *mrb, mrb_value self) { return str_strip_impl(mrb, self, TRUE, FALSE, FALSE); }
static mrb_value str_lstrip_bang_m(mrb_state *mrb, mrb_value self) { return str_strip_impl(mrb, self, TRUE, FALSE, TRUE); }
static mrb_value str_rstrip_m(mrb_state *mrb, mrb_value self) { return str_strip_impl(mrb, self, FALSE, TRUE, FALSE); }
static mrb_value str_rstrip_bang_m(mrb_state *mrb, mrb_value self) { return str_strip_impl(mrb, self, FALSE, TRUE, TRUE); }
static mrb_value str_strip_m(mrb_state *mrb, mrb_value self) { return str_strip_impl(mrb, self, TRUE, TRUE, FALSE); }
static mrb_value str_strip_bang_m(mrb_state *mrb, mrb_value self) { return str_strip_impl(mrb, self, TRUE, TRUE, TRUE); }

/* ---- String#<< & String#concat ---- */

static void str_append_one(mrb_state *mrb, mrb_value self, mrb_value arg) {
  if (mrb_string_p(arg)) {
    if (str_enc_fast_same(mrb, self, arg)) {
      mrb_str_cat(mrb, self, RSTRING_PTR(arg), RSTRING_LEN(arg));
      return;
    }
    mrb_value enc = mrb_funcall_id(mrb, mrb_obj_value(mrb->kernel_module), MRB_SYM(__oruby_enc_concat), 2, self, arg);
    mrb_str_cat(mrb, self, RSTRING_PTR(arg), RSTRING_LEN(arg));
    if (!mrb_nil_p(enc)) {
      mrb_funcall_id(mrb, self, MRB_SYM(force_encoding), 1, enc);
    }
    return;
  }
  if (mrb_integer_p(arg) || mrb_bigint_p(arg)) {
    if (mrb_bigint_p(arg)) {
      mrb_raisef(mrb, E_RANGE_ERROR, "%v out of char range", arg);
    }
    mrb_int cp = mrb_integer(arg);
    if (cp < 0) {
      mrb_raisef(mrb, E_RANGE_ERROR, "%i out of char range", cp);
    }
    mrb_value tag = mrb_iv_get(mrb, self, MRB_SYM(__encoding));
    if (mrb_nil_p(tag)) {
      if (RSTR_BINARY_P(mrb_str_ptr(self))) {
        if (cp > 0xFF) mrb_raisef(mrb, E_RANGE_ERROR, "%i out of char range", cp);
        char b = (char)cp;
        mrb_str_cat(mrb, self, &b, 1);
        return;
      }
      /* Default UTF-8 */
      if (cp > 0x10FFFF || (cp >= 0xD800 && cp <= 0xDFFF)) {
        mrb_raisef(mrb, E_RANGE_ERROR, "%i out of char range", cp);
      }
      char buf[4];
      int n = utf8_encode_one((uint32_t)cp, buf);
      mrb_str_cat(mrb, self, buf, n);
      return;
    }
    /* Tagged encoding (US-ASCII, ISO-8859-*, UTF-16*, etc.) */
    mrb_value nv = mrb_funcall_id(mrb, tag, MRB_SYM(name), 0);
    if (mrb_string_p(nv) && RSTRING_LEN(nv) == 8 && memcmp(RSTRING_PTR(nv), "US-ASCII", 8) == 0) {
      if (cp > 0xFF) mrb_raisef(mrb, E_RANGE_ERROR, "%i out of char range", cp);
      char b = (char)cp;
      mrb_str_cat(mrb, self, &b, 1);
      if (cp >= 0x80) {
        mrb_value enc_cls = mrb_obj_value(mrb_class_get(mrb, "Encoding"));
        mrb_value bin = mrb_const_get(mrb, enc_cls, mrb_intern_lit(mrb, "BINARY"));
        mrb_funcall_id(mrb, self, MRB_SYM(force_encoding), 1, bin);
      }
      return;
    }
    /* Delegate other encodings to Integer#chr(self.encoding) */
    mrb_value ch = mrb_funcall_id(mrb, arg, MRB_SYM(chr), 1, tag);
    mrb_str_cat(mrb, self, RSTRING_PTR(ch), RSTRING_LEN(ch));
    return;
  }
  /* Non-String, non-Integer: implicit #to_str */
  mrb_value s = str_implicit_to_str(mrb, arg);
  str_append_one(mrb, self, s);
}

static mrb_value str_lshift_m(mrb_state *mrb, mrb_value self) {
  mrb_value arg;
  mrb_get_args(mrb, "o", &arg);
  str_check_frozen(mrb, self);
  /* Fast path: appending a plain String with same encoding */
  if (mrb_string_p(arg) && str_enc_fast_same(mrb, self, arg)) {
    mrb_str_cat(mrb, self, RSTRING_PTR(arg), RSTRING_LEN(arg));
    return self;
  }
  str_append_one(mrb, self, arg);
  return self;
}

static mrb_value str_concat_m(mrb_state *mrb, mrb_value self) {
  mrb_int argc = mrb_get_argc(mrb);
  const mrb_value *argv = mrb_get_argv(mrb);
  str_check_frozen(mrb, self);
  if (argc == 0) return self;
  if (argc == 1) {
    mrb_value arg = argv[0];
    if (mrb_string_p(arg) && str_enc_fast_same(mrb, self, arg)) {
      mrb_str_cat(mrb, self, RSTRING_PTR(arg), RSTRING_LEN(arg));
      return self;
    }
    str_append_one(mrb, self, arg);
    return self;
  }
  /* Multi-arg: snapshot self if self appears among args, and copy argv since conversions may move stack */
  mrb_value snapshot = mrb_nil_value();
  mrb_value args_ary = mrb_ary_new_capa(mrb, argc);
  for (mrb_int i = 0; i < argc; i++) {
    mrb_value a = argv[i];
    if (mrb_obj_eq(mrb, self, a)) {
      if (mrb_nil_p(snapshot)) {
        snapshot = mrb_str_dup(mrb, self);
        str_copy_enc(mrb, self, snapshot);
      }
      a = snapshot;
    }
    mrb_ary_push(mrb, args_ary, a);
  }
  for (mrb_int i = 0; i < argc; i++) {
    str_append_one(mrb, self, RARRAY_PTR(args_ary)[i]);
  }
  return self;
}

/* ---- String#byteindex / #byterindex wrapper ---- */

static mrb_func_t orig_byteindex;
static mrb_func_t orig_byterindex;

static mrb_value str_byteidx_wrap(mrb_state *mrb, mrb_value self, mrb_func_t orig) {
  mrb_int argc = mrb_get_argc(mrb);
  if (argc >= 1) {
    mrb_value pat = mrb_get_argv(mrb)[0];
    struct RClass *re_cls = mrb_class_defined(mrb, "Regexp") ? mrb_class_get(mrb, "Regexp") : NULL;
    if (mrb_string_p(pat)) {
      str_check_compat(mrb, self, pat);
    } else if (re_cls && mrb_obj_is_kind_of(mrb, pat, re_cls)) {
      mrb_value enc_cls = mrb_obj_value(mrb_class_get(mrb, "Encoding"));
      mrb_value c = mrb_funcall_id(mrb, enc_cls, mrb_intern_lit(mrb, "compatible?"), 2, self, pat);
      if (mrb_nil_p(c)) {
        struct RClass *compat_err = mrb_class_get_under(mrb, mrb_class_ptr(enc_cls), "CompatibilityError");
        mrb_value re_enc = mrb_funcall_id(mrb, pat, MRB_SYM(encoding), 0);
        mrb_value s_enc = mrb_funcall_id(mrb, self, MRB_SYM(encoding), 0);
        mrb_raisef(mrb, compat_err, "incompatible encoding regexp match (%v regexp with %v string)", re_enc, s_enc);
      }
    } else {
      mrb_value s = str_implicit_to_str(mrb, pat);
      str_check_compat(mrb, self, s);
      ((mrb_value *)mrb_get_argv(mrb))[0] = s;
    }
    if (argc >= 2) {
      mrb_value off = mrb_get_argv(mrb)[1];
      if (!mrb_integer_p(off)) {
        mrb_value iv = mrb_funcall_id(mrb, mrb_obj_value(mrb->kernel_module), mrb_intern_lit(mrb, "__to_int"), 1, off);
        ((mrb_value *)mrb_get_argv(mrb))[1] = iv;
      }
    }
  }
  return orig(mrb, self);
}

static mrb_value str_byteindex_m(mrb_state *mrb, mrb_value self) { return str_byteidx_wrap(mrb, self, orig_byteindex); }
static mrb_value str_byterindex_m(mrb_state *mrb, mrb_value self) { return str_byteidx_wrap(mrb, self, orig_byterindex); }

/* ---- Math.expm1, Math.log1p, Math.log2, Math.ldexp ---- */

static double math_to_float(mrb_state *mrb, mrb_value v) {
  if (mrb_float_p(v)) return mrb_float(v);
  if (mrb_integer_p(v)) return (double)mrb_integer(v);
  if (mrb_bigint_p(v)) {
    mrb_value f = mrb_funcall_id(mrb, v, MRB_SYM(to_f), 0);
    return mrb_float(f);
  }
  if (!mrb_nil_p(v) && !mrb_string_p(v) && mrb_respond_to(mrb, v, MRB_SYM(to_f))) {
    mrb_value f = mrb_funcall_id(mrb, v, MRB_SYM(to_f), 0);
    if (mrb_float_p(f)) return mrb_float(f);
  }
  const char *cname = mrb_nil_p(v) ? "nil" : mrb_obj_classname(mrb, v);
  mrb_raisef(mrb, E_TYPE_ERROR, "can't convert %s into Float", cname);
  return 0.0;
}

static void math_domain_error(mrb_state *mrb, const char *name) {
  struct RClass *math_mod = mrb_module_get(mrb, "Math");
  struct RClass *dom_err = mrb_class_get_under(mrb, math_mod, "DomainError");
  mrb_raisef(mrb, dom_err, "Numerical argument is out of domain - %s", name);
}

static mrb_value math_expm1_m(mrb_state *mrb, mrb_value self) {
  mrb_value x;
  mrb_get_args(mrb, "o", &x);
  return mrb_float_value(mrb, expm1(math_to_float(mrb, x)));
}

static mrb_value math_log1p_m(mrb_state *mrb, mrb_value self) {
  mrb_value x;
  mrb_get_args(mrb, "o", &x);
  double d = math_to_float(mrb, x);
  if (d < -1.0) math_domain_error(mrb, "log1p");
  return mrb_float_value(mrb, log1p(d));
}

static mrb_value math_ldexp_m(mrb_state *mrb, mrb_value self) {
  mrb_value xv, ev;
  mrb_get_args(mrb, "oo", &xv, &ev);
  double d = math_to_float(mrb, xv);
  mrb_int e;
  if (mrb_integer_p(ev)) {
    e = mrb_integer(ev);
  } else {
    mrb_value iv = mrb_funcall_id(mrb, mrb_obj_value(mrb->kernel_module), mrb_intern_lit(mrb, "__to_int"), 1, ev);
    e = mrb_integer(iv);
  }
  return mrb_float_value(mrb, ldexp(d, (int)e));
}

static mrb_value math_log2_m(mrb_state *mrb, mrb_value self) {
  mrb_value x;
  mrb_get_args(mrb, "o", &x);
  if (mrb_bigint_p(x)) {
    /* BigInt: avoid Infinity overflow past 2**1024 via bit_length + top 53 bits */
    mrb_value cmp0 = mrb_funcall_id(mrb, x, MRB_OPSYM(lt), 1, mrb_fixnum_value(0));
    if (mrb_test(cmp0)) math_domain_error(mrb, "log2");
    mrb_value blv = mrb_funcall_id(mrb, x, mrb_intern_lit(mrb, "bit_length"), 0);
    mrb_int bl = mrb_integer(blv);
    if (bl > 1000) {
      mrb_int shift = bl - 54;
      mrb_value top = mrb_funcall_id(mrb, x, MRB_OPSYM(rshift), 1, mrb_fixnum_value(shift));
      double mant = mrb_integer_p(top) ? (double)mrb_integer(top) : mrb_float(mrb_funcall_id(mrb, top, MRB_SYM(to_f), 0));
      return mrb_float_value(mrb, log2(mant) + (double)shift);
    }
  }
  double d = math_to_float(mrb, x);
  if (d < 0.0) math_domain_error(mrb, "log2");
  return mrb_float_value(mrb, log2(d));
}

/* ---- Install ---- */

static mrb_value kernel_dstr_enc_m(mrb_state *mrb, mrb_value self) {
  mrb_value s, enc;
  mrb_get_args(mrb, "oo", &s, &enc);
  if (mrb_string_p(s) && str_is_ascii_only((const uint8_t *)RSTRING_PTR(s), RSTRING_LEN(s))) {
    mrb_funcall_id(mrb, s, MRB_SYM(force_encoding), 1, enc);
  }
  return s;
}

void _orb_string_install(mrb_state *mrb) {
  struct RClass *sc = mrb->string_class;
  mrb_define_class_method(mrb, mrb->kernel_module, "__oruby_dstr_enc", kernel_dstr_enc_m, MRB_ARGS_REQ(2));
  mrb_define_method(mrb, sc, "count", str_count_m, MRB_ARGS_REQ(1) | MRB_ARGS_REST());
  mrb_define_method(mrb, sc, "delete", str_delete_m, MRB_ARGS_REQ(1) | MRB_ARGS_REST());
  mrb_define_method(mrb, sc, "delete!", str_delete_bang_m, MRB_ARGS_REQ(1) | MRB_ARGS_REST());
  mrb_define_method(mrb, sc, "squeeze", str_squeeze_m, MRB_ARGS_ANY());
  mrb_define_method(mrb, sc, "squeeze!", str_squeeze_bang_m, MRB_ARGS_ANY());
  mrb_define_method(mrb, sc, "tr", str_tr_m, MRB_ARGS_ANY());
  mrb_define_method(mrb, sc, "tr!", str_tr_bang_m, MRB_ARGS_ANY());
  mrb_define_method(mrb, sc, "tr_s", str_tr_s_m, MRB_ARGS_REQ(2));
  mrb_define_method(mrb, sc, "tr_s!", str_tr_s_bang_m, MRB_ARGS_REQ(2));
  mrb_define_method(mrb, sc, "lstrip", str_lstrip_m, MRB_ARGS_ANY());
  mrb_define_method(mrb, sc, "lstrip!", str_lstrip_bang_m, MRB_ARGS_ANY());
  mrb_define_method(mrb, sc, "rstrip", str_rstrip_m, MRB_ARGS_ANY());
  mrb_define_method(mrb, sc, "rstrip!", str_rstrip_bang_m, MRB_ARGS_ANY());
  mrb_define_method(mrb, sc, "strip", str_strip_m, MRB_ARGS_ANY());
  mrb_define_method(mrb, sc, "strip!", str_strip_bang_m, MRB_ARGS_ANY());
  mrb_define_method(mrb, sc, "<<", str_lshift_m, MRB_ARGS_REQ(1));
  mrb_define_method(mrb, sc, "concat", str_concat_m, MRB_ARGS_ANY());

  /* Wrap byteindex / byterindex if C builtins */
  {
    struct RClass *o = sc;
    mrb_method_t m = mrb_method_search_vm(mrb, &o, mrb_intern_lit(mrb, "byteindex"));
    if (!MRB_METHOD_UNDEF_P(m) && MRB_METHOD_CFUNC_P(m) && MRB_METHOD_CFUNC(m) != str_byteindex_m) {
      orig_byteindex = MRB_METHOD_CFUNC(m);
      mrb_define_method(mrb, sc, "byteindex", str_byteindex_m, MRB_ARGS_ARG(1, 1));
    }
  }
  {
    struct RClass *o = sc;
    mrb_method_t m = mrb_method_search_vm(mrb, &o, mrb_intern_lit(mrb, "byterindex"));
    if (!MRB_METHOD_UNDEF_P(m) && MRB_METHOD_CFUNC_P(m) && MRB_METHOD_CFUNC(m) != str_byterindex_m) {
      orig_byterindex = MRB_METHOD_CFUNC(m);
      mrb_define_method(mrb, sc, "byterindex", str_byterindex_m, MRB_ARGS_ARG(1, 1));
    }
  }

  /* Math module */
  if (mrb_class_defined(mrb, "Math")) {
    struct RClass *mm = mrb_module_get(mrb, "Math");
    mrb_define_module_function(mrb, mm, "expm1", math_expm1_m, MRB_ARGS_REQ(1));
    mrb_define_module_function(mrb, mm, "log1p", math_log1p_m, MRB_ARGS_REQ(1));
    mrb_define_module_function(mrb, mm, "log2", math_log2_m, MRB_ARGS_REQ(1));
    mrb_define_module_function(mrb, mm, "ldexp", math_ldexp_m, MRB_ARGS_REQ(2));
  }
}
