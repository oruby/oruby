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
  bool relits;  /* regexp literals get a per-site object (ORB_REGEXP_LITERAL) */
  bool regexp_ranges;
  bool eval;    /* eval'd code: anonymous forwarding arguments are rewritten (ORB_ANON_ARG) */
  const pm_node_t *keep_re; /* the literal of a named-capture =~ (assigns locals) */
  const pm_node_t *defined_target; /* the direct operand of defined? */
  /* warnings MRI's compiler gives that Prism does not (ORB_PARSE_WARN) */
  struct { int32_t s, e; const char *msg; int verbose; } xw[16];
  int nxw;
  const char *src;                 /* the source, the locations' base */
  const pm_node_t *alias_dsym[8];  /* dsyms ORB_ALIAS/UNDEF_DYNAMIC render themselves */
  int nalias_dsym;
} orb_scan;

static bool is_dsym(const pm_node_t *n);

static void
keep_dsym(orb_scan *sc, const pm_node_t *n)
{
  if (is_dsym(n) && sc->nalias_dsym < 8) sc->alias_dsym[sc->nalias_dsym++] = n;
}

static void
compile_warn(orb_scan *sc, const pm_node_t *n, const char *msg, int verbose)
{
  if (sc->nxw >= 16) return;
  sc->xw[sc->nxw].s = (int32_t)n->location.start;
  sc->xw[sc->nxw].e = (int32_t)(n->location.start + n->location.length);
  sc->xw[sc->nxw].msg = msg;
  sc->xw[sc->nxw].verbose = verbose;
  sc->nxw++;
}

/* a return with an argument at the top level of a file (not in a method,
   block, lambda or class body): MRI ignores the argument and says so */
static pm_visit_result_t
toplevel_return_visit(const pm_node_t *node, void *data)
{
  switch (PM_NODE_TYPE(node)) {
  case PM_DEF_NODE: case PM_BLOCK_NODE: case PM_LAMBDA_NODE:
  case PM_CLASS_NODE: case PM_MODULE_NODE: case PM_SINGLETON_CLASS_NODE:
    return PM_VISIT_SKIP;
  case PM_RETURN_NODE:
    if (((const pm_return_node_t *)node)->arguments)
      compile_warn((orb_scan *)data, node, "argument of top-level return is ignored", 0);
    return PM_VISIT_DESCEND;
  default:
    return PM_VISIT_DESCEND;
  }
}

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

/* a range end MRI's compiler folds into a literal Range: none, nil, or a
   (signed) numeric literal */
static bool
static_range_end(const pm_node_t *n)
{
  if (!n) return true;
  switch (PM_NODE_TYPE(n)) {
  case PM_NIL_NODE: case PM_INTEGER_NODE: case PM_FLOAT_NODE:
    return true;
  default:
    return false;
  }
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

/* What mruby's codegen gets wrong in a pattern: a lambda in it (ORB_PAT_LAMBDA)
   and an anonymous ** after keys (ORB_PAT_ANON_REST) */
static void
walk_pattern(orb_scan *sc, const pm_node_t *n)
{
  if (!n) return;
  switch (PM_NODE_TYPE(n)) {
  case PM_LAMBDA_NODE: {
    orb_rec *r = add(sc, ORB_PAT_LAMBDA);
    span(r, 0, n);
    break;
  }
  case PM_IF_NODE: {
    const pm_statements_node_t *st = ((const pm_if_node_t *)n)->statements;
    if (st && st->body.size > 0) walk_pattern(sc, st->body.nodes[0]);
    break;
  }
  case PM_UNLESS_NODE: {
    const pm_statements_node_t *st = ((const pm_unless_node_t *)n)->statements;
    if (st && st->body.size > 0) walk_pattern(sc, st->body.nodes[0]);
    break;
  }
  case PM_ALTERNATION_PATTERN_NODE:
    walk_pattern(sc, ((const pm_alternation_pattern_node_t *)n)->left);
    walk_pattern(sc, ((const pm_alternation_pattern_node_t *)n)->right);
    break;
  case PM_CAPTURE_PATTERN_NODE:
    walk_pattern(sc, ((const pm_capture_pattern_node_t *)n)->value);
    break;
  case PM_ARRAY_PATTERN_NODE: {
    const pm_array_pattern_node_t *a = (const pm_array_pattern_node_t *)n;
    for (size_t i = 0; i < a->requireds.size; i++) walk_pattern(sc, a->requireds.nodes[i]);
    for (size_t i = 0; i < a->posts.size; i++) walk_pattern(sc, a->posts.nodes[i]);
    break;
  }
  case PM_FIND_PATTERN_NODE: {
    const pm_find_pattern_node_t *f = (const pm_find_pattern_node_t *)n;
    for (size_t i = 0; i < f->requireds.size; i++) walk_pattern(sc, f->requireds.nodes[i]);
    break;
  }
  case PM_HASH_PATTERN_NODE: {
    const pm_hash_pattern_node_t *h = (const pm_hash_pattern_node_t *)n;
    for (size_t i = 0; i < h->elements.size; i++) {
      const pm_node_t *e = h->elements.nodes[i];
      if (PM_NODE_TYPE(e) == PM_ASSOC_NODE) walk_pattern(sc, ((const pm_assoc_node_t *)e)->value);
    }
    if (h->rest && PM_NODE_TYPE(h->rest) == PM_ASSOC_SPLAT_NODE && !((const pm_assoc_splat_node_t *)h->rest)->value &&
        h->elements.size > 0) {
      const pm_node_t *last = h->elements.nodes[h->elements.size - 1];
      if (last->location.start + last->location.length <= h->rest->location.start) {
        orb_rec *r = add(sc, ORB_PAT_ANON_REST);
        r->s[0] = (int32_t)(last->location.start + last->location.length);
        r->e[0] = (int32_t)(h->rest->location.start + h->rest->location.length);
      }
    }
    break;
  }
  default:
    break;
  }
}

/* eval'd code's anonymous *, ** and ... arguments among l (ORB_ANON_ARG) */
static void
anon_args(orb_scan *sc, const pm_node_list_t *l)
{
  if (!sc->eval) return;
  for (size_t i = 0; i < l->size; i++) {
    const pm_node_t *a = l->nodes[i];
    int kind = 0;
    if (PM_NODE_TYPE(a) == PM_SPLAT_NODE && !((const pm_splat_node_t *)a)->expression) kind = 1;
    else if (PM_NODE_TYPE(a) == PM_ASSOC_SPLAT_NODE && !((const pm_assoc_splat_node_t *)a)->value) kind = 2;
    else if (PM_NODE_TYPE(a) == PM_FORWARDING_ARGUMENTS_NODE) kind = 4;
    if (kind) {
      orb_rec *r = add(sc, ORB_ANON_ARG);
      r->flags = kind;
      span(r, 0, a);
    }
  }
}

/* the nested targets ((a, b)) in t, outer ones first */
static void
nested_targets(orb_scan *sc, const pm_node_t *t, int32_t s1, int32_t e1)
{
  if (!t || PM_NODE_TYPE(t) != PM_MULTI_TARGET_NODE) return;
  const pm_multi_target_node_t *mt = (const pm_multi_target_node_t *)t;
  if (mt->lparen_loc.length == 0) return;
  orb_rec *r = add(sc, ORB_MASGN_NESTED);
  span(r, 0, t);
  r->s[1] = s1;
  r->e[1] = e1;
  r->s[2] = (int32_t)(mt->lparen_loc.start + mt->lparen_loc.length);
  r->e[2] = (int32_t)mt->rparen_loc.start;
  for (size_t i = 0; i < mt->lefts.size; i++) nested_targets(sc, mt->lefts.nodes[i], s1, e1);
  for (size_t i = 0; i < mt->rights.size; i++) nested_targets(sc, mt->rights.nodes[i], s1, e1);
}

/* the constant path targets (A::B) among t's targets: mruby's code
   generator rejects them in a multiple assignment and a for loop */
static void
cpath_targets(orb_scan *sc, const pm_node_t *t, int kind, int32_t s1, int32_t e1)
{
  if (!t) return;
  switch (PM_NODE_TYPE(t)) {
  case PM_CONSTANT_PATH_TARGET_NODE: {
    orb_rec *r = add(sc, kind);
    span(r, 0, t);
    r->s[1] = s1;
    r->e[1] = e1;
    break;
  }
  case PM_MULTI_TARGET_NODE: {
    const pm_multi_target_node_t *m = (const pm_multi_target_node_t *)t;
    for (size_t i = 0; i < m->lefts.size; i++) cpath_targets(sc, m->lefts.nodes[i], kind, s1, e1);
    cpath_targets(sc, m->rest, kind, s1, e1);
    for (size_t i = 0; i < m->rights.size; i++) cpath_targets(sc, m->rights.nodes[i], kind, s1, e1);
    break;
  }
  case PM_SPLAT_NODE:
    cpath_targets(sc, ((const pm_splat_node_t *)t)->expression, kind, s1, e1);
    break;
  default:
    break;
  }
}

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
    case PM_CALL_NODE: {
      const pm_node_t *previous = sc->defined_target;
      sc->defined_target = v;
      visit_child(v, data);
      sc->defined_target = previous;
      return PM_VISIT_SKIP;
    }
    default:
      break;
    }
    break;
  }
  case PM_CALL_NODE: {
    const pm_call_node_t *call = (const pm_call_node_t *)node;
    if (node != sc->defined_target && call->receiver &&
        PM_NODE_TYPE(call->receiver) == PM_STRING_NODE &&
        !PM_NODE_FLAG_P(node, PM_CALL_NODE_FLAGS_SAFE_NAVIGATION) &&
        (!call->arguments || call->arguments->arguments.size == 0) && !call->block &&
        call->message_loc.length == 6 &&
        memcmp(sc->src + call->message_loc.start, "freeze", 6) == 0) {
      r = add(sc, ORB_LITERAL_FREEZE);
      span(r, 0, node);
      span(r, 1, call->receiver);
    }
    break;
  }
  case PM_ALIAS_METHOD_NODE: {
    const pm_alias_method_node_t *a = (const pm_alias_method_node_t *)node;
    if (is_dsym(a->new_name) || is_dsym(a->old_name)) {
      r = add(sc, ORB_ALIAS_DYNAMIC);
      keep_dsym(sc, a->new_name);
      keep_dsym(sc, a->old_name);
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
        keep_dsym(sc, u->names.nodes[i]);
      }
    }
    break;
  }
  case PM_INTERPOLATED_SYMBOL_NODE: {
    /* :"a#{x}": checked as String#to_sym (mruby's OP_INTERN takes any
       bytes); a %I[] element is checked by its array */
    const pm_interpolated_symbol_node_t *is = (const pm_interpolated_symbol_node_t *)node;
    bool kept = false;
    for (int i = 0; i < sc->nalias_dsym; i++) kept = kept || sc->alias_dsym[i] == node;
    if (!kept && is->opening_loc.length == 2 && sc->src[is->opening_loc.start] == ':' && sc->src[is->opening_loc.start + 1] == '"' &&
        is->closing_loc.length == 1) {
      r = add(sc, ORB_DSYM);
      span(r, 0, node);
    }
    break;
  }
  case PM_RESCUE_NODE: {
    const pm_rescue_node_t *rn = (const pm_rescue_node_t *)node;
    /* rescue => obj.attr / obj[k] / a&.b: mruby's code generator assigns
       the receiver instead; the exception goes through a local */
    if (rn->reference && (PM_NODE_TYPE(rn->reference) == PM_CALL_TARGET_NODE ||
                          PM_NODE_TYPE(rn->reference) == PM_INDEX_TARGET_NODE)) {
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
    {
      int32_t at = f->statements ? (int32_t)f->statements->base.location.start : (int32_t)f->end_keyword_loc.start;
      cpath_targets(sc, f->index, ORB_FOR_CPATH, at, at);
    }
    break;
  }
  case PM_BLOCK_NODE: case PM_LAMBDA_NODE: {
    // a block of `it`: mruby gives its parameter no name, so it is marked
    // by a hidden local for Binding#implicit_parameters (go-mrb.c)
    const pm_node_t *params;
    pm_location_t open;
    if (PM_NODE_TYPE(node) == PM_BLOCK_NODE) {
      params = ((const pm_block_node_t *)node)->parameters;
      open = ((const pm_block_node_t *)node)->opening_loc;
    } else {
      params = ((const pm_lambda_node_t *)node)->parameters;
      open = ((const pm_lambda_node_t *)node)->opening_loc;
    }
    if (params && PM_NODE_TYPE(params) == PM_IT_PARAMETERS_NODE && open.length > 0) {
      r = add(sc, ORB_IT_BLOCK);
      r->s[0] = r->e[0] = (int32_t)(open.start + open.length);
    }
    // |a, b| given one object: MRI spreads it by #to_ary, mruby only an
    // Array; a nested |(a, b)| destructures by #to_ary too (prism.go)
    if (params && PM_NODE_TYPE(params) == PM_BLOCK_PARAMETERS_NODE) {
      const pm_block_parameters_node_t *bp = (const pm_block_parameters_node_t *)params;
      const pm_parameters_node_t *pp = bp->parameters;
      int lambda = PM_NODE_TYPE(node) == PM_LAMBDA_NODE;
      int32_t body_at = lambda ? (int32_t)(open.start + open.length)
                               : (int32_t)(bp->closing_loc.start + bp->closing_loc.length);
      if (pp && (lambda ? open.length > 0 : bp->closing_loc.length > 0)) {
        int nested = 0, named_rest = 1;
        size_t n = pp->requireds.size + pp->posts.size;
        for (size_t i = 0; i < pp->requireds.size; i++) nested |= PM_NODE_TYPE(pp->requireds.nodes[i]) == PM_MULTI_TARGET_NODE;
        for (size_t i = 0; i < pp->posts.size; i++) nested |= PM_NODE_TYPE(pp->posts.nodes[i]) == PM_MULTI_TARGET_NODE;
        if (pp->rest && PM_NODE_TYPE(pp->rest) == PM_REST_PARAMETER_NODE && ((const pm_rest_parameter_node_t *)pp->rest)->name == 0)
          named_rest = 0;
        int plain = pp->optionals.size == 0 && pp->keywords.size == 0 && !pp->keyword_rest && named_rest;
        int spread = !lambda && plain && pp->requireds.size > 0 && (n >= 2 || pp->rest);
        if (spread || nested) {
          r = add(sc, ORB_BLOCK_PARAMS);
          r->s[0] = r->e[0] = body_at;
          r->flags = spread;
          for (size_t i = 0; i < pp->requireds.size; i++) {
            const pm_node_t *p = pp->requireds.nodes[i];
            orb_rec *it = add(sc, ORB_LIST_ITEM);
            span(it, 0, p);
            it->flags = PM_NODE_TYPE(p) == PM_MULTI_TARGET_NODE ? 8 : 0;
          }
          if (pp->rest) {
            orb_rec *it = add(sc, ORB_LIST_ITEM);
            if (PM_NODE_TYPE(pp->rest) == PM_IMPLICIT_REST_NODE) {
              it->flags = 3;
            } else {
              const pm_rest_parameter_node_t *rp = (const pm_rest_parameter_node_t *)pp->rest;
              lspan(it, 0, rp->name_loc);
              it->flags = 1;
            }
          }
          for (size_t i = 0; i < pp->posts.size; i++) {
            const pm_node_t *p = pp->posts.nodes[i];
            orb_rec *it = add(sc, ORB_LIST_ITEM);
            span(it, 0, p);
            it->flags = 2 | (PM_NODE_TYPE(p) == PM_MULTI_TARGET_NODE ? 8 : 0);
          }
        }
      }
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
      /* one warning per Integer end, as MRI's */
      if (r->flags & 2) compile_warn(sc, node, "integer literal in flip-flop", 1);
      if (r->flags & 4) compile_warn(sc, node, "integer literal in flip-flop", 1);
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
      int has_embed = 0, has_nonascii_lit = 0;
      for (size_t i = 0; i < is->parts.size; i++) {
        const pm_node_t *pt = is->parts.nodes[i];
        if (pt->type == PM_STRING_NODE) {
          const pm_string_node_t *ps = (const pm_string_node_t *)pt;
          const uint8_t *bs = pm_string_source(&ps->unescaped);
          size_t bl = pm_string_length(&ps->unescaped);
          for (size_t j = 0; j < bl; j++) {
            if (bs[j] >= 0x80) { has_nonascii_lit = 1; break; }
          }
        } else {
          has_embed = 1;
        }
      }
      r = add(sc, ORB_LITERAL_ENCODING);
      span(r, 0, node);
      if (has_embed && !has_nonascii_lit) r->flags = 4;
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
    walk_pattern(sc, ((const pm_in_node_t *)node)->pattern);
    visit_child((const pm_node_t *)((const pm_in_node_t *)node)->statements, data);
    return PM_VISIT_SKIP;
  case PM_MATCH_PREDICATE_NODE:
    walk_pattern(sc, ((const pm_match_predicate_node_t *)node)->pattern);
    visit_child(((const pm_match_predicate_node_t *)node)->value, data);
    return PM_VISIT_SKIP;
  case PM_MATCH_REQUIRED_NODE:
    walk_pattern(sc, ((const pm_match_required_node_t *)node)->pattern);
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
    {
      const pm_multi_write_node_t *m = (const pm_multi_write_node_t *)node;
      int32_t s1 = (int32_t)node->location.start, e1 = (int32_t)(node->location.start + node->location.length);
      /* (a, b), c = obj, 1: MRI converts a nested target's value with
         #to_ary, mruby does not; nested targets are assigned after, outer
         ones first, then the constant paths among them */
      for (size_t i = 0; i < m->lefts.size; i++) nested_targets(sc, m->lefts.nodes[i], s1, e1);
      for (size_t i = 0; i < m->rights.size; i++) nested_targets(sc, m->rights.nodes[i], s1, e1);
      for (size_t i = 0; i < m->lefts.size; i++) cpath_targets(sc, m->lefts.nodes[i], ORB_MASGN_CPATH, s1, e1);
      cpath_targets(sc, m->rest, ORB_MASGN_CPATH, s1, e1);
      for (size_t i = 0; i < m->rights.size; i++) cpath_targets(sc, m->rights.nodes[i], ORB_MASGN_CPATH, s1, e1);
    }
    break;
  }

  case PM_MATCH_WRITE_NODE: {
    /* /(?<name>..)/ =~ s assigns the captures to locals: mruby does that for
       a literal receiver only, so the literal stays */
    const pm_call_node_t *call = ((const pm_match_write_node_t *)node)->call;
    if (call) sc->keep_re = call->receiver;
    break;
  }
  case PM_REGULAR_EXPRESSION_NODE:
    if (sc->regexp_ranges) {
      const pm_regular_expression_node_t *re = (const pm_regular_expression_node_t *)node;
      r = add(sc, ORB_REGEXP_RANGE);
      lspan(r, 0, re->content_loc);
      r->flags = (node->flags & PM_REGULAR_EXPRESSION_FLAGS_EXTENDED) != 0;
    }
    if (sc->relits && node != sc->keep_re) {
      r = add(sc, ORB_REGEXP_LITERAL);
      span(r, 0, node);
    }
    break;
  case PM_ARRAY_NODE: {
    // [a, k: v]: mruby's code generator drops a brace-less trailing Hash
    const pm_node_list_t *el = &((const pm_array_node_t *)node)->elements;
    anon_args(sc, el);
    {
      const pm_array_node_t *an = (const pm_array_node_t *)node;
      bool interp = false;
      for (size_t i = 0; i < el->size; i++) interp = interp || is_dsym(el->nodes[i]);
      if (interp && an->opening_loc.length == 3 && sc->src[an->opening_loc.start] == '%' && sc->src[an->opening_loc.start + 1] == 'I') {
        r = add(sc, ORB_DSYM);
        r->flags = 1;
        span(r, 0, node);
        r->s[1] = (int32_t)an->opening_loc.start;
        r->e[1] = r->s[1] + 2;
      }
    }
    if (el->size > 0 && PM_NODE_TYPE(el->nodes[el->size - 1]) == PM_KEYWORD_HASH_NODE) {
      const pm_keyword_hash_node_t *kh = (const pm_keyword_hash_node_t *)el->nodes[el->size - 1];
      r = add(sc, ORB_ARRAY_KWHASH);
      span(r, 0, el->nodes[el->size - 1]);
      /* [**h]: no element at all when h is empty, as MRI's */
      r->flags = 1;
      for (size_t i = 0; i < kh->elements.size; i++) {
        if (PM_NODE_TYPE(kh->elements.nodes[i]) != PM_ASSOC_SPLAT_NODE) r->flags = 0;
      }
    }
    break;
  }
  case PM_ASSOC_SPLAT_NODE: {
    // f(**obj), {**obj}: MRI converts obj with #to_hash (mruby wants a Hash)
    const pm_node_t *v = ((const pm_assoc_splat_node_t *)node)->value;
    if (v && PM_NODE_TYPE(v) != PM_HASH_NODE) {
      r = add(sc, ORB_KWSPLAT);
      span(r, 0, v);
    }
    break;
  }
  case PM_INTERPOLATED_REGULAR_EXPRESSION_NODE: {
    // /a#{x}/n: with an encoding flag but no i, m or x, mruby's code
    // generator passes the letter as Regexp.compile's options
    const pm_interpolated_regular_expression_node_t *re = (const pm_interpolated_regular_expression_node_t *)node;
    pm_node_flags_t f = node->flags;
    if (f & PM_REGULAR_EXPRESSION_FLAGS_ONCE) {
      r = add(sc, ORB_REGEXP_LITERAL);
      span(r, 0, node);
    }
    char letter = 0;
    if (f & PM_REGULAR_EXPRESSION_FLAGS_EUC_JP) letter = 'e';
    else if (f & PM_REGULAR_EXPRESSION_FLAGS_ASCII_8BIT) letter = 'n';
    else if (f & PM_REGULAR_EXPRESSION_FLAGS_WINDOWS_31J) letter = 's';
    else if (f & PM_REGULAR_EXPRESSION_FLAGS_UTF_8) letter = 'u';
    if (letter && !(f & (PM_REGULAR_EXPRESSION_FLAGS_IGNORE_CASE | PM_REGULAR_EXPRESSION_FLAGS_EXTENDED | PM_REGULAR_EXPRESSION_FLAGS_MULTI_LINE))) {
      r = add(sc, ORB_REGEXP_ENC);
      span(r, 0, node);
      r->flags = letter;
      for (size_t i = 0; i < re->parts.size; i++) {
        orb_rec *pr = add(sc, ORB_LIST_ITEM);
        span(pr, 0, re->parts.nodes[i]);
        pr->flags = PM_NODE_TYPE(re->parts.nodes[i]) == PM_STRING_NODE;
      }
    }
    break;
  }
  case PM_ARGUMENTS_NODE:
    anon_args(sc, &((const pm_arguments_node_t *)node)->arguments);
    break;
  case PM_KEYWORD_HASH_NODE:
    anon_args(sc, &((const pm_keyword_hash_node_t *)node)->elements);
    break;
  case PM_HASH_NODE:
    anon_args(sc, &((const pm_hash_node_t *)node)->elements);
    break;
  case PM_BLOCK_ARGUMENT_NODE:
    if (sc->eval && !((const pm_block_argument_node_t *)node)->expression) {
      r = add(sc, ORB_ANON_ARG);
      r->flags = 3;
      span(r, 0, node);
    }
    break;
  case PM_RANGE_NODE: {
    const pm_range_node_t *rg = (const pm_range_node_t *)node;
    if (sc->relits && static_range_end(rg->left) && static_range_end(rg->right) && (rg->left || rg->right)) {
      r = add(sc, ORB_RANGE_LITERAL);
      span(r, 0, node);
    }
    break;
  }
  case PM_CASE_NODE: {
    const pm_case_node_t *cn = (const pm_case_node_t *)node;
    if (cn->predicate) break;
    for (size_t i = 0; i < cn->conditions.size; i++) {
      const pm_node_t *w = cn->conditions.nodes[i];
      if (PM_NODE_TYPE(w) != PM_WHEN_NODE) continue;
      const pm_node_list_t *cl = &((const pm_when_node_t *)w)->conditions;
      for (size_t j = 0; j < cl->size; j++) {
        if (PM_NODE_TYPE(cl->nodes[j]) == PM_SPLAT_NODE && ((const pm_splat_node_t *)cl->nodes[j])->expression) {
          r = add(sc, ORB_WHEN_SPLAT);
          span(r, 0, cl->nodes[j]);
        }
      }
    }
    break;
  }
  case PM_CASE_MATCH_NODE: {
    // case/in without else: MRI's NoMatchingPatternError names the subject
    const pm_case_match_node_t *cm = (const pm_case_match_node_t *)node;
    if (!cm->else_clause && cm->predicate) {
      r = add(sc, ORB_CASE_IN);
      span(r, 0, cm->predicate);
      r->s[1] = r->e[1] = (int32_t)cm->end_keyword_loc.start;
      /* one clause, a Hash pattern: a missing key is a NoMatchingPatternKeyError */
      if (cm->conditions.size == 1 && PM_NODE_TYPE(cm->conditions.nodes[0]) == PM_IN_NODE) {
        const pm_node_t *pat = ((const pm_in_node_t *)cm->conditions.nodes[0])->pattern;
        if (pat && PM_NODE_TYPE(pat) == PM_HASH_PATTERN_NODE && !((const pm_hash_pattern_node_t *)pat)->constant) {
          const pm_hash_pattern_node_t *h = (const pm_hash_pattern_node_t *)pat;
          r->flags = 1;
          for (size_t i = 0; i < h->elements.size; i++) {
            const pm_node_t *e = h->elements.nodes[i];
            if (PM_NODE_TYPE(e) != PM_ASSOC_NODE) continue;
            orb_rec *k = add(sc, ORB_CASE_IN_KEY);
            span(k, 0, ((const pm_assoc_node_t *)e)->key);
            k->s[1] = k->e[1] = (int32_t)cm->end_keyword_loc.start;
          }
        }
      }
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

/* The offset of the data after a script's __END__ line (Prism's data_loc,
   past that line), or -1 without one */
long
_oruby_prism_data_offset(const char *src, size_t len)
{
  static mrb_state *data_mrb = NULL;
  if (!data_mrb) data_mrb = mrb_open_core();
  void *cc = mrc_ccontext_new(data_mrb);
  pm_arena_t *arena = pm_arena_new();
  pm_parser_t *parser = pm_parser_new(arena, (const uint8_t *)src, len, NULL);
  pm_parse(parser);
  const pm_location_t *d = pm_parser_data_loc(parser);
  long off = -1;
  if (d && d->length > 0) {
    size_t p = (size_t)d->start + 7; /* __END__ */
    if (p < len && src[p] == '\r') p++;
    if (p < len && src[p] == '\n') p++;
    off = (long)p;
  }
  pm_parser_free(parser);
  pm_arena_free(arena);
  mrc_ccontext_free(cc);
  return off;
}

orb_rec *
_oruby_prism_scan(mrb_state *mrb, const char *src, size_t len, const orb_scan_opts *opts, int *count, int *errors, char *encname, size_t encname_len, char **msgs)
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
  mrb_bool eval_src = opts && opts->no_regexp_literals;
  if (opts && (opts->frozen >= 0 || (opts->encoding && opts->encoding[0]) || eval_src)) {
    options = pm_options_new();
    if (opts->frozen >= 0) pm_options_frozen_string_literal_set(options, opts->frozen != 0);
    if (opts->encoding && opts->encoding[0]) pm_options_encoding_set(options, opts->encoding);
    if (eval_src) {
      /* eval'd code may forward the method's anonymous parameters (*, **,
         &, ...): parsed as in such a method, rewritten (ORB_ANON_ARG) */
      pm_options_scopes_init(options, 1);
      pm_options_scope_t *scope = pm_options_scope_mut(options, 0);
      pm_options_scope_init(scope, 0);
      pm_options_scope_forwarding_set(scope, PM_OPTIONS_SCOPE_FORWARDING_POSITIONALS | PM_OPTIONS_SCOPE_FORWARDING_KEYWORDS |
                                             PM_OPTIONS_SCOPE_FORWARDING_BLOCK | PM_OPTIONS_SCOPE_FORWARDING_ALL);
    }
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
  sc.relits = !(opts && opts->no_regexp_literals);
  sc.regexp_ranges = opts && opts->check_regexp_ranges;
  sc.eval = eval_src;
  sc.src = src;
  {
    const char *en = pm_parser_encoding_name(parser);
    sc.nonutf8 = en && strcmp(en, "UTF-8") != 0;
  }
  if (root && *errors == 0) pm_visit_node(root, visit, &sc);
  if (root && *errors == 0 && !sc.eval) pm_visit_node(root, toplevel_return_visit, &sc);
  /* the parse warnings MRI prints (mruby's compiler prints none), for
     prism.go to have the program print them as it starts: their texts in
     *msgs, NUL separated, one ORB_PARSE_WARN each */
  if (msgs) *msgs = NULL;
  if (msgs && root && *errors == 0 && PM_NODE_TYPE(root) == PM_PROGRAM_NODE) {
    const pm_statements_node_t *st = ((const pm_program_node_t *)root)->statements;
    int32_t first = (st && st->body.size > 0) ? (int32_t)st->body.nodes[0]->location.start : -1;
    size_t mlen = 0;
    char *mbuf = NULL;
    for (const pm_diagnostic_t *w = pm_parser_warnings_first(parser); w && first >= 0; w = pm_diagnostic_next(w)) {
      const char *ty = pm_diagnostic_type(w);
      int verbose;
      if (strcmp(ty, "duplicated_hash_key") == 0) verbose = 0;
      else if (strcmp(ty, "duplicated_when_clause") == 0) verbose = 1;
      else if (strcmp(ty, "void_statement") == 0) verbose = 1;
      else if (strcmp(ty, "ignored_frozen_string_literal") == 0) verbose = 1;
      else continue;
      const char *m = pm_diagnostic_message(w);
      size_t ml = strlen(m);
      char *nb = (char *)realloc(mbuf, mlen + ml + 1);
      if (!nb) break;
      mbuf = nb;
      memcpy(mbuf + mlen, m, ml + 1);
      mlen += ml + 1;
      orb_rec *r = add(&sc, ORB_PARSE_WARN);
      if (!r) break;
      r->s[0] = r->e[0] = first;
      pm_location_t loc = pm_diagnostic_location(w);
      r->s[1] = (int32_t)loc.start;
      r->e[1] = (int32_t)(loc.start + loc.length);
      r->flags = verbose;
    }
    for (int i = 0; i < sc.nxw && first >= 0; i++) {
      const char *m = sc.xw[i].msg;
      size_t ml = strlen(m);
      char *nb = (char *)realloc(mbuf, mlen + ml + 1);
      if (!nb) break;
      mbuf = nb;
      memcpy(mbuf + mlen, m, ml + 1);
      mlen += ml + 1;
      orb_rec *r = add(&sc, ORB_PARSE_WARN);
      if (!r) break;
      r->s[0] = r->e[0] = first;
      r->s[1] = sc.xw[i].s;
      r->e[1] = sc.xw[i].e;
      r->flags = sc.xw[i].verbose;
    }
    *msgs = mbuf;
  }
  pm_parser_free(parser);
  pm_arena_free(arena);
  if (options) pm_options_free(options);
  mrc_ccontext_free(cc);
  *count = sc.n;
  return sc.recs;
}
