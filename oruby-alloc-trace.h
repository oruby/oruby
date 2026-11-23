/* Included by the generated GC source. Records own their text and never
 * retain Ruby objects, so tracing cannot keep an otherwise dead object alive. */
#include <mruby/array.h>
#include <mruby/class.h>
#include <mruby/data.h>
#include <mruby/debug.h>
#include <mruby/internal.h>
#include <mruby/proc.h>
#include <mruby/string.h>
#include <mruby/variable.h>
#include <stdlib.h>
#include <string.h>

#define ORUBY_TRACE_ACTIVE (1u << 14)
#define ORUBY_TRACE_HISTORY (1u << 13)

struct orb_alloc_record {
  struct orb_alloc_record *next;
  struct RBasic *object;
  uint32_t generation;
  int32_t line;
  size_t file_len, class_len, method_len;
  char text[];
};

struct orb_alloc_trace {
  size_t depth, count, capacity;
  uint32_t generation;
  struct orb_alloc_record **buckets;
};

static void orb_alloc_destroy(mrb_state *, void *);
static const mrb_data_type orb_alloc_type = {"allocation_trace", orb_alloc_destroy};

static struct orb_alloc_trace *
orb_alloc_state(mrb_state *mrb)
{
  mrb_value v = mrb_obj_iv_get(mrb, (struct RObject*)mrb->kernel_module, MRB_IVSYM(__oruby_alloc_trace));
  return mrb_data_p(v) && DATA_TYPE(v) == &orb_alloc_type ? DATA_PTR(v) : NULL;
}

static size_t
orb_alloc_bucket(const struct RBasic *object, size_t capacity)
{
  uintptr_t n = (uintptr_t)object >> 3;
  return (size_t)((n ^ (n >> 13)) & (capacity - 1));
}

static mrb_bool
orb_alloc_grow(struct orb_alloc_trace *trace)
{
  size_t capacity = trace->capacity ? trace->capacity * 2 : 64;
  if (capacity < trace->capacity || capacity > SIZE_MAX / sizeof(*trace->buckets)) return FALSE;
  struct orb_alloc_record **buckets = calloc(capacity, sizeof(*buckets));
  if (!buckets) return FALSE;
  for (size_t i = 0; i < trace->capacity; ++i) {
    struct orb_alloc_record *record = trace->buckets[i];
    while (record) {
      struct orb_alloc_record *next = record->next;
      size_t j = orb_alloc_bucket(record->object, capacity);
      record->next = buckets[j];
      buckets[j] = record;
      record = next;
    }
  }
  free(trace->buckets);
  trace->capacity = capacity;
  trace->buckets = buckets;
  return TRUE;
}

static void
orb_alloc_record(mrb_state *mrb, struct RBasic *object)
{
  struct orb_alloc_trace *trace = orb_alloc_state(mrb);
  if (!trace || !mrb->c || !mrb->c->cibase) return;

  const mrb_callinfo *current = mrb->c->ci;
  const char *file = NULL, *klass = NULL, *method = NULL;
  mrb_int class_len = 0, method_len = 0;
  int32_t line = 0;
  char short_method[8];
  if (current->mid && current->mid != MRB_SYM(__oruby_allocate_instance) &&
      (!current->proc || MRB_PROC_CFUNC_P(current->proc) || MRB_PROC_SCOPE_P(current->proc))) {
    method = mrb_sym_name_len(mrb, current->mid, &method_len);
    if (method && method_len <= sizeof(short_method)) {
      memcpy(short_method, method, method_len);
      method = short_method;
    }
    struct RClass *owner = mrb_vm_ci_target_class(current);
    if (owner && owner->tt == MRB_TT_ICLASS) owner = owner->c;
    if (owner && (owner->tt == MRB_TT_CLASS || owner->tt == MRB_TT_MODULE)) {
      mrb_value name = mrb_obj_iv_get(mrb, (struct RObject*)owner, MRB_SYM(__classname__));
      if (mrb_symbol_p(name)) klass = mrb_sym_name_len(mrb, mrb_symbol(name), &class_len);
      else if (mrb_string_p(name)) {
        klass = RSTRING_PTR(name);
        class_len = RSTRING_LEN(name);
      }
    }
  }
  for (ptrdiff_t index = current - mrb->c->cibase; index >= 0; --index) {
    const mrb_callinfo *ci = mrb->c->cibase + index;
    const struct RProc *proc = ci->proc;
    if (!proc || MRB_PROC_CFUNC_P(proc) || MRB_PROC_ALIAS_P(proc) || !ci->pc) continue;
    const mrb_irep *irep = proc->body.irep;
    if (ci->pc > irep->iseq && ci->pc <= irep->iseq + irep->ilen &&
        mrb_debug_get_position(mrb, irep, (uint32_t)(ci->pc - irep->iseq - 1), &line, &file) && file) break;
    file = NULL;
  }
  size_t file_len = file ? strlen(file) : 0;
  if (!klass) class_len = 0;
  if (!method) method_len = 0;
  if (trace->count >= trace->capacity && !orb_alloc_grow(trace)) return;

  struct orb_alloc_record *record = malloc(sizeof(*record) + file_len + class_len + method_len);
  if (!record) return;
  record->object = object;
  record->generation = trace->generation;
  record->line = line;
  record->file_len = file_len;
  record->class_len = class_len;
  record->method_len = method_len;
  if (file_len) memcpy(record->text, file, file_len);
  if (class_len) memcpy(record->text + file_len, klass, class_len);
  if (method_len) memcpy(record->text + file_len + class_len, method, method_len);
  size_t slot = orb_alloc_bucket(object, trace->capacity);
  record->next = trace->buckets[slot];
  trace->buckets[slot] = record;
  if (trace->count++ == 0) mrb->kernel_module->flags |= ORUBY_TRACE_HISTORY;
}

static void
orb_alloc_forget(mrb_state *mrb, struct RBasic *object)
{
  struct orb_alloc_trace *trace = orb_alloc_state(mrb);
  if (!trace || !trace->capacity) return;
  struct orb_alloc_record **link = &trace->buckets[orb_alloc_bucket(object, trace->capacity)];
  while (*link) {
    struct orb_alloc_record *record = *link;
    if (record->object == object) {
      *link = record->next;
      free(record);
      if (--trace->count == 0) mrb->kernel_module->flags &= ~ORUBY_TRACE_HISTORY;
      return;
    }
    link = &record->next;
  }
}

static void
orb_alloc_clear(struct orb_alloc_trace *trace)
{
  for (size_t i = 0; i < trace->capacity; ++i) {
    struct orb_alloc_record *record = trace->buckets[i];
    while (record) {
      struct orb_alloc_record *next = record->next;
      free(record);
      record = next;
    }
  }
  free(trace->buckets);
  trace->buckets = NULL;
  trace->capacity = trace->count = 0;
}

static void
orb_alloc_destroy(mrb_state *mrb, void *data)
{
  struct orb_alloc_trace *trace = data;
  if (trace) {
    orb_alloc_clear(trace);
    free(trace);
  }
}

mrb_value
oruby_alloc_trace_start(mrb_state *mrb, mrb_value self)
{
  struct orb_alloc_trace *trace = orb_alloc_state(mrb);
  if (!trace) {
    struct RData *owner = mrb_data_object_alloc(mrb, NULL, NULL, &orb_alloc_type);
    mrb_obj_iv_set(mrb, (struct RObject*)mrb->kernel_module, MRB_IVSYM(__oruby_alloc_trace), mrb_obj_value(owner));
    trace = calloc(1, sizeof(*trace));
    if (!trace) mrb_raise_nomemory(mrb);
    owner->data = trace;
  }
  ++trace->depth;
  mrb->kernel_module->flags |= ORUBY_TRACE_ACTIVE;
  return mrb_nil_value();
}

mrb_value
oruby_alloc_trace_stop(mrb_state *mrb, mrb_value self)
{
  struct orb_alloc_trace *trace = orb_alloc_state(mrb);
  if (trace && trace->depth && --trace->depth == 0) mrb->kernel_module->flags &= ~ORUBY_TRACE_ACTIVE;
  return mrb_nil_value();
}

mrb_value
oruby_alloc_trace_clear(mrb_state *mrb, mrb_value self)
{
  struct orb_alloc_trace *trace = orb_alloc_state(mrb);
  if (trace) orb_alloc_clear(trace);
  mrb->kernel_module->flags &= ~ORUBY_TRACE_HISTORY;
  return mrb_nil_value();
}

mrb_value
oruby_allocation_info(mrb_state *mrb, mrb_value self)
{
  mrb_value obj = mrb_get_arg1(mrb);
  if (mrb_immediate_p(obj)) return mrb_nil_value();
  struct orb_alloc_trace *trace = orb_alloc_state(mrb);
  if (!trace || !trace->capacity) return mrb_nil_value();
  struct RBasic *object = mrb_basic_ptr(obj);
  struct orb_alloc_record *record = trace->buckets[orb_alloc_bucket(object, trace->capacity)];
  while (record && record->object != object) record = record->next;
  if (!record) return mrb_nil_value();

  mrb_value values[] = {
    record->file_len ? mrb_str_new(mrb, record->text, record->file_len) : mrb_nil_value(),
    record->file_len ? mrb_int_value(mrb, record->line) : mrb_nil_value(),
    record->class_len ? mrb_str_new(mrb, record->text + record->file_len, record->class_len) : mrb_nil_value(),
    record->method_len ? mrb_symbol_value(mrb_intern(mrb, record->text + record->file_len + record->class_len, record->method_len)) : mrb_nil_value(),
    mrb_int_value(mrb, record->generation)
  };
  return mrb_ary_new_from_values(mrb, 5, values);
}
