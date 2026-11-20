// Coverage for ORuby (gem/coverage, prism_coverage.go): which lines, branches
// and methods of a source MRI's Coverage counts, found on Prism's tree, and
// where the counters go.
//
// MRI 4.1 compiles with Prism and pushes a line event before each node with
// Prism's NEWLINE flag whose line differs from its iseq's last event line
// (prism_compile.c, pm_compile_node). An event attaches to the next
// instruction emitted, and the line it reports is that instruction's: events
// with no instruction between them merge, and a statement that compiles to
// nothing (a popped literal, `x = x`, a branch of `if false`) reports no line.
// This walks the tree in compile order with the same rules, per scope (an
// iseq: a method, block, class body, rescue clauses), and records:
//   - each counted event: where its counter goes (before the statement, or
//     around an if's predicate or a statement in expression position) and the
//     line it counts;
//   - branch bases and targets (if/unless/while/until/case/case-in/&.), in
//     MRI's declaration order, and the counters that count them;
//   - each def: its location, name and owner for the methods table.
#include <stdlib.h>
#include <string.h>
#include <mruby.h>
#include "prism.h"
#include "go-prism.h"

void *mrc_ccontext_new(mrb_state *mrb);
void mrc_ccontext_free(void *c);

typedef struct {
  int32_t last_line; /* the scope's last event line */
  int pend;          /* the event rec the next instruction gives its line, -1 none */
} cov_scope;

typedef struct {
  const pm_line_offset_list_t *lines;
  int32_t start_line;
  orb_cov_rec *recs;
  int n, cap;
  int8_t *cover; /* per line: 1 when line coverage counts it */
  int nlines;
  cov_scope *sc;
  int wrap_stmts;          /* the statements walked next are in expression position */
  const pm_node_t *endless; /* an endless def's body statement */
  int counters, methods;
  int no_events;           /* in a pattern guard: MRI emits no line events there */
  int want_branches, want_methods;
} cov;

static orb_cov_rec *
cov_rec(cov *c, int kind)
{
  if (c->n == c->cap) {
    c->cap = c->cap ? c->cap * 2 : 128;
    c->recs = (orb_cov_rec *)realloc(c->recs, sizeof(orb_cov_rec) * c->cap);
  }
  orb_cov_rec *r = &c->recs[c->n++];
  memset(r, 0xff, sizeof(*r)); /* every field -1 */
  r->kind = kind;
  return r;
}

static int32_t
line_at(cov *c, uint32_t off)
{
  return pm_line_offset_list_line_column(c->lines, off, c->start_line).line;
}

static void
loc_of(cov *c, orb_cov_rec *r, uint32_t s, uint32_t e)
{
  pm_line_column_t a = pm_line_offset_list_line_column(c->lines, s, c->start_line);
  pm_line_column_t b = pm_line_offset_list_line_column(c->lines, e, c->start_line);
  r->l1 = a.line; r->c1 = (int32_t)a.column;
  r->l2 = b.line; r->c2 = (int32_t)b.column;
}

static void
node_loc(cov *c, orb_cov_rec *r, const pm_node_t *n)
{
  loc_of(c, r, n->location.start, n->location.start + n->location.length);
}

/* an instruction at line: the pending event counts that line */
static void
emit(cov *c, int32_t line)
{
  if (c->sc->pend < 0) return;
  c->recs[c->sc->pend].line = line;
  if (line >= 1 && line <= c->nlines) c->cover[line - 1] = 1;
  c->sc->pend = -1;
}

static void emit_node(cov *c, const pm_node_t *n) { emit(c, line_at(c, n->location.start)); }

/* a branch or scope boundary: a pending event no instruction followed is
   MRI's on a jump or leave, a line already counted */
static void drop(cov *c) { c->sc->pend = -1; }

/* the counter of a statement: before it, or around it in expression position */
static orb_cov_rec *
at_stmt(cov *c, const pm_node_t *n, int wrap)
{
  orb_cov_rec *r = cov_rec(c, wrap ? ORB_COV_WRAP : ORB_COV_PREFIX);
  r->s = (int32_t)n->location.start;
  r->e = wrap ? (int32_t)(n->location.start + n->location.length) : r->s;
  return r;
}

static int
special_predicate_p(const pm_node_t *p)
{
  switch (PM_NODE_TYPE(p)) {
  case PM_FLIP_FLOP_NODE:
  case PM_MATCH_LAST_LINE_NODE:
  case PM_INTERPOLATED_MATCH_LAST_LINE_NODE:
    return 1; /* their meaning is being the predicate itself */
  default:
    return 0;
  }
}

static void
event(cov *c, const pm_node_t *n, int wrap)
{
  int32_t line = line_at(c, n->location.start);
  if (PM_NODE_TYPE(n) == PM_BEGIN_NODE) {
    const pm_begin_node_t *b = (const pm_begin_node_t *)n;
    if (!b->statements && b->rescue_clause) line = line_at(c, b->rescue_clause->base.location.start);
  }
  if (line == c->sc->last_line) return;
  c->sc->last_line = line;
  if (c->sc->pend >= 0) return; /* merges into the pending one */
  orb_cov_rec *r;
  const pm_node_t *pred = NULL;
  if (PM_NODE_TYPE(n) == PM_IF_NODE) pred = ((const pm_if_node_t *)n)->predicate;
  else if (PM_NODE_TYPE(n) == PM_UNLESS_NODE) pred = ((const pm_unless_node_t *)n)->predicate;
  if (pred && !special_predicate_p(pred)) {
    r = at_stmt(c, pred, 1);
  } else if (pred && !wrap) {
    r = at_stmt(c, n, 0);
  } else if (pred) {
    r = cov_rec(c, ORB_COV_NONE);
  } else {
    r = at_stmt(c, n, wrap || n == c->endless);
  }
  c->sc->pend = (int)(r - c->recs);
}

static void walk(cov *c, const pm_node_t *n, int popped);

static pm_visit_result_t
walk_child(const pm_node_t *n, void *data)
{
  walk((cov *)data, n, 0);
  return PM_VISIT_SKIP;
}

static void walk_children(cov *c, const pm_node_t *n) { pm_visit_child_nodes(n, walk_child, c); }

static void
walk_stmts(cov *c, const pm_node_t *n, int popped, int wrap)
{
  int saved = c->wrap_stmts;
  c->wrap_stmts = wrap;
  walk(c, n, popped);
  c->wrap_stmts = saved;
}

/* a new iseq: a method, block, class body or rescue clauses */
static void
walk_scope(cov *c, const pm_node_t *body, int popped)
{
  cov_scope sc = {0, -1}, *outer = c->sc;
  int saved = c->wrap_stmts;
  c->sc = &sc;
  c->wrap_stmts = 0;
  if (body) walk(c, body, popped);
  c->sc = outer;
  c->wrap_stmts = saved;
}

static int
no_effect_p(const pm_node_t *n)
{
  switch (PM_NODE_TYPE(n)) {
  case PM_LOCAL_VARIABLE_READ_NODE: case PM_INTEGER_NODE: case PM_FLOAT_NODE:
  case PM_RATIONAL_NODE: case PM_IMAGINARY_NODE: case PM_STRING_NODE:
  case PM_SYMBOL_NODE: case PM_NIL_NODE: case PM_TRUE_NODE: case PM_FALSE_NODE:
  case PM_SELF_NODE: case PM_INSTANCE_VARIABLE_READ_NODE: case PM_SOURCE_FILE_NODE:
  case PM_SOURCE_LINE_NODE: case PM_SOURCE_ENCODING_NODE:
    return 1;
  case PM_ARRAY_NODE: {
    const pm_node_list_t *l = &((const pm_array_node_t *)n)->elements;
    for (size_t i = 0; i < l->size; i++) if (!no_effect_p(l->nodes[i])) return 0;
    return 1;
  }
  default:
    return 0;
  }
}

/* 1: always truthy, 0: always falsy, -1: decided at run time (MRI's
   pm_compile_branch_condition) */
static int
static_predicate(const pm_node_t *p)
{
  switch (PM_NODE_TYPE(p)) {
  case PM_FALSE_NODE: case PM_NIL_NODE:
    return 0;
  case PM_FLOAT_NODE: case PM_IMAGINARY_NODE: case PM_INTEGER_NODE: case PM_LAMBDA_NODE:
  case PM_RATIONAL_NODE: case PM_REGULAR_EXPRESSION_NODE: case PM_STRING_NODE:
  case PM_SYMBOL_NODE: case PM_TRUE_NODE:
    return 1;
  default:
    return -1;
  }
}

static int
base(cov *c, int type, const pm_node_t *n)
{
  orb_cov_rec *r = cov_rec(c, ORB_COV_BASE);
  r->aux = type;
  node_loc(c, r, n);
  return (int)(r - c->recs);
}

static int
target(cov *c, int b, int label, uint32_t s, uint32_t e)
{
  orb_cov_rec *r = cov_rec(c, ORB_COV_TARGET);
  r->aux = label;
  r->f = b;
  r->t = c->counters++;
  loc_of(c, r, s, e);
  return r->t;
}

static int target_node(cov *c, int b, int label, const pm_node_t *n)
{
  return target(c, b, label, n->location.start, n->location.start + n->location.length);
}

/* the first statement of a body (StatementsNode, or a BeginNode's) */
static const pm_node_t *
first_stmt(const pm_node_t *body)
{
  if (!body) return NULL;
  if (PM_NODE_TYPE(body) == PM_BEGIN_NODE) body = (const pm_node_t *)((const pm_begin_node_t *)body)->statements;
  if (!body || PM_NODE_TYPE(body) != PM_STATEMENTS_NODE) return NULL;
  const pm_statements_node_t *s = (const pm_statements_node_t *)body;
  return s->body.size > 0 ? s->body.nodes[0] : NULL;
}

/* a branch counter at a body's first statement */
static void
count_body(cov *c, const pm_node_t *body, int counter, int dec, int wrap)
{
  const pm_node_t *s = first_stmt(body);
  if (!s || counter < 0) return;
  orb_cov_rec *r = at_stmt(c, s, wrap);
  r->t = counter;
  r->f = dec;
}

static void
walk_conditional(cov *c, const pm_node_t *n, int popped)
{
  /* MRI compiles an unless as an if whose then is the else clause and whose
     else is the unless body: tpath runs when the predicate is truthy */
  int is_if = PM_NODE_TYPE(n) == PM_IF_NODE;
  const pm_node_t *pred, *tpath, *fpath;
  int modifier, ternary = 0;
  if (is_if) {
    const pm_if_node_t *i = (const pm_if_node_t *)n;
    pred = i->predicate; tpath = (const pm_node_t *)i->statements; fpath = i->subsequent;
    ternary = i->if_keyword_loc.length == 0;
    modifier = !ternary && i->end_keyword_loc.length == 0 && i->if_keyword_loc.start > n->location.start;
  } else {
    const pm_unless_node_t *u = (const pm_unless_node_t *)n;
    pred = u->predicate;
    tpath = u->else_clause ? (const pm_node_t *)u->else_clause->statements : NULL;
    fpath = (const pm_node_t *)u->statements;
    modifier = u->end_keyword_loc.length == 0;
  }
  int wrap = ternary || modifier;
  int st = static_predicate(pred);
  if (st == 1) { if (tpath) walk_stmts(c, tpath, popped, wrap); drop(c); return; }
  if (st == 0) { if (fpath) walk_stmts(c, fpath, popped, wrap); drop(c); return; }
  walk(c, pred, 0);
  if (c->want_branches) {
    int b = base(c, is_if ? 0 : 1, n), c0, c1;
    /* branch 0 (if: then, unless: else) the truthy path, branch 1 the other */
    if (tpath) c0 = target_node(c, b, is_if ? 0 : 1, tpath);
    else if (is_if) c0 = target(c, b, 0, pred->location.start + pred->location.length, pred->location.start + pred->location.length);
    else c0 = target_node(c, b, 1, n);
    if (!fpath) c1 = target_node(c, b, is_if ? 1 : 0, n);
    else if (PM_NODE_TYPE(fpath) == PM_ELSE_NODE) {
      const pm_else_node_t *e = (const pm_else_node_t *)fpath;
      c1 = target_node(c, b, is_if ? 1 : 0, e->statements ? (const pm_node_t *)e->statements : fpath);
    } else c1 = target_node(c, b, is_if ? 1 : 0, fpath);
    if (!special_predicate_p(pred)) {
      orb_cov_rec *r = at_stmt(c, pred, 1);
      r->kind = ORB_COV_COND;
      r->t = c0;
      r->f = c1;
    }
  }
  if (tpath) walk_stmts(c, tpath, popped, wrap);
  drop(c);
  if (fpath) walk_stmts(c, fpath, popped, wrap);
  drop(c);
}

static void
walk_loop(cov *c, const pm_node_t *n)
{
  const pm_node_t *pred, *stmts;
  pm_location_t kw, close;
  int is_while = PM_NODE_TYPE(n) == PM_WHILE_NODE;
  if (is_while) {
    const pm_while_node_t *w = (const pm_while_node_t *)n;
    pred = w->predicate; stmts = (const pm_node_t *)w->statements; kw = w->while_keyword_loc; close = w->end_keyword_loc;
  } else {
    const pm_until_node_t *u = (const pm_until_node_t *)n;
    pred = u->predicate; stmts = (const pm_node_t *)u->statements; kw = u->until_keyword_loc; close = u->end_keyword_loc;
  }
  int modifier = close.length == 0 && kw.start > n->location.start &&
                 !(n->flags & PM_LOOP_FLAGS_BEGIN_MODIFIER);
  emit_node(c, n);
  if (c->want_branches) {
    int b = base(c, is_while ? 2 : 3, n);
    int ctr = target_node(c, b, 2, stmts ? stmts : n);
    count_body(c, stmts, ctr, -1, modifier);
  }
  if (stmts) walk_stmts(c, stmts, 1, modifier);
  drop(c);
  walk(c, pred, 0);
}

static void
walk_case(cov *c, const pm_node_t *n, int popped)
{
  int match = PM_NODE_TYPE(n) == PM_CASE_MATCH_NODE;
  const pm_node_t *pred;
  const pm_node_list_t *conds;
  const pm_else_node_t *els;
  if (match) {
    const pm_case_match_node_t *m = (const pm_case_match_node_t *)n;
    pred = m->predicate; conds = &m->conditions; els = m->else_clause;
  } else {
    const pm_case_node_t *k = (const pm_case_node_t *)n;
    pred = k->predicate; conds = &k->conditions; els = k->else_clause;
  }
  if (pred) walk(c, pred, 0);
  int b = -1, else_ctr = -1, *ctrs = NULL;
  if (c->want_branches) {
    b = base(c, 4, n);
    ctrs = (int *)calloc(conds->size + 1, sizeof(int));
    for (size_t i = 0; i < conds->size; i++) {
      const pm_node_t *w = conds->nodes[i];
      const pm_node_t *st = match ? (const pm_node_t *)((const pm_in_node_t *)w)->statements
                                  : (const pm_node_t *)((const pm_when_node_t *)w)->statements;
      ctrs[i] = target_node(c, b, match ? 4 : 3, st ? st : w);
    }
    else_ctr = target_node(c, b, 1, els ? (els->statements ? (const pm_node_t *)els->statements : (const pm_node_t *)els) : n);
    if (!els || !els->statements) {
      /* no else body to count: each run of the case counts it, each when
         taken takes it back */
      const pm_node_t *at = pred;
      if (!at && conds->size > 0 && !match) {
        const pm_when_node_t *w = (const pm_when_node_t *)conds->nodes[0];
        if (w->conditions.size > 0) at = w->conditions.nodes[0];
      }
      if (at) {
        orb_cov_rec *r = at_stmt(c, at, 1);
        r->t = else_ctr;
      }
    } else {
      count_body(c, (const pm_node_t *)els->statements, else_ctr, -1, 0);
      else_ctr = -1;
    }
  }
  for (size_t i = 0; i < conds->size; i++) {
    const pm_node_t *w = conds->nodes[i];
    const pm_node_t *st;
    if (match) {
      const pm_in_node_t *in = (const pm_in_node_t *)w;
      const pm_node_t *pat = in->pattern;
      if (PM_NODE_TYPE(pat) == PM_IF_NODE || PM_NODE_TYPE(pat) == PM_UNLESS_NODE) {
        /* a guard (in pat if cond): compiled as a plain test, no branch */
        const pm_node_t *gp = PM_NODE_TYPE(pat) == PM_IF_NODE ? ((const pm_if_node_t *)pat)->predicate
                                                             : ((const pm_unless_node_t *)pat)->predicate;
        const pm_statements_node_t *gs = PM_NODE_TYPE(pat) == PM_IF_NODE ? ((const pm_if_node_t *)pat)->statements
                                                                        : ((const pm_unless_node_t *)pat)->statements;
        c->no_events++;
        if (gs) walk(c, (const pm_node_t *)gs, 0);
        walk(c, gp, 0);
        c->no_events--;
      } else {
        walk(c, pat, 0);
      }
      st = (const pm_node_t *)in->statements;
    } else {
      const pm_when_node_t *wn = (const pm_when_node_t *)w;
      for (size_t j = 0; j < wn->conditions.size; j++) walk(c, wn->conditions.nodes[j], 0);
      st = (const pm_node_t *)wn->statements;
    }
    if (ctrs) count_body(c, st, ctrs[i], else_ctr, 0);
    if (st) walk(c, st, popped);
    drop(c);
  }
  free(ctrs);
  if (els && els->statements) walk(c, (const pm_node_t *)els->statements, popped);
  drop(c);
}

static void
walk_def(cov *c, const pm_node_t *n)
{
  const pm_def_node_t *d = (const pm_def_node_t *)n;
  emit_node(c, n);
  int endless = d->equal_loc.length > 0;
  if (c->want_methods) {
    orb_cov_rec *r = cov_rec(c, ORB_COV_DEF);
    r->s = (int32_t)n->location.start;
    r->e = (int32_t)(n->location.start + n->location.length);
    r->t = c->methods++;
    r->aux = d->name_loc.start; /* the name: aux..aux2 */
    r->aux2 = d->name_loc.start + d->name_loc.length;
    if (d->receiver) {
      r->f = PM_NODE_TYPE(d->receiver) == PM_SELF_NODE ? 1 : 2;
      r->line = (int32_t)d->receiver->location.start; /* the receiver: line..m */
      r->m = (int32_t)(d->receiver->location.start + d->receiver->location.length);
    } else {
      r->f = 0;
    }
    node_loc(c, r, n);
    int mid = r->t;
    const pm_node_t *s = first_stmt(d->body);
    orb_cov_rec *h = NULL;
    if (s) h = at_stmt(c, s, endless);
    else if (!d->body && d->end_keyword_loc.length > 0) {
      h = cov_rec(c, ORB_COV_PREFIX);
      h->s = h->e = (int32_t)d->end_keyword_loc.start;
    }
    if (h) h->m = mid;
  }
  const pm_node_t *saved = c->endless;
  c->endless = endless ? first_stmt(d->body) : NULL;
  walk_scope(c, d->body, 0);
  c->endless = saved;
}

static void
walk_begin(cov *c, const pm_node_t *n, int popped)
{
  const pm_begin_node_t *b = (const pm_begin_node_t *)n;
  if (b->statements) walk(c, (const pm_node_t *)b->statements, popped);
  if (b->rescue_clause) {
    /* the rescue clauses are an iseq of their own */
    cov_scope sc = {0, -1}, *outer = c->sc;
    c->sc = &sc;
    for (const pm_rescue_node_t *r = b->rescue_clause; r; r = r->subsequent) {
      for (size_t i = 0; i < r->exceptions.size; i++) walk(c, r->exceptions.nodes[i], 0);
      if (r->statements) walk(c, (const pm_node_t *)r->statements, 0);
      drop(c);
    }
    c->sc = outer;
  }
  if (b->else_clause && b->else_clause->statements) walk(c, (const pm_node_t *)b->else_clause->statements, popped);
  if (b->ensure_clause && b->ensure_clause->statements) walk(c, (const pm_node_t *)b->ensure_clause->statements, 1);
}

static void
walk(cov *c, const pm_node_t *n, int popped)
{
  if (!n) return;
  if ((n->flags & PM_NODE_FLAG_NEWLINE) && !c->no_events) event(c, n, c->wrap_stmts);
  switch (PM_NODE_TYPE(n)) {
  case PM_PROGRAM_NODE:
    walk(c, (const pm_node_t *)((const pm_program_node_t *)n)->statements, 0);
    return;
  case PM_STATEMENTS_NODE: {
    const pm_node_list_t *l = &((const pm_statements_node_t *)n)->body;
    int wrap = c->wrap_stmts;
    for (size_t i = 0; i < l->size; i++) {
      c->wrap_stmts = wrap;
      walk(c, l->nodes[i], popped || i + 1 < l->size);
    }
    c->wrap_stmts = wrap;
    return;
  }
  case PM_LOCAL_VARIABLE_READ_NODE: case PM_INTEGER_NODE: case PM_FLOAT_NODE:
  case PM_RATIONAL_NODE: case PM_IMAGINARY_NODE: case PM_STRING_NODE:
  case PM_SYMBOL_NODE: case PM_NIL_NODE: case PM_TRUE_NODE: case PM_FALSE_NODE:
  case PM_SELF_NODE: case PM_INSTANCE_VARIABLE_READ_NODE: case PM_SOURCE_FILE_NODE:
  case PM_SOURCE_LINE_NODE: case PM_SOURCE_ENCODING_NODE:
    if (!popped) emit_node(c, n);
    return;
  case PM_ARRAY_NODE: case PM_HASH_NODE: {
    /* a static array is one duparray, at the bracket, and a hash starting
       with a static pair a duphash chunk there; otherwise the first
       element's own instruction comes first */
    if (PM_NODE_TYPE(n) == PM_ARRAY_NODE && popped && no_effect_p(n)) return;
    const pm_node_list_t *l = PM_NODE_TYPE(n) == PM_ARRAY_NODE ? &((const pm_array_node_t *)n)->elements
                                                              : &((const pm_hash_node_t *)n)->elements;
    int at_bracket = PM_NODE_TYPE(n) == PM_ARRAY_NODE ? (l->size == 0 || (n->flags & PM_NODE_FLAG_STATIC_LITERAL))
                                                      : (l->size == 0 || (l->nodes[0]->flags & PM_NODE_FLAG_STATIC_LITERAL));
    if (at_bracket) emit_node(c, n);
    walk_children(c, n);
    emit_node(c, n);
    return;
  }
  case PM_MULTI_WRITE_NODE:
    /* the value first */
    walk(c, ((const pm_multi_write_node_t *)n)->value, 0);
    walk_children(c, n);
    emit_node(c, n);
    return;
  case PM_LOCAL_VARIABLE_OPERATOR_WRITE_NODE: case PM_LOCAL_VARIABLE_OR_WRITE_NODE:
  case PM_LOCAL_VARIABLE_AND_WRITE_NODE: case PM_INSTANCE_VARIABLE_OPERATOR_WRITE_NODE:
  case PM_INSTANCE_VARIABLE_OR_WRITE_NODE: case PM_INSTANCE_VARIABLE_AND_WRITE_NODE:
  case PM_GLOBAL_VARIABLE_OPERATOR_WRITE_NODE: case PM_GLOBAL_VARIABLE_OR_WRITE_NODE:
  case PM_GLOBAL_VARIABLE_AND_WRITE_NODE: case PM_CLASS_VARIABLE_OPERATOR_WRITE_NODE:
  case PM_CLASS_VARIABLE_OR_WRITE_NODE: case PM_CLASS_VARIABLE_AND_WRITE_NODE:
  case PM_CONSTANT_OPERATOR_WRITE_NODE: case PM_CONSTANT_OR_WRITE_NODE:
  case PM_CONSTANT_AND_WRITE_NODE:
    /* the variable is read first */
    emit_node(c, n);
    walk_children(c, n);
    return;
  case PM_LOCAL_VARIABLE_WRITE_NODE: {
    const pm_local_variable_write_node_t *w = (const pm_local_variable_write_node_t *)n;
    if (popped && PM_NODE_TYPE(w->value) == PM_LOCAL_VARIABLE_READ_NODE) {
      const pm_local_variable_read_node_t *v = (const pm_local_variable_read_node_t *)w->value;
      if (v->name == w->name && v->depth == w->depth) return; /* x = x */
    }
    walk(c, w->value, 0);
    emit_node(c, n);
    return;
  }
  case PM_IF_NODE: case PM_UNLESS_NODE:
    walk_conditional(c, n, popped);
    return;
  case PM_ELSE_NODE:
    walk(c, (const pm_node_t *)((const pm_else_node_t *)n)->statements, popped);
    return;
  case PM_WHILE_NODE: case PM_UNTIL_NODE:
    walk_loop(c, n);
    return;
  case PM_CASE_NODE: case PM_CASE_MATCH_NODE:
    walk_case(c, n, popped);
    return;
  case PM_CALL_NODE: {
    const pm_call_node_t *k = (const pm_call_node_t *)n;
    if (k->receiver) {
      walk(c, k->receiver, 0);
      if ((n->flags & PM_CALL_NODE_FLAGS_SAFE_NAVIGATION) && c->want_branches) {
        /* MRI's location: to the end of the arguments, closing paren or
           name, whichever is last (a block is not in it) */
        uint32_t end = 0;
        if (k->closing_loc.length > 0) end = k->closing_loc.start + k->closing_loc.length;
        if (k->arguments) {
          uint32_t a = k->arguments->base.location.start + k->arguments->base.location.length;
          if (a > end) end = a;
        }
        if (k->message_loc.length > 0 && k->message_loc.start + k->message_loc.length > end) end = k->message_loc.start + k->message_loc.length;
        orb_cov_rec *br = cov_rec(c, ORB_COV_BASE);
        br->aux = 5;
        loc_of(c, br, n->location.start, end);
        int b = (int)(br - c->recs);
        int t = target(c, b, 0, n->location.start, end), f = target(c, b, 1, n->location.start, end);
        orb_cov_rec *r = at_stmt(c, k->receiver, 1);
        r->kind = ORB_COV_NILCOND;
        r->t = t;
        r->f = f;
      }
    } else {
      emit_node(c, n);
    }
    if (k->arguments) walk(c, (const pm_node_t *)k->arguments, 0);
    if (k->block) walk(c, k->block, 0);
    emit_node(c, n);
    return;
  }
  case PM_DEF_NODE:
    walk_def(c, n);
    return;
  case PM_CLASS_NODE: {
    const pm_class_node_t *k = (const pm_class_node_t *)n;
    walk(c, k->constant_path, 0);
    walk(c, k->superclass, 0);
    emit_node(c, n);
    walk_scope(c, k->body, 0);
    return;
  }
  case PM_MODULE_NODE: {
    const pm_module_node_t *m = (const pm_module_node_t *)n;
    walk(c, m->constant_path, 0);
    emit_node(c, n);
    walk_scope(c, m->body, 0);
    return;
  }
  case PM_SINGLETON_CLASS_NODE: {
    const pm_singleton_class_node_t *s = (const pm_singleton_class_node_t *)n;
    walk(c, s->expression, 0);
    emit_node(c, n);
    walk_scope(c, s->body, 0);
    return;
  }
  case PM_BLOCK_NODE:
    walk_scope(c, ((const pm_block_node_t *)n)->body, 0);
    return;
  case PM_LAMBDA_NODE:
    emit_node(c, n);
    walk_scope(c, ((const pm_lambda_node_t *)n)->body, 0);
    return;
  case PM_FOR_NODE: {
    const pm_for_node_t *f = (const pm_for_node_t *)n;
    walk(c, f->collection, 0);
    emit_node(c, n);
    walk_scope(c, (const pm_node_t *)f->statements, 1);
    return;
  }
  case PM_BEGIN_NODE:
    walk_begin(c, n, popped);
    return;
  case PM_PARENTHESES_NODE:
    walk(c, ((const pm_parentheses_node_t *)n)->body, popped);
    return;
  case PM_EMBEDDED_STATEMENTS_NODE:
    walk(c, (const pm_node_t *)((const pm_embedded_statements_node_t *)n)->statements, 0);
    return;
  case PM_INTERPOLATED_STRING_NODE: case PM_INTERPOLATED_SYMBOL_NODE:
  case PM_INTERPOLATED_X_STRING_NODE: case PM_INTERPOLATED_REGULAR_EXPRESSION_NODE: {
    /* at the first part (a heredoc's body, not its opener) */
    const pm_node_list_t *parts = NULL;
    switch (PM_NODE_TYPE(n)) {
    case PM_INTERPOLATED_STRING_NODE: parts = &((const pm_interpolated_string_node_t *)n)->parts; break;
    case PM_INTERPOLATED_SYMBOL_NODE: parts = &((const pm_interpolated_symbol_node_t *)n)->parts; break;
    case PM_INTERPOLATED_X_STRING_NODE: parts = &((const pm_interpolated_x_string_node_t *)n)->parts; break;
    default: parts = &((const pm_interpolated_regular_expression_node_t *)n)->parts; break;
    }
    /* leading text: at the literal; an embedded part first: at that part */
    if (parts->size > 0 && PM_NODE_TYPE(parts->nodes[0]) != PM_STRING_NODE) emit_node(c, parts->nodes[0]);
    emit_node(c, n);
    walk_children(c, n);
    return;
  }
  case PM_DEFINED_NODE:
    emit_node(c, n);
    return;
  case PM_RESCUE_MODIFIER_NODE: {
    const pm_rescue_modifier_node_t *r = (const pm_rescue_modifier_node_t *)n;
    walk(c, r->expression, popped);
    cov_scope sc = {0, -1}, *outer = c->sc;
    c->sc = &sc;
    walk(c, r->rescue_expression, 0);
    c->sc = outer;
    return;
  }
  default:
    walk_children(c, n);
    emit_node(c, n);
    return;
  }
}

orb_cov_rec *
_oruby_prism_coverage(const char *src, size_t len, int branches, int methods,
                      int *count, int8_t **cover, int *nlines)
{
  static mrb_state *cov_mrb = NULL; /* Prism's allocator state */
  if (!cov_mrb) cov_mrb = mrb_open_core();
  cov c = {0};
  c.want_branches = branches;
  c.want_methods = methods;
  int lines = 0;
  for (size_t i = 0; i < len; i++) if (src[i] == '\n') lines++;
  if (len > 0 && src[len - 1] != '\n') lines++;
  c.nlines = lines;
  c.cover = (int8_t *)calloc(lines + 1, 1);
  *count = 0;
  *nlines = lines;
  *cover = c.cover;
  if (len == 0) return NULL;
  void *cc = mrc_ccontext_new(cov_mrb);
  pm_arena_t *arena = pm_arena_new();
  pm_parser_t *parser = pm_parser_new(arena, (const uint8_t *)src, len, NULL);
  pm_node_t *root = pm_parse(parser);
  if (root && pm_parser_errors_size(parser) == 0) {
    c.lines = pm_parser_line_offsets(parser);
    c.start_line = pm_parser_start_line(parser);
    cov_scope top = {0, -1};
    c.sc = &top;
    walk(&c, root, 0);
  }
  pm_parser_free(parser);
  pm_arena_free(arena);
  mrc_ccontext_free(cc);
  *count = c.n;
  return c.recs;
}
