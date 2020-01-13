package oruby

// #include "go-mrb.h"
import "C"
import (
	"io/ioutil"
	"runtime"
	"unsafe"
)

// enum irep_pool_type
const (
	IrepTtString = iota
	IrepTtFixnum
	IrepTtFloat
)

// MrbIseqNoFree constant
const MrbIseqNoFree = 1

// MrbIrep irep struct
type MrbIrep struct{ p *C.mrb_irep }

// AddIrep irep api
func (mrb *MrbState) AddIrep() MrbIrep {
	return MrbIrep{C.mrb_add_irep(mrb.p)}
}

// LoadIrep irep from buffer bytes array
func (mrb *MrbState) LoadIrep(buffer []byte) (Value, error) {
	return mrb.try(func() C.mrb_value {
		bufLen := len(buffer)
		if bufLen == 0 {
			return C.mrb_load_irep(mrb.p, nil)
		}
		ret := C.mrb_load_irep_buf(mrb.p, unsafe.Pointer(&buffer[0]), C.size_t(bufLen))
		runtime.KeepAlive(buffer)

		return ret
	})
}

// LoadIrepBuf irep load from buffer, same as LoadIrep()
func (mrb *MrbState) LoadIrepBuf(buffer []byte) (Value, error) {
	return mrb.LoadIrep(buffer)
}

// LoadIrepFile irep load from buffer
func (mrb *MrbState) LoadIrepFile(filename string) (Value, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return mrb.NilValue(), err
	}

	return mrb.LoadIrep(data)
	// C.mrb_load_irep_file() is never called
}

// LoadIrepCxt irep api
func (mrb *MrbState) LoadIrepCxt(buffer []byte, context *MrbcContext) (Value, error) {
	return mrb.try(func() C.mrb_value {
		bufLen := len(buffer)
		if bufLen == 0 {
			return C.mrb_load_irep_cxt(mrb.p, nil, context.p)
		}

		ret := C.mrb_load_irep_buf_cxt(mrb.p, unsafe.Pointer(&buffer[0]), C.size_t(bufLen), context.p)
		runtime.KeepAlive(buffer)

		return ret
	})
}

// LoadIrepBuf for context
func (c *MrbcContext) LoadIrepBuf(buffer []byte) (Value, error) {
	return c.mrb.LoadIrepCxt(buffer, c)
}

// LoadIrepFile for context
func (c *MrbcContext) LoadIrepFile(filename string) (Value, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return c.mrb.NilValue(), err
	}

	return c.mrb.LoadIrepCxt(data, c)
	// C.mrb_load_irep_file_cxt() is never called
}

// LoadIrepBufCxt irep load form buffer with context, same as LoadIrepCxt
func (mrb *MrbState) LoadIrepBufCxt(buffer []byte, context *MrbcContext) (Value, error) {
	return mrb.LoadIrepCxt(buffer, context)
}

// IrepFree free irep
func (mrb *MrbState) IrepFree(irep MrbIrep) { C.mrb_irep_free(mrb.p, irep.p) }

// IrepIncref increase reference to irep
func (mrb *MrbState) IrepIncref(irep MrbIrep) { C.mrb_irep_incref(mrb.p, irep.p) }

// IrepDecref decrease reference to irep
func (mrb *MrbState) IrepDecref(irep MrbIrep) { C.mrb_irep_decref(mrb.p, irep.p) }

// IrepCutref cut reference form irep
func (mrb *MrbState) IrepCutref(irep MrbIrep) { C.mrb_irep_cutref(mrb.p, irep.p) }

// IrepRemoveLV removes local variables from irep
func (mrb *MrbState) IrepRemoveLV(irep MrbIrep) { C.mrb_irep_remove_lv(mrb.p, irep.p) }
