// Prism-based scan for ORuby's source rewriter (prism.go): parses Ruby source
// with the Prism parser linked into libmruby and records the nodes mruby's code
// generator rejects, with the byte spans the Go side needs to rewrite them.
#include <stdlib.h>
#include <string.h>
#include <mruby.h>
#include "prism.h"
#include "go-prism.h"

// mruby-compiler's context sets the state Prism's allocator uses
void *mrc_ccontext_new(mrb_state *mrb);
void mrc_ccontext_free(void *c);

typedef struct {
  orb_rec *recs;
  int n, cap;
  bool frozen; /* frozen_string_literal: true for this source */
  bool nonutf8; /* source encoding other than UTF-8: literals are tagged */
} orb_scan;

static orb_rec *
add(orb_scan *sc, int kind)
{
  if (sc->n == sc->cap) {
    sc->cap = sc->cap ? sc->cap * 2 : 64;
    sc->recs = (orb_rec *)realloc(sc->recs, sizeof(orb_rec) * sc->cap);
  }
  orb_rec *r = &sc->recs[sc->n++];
  memset(r, 0, sizeof(*r));
  r->kind = kind;
  for (int i = 0; i < ORB_SPANS; i++) r->s[i] = r->e[i] = -1;
  return r;
}

static void
span(orb_rec *r, int i, const pm_node_t *n)
{
  if (!n) return;
  r->s[i] = (int32_t)n->location.start;
  r->e[i] = (int32_t)(n->location.start + n->location.length);
}

static void
lspan(orb_rec *r, int i, pm_location_t loc)
{
  if (loc.length == 0 && loc.start == 0) return;
  r->s[i] = (int32_t)loc.start;
  r->e[i] = (int32_t)(loc.start + loc.length);
}

static bool
safe_nav_target(const pm_node_t *n)
{
  return n && PM_NODE_TYPE(n) == PM_CALL_TARGET_NODE && PM_NODE_FLAG_P(n, PM_CALL_NODE_FLAGS_SAFE_NAVIGATION);
}

static bool
is_dsym(const pm_node_t *n)
{
  return n && PM_NODE_TYPE(n) == PM_INTERPOLATED_SYMBOL_NODE;
}

static void
cpath_write(orb_scan *sc, int kind, const pm_node_t *node, const pm_constant_path_node_t *t, const pm_node_t *value, pm_location_t op)
{
  orb_rec *r = add(sc, kind);
  span(r, 0, node);
  span(r, 1, t->parent);
  lspan(r, 2, t->name_loc);
  span(r, 3, value);
  lspan(r, 4, op);
}

static pm_visit_result_t visit(const pm_node_t *node, void *data);

static void
visit_child(const pm_node_t *node, void *data)
{
  if (node) pm_visit_node(node, visit, data);
}

// the embedded code of "a" "b#{x}": adjacent literals are parts of one
// literal, so only the code inside them is visited
static void
visit_string_parts(const pm_interpolated_string_node_t *is, void *data)
{
  for (size_t i = 0; i < is->parts.size; i++) {
    const pm_node_t *part = is->parts.nodes[i];
    switch (PM_NODE_TYPE(part)) {
    case PM_STRING_NODE:
      break;
    case PM_INTERPOLATED_STRING_NODE:
      visit_string_parts((const pm_interpolated_string_node_t *)part, data);
      break;
    default:
      visit_child(part, data);
    }
  }
}

static pm_visit_result_t
visit(const pm_node_t *node, void *data)
{
  orb_scan *sc = (orb_scan *)data;
  orb_rec *r;
  switch (PM_NODE_TYPE(node)) {
  case PM_DEFINED_NODE: {
    // defined?(A::B ||= v) is answered without generating the assignment
    const pm_node_t *v = ((const pm_defined_node_t *)node)->value;
    switch (v ? PM_NODE_TYPE(v) : 0) {
    case PM_CONSTANT_PATH_OR_WRITE_NODE:
    case PM_CONSTANT_PATH_AND_WRITE_NODE:
    case PM_CONSTANT_PATH_OPERATOR_WRITE_NODE:
      return PM_VISIT_SKIP;
    default:
      break;
    }
    break;
  }
  case PM_NO_KEYWORDS_PARAMETER_NODE:
    r = add(sc, ORB_NO_KEYWORDS);
    span(r, 0, node);
    break;
  case PM_ALIAS_METHOD_NODE: {
    const pm_alias_method_node_t *a = (const pm_alias_method_node_t *)node;
    if (is_dsym(a->new_name) || is_dsym(a->old_name)) {
      r = add(sc, ORB_ALIAS_DYNAMIC);
      span(r, 0, node);
      span(r, 1, a->new_name);
      span(r, 2, a->old_name);
    }
    break;
  }
  case PM_UNDEF_NODE: {
    const pm_undef_node_t *u = (const pm_undef_node_t *)node;
    bool dyn = false;
    for (size_t i = 0; i < u->names.size; i++) dyn = dyn || is_dsym(u->names.nodes[i]);
    if (dyn) {
      r = add(sc, ORB_UNDEF_DYNAMIC);
      span(r, 0, node);
      for (size_t i = 0; i < u->names.size; i++) {
        orb_rec *nr = add(sc, ORB_LIST_ITEM);
        span(nr, 0, u->names.nodes[i]);
      }
    }
    break;
  }
  case PM_RESCUE_NODE: {
    const pm_rescue_node_t *rn = (const pm_rescue_node_t *)node;
    if (safe_nav_target(rn->reference)) {
      r = add(sc, ORB_RESCUE_SAFE_NAV);
      span(r, 0, rn->reference);
    }
    break;
  }
  case PM_FOR_NODE: {
    const pm_for_node_t *f = (const pm_for_node_t *)node;
    if (safe_nav_target(f->index)) {
      r = add(sc, ORB_FOR_SAFE_NAV);
      span(r, 0, f->index);
      span(r, 1, f->collection);
      lspan(r, 2, f->do_keyword_loc);
    }
    break;
  }
  case PM_PRE_EXECUTION_NODE: {
    const pm_pre_execution_node_t *p = (const pm_pre_execution_node_t *)node;
    r = add(sc, ORB_BEGIN_BLOCK);
    lspan(r, 0, p->keyword_loc);
    lspan(r, 1, p->opening_loc);
    lspan(r, 2, p->closing_loc);
    break;
  }
  case PM_FLIP_FLOP_NODE: {
    const pm_flip_flop_node_t *ff = (const pm_flip_flop_node_t *)node;
    if (ff->left && ff->right) {
      r = add(sc, ORB_FLIP_FLOP);
      r->flags = PM_NODE_FLAG_P(node, PM_RANGE_FLAGS_EXCLUDE_END) ? 1 : 0;
      if (PM_NODE_TYPE(ff->left) == PM_INTEGER_NODE) r->flags |= 2;
      if (PM_NODE_TYPE(ff->right) == PM_INTEGER_NODE) r->flags |= 4;
      span(r, 0, node);
      span(r, 1, ff->left);
      span(r, 2, ff->right);
    }
    break;
  }
  case PM_CONSTANT_PATH_OR_WRITE_NODE: {
    const pm_constant_path_or_write_node_t *w = (const pm_constant_path_or_write_node_t *)node;
    cpath_write(sc, ORB_CPATH_OR_WRITE, node, w->target, w->value, w->operator_loc);
    break;
  }
  case PM_CONSTANT_PATH_AND_WRITE_NODE: {
    const pm_constant_path_and_write_node_t *w = (const pm_constant_path_and_write_node_t *)node;
    cpath_write(sc, ORB_CPATH_AND_WRITE, node, w->target, w->value, w->operator_loc);
    break;
  }
  case PM_CONSTANT_PATH_OPERATOR_WRITE_NODE: {
    const pm_constant_path_operator_write_node_t *w = (const pm_constant_path_operator_write_node_t *)node;
    cpath_write(sc, ORB_CPATH_OP_WRITE, node, w->target, w->value, w->binary_operator_loc);
    break;
  }
  case PM_ALIAS_GLOBAL_VARIABLE_NODE: {
    const pm_alias_global_variable_node_t *a = (const pm_alias_global_variable_node_t *)node;
    r = add(sc, ORB_ALIAS_GVAR);
    span(r, 0, node);
    span(r, 1, a->new_name);
    span(r, 2, a->old_name);
    break;
  }
  case PM_GLOBAL_VARIABLE_READ_NODE:
    r = add(sc, ORB_GVAR_REF);
    span(r, 0, node);
    break;
  case PM_GLOBAL_VARIABLE_WRITE_NODE:
    r = add(sc, ORB_GVAR_REF);
    lspan(r, 0, ((const pm_global_variable_write_node_t *)node)->name_loc);
    span(r, 1, node);
    span(r, 2, ((const pm_global_variable_write_node_t *)node)->value);
    break;
  case PM_GLOBAL_VARIABLE_OPERATOR_WRITE_NODE:
    r = add(sc, ORB_GVAR_REF);
    lspan(r, 0, ((const pm_global_variable_operator_write_node_t *)node)->name_loc);
    break;
  case PM_GLOBAL_VARIABLE_OR_WRITE_NODE:
    r = add(sc, ORB_GVAR_REF);
    lspan(r, 0, ((const pm_global_variable_or_write_node_t *)node)->name_loc);
    break;
  case PM_GLOBAL_VARIABLE_AND_WRITE_NODE:
    r = add(sc, ORB_GVAR_REF);
    lspan(r, 0, ((const pm_global_variable_and_write_node_t *)node)->name_loc);
    break;
  case PM_GLOBAL_VARIABLE_TARGET_NODE:
    r = add(sc, ORB_GVAR_REF);
    span(r, 0, node);
    break;
  case PM_POST_EXECUTION_NODE: {
    const pm_post_execution_node_t *p = (const pm_post_execution_node_t *)node;
    r = add(sc, ORB_END_BLOCK);
    lspan(r, 0, p->keyword_loc);
    lspan(r, 1, p->opening_loc);
    lspan(r, 2, p->closing_loc);
    break;
  }
  case PM_STRING_NODE: {
    const pm_string_node_t *sn = (const pm_string_node_t *)node;
    // %w elements have no opening; they can't take a method call
    if (sc->nonutf8 && sn->opening_loc.length > 0) {
      r = add(sc, ORB_LITERAL_ENCODING);
      span(r, 0, node);
      if (PM_NODE_FLAG_P(node, PM_STRING_FLAGS_FORCED_UTF8_ENCODING)) r->flags = 1;
      else if (PM_NODE_FLAG_P(node, PM_STRING_FLAGS_FORCED_BINARY_ENCODING)) r->flags = 2;
    }
    if (sc->frozen && PM_NODE_FLAG_P(node, PM_STRING_FLAGS_FROZEN) && sn->opening_loc.length > 0) {
      r = add(sc, ORB_FROZEN_STRING);
      span(r, 0, node);
    }
    break;
  }
  case PM_INTERPOLATED_STRING_NODE: {
    // the parts of "a" "b" or "a#{x}" are not literals of their own
    const pm_interpolated_string_node_t *is = (const pm_interpolated_string_node_t *)node;
    if (sc->nonutf8 && is->opening_loc.length > 0) {
      r = add(sc, ORB_LITERAL_ENCODING);
      span(r, 0, node);
    }
    if (sc->frozen && PM_NODE_FLAG_P(node, PM_INTERPOLATED_STRING_NODE_FLAGS_FROZEN) && is->opening_loc.length > 0) {
      r = add(sc, ORB_FROZEN_STRING);
      span(r, 0, node);
    }
    visit_string_parts(is, data);
    return PM_VISIT_SKIP;
  }
  // patterns take literals only: leave their strings alone
  case PM_IN_NODE:
    visit_child((const pm_node_t *)((const pm_in_node_t *)node)->statements, data);
    return PM_VISIT_SKIP;
  case PM_MATCH_PREDICATE_NODE:
    visit_child(((const pm_match_predicate_node_t *)node)->value, data);
    return PM_VISIT_SKIP;
  case PM_MATCH_REQUIRED_NODE:
    visit_child(((const pm_match_required_node_t *)node)->value, data);
    return PM_VISIT_SKIP;
  case PM_MULTI_WRITE_NODE: {
    // a, b = obj: MRI converts a single value with #to_ary
    const pm_node_t *v = ((const pm_multi_write_node_t *)node)->value;
    if (v && PM_NODE_TYPE(v) != PM_ARRAY_NODE && PM_NODE_TYPE(v) != PM_SPLAT_NODE) {
      r = add(sc, ORB_MASGN_VALUE);
      span(r, 0, v);
      span(r, 1, node);
    }
    break;
  }
  case PM_SOURCE_ENCODING_NODE:
    r = add(sc, ORB_SOURCE_ENCODING);
    span(r, 0, node);
    break;
  case PM_INTEGER_NODE:
    if (node->location.length > 255) {
      r = add(sc, ORB_BIG_INTEGER);
      span(r, 0, node);
    }
    break;
  default:
    break;
  }
  return PM_VISIT_DESCEND;
}

orb_rec *
_oruby_prism_scan(mrb_state *mrb, const char *src, size_t len, const orb_scan_opts *opts, int *count, int *errors, char *encname, size_t encname_len)
{
  static mrb_state *scan_mrb = NULL; // allocator state when no mrb is given
  orb_scan sc = {0};
  if (!mrb) {
    if (!scan_mrb) scan_mrb = mrb_open_core();
    mrb = scan_mrb;
  }
  void *cc = mrc_ccontext_new(mrb);
  pm_arena_t *arena = pm_arena_new();
  pm_options_t *options = NULL;
  if (opts && (opts->frozen >= 0 || (opts->encoding && opts->encoding[0]))) {
    options = pm_options_new();
    if (opts->frozen >= 0) pm_options_frozen_string_literal_set(options, opts->frozen != 0);
    if (opts->encoding && opts->encoding[0]) pm_options_encoding_set(options, opts->encoding);
  }
  pm_parser_t *parser = pm_parser_new(arena, (const uint8_t *)src, len, options);
  pm_node_t *root = pm_parse(parser);
  *errors = (int)pm_parser_errors_size(parser);
  if (encname && encname_len > 0) {
    const char *en = pm_parser_encoding_name(parser);
    strncpy(encname, en ? en : "", encname_len - 1);
    encname[encname_len - 1] = 0;
  }
  sc.frozen = pm_parser_frozen_string_literal(parser) > 0;
  {
    const char *en = pm_parser_encoding_name(parser);
    sc.nonutf8 = en && strcmp(en, "UTF-8") != 0;
  }
  if (root && *errors == 0) pm_visit_node(root, visit, &sc);
  pm_parser_free(parser);
  pm_arena_free(arena);
  if (options) pm_options_free(options);
  mrc_ccontext_free(cc);
  *count = sc.n;
  return sc.recs;
}
