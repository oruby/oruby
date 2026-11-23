#ifndef GO_PRISM_H
#define GO_PRISM_H

#include <stdint.h>
#include <stddef.h>
#include <mruby.h>

// Rewrite sites found by _oruby_prism_scan (see prism.go)
enum {
  ORB_LIST_ITEM = 1,      // s0: one name of the preceding ORB_UNDEF_DYNAMIC
  ORB_ALIAS_DYNAMIC,      // s0: node, s1: new name, s2: old name
  ORB_UNDEF_DYNAMIC,      // s0: node; names follow as ORB_LIST_ITEM
  ORB_RESCUE_SAFE_NAV,    // s0: reference (a&.b)
  ORB_FOR_SAFE_NAV,       // s0: index, s1: collection, s2: do keyword
  ORB_BEGIN_BLOCK,        // s0: BEGIN, s1: {, s2: }, s3: (zero width) the program's first statement
  ORB_FLIP_FLOP,          // s0: node, s1: left, s2: right; flags 1: ..., 2/4: left/right integer
  ORB_CPATH_OR_WRITE,     // s0: node, s1: parent, s2: name, s3: value, s4: operator
  ORB_CPATH_AND_WRITE,
  ORB_CPATH_OP_WRITE,
  ORB_ALIAS_GVAR,         // s0: node, s1: new name, s2: old name
  ORB_GVAR_REF,           // s0: global variable name, s1: whole assignment (writes)
  ORB_BIG_INTEGER,        // s0: literal
  ORB_END_BLOCK,          // s0: END, s1: {, s2: }
  ORB_FROZEN_STRING,      // s0: literal (frozen by magic comment or option)
  ORB_LITERAL_FREEZE,     // s0: no-argument freeze call, s1: literal receiver
  ORB_SOURCE_ENCODING,    // s0: __ENCODING__
  ORB_LITERAL_ENCODING,   // s0: literal in non-UTF-8 source; flags 1: \u (UTF-8), 2: binary
  ORB_HASH_LABEL_ENCODING, // s0: bare non-ASCII Hash label, s1: label text
  ORB_MASGN_VALUE,        // s0: the single value of a, b = value, s1: the assignment; flags 1: its value is unused
  ORB_MASGN_OPERAND,      // s0: a receiver or index evaluated before the RHS, s1: the assignment
  ORB_KWSPLAT,            // s0: the value of **value in a call or Hash literal
  ORB_ARRAY_KWHASH,       // s0: a brace-less Hash at the end of an Array literal; flags 1: only **splats (dropped when empty)
  ORB_REGEXP_ENC,         // s0: interpolated regexp; flags: encoding letter and options in the high byte; parts follow as ORB_LIST_ITEM
  ORB_REGEXP_LITERAL,     // s0: a static regexp or interpolated /o literal cached per site
  ORB_REGEXP_RANGE,       // s0: static regexp content, flags: extended mode
  ORB_REGEXP_LAST_LINE,   // s0: implicit match against the frame's $_
  ORB_MASGN_CPATH,        // s0: a constant path target of a multiple assignment, s1: the assignment
  ORB_FOR_CPATH,          // s0: a constant path target of a for loop, s1: (zero width) where its body starts
  ORB_MASGN_NESTED,       // s0: a nested target (a, b) of a multiple assignment, s1: the assignment, s2: inside its parentheses
  ORB_IT_BLOCK,           // s0: (zero width) just after the opening of a block or lambda whose parameter is `it`
  ORB_BLOCK_PARAMS,       // s0: (zero width) where a block's body starts; flags 1: one object given is spread by #to_ary (a non-lambda block of several parameters); its parameters follow as ORB_LIST_ITEM: s0 the parameter (a name, or a nested (a, b)), flags 0 required, 1 rest, 2 post, 3 the implicit rest of |a, |, +8 nested
  ORB_PARSE_WARN,         // s0: (zero width) the program's first statement, s1: where a parse warning points; flags 1: verbose only; its text is the next of _oruby_prism_scan's msgs
  ORB_CASE_IN,            // s0: the subject of a case/in without else, s1: (zero width) its end keyword
  ORB_RANGE_LITERAL,      // s0: a range of literal (numeric or nil) ends: one frozen Range per site, as MRI's
  ORB_PAT_LAMBDA,         // s0: a lambda in a pattern, pinned ^(->{}) (mruby's codegen fails the match)
  ORB_PAT_ANON_REST,      // s0: ", **" ending a hash pattern with keys: dropped (deconstruct_keys gets the keys, as MRI's)
  ORB_CASE_IN_KEY,        // s0: a key of the one Hash pattern of a case/in without else, s1: that case's end (ORB_CASE_IN)
  ORB_WHEN_SPLAT,         // s0: the *x of a when in a case without a subject: true when an element is (mruby tests the Array)
  ORB_DSYM,               // s0: an interpolated :"…" (s1 its ':'), flags 1: a %I[] (s1 its "%I"): names checked as String#to_sym does
  ORB_ANON_ARG,           // s0: an anonymous *, **, & or ... argument in eval'd code; flags 1 rest, 2 keywords, 3 block, 4 all
  ORB_ZSUPER_NODEF,       // s0: a bare super with no def around it (a define_method or other block): MRI's run-time errors
  ORB_LAMBDA_BLOCK_ARG,   // s0: x of lambda(&x): MRI's lambda takes a lambda only from a literal block
  ORB_XSTRING_FROZEN,     // s0: `cmd` or %x(cmd) without interpolation, s1: its opening: called with a frozen String
  ORB_CLASS_BASE,         // s0: the expression qualifying class expr::Foo / module expr::Foo (not a constant): checked as MRI's
  ORB_INVALID_YIELD,      // s0: a yield in a class body of eval'd code (Prism allows it there): MRI's SyntaxError
  ORB_SCLASS_RETURN,      // s0: a return in class << obj inside a def, s1: that class << obj, s2: its arguments; flags 1: one plain argument
};

// Scan options: frozen_string_literal default (-1 unset, 0, 1) and the
// source encoding name ("" for the default UTF-8)
typedef struct {
  int frozen;
  const char *encoding;
  int no_regexp_literals; // eval'd code: its ordinary literals need no per-site cache
  int check_regexp_ranges;
} orb_scan_opts;

#define ORB_SPANS 5

typedef struct {
  int32_t kind;
  int32_t flags;
  int32_t s[ORB_SPANS];
  int32_t e[ORB_SPANS];
} orb_rec;

// Coverage (go-coverage.c): what to insert where, and the branch and method
// tables, in MRI's order
enum {
  ORB_COV_NONE,      // a counted line no counter can go to
  ORB_COV_PREFIX,    // insert at s
  ORB_COV_WRAP,      // around s..e
  ORB_COV_COND,      // around a predicate s..e: counter t when truthy, f when not
  ORB_COV_NILCOND,   // around a &. receiver s..e: t when not nil, f when nil
  ORB_COV_BASE,      // a branch base: aux its type (if unless while until case &.), l1..c2
  ORB_COV_TARGET,    // a branch target of base f: aux its label (then else body when in), counter t, l1..c2
  ORB_COV_DEF,       // a def s..e: method t, name aux..aux2, f 0/1 (self.)/2 (receiver line..m), l1..c2
};

// The PREFIX and WRAP inserts count: line (its line coverage, -1 none),
// t (a branch counter, +1), f (a branch counter, -1), m (a method's calls)
typedef struct {
  int32_t kind;
  int32_t s, e;
  int32_t line;
  int32_t t, f, m;
  int32_t aux, aux2;
  int32_t l1, c1, l2, c2;
} orb_cov_rec;

orb_cov_rec *_oruby_prism_coverage(const char *src, size_t len, int branches, int methods,
                                   int *count, int8_t **cover, int *nlines);

long _oruby_prism_data_offset(const char *src, size_t len);
orb_rec *_oruby_prism_scan(mrb_state *mrb, const char *src, size_t len, const orb_scan_opts *opts, int *count, int *errors, char *encname, size_t encname_len, char **msgs);

#endif
