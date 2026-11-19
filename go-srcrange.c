// Support for MRI 4.1's #source_range (Proc, Method, UnboundMethod and
// Thread::Backtrace::Location): mruby's debug info has lines but no columns,
// so the range is found by parsing the source again with Prism and picking,
// among the nodes on the line, the one the frame runs (core_ext's
// 43_source_range.rb does the picking).
//
//   - Kernel.__oruby_prism_nodes(src, line, hi = line): the nodes on the
//     lines line..hi, as
//     [type, start_line, start_column, end_line, end_column, name, operator,
//     write_name] (columns in bytes, end exclusive); nil when src does not
//     parse
//   - the source of code eval runs is kept (the last ORB_KEPT_SOURCES), as
//     [file or nil, first line, source] in Kernel.__oruby_kept_sources
//   - a backtrace location records the instruction its frame was at
//     (_orb_insn_site): [opcode name, symbol operand or nil]
#include <string.h>
#include <ctype.h>
#include <mruby.h>
#include <mruby/array.h>
#include <mruby/string.h>
#include <mruby/variable.h>
#include <mruby/irep.h>
#include <mruby/opcode.h>
#include "prism.h"

void *mrc_ccontext_new(mrb_state *mrb);
void mrc_ccontext_free(void *c);

#define ORB_KEPT_SOURCES 32

/* ---- Prism nodes on a line ---- */

typedef struct {
  mrb_state *mrb;
  const pm_parser_t *parser;
  const pm_line_offset_list_t *lines;
  int32_t line, hi;
  mrb_value ary;
} nodes_scan;

static mrb_value const_sym(nodes_scan *sc, pm_constant_id_t id) {
  if (id == 0) return mrb_nil_value();
  const pm_constant_t *c = pm_parser_constant(sc->parser, id);
  if (!c) return mrb_nil_value();
  return mrb_symbol_value(mrb_intern(sc->mrb, (const char *)pm_constant_start(c), pm_constant_length(c)));
}

/* "PM_CALL_OPERATOR_WRITE_NODE" -> "CallOperatorWriteNode" */
static mrb_value type_name(mrb_state *mrb, pm_node_type_t t) {
  const char *s = pm_node_type(t);
  char buf[64];
  size_t n = 0;
  int up = 1;
  if (s && strncmp(s, "PM_", 3) == 0) s += 3;
  for (; s && *s && n < sizeof(buf) - 1; s++) {
    if (*s == '_') { up = 1; continue; }
    buf[n++] = up ? (char)toupper((unsigned char)*s) : (char)tolower((unsigned char)*s);
    up = 0;
  }
  return mrb_str_new(mrb, buf, n);
}

static bool nodes_visit(const pm_node_t *node, void *data) {
  nodes_scan *sc = (nodes_scan *)data;
  uint32_t start = node->location.start, end = start + node->location.length;
  pm_line_column_t s = pm_line_offset_list_line_column(sc->lines, start, 1);
  pm_line_column_t e = pm_line_offset_list_line_column(sc->lines, end, 1);
  if (s.line > sc->hi || e.line < sc->line) return false;
  mrb_value name = mrb_nil_value(), op = mrb_nil_value(), wname = mrb_nil_value();
  switch (PM_NODE_TYPE(node)) {
  case PM_CALL_NODE: name = const_sym(sc, ((const pm_call_node_t *)node)->name); break;
  case PM_CALL_TARGET_NODE: name = const_sym(sc, ((const pm_call_target_node_t *)node)->name); break;
  case PM_CALL_OPERATOR_WRITE_NODE: {
    const pm_call_operator_write_node_t *n = (const pm_call_operator_write_node_t *)node;
    name = const_sym(sc, n->read_name); op = const_sym(sc, n->binary_operator); wname = const_sym(sc, n->write_name);
    break;
  }
  case PM_CALL_OR_WRITE_NODE: {
    const pm_call_or_write_node_t *n = (const pm_call_or_write_node_t *)node;
    name = const_sym(sc, n->read_name); wname = const_sym(sc, n->write_name);
    break;
  }
  case PM_CALL_AND_WRITE_NODE: {
    const pm_call_and_write_node_t *n = (const pm_call_and_write_node_t *)node;
    name = const_sym(sc, n->read_name); wname = const_sym(sc, n->write_name);
    break;
  }
  case PM_INDEX_OPERATOR_WRITE_NODE: op = const_sym(sc, ((const pm_index_operator_write_node_t *)node)->binary_operator); break;
#define NAME_OP(T, t) case T: { const t *n = (const t *)node; name = const_sym(sc, n->name); op = const_sym(sc, n->binary_operator); break; }
  NAME_OP(PM_LOCAL_VARIABLE_OPERATOR_WRITE_NODE, pm_local_variable_operator_write_node_t)
  NAME_OP(PM_INSTANCE_VARIABLE_OPERATOR_WRITE_NODE, pm_instance_variable_operator_write_node_t)
  NAME_OP(PM_CLASS_VARIABLE_OPERATOR_WRITE_NODE, pm_class_variable_operator_write_node_t)
  NAME_OP(PM_GLOBAL_VARIABLE_OPERATOR_WRITE_NODE, pm_global_variable_operator_write_node_t)
  NAME_OP(PM_CONSTANT_OPERATOR_WRITE_NODE, pm_constant_operator_write_node_t)
#undef NAME_OP
  case PM_CONSTANT_PATH_OPERATOR_WRITE_NODE: {
    const pm_constant_path_operator_write_node_t *n = (const pm_constant_path_operator_write_node_t *)node;
    name = const_sym(sc, n->target->name); op = const_sym(sc, n->binary_operator);
    break;
  }
  case PM_CONSTANT_PATH_OR_WRITE_NODE: name = const_sym(sc, ((const pm_constant_path_or_write_node_t *)node)->target->name); break;
  case PM_CONSTANT_PATH_AND_WRITE_NODE: name = const_sym(sc, ((const pm_constant_path_and_write_node_t *)node)->target->name); break;
  case PM_CONSTANT_PATH_WRITE_NODE: name = const_sym(sc, ((const pm_constant_path_write_node_t *)node)->target->name); break;
#define NAME(T, t) case T: name = const_sym(sc, ((const t *)node)->name); break;
  NAME(PM_CONSTANT_PATH_NODE, pm_constant_path_node_t)
  NAME(PM_CONSTANT_PATH_TARGET_NODE, pm_constant_path_target_node_t)
  NAME(PM_CONSTANT_READ_NODE, pm_constant_read_node_t)
  NAME(PM_CONSTANT_WRITE_NODE, pm_constant_write_node_t)
  NAME(PM_CONSTANT_OR_WRITE_NODE, pm_constant_or_write_node_t)
  NAME(PM_CONSTANT_AND_WRITE_NODE, pm_constant_and_write_node_t)
  NAME(PM_CONSTANT_TARGET_NODE, pm_constant_target_node_t)
  NAME(PM_CLASS_VARIABLE_READ_NODE, pm_class_variable_read_node_t)
  NAME(PM_CLASS_VARIABLE_OR_WRITE_NODE, pm_class_variable_or_write_node_t)
  NAME(PM_CLASS_VARIABLE_AND_WRITE_NODE, pm_class_variable_and_write_node_t)
  NAME(PM_DEF_NODE, pm_def_node_t)
#undef NAME
  default: break;
  }
  mrb_value rec[8] = {
    type_name(sc->mrb, PM_NODE_TYPE(node)),
    mrb_fixnum_value(s.line), mrb_fixnum_value(s.column),
    mrb_fixnum_value(e.line), mrb_fixnum_value(e.column),
    name, op, wname,
  };
  mrb_ary_push(sc->mrb, sc->ary, mrb_ary_new_from_values(sc->mrb, 8, rec));
  return true;
}

static pm_visit_result_t nodes_cb(const pm_node_t *node, void *data) {
  return nodes_visit(node, data) ? PM_VISIT_DESCEND : PM_VISIT_SKIP;
}

static mrb_value prism_nodes_m(mrb_state *mrb, mrb_value self) {
  mrb_value src;
  mrb_int line, hi = 0;
  if (mrb_get_args(mrb, "Si|i", &src, &line, &hi) < 3 || hi < line) hi = line;
  void *cc = mrc_ccontext_new(mrb);
  pm_arena_t *arena = pm_arena_new();
  pm_parser_t *parser = pm_parser_new(arena, (const uint8_t *)RSTRING_PTR(src), (size_t)RSTRING_LEN(src), NULL);
  pm_node_t *root = pm_parse(parser);
  mrb_value out = mrb_nil_value();
  if (root && pm_parser_errors_size(parser) == 0) {
    nodes_scan sc = {mrb, parser, pm_parser_line_offsets(parser), (int32_t)line, (int32_t)hi, mrb_ary_new(mrb)};
    pm_visit_node(root, nodes_cb, &sc);
    out = sc.ary;
  }
  pm_parser_free(parser);
  pm_arena_free(arena);
  mrc_ccontext_free(cc);
  return out;
}

/* ---- kept sources ---- */

static mrb_value kept_sources(mrb_state *mrb) {
  mrb_value k = mrb_obj_value(mrb->kernel_module);
  mrb_sym iv = mrb_intern_lit(mrb, "__oruby_kept_sources");
  mrb_value ary = mrb_iv_get(mrb, k, iv);
  if (!mrb_array_p(ary)) {
    ary = mrb_ary_new(mrb);
    mrb_iv_set(mrb, k, iv, ary);
  }
  return ary;
}

/* the source of code eval runs (file nil: eval's own name) */
void _orb_keep_source(mrb_state *mrb, mrb_value file, mrb_int line, mrb_value src) {
  if (!mrb_string_p(src)) return;
  mrb_value ary = kept_sources(mrb);
  if (!mrb_string_p(file)) file = mrb_nil_value();
  mrb_value rec[3] = {file, mrb_fixnum_value(line > 0 ? line : 1), src};
  mrb_ary_push(mrb, ary, mrb_ary_new_from_values(mrb, 3, rec));
  if (RARRAY_LEN(ary) > ORB_KEPT_SOURCES) mrb_ary_shift(mrb, ary);
}

static mrb_value kept_sources_m(mrb_state *mrb, mrb_value self) {
  return kept_sources(mrb);
}

/* Kernel.__oruby_keep_source(file, line, src): a source given by name (-e) */
static mrb_value keep_source_m(mrb_state *mrb, mrb_value self) {
  mrb_value file, src;
  mrb_int line;
  mrb_get_args(mrb, "oiS", &file, &line, &src);
  _orb_keep_source(mrb, file, line, src);
  return mrb_nil_value();
}

/* ---- the instruction a frame is at ---- */

enum { OT_Z, OT_B, OT_BB, OT_BBB, OT_BS, OT_BSS, OT_S, OT_W };
static const uint8_t op_types[] = {
#define OPCODE(x, t) OT_##t,
#include <mruby/ops.h>
#undef OPCODE
};
static const char *op_names[] = {
#define OPCODE(x, t) #x,
#include <mruby/ops.h>
#undef OPCODE
};

static uint32_t rd(const mrb_code *p, int n) { return n == 2 ? (uint32_t)(p[0] << 8 | p[1]) : p[0]; }

/* [opcode name, symbol operand (sends, constants, variables) or nil] of the
   instruction at byte idx of irep's iseq */
mrb_value _orb_insn_site(mrb_state *mrb, const mrb_irep *irep, uint32_t idx) {
  if (!irep || !irep->iseq) return mrb_nil_value();
  const mrb_code *p = irep->iseq;
  uint32_t i = 0;
  while (i < irep->ilen) {
    int ext = 0;
    uint8_t op = p[i++];
    if (op == OP_EXT1 || op == OP_EXT2 || op == OP_EXT3) {
      ext = op - OP_EXT1 + 1;
      if (i >= irep->ilen) break;
      op = p[i++];
    }
    if (op >= sizeof(op_types)) break;
    int asz = (ext & 1) ? 2 : 1, bsz = (ext & 2) ? 2 : 1;
    uint32_t b = 0;
    switch (op_types[op]) {
    case OT_Z: break;
    case OT_B: i += asz; break;
    case OT_BB: i += asz; b = rd(p + i, bsz); i += bsz; break;
    case OT_BBB: i += asz; b = rd(p + i, bsz); i += bsz + 1; break;
    case OT_BS: i += asz + 2; break;
    case OT_BSS: i += asz + 4; break;
    case OT_S: i += 2; break;
    case OT_W: i += 3; break;
    }
    if (idx < i) {
      mrb_value sym = mrb_nil_value();
      switch (op) {
      case OP_SEND: case OP_SEND0: case OP_SENDB: case OP_SSEND: case OP_SSEND0: case OP_SSENDB:
      case OP_GETCONST: case OP_SETCONST: case OP_GETMCNST: case OP_SETMCNST:
      case OP_GETCV: case OP_SETCV: case OP_GETGV: case OP_SETGV: case OP_GETIV: case OP_SETIV:
        if (b < irep->slen) sym = mrb_symbol_value(irep->syms[b]);
        break;
      default: break;
      }
      mrb_value rec[2] = {mrb_symbol_value(mrb_intern_cstr(mrb, op_names[op])), sym};
      return mrb_ary_new_from_values(mrb, 2, rec);
    }
  }
  return mrb_nil_value();
}

void _orb_srcrange_init(mrb_state *mrb) {
  struct RClass *k = mrb->kernel_module;
  mrb_define_module_function(mrb, k, "__oruby_prism_nodes", prism_nodes_m, MRB_ARGS_ARG(2, 1));
  mrb_define_module_function(mrb, k, "__oruby_kept_sources", kept_sources_m, MRB_ARGS_NONE());
  mrb_define_module_function(mrb, k, "__oruby_keep_source", keep_source_m, MRB_ARGS_REQ(3));
}
