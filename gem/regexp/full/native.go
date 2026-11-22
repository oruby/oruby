package full

/*
#cgo CFLAGS: -I${SRCDIR}/../../../mruby/include -I${SRCDIR}/../../../mruby/build/host/include
#cgo CFLAGS: -I${SRCDIR}/../../../mruby/mrbgems/mruby-regexp/include
#cgo CFLAGS: -DMRB_INT64 -DMRB_HIGH_PROFILE -DMRB_NO_BOXING -DMRB_SYMBOL_MAX=0 -DMRB_USE_DEBUG_HOOK -DMRB_UTF8_STRING -DMRB_USE_REFINEMENTS -DMRB_USE_GC_WEAK -DHAVE_MRUBY_REGEXP_GEM -DHAVE_MRUBY_ENCODING_GEM
#cgo CFLAGS: -DMRB_USE_RATIONAL -DMRB_USE_SET -DMRB_USE_COERCE -DMRB_USE_BIGINT -DMRB_USE_COMPLEX
#include "native.h"
*/
import "C"

import (
	"errors"
	"runtime"
	"sync"
	"unsafe"

	rx "github.com/oruby/oruby/gem/regexp"
)

// The native API allocates through mruby, and matching mutates pattern
// workspace. A private, serialized state keeps native patterns independent
// of the lifetime of any Ruby embedding state.
var native struct {
	sync.Mutex
	state *C.mrb_state
}

type nativeEngine struct {
	pattern *C.mrb_regexp_pattern
	names   []string
	binary  bool
}

func compileNative(source string, options int) (*nativeEngine, error) {
	native.Lock()
	defer native.Unlock()
	if native.state == nil {
		native.state = C.orb_regexp_state()
		if native.state == nil {
			return nil, rx.NewRegexpError("cannot initialize native regexp engine")
		}
	}

	flags := C.uint32_t(C.RE_FLAG_MULTILINE)
	if options&rx.IgnoreCase != 0 {
		flags |= C.RE_FLAG_IGNORECASE
	}
	if options&rx.Extended != 0 {
		flags |= C.RE_FLAG_EXTENDED
	}
	if options&rx.Multiline != 0 {
		flags |= C.RE_FLAG_DOTALL
	}
	binary := options&rx.NoEncoding != 0
	var message [512]C.char
	pattern := C.orb_regexp_compile(native.state, (*C.char)(unsafe.Pointer(unsafe.StringData(source))),
		C.size_t(len(source)), flags, C.mrb_bool(binary), &message[0], C.size_t(len(message)))
	if pattern == nil {
		msg := C.GoString(&message[0])
		if msg == "" {
			msg = "native regexp compilation failed: /" + source + "/"
		}
		return nil, rx.NewRegexpError(msg)
	}

	names := make([]string, int(pattern.num_captures))
	for _, entry := range unsafe.Slice(pattern.named_captures, int(pattern.num_named)) {
		names[entry.group] = C.GoStringN(entry.name, C.int(entry.name_len))
	}
	e := &nativeEngine{pattern: pattern, names: names, binary: binary}
	runtime.AddCleanup(e, freeNativePattern, pattern)
	return e, nil
}

func freeNativePattern(pattern *C.mrb_regexp_pattern) {
	native.Lock()
	defer native.Unlock()
	C.mrb_re_free(native.state, pattern)
}

func (e *nativeEngine) NumSubexp() int        { return len(e.names) - 1 }
func (e *nativeEngine) SubexpNames() []string { return e.names }

func (e *nativeEngine) FindAt(s string, pos int) ([]int, error) {
	var captures [C.RE_MAX_CAPTURES*2 + 2]C.int
	size := len(e.names) * 2
	if e.pattern.keep_slot != 0 {
		size = int(e.pattern.keep_slot) + 2
	}
	native.Lock()
	count := C.orb_regexp_match(native.state, e.pattern, (*C.char)(unsafe.Pointer(unsafe.StringData(s))),
		C.size_t(len(s)), C.mrb_int(pos), &captures[0], C.int(size), C.mrb_bool(e.binary))
	native.Unlock()
	runtime.KeepAlive(e)
	switch count {
	case 0:
		return nil, nil
	case C.RE_OVER_STACK_LIMIT, C.RE_OVER_STEP_LIMIT:
		return nil, errors.New("regexp match timeout: native engine limit exceeded")
	case C.RE_NOMEM:
		return nil, errors.New("native regexp workspace allocation failed")
	}
	offsets := make([]int, len(e.names)*2)
	for i := range offsets {
		offsets[i] = int(captures[i])
	}
	return offsets, nil
}
