/* ORuby C API: MRI's generated configuration header; what extensions test
 * with HAVE_ macros is in ruby/ruby.h. */
#ifndef ORUBY_CAPI_CONFIG_H
#define ORUBY_CAPI_CONFIG_H 1
#define HAVE_STDINT_H 1
#define HAVE_STDDEF_H 1
#define HAVE_STDLIB_H 1
#define HAVE_STRING_H 1
#define HAVE_UNISTD_H 1
#define HAVE_SYS_TYPES_H 1
#define HAVE_LONG_LONG 1
/* what MRI's configure finds on every platform ORuby builds on */
#define HAVE_STDIO_H 1
#define HAVE_INTTYPES_H 1
#define HAVE_STRINGS_H 1
#define HAVE_SYS_STAT_H 1
#define HAVE_WCHAR_H 1
#define HAVE_PTHREAD_H 1
#define HAVE_DIRENT_H 1
#define HAVE__BOOL 1
#define HAVE_STDBOOL_H 1
#define HAVE_SYS_WAIT_H 1
#define HAVE_GRP_H 1
#define HAVE_FCNTL_H 1
#define HAVE_FLOAT_H 1
#define HAVE_LANGINFO_H 1
#define HAVE_LIMITS_H 1
#define HAVE_LOCALE_H 1
#define HAVE_PWD_H 1
#define HAVE_STDALIGN_H 1
#define HAVE_STDATOMIC_H 1
#define HAVE_SYS_FILE_H 1
#define HAVE_SYS_IOCTL_H 1
#define HAVE_SYS_PARAM_H 1
#define HAVE_SYS_RESOURCE_H 1
#define HAVE_SYS_SELECT_H 1
#define HAVE_SYS_SOCKET_H 1
#define HAVE_SYS_TIME_H 1
#define HAVE_SYS_TIMES_H 1
#define HAVE_SYS_UIO_H 1
#define HAVE_TIME_H 1
#define HAVE_UTIME_H 1
#define HAVE_ALLOCA_H 1
#define HAVE_VA_ARGS_MACRO 1
#define HAVE_STRUCT_TIMEVAL 1
#define HAVE_STRUCT_TIMESPEC 1
#define HAVE_INTPTR_T 1
#define HAVE_UINTPTR_T 1
#define HAVE_SSIZE_T 1
#define SIZEOF_INT 4
#define SIZEOF_LONG 8
#define SIZEOF_LONG_LONG 8
#define SIZEOF_VOIDP 8
#define SIZEOF_SIZE_T 8
/* the rest of MRI's sizes: the same on every LP64 platform ORuby builds on,
   but dev_t (glibc's is 8 bytes) and long double (the compiler's) */
#define SIZEOF_SHORT 2
#define SIZEOF___INT64 0
#ifdef __SIZEOF_INT128__
#define SIZEOF___INT128 16
#define SIZEOF_INT128_T SIZEOF___INT128
#define SIZEOF_UINT128_T SIZEOF___INT128
#endif
#define SIZEOF_OFF_T 8
#define SIZEOF_FLOAT 4
#define SIZEOF_DOUBLE 8
#ifdef __SIZEOF_LONG_DOUBLE__
#define SIZEOF_LONG_DOUBLE __SIZEOF_LONG_DOUBLE__
#endif
#define SIZEOF_TIME_T 8
#define SIZEOF_CLOCK_T 8
#define SIZEOF_PTRDIFF_T 8
#ifdef __linux__
#define SIZEOF_DEV_T 8
#else
#define SIZEOF_DEV_T 4
#endif
#define SIZEOF_STRUCT_STAT_ST_SIZE SIZEOF_OFF_T
#define SIZEOF_STRUCT_STAT_ST_BLOCKS SIZEOF_OFF_T
#define SIZEOF_STRUCT_STAT_ST_INO SIZEOF_LONG
#define SIZEOF_STRUCT_STAT_ST_DEV SIZEOF_DEV_T
#define SIZEOF_STRUCT_STAT_ST_RDEV SIZEOF_DEV_T
#define SIZEOF_STRUCT_TIMEVAL_TV_SEC SIZEOF_TIME_T
#define SIZEOF_INT8_T 1
#define SIZEOF_UINT8_T 1
#define SIZEOF_INT16_T 2
#define SIZEOF_UINT16_T 2
#define SIZEOF_INT32_T 4
#define SIZEOF_UINT32_T 4
#define SIZEOF_INT64_T 8
#define SIZEOF_UINT64_T 8
#define SIZEOF_INTPTR_T 8
#define SIZEOF_UINTPTR_T 8
#define SIZEOF_SSIZE_T 8
#endif
