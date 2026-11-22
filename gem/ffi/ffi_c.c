#include "ffi_c.h"
#include <stdio.h>

extern void orubyFFIClosureTrampoline(uintptr_t handle, void *ret, void **args);

ffi_abi oruby_ffi_abi(int conv) {
#if defined(_WIN32) && !defined(_WIN64) && defined(FFI_STDCALL)
    if (conv == ORUBY_FFI_CONV_STDCALL) {
        return FFI_STDCALL;
    }
#else
    (void)conv;
#endif
    return FFI_DEFAULT_ABI;
}

ffi_type *oruby_ffi_primitive_type(int kind) {
    switch (kind) {
    case ORUBY_FFI_VOID:    return &ffi_type_void;
    case ORUBY_FFI_BOOL:    return &ffi_type_uint8;
    case ORUBY_FFI_INT8:    return &ffi_type_sint8;
    case ORUBY_FFI_UINT8:   return &ffi_type_uint8;
    case ORUBY_FFI_INT16:   return &ffi_type_sint16;
    case ORUBY_FFI_UINT16:  return &ffi_type_uint16;
    case ORUBY_FFI_INT32:   return &ffi_type_sint32;
    case ORUBY_FFI_UINT32:  return &ffi_type_uint32;
    case ORUBY_FFI_INT64:   return &ffi_type_sint64;
    case ORUBY_FFI_UINT64:  return &ffi_type_uint64;
    case ORUBY_FFI_LONG:    return &ffi_type_slong;
    case ORUBY_FFI_ULONG:   return &ffi_type_ulong;
    case ORUBY_FFI_FLOAT:   return &ffi_type_float;
    case ORUBY_FFI_DOUBLE:  return &ffi_type_double;
    case ORUBY_FFI_POINTER:
    case ORUBY_FFI_STRING:
    case ORUBY_FFI_STRPTR:
    case ORUBY_FFI_CALLBACK:
        return &ffi_type_pointer;
    default:
        return NULL;
    }
}

ffi_type *oruby_ffi_new_struct_type(ffi_type **fields, int nfields) {
    ffi_type *st = (ffi_type *)calloc(1, sizeof(ffi_type));
    if (!st) return NULL;
    ffi_type **elems = (ffi_type **)calloc((size_t)(nfields + 1), sizeof(ffi_type *));
    if (!elems) {
        free(st);
        return NULL;
    }
    for (int i = 0; i < nfields; i++) {
        elems[i] = fields[i];
    }
    elems[nfields] = NULL;
    st->size = 0;
    st->alignment = 0;
    st->type = FFI_TYPE_STRUCT;
    st->elements = elems;
    return st;
}

void oruby_ffi_free_struct_type(ffi_type *st) {
    if (!st) return;
    free(st->elements);
    free(st);
}

static int capture_last_error(void) {
#ifdef _WIN32
    DWORD werr = GetLastError();
    int cerr = errno;
    return (werr != 0) ? (int)werr : cerr;
#else
    return errno;
#endif
}

static void clear_last_error(void) {
    errno = 0;
#ifdef _WIN32
    SetLastError(0);
#endif
}

int oruby_ffi_call_cif(
    ffi_cif *cif,
    void *fn,
    void *rvalue,
    void **avalues,
    int *out_errno
) {
    clear_last_error();
    ffi_call(cif, FFI_FN(fn), rvalue, avalues);
    if (out_errno) {
        *out_errno = capture_last_error();
    }
    return 0;
}

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
) {
    ffi_cif cif;
    ffi_status st = ffi_prep_cif_var(&cif, oruby_ffi_abi(conv), nfixed, ntotal, rtype, atypes);
    if (st != FFI_OK) {
        return (int)st;
    }
    clear_last_error();
    ffi_call(&cif, FFI_FN(fn), rvalue, avalues);
    if (out_errno) {
        *out_errno = capture_last_error();
    }
    return 0;
}

static void closure_bridge(ffi_cif *cif, void *ret, void **args, void *user_data) {
    (void)cif;
    orubyFFIClosureTrampoline((uintptr_t)user_data, ret, args);
}

void *oruby_ffi_alloc_closure(
    ffi_cif *cif,
    uintptr_t handle,
    void **out_codeloc
) {
    void *codeloc = NULL;
    ffi_closure *closure = (ffi_closure *)ffi_closure_alloc(sizeof(ffi_closure), &codeloc);
    if (!closure) {
        return NULL;
    }
    ffi_status st = ffi_prep_closure_loc(closure, cif, closure_bridge, (void *)handle, codeloc);
    if (st != FFI_OK) {
        ffi_closure_free(closure);
        return NULL;
    }
    *out_codeloc = codeloc;
    return closure;
}

void oruby_ffi_free_closure(void *closure) {
    if (closure) {
        ffi_closure_free(closure);
    }
}

#ifdef _WIN32

static char *win32_format_error(DWORD code) {
    LPSTR msg_buf = NULL;
    DWORD len = FormatMessageA(
        FORMAT_MESSAGE_ALLOCATE_BUFFER | FORMAT_MESSAGE_FROM_SYSTEM | FORMAT_MESSAGE_IGNORE_INSERTS,
        NULL,
        code,
        MAKELANGID(LANG_NEUTRAL, SUBLANG_DEFAULT),
        (LPSTR)&msg_buf,
        0,
        NULL
    );
    char *out = NULL;
    if (len > 0 && msg_buf) {
        while (len > 0 && (msg_buf[len - 1] == '\r' || msg_buf[len - 1] == '\n')) {
            msg_buf[--len] = '\0';
        }
        out = _strdup(msg_buf);
        LocalFree(msg_buf);
    } else {
        char tmp[64];
        snprintf(tmp, sizeof(tmp), "Win32 error %lu", (unsigned long)code);
        out = _strdup(tmp);
    }
    return out;
}

void *oruby_ffi_dlopen(const char *path, int flags, char **err_out) {
    (void)flags;
    HMODULE h = NULL;
    if (path == NULL) {
        h = GetModuleHandleA(NULL);
    } else {
        h = LoadLibraryA(path);
    }
    if (!h && err_out) {
        *err_out = win32_format_error(GetLastError());
    }
    return (void *)h;
}

void *oruby_ffi_dlsym(void *handle, const char *name, char **err_out) {
    FARPROC sym = GetProcAddress((HMODULE)handle, name);
    if (!sym && (HMODULE)handle == GetModuleHandleA(NULL)) {
        /* Fallback to already-loaded CRT/kernel32 when searching CURRENT_PROCESS */
        static const char *const fallbacks[] = { "ucrtbase.dll", "msvcrt.dll", "kernel32.dll", NULL };
        for (int i = 0; fallbacks[i] != NULL && !sym; i++) {
            HMODULE m = GetModuleHandleA(fallbacks[i]);
            if (m) {
                sym = GetProcAddress(m, name);
            }
        }
    }
    if (!sym && err_out) {
        *err_out = win32_format_error(GetLastError());
        return NULL;
    }
    return (void *)(uintptr_t)sym;
}

int oruby_ffi_dlclose(void *handle) {
    if (!handle || (HMODULE)handle == GetModuleHandleA(NULL)) {
        return 0;
    }
    return FreeLibrary((HMODULE)handle) ? 0 : -1;
}

#else

void *oruby_ffi_dlopen(const char *path, int flags, char **err_out) {
    dlerror();
    void *h = dlopen(path, flags);
    if (!h && err_out) {
        const char *e = dlerror();
        *err_out = e ? strdup(e) : strdup("dlopen failed");
    }
    return h;
}

void *oruby_ffi_dlsym(void *handle, const char *name, char **err_out) {
    dlerror();
    void *sym = dlsym(handle, name);
    const char *e = dlerror();
    if (e && err_out) {
        *err_out = strdup(e);
        return NULL;
    }
    return sym;
}

int oruby_ffi_dlclose(void *handle) {
    if (!handle) return 0;
    return dlclose(handle);
}

#endif

size_t oruby_ffi_sizeof_long(void) {
    return sizeof(long);
}

size_t oruby_ffi_sizeof_pointer(void) {
    return sizeof(void *);
}
