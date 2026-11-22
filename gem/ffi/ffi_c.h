#ifndef ORUBY_FFI_C_H
#define ORUBY_FFI_C_H

#include <stdint.h>
#include <stddef.h>
#include <stdlib.h>
#include <string.h>
#include <errno.h>

#ifdef _WIN32
#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#include <windows.h>
#include <ffi.h>
#elif defined(__APPLE__)
#include <ffi/ffi.h>
#include <dlfcn.h>
#else
#include <ffi.h>
#include <dlfcn.h>
#endif

/* Portable dynamic loader flags */
#ifdef _WIN32
enum {
    ORUBY_RTLD_LAZY   = 1,
    ORUBY_RTLD_NOW    = 2,
    ORUBY_RTLD_LOCAL  = 4,
    ORUBY_RTLD_GLOBAL = 8
};
#else
enum {
    ORUBY_RTLD_LAZY   = RTLD_LAZY,
    ORUBY_RTLD_NOW    = RTLD_NOW,
    ORUBY_RTLD_LOCAL  = RTLD_LOCAL,
    ORUBY_RTLD_GLOBAL = RTLD_GLOBAL
};
#endif

/* Type kind constants shared with Go */
enum {
    ORUBY_FFI_VOID = 0,
    ORUBY_FFI_BOOL,
    ORUBY_FFI_INT8,
    ORUBY_FFI_UINT8,
    ORUBY_FFI_INT16,
    ORUBY_FFI_UINT16,
    ORUBY_FFI_INT32,
    ORUBY_FFI_UINT32,
    ORUBY_FFI_INT64,
    ORUBY_FFI_UINT64,
    ORUBY_FFI_LONG,
    ORUBY_FFI_ULONG,
    ORUBY_FFI_FLOAT,
    ORUBY_FFI_DOUBLE,
    ORUBY_FFI_POINTER,
    ORUBY_FFI_STRING,
    ORUBY_FFI_STRPTR,
    ORUBY_FFI_STRUCT_VAL,
    ORUBY_FFI_CALLBACK
};

/* Calling conventions */
enum {
    ORUBY_FFI_CONV_DEFAULT = 0,
    ORUBY_FFI_CONV_STDCALL = 1
};

ffi_abi oruby_ffi_abi(int conv);
ffi_type *oruby_ffi_primitive_type(int kind);
ffi_type *oruby_ffi_new_struct_type(ffi_type **fields, int nfields);
void oruby_ffi_free_struct_type(ffi_type *st);

int oruby_ffi_call_cif(
    ffi_cif *cif,
    void *fn,
    void *rvalue,
    void **avalues,
    int *out_errno
);

int oruby_ffi_call_var(
    int conv,
    ffi_type *rtype,
    unsigned int nfixed,
    unsigned int ntotal,
    ffi_type **atypes,
    void *fn,
    void *rvalue,
    void **avalues,
    int *out_errno
);

void *oruby_ffi_alloc_closure(
    ffi_cif *cif,
    uintptr_t handle,
    void **out_codeloc
);
void oruby_ffi_free_closure(void *closure);

void *oruby_ffi_dlopen(const char *path, int flags, char **err_out);
void *oruby_ffi_dlsym(void *handle, const char *name, char **err_out);
int oruby_ffi_dlclose(void *handle);

size_t oruby_ffi_sizeof_long(void);
size_t oruby_ffi_sizeof_pointer(void);

static inline void *oruby_ffi_u2p(uintptr_t a) {
    return (void *)a;
}

#endif
