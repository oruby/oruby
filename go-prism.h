#ifndef GO_PRISM_H
#define GO_PRISM_H

#include <stdint.h>
#include <stddef.h>
#include <mruby.h>

// Rewrite sites found by _oruby_prism_scan (see prism.go)
enum {
  ORB_LIST_ITEM = 1,      // s0: one name of the preceding ORB_UNDEF_DYNAMIC
  ORB_NO_KEYWORDS,        // s0: **nil
  ORB_ALIAS_DYNAMIC,      // s0: node, s1: new name, s2: old name
  ORB_UNDEF_DYNAMIC,      // s0: node; names follow as ORB_LIST_ITEM
  ORB_RESCUE_SAFE_NAV,    // s0: reference (a&.b)
  ORB_FOR_SAFE_NAV,       // s0: index, s1: collection, s2: do keyword
  ORB_BEGIN_BLOCK,        // s0: BEGIN, s1: {, s2: }
  ORB_FLIP_FLOP,          // s0: node, s1: left, s2: right; flags 1: ..., 2/4: left/right integer
  ORB_CPATH_OR_WRITE,     // s0: node, s1: parent, s2: name, s3: value, s4: operator
  ORB_CPATH_AND_WRITE,
  ORB_CPATH_OP_WRITE,
  ORB_ALIAS_GVAR,         // s0: node, s1: new name, s2: old name
  ORB_GVAR_REF,           // s0: global variable name, s1: whole assignment (writes)
  ORB_BIG_INTEGER,        // s0: literal
  ORB_END_BLOCK,          // s0: END, s1: {, s2: }
  ORB_FROZEN_STRING,      // s0: literal (frozen by magic comment or option)
  ORB_SOURCE_ENCODING,    // s0: __ENCODING__
  ORB_LITERAL_ENCODING,   // s0: literal in non-UTF-8 source; flags 1: \u (UTF-8), 2: binary
  ORB_MASGN_VALUE,        // s0: the single value of a, b = value, s1: the assignment
  ORB_KWSPLAT,            // s0: the value of **value in a call or Hash literal
  ORB_ARRAY_KWHASH,       // s0: a brace-less Hash at the end of an Array literal; flags 1: only **splats (dropped when empty)
  ORB_REGEXP_ENC,         // s0: /..#{x}../n with only an encoding flag (flags: its letter); parts follow as ORB_LIST_ITEM (flags 1: literal text)
  ORB_REGEXP_LITERAL,     // s0: a regexp literal without interpolation (one object per site, as MRI's)
  ORB_MASGN_CPATH,        // s0: a constant path target of a multiple assignment, s1: the assignment
  ORB_FOR_CPATH,          // s0: a constant path target of a for loop, s1: (zero width) where its body starts
  ORB_MASGN_NESTED,       // s0: a nested target (a, b) of a multiple assignment, s1: the assignment, s2: inside its parentheses
  ORB_IT_BLOCK,           // s0: (zero width) just after the opening of a block or lambda whose parameter is `it`
  ORB_PARSE_WARN,         // s0: (zero width) the program's first statement, s1: where a parse warning points; flags 1: verbose only; its text is the next of _oruby_prism_scan's msgs
  ORB_NOKW,               // as ORB_NOBLOCK, for a **nil parameter (renamed by ORB_NO_KEYWORDS): the check only
  ORB_NOBLOCK,            // s0: an &nil parameter, s1: (zero width) where the body starts; flags 1: s2 an endless def's body, to wrap
};

// Scan options: frozen_string_literal default (-1 unset, 0, 1) and the
// source encoding name ("" for the default UTF-8)
typedef struct {
  int frozen;
  const char *encoding;
  int no_regexp_literals; // eval'd code: its literals are made as it is compiled
} orb_scan_opts;

#define ORB_SPANS 5

typedef struct {
  int32_t kind;
  int32_t flags;
  int32_t s[ORB_SPANS];
  int32_t e[ORB_SPANS];
} orb_rec;

long _oruby_prism_data_offset(const char *src, size_t len);
orb_rec *_oruby_prism_scan(mrb_state *mrb, const char *src, size_t len, const orb_scan_opts *opts, int *count, int *errors, char *encname, size_t encname_len, char **msgs);

#endif
