/* ORuby C API: what MRI declares in ruby/util.h is in ruby/ruby.h */
#ifndef ORUBY_CAPI_UTIL_H
#define ORUBY_CAPI_UTIL_H 1
#include "ruby/ruby.h"
#ifndef ORUBY_CAPI_IMPL
# undef strtod
# define strtod(s, e) ruby_strtod((s), (e))
#endif
#endif
