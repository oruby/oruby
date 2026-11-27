/* ORuby C API: MRI's ruby/debug.h: TracePoint and the debug inspector */
#ifndef ORUBY_CAPI_DEBUG_H
#define ORUBY_CAPI_DEBUG_H 1
#include "ruby/ruby.h"

#ifdef __cplusplus
extern "C" {
#endif

typedef uint32_t rb_event_flag_t;
typedef void (*rb_event_hook_func_t)(rb_event_flag_t, VALUE, VALUE, ID, VALUE);

#define RUBY_EVENT_NONE          0x0000
#define RUBY_EVENT_LINE          0x0001
#define RUBY_EVENT_CLASS         0x0002
#define RUBY_EVENT_END           0x0004
#define RUBY_EVENT_CALL          0x0008
#define RUBY_EVENT_RETURN        0x0010
#define RUBY_EVENT_C_CALL        0x0020
#define RUBY_EVENT_C_RETURN      0x0040
#define RUBY_EVENT_RAISE         0x0080
#define RUBY_EVENT_ALL           0x00ff
#define RUBY_EVENT_B_CALL        0x0100
#define RUBY_EVENT_B_RETURN      0x0200
#define RUBY_EVENT_THREAD_BEGIN  0x0400
#define RUBY_EVENT_THREAD_END    0x0800
#define RUBY_EVENT_FIBER_SWITCH  0x1000
#define RUBY_EVENT_SCRIPT_COMPILED 0x2000
#define RUBY_EVENT_RESCUE        0x4000

VALUE rb_tracepoint_new(VALUE, rb_event_flag_t, void (*)(VALUE, void *), void *);
VALUE rb_tracepoint_enable(VALUE);
VALUE rb_tracepoint_disable(VALUE);
VALUE rb_tracepoint_enabled_p(VALUE);

typedef struct rb_debug_inspector_struct rb_debug_inspector_t;
typedef VALUE (*rb_debug_inspector_func_t)(const rb_debug_inspector_t *, void *);

VALUE rb_debug_inspector_open(rb_debug_inspector_func_t, void *);
VALUE rb_debug_inspector_backtrace_locations(const rb_debug_inspector_t *);
VALUE rb_debug_inspector_frame_self_get(const rb_debug_inspector_t *, long);
VALUE rb_debug_inspector_frame_class_get(const rb_debug_inspector_t *, long);
VALUE rb_debug_inspector_frame_binding_get(const rb_debug_inspector_t *, long);
VALUE rb_debug_inspector_frame_iseq_get(const rb_debug_inspector_t *, long);

#ifdef __cplusplus
}
#endif

#endif
