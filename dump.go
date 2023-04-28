package oruby

// #include "go-mrb.h"
import "C"
import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"os"
	"runtime"
	"unsafe"
)

// Dump
const (
	MrbDumpDebugInfo = uint8(1)
	MrbDumpStatic    = uint8(2)
	DumpDebugInfo    = MrbDumpDebugInfo
)

// DumpIrep Go implementation
func (mrb *MrbState) DumpIrep(irep MrbIrep, flags uint8, writer io.Writer) (int, error) {
	ret := C._dump_irep(mrb.p, irep.p, C.uint8_t(flags))
	defer mrb.Free(Buff{unsafe.Pointer(ret.bin)})

	if int(ret.result) != MrbDumpOK {
		return int(ret.result), fmt.Errorf("dumping MrbIrep error. Code %v", ret.result)
	}

	buff := C.GoBytes(unsafe.Pointer(ret.bin), C.int(ret.bin_size))
	written, err := writer.Write(buff)
	if err != nil {
		return MrbDumpWriteFault, err
	}
	if written != int(ret.bin_size) {
		return MrbDumpWriteFault, fmt.Errorf("dumping MrbIrep error - write fault")
	}

	return MrbDumpOK, nil
	// C.mrb_dump_irep(mrb_state *mrb, mrb_irep *irep, uint8_t flags, uint8_t **bin, size_t *bin_size);
	// never called
}

// ReadIrep read irep
func (mrb *MrbState) ReadIrep(buffer []byte) (irep MrbIrep, err error) {
	bufLen := len(buffer)

	if bufLen == 0 {
		irep = MrbIrep{C.mrb_read_irep(mrb.p, nil), mrb}
	} else {
		irep = MrbIrep{C.mrb_read_irep_buf(mrb.p, unsafe.Pointer(&buffer[0]), C.size_t(bufLen)), mrb}
	}

	runtime.KeepAlive(buffer)

	if irep.IsNil() {
		return irep, errors.New("read irep error")
	}
	return irep, nil
}

// ReadIrepBuf reads irep from buffer, same as ReadIrep
func (mrb *MrbState) ReadIrepBuf(buffer []byte) (MrbIrep, error) {
	return mrb.ReadIrep(buffer)
}

// ReadIrepFile read irep from file
func (mrb *MrbState) ReadIrepFile(fileName string) (MrbIrep, error) {
	data, err := os.ReadFile(fileName)
	if err != nil {
		return MrbIrep{nil, mrb}, err
	}

	return mrb.ReadIrep(data)
	// C.mrb_read_irep_file() is never called
}

// LoadIrepFile irep load from file
func (mrb *MrbState) LoadIrepFile(filename string) (Value, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return mrb.NilValue(), err
	}

	return mrb.LoadIrep(data)
	// C.mrb_load_irep_file() is never called
}

// LoadIrepFileCxt irep load from file with mrbc context
func (mrb *MrbState) LoadIrepFileCxt(filename string, context *MrbcContext) (Value, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return mrb.NilValue(), err
	}
	return mrb.LoadIrepCxt(data, context)
	// C.mrb_load_irep_file_cxt() is never called
}

// dump/load error codes
//
// NOTE: MRB_DUMP_GENERAL_FAILURE is caused by
// unspecified issues like malloc failed.
const (
	MrbDumpOK                = 0
	MrbDumpGeneralFailure    = -1
	MrbDumpWriteFault        = -2
	MrbDumpReadFault         = -3
	MrbDumpInvalidFileHeader = -4
	MrbDumpInvalidIrep       = -5
	MrbDumpInvalidArgument   = -6
)

// MrbDumpNullSymLen is null symbol length
const MrbDumpNullSymLen = 0xFFFF

// Rite Binary File header
const (
	RiteBinaryIdent       = "RITE"
	RiteBinaryMajorVer    = "03"
	RiteBinaryMinorVer    = "00"
	RiteBinaryFormatVer   = RiteBinaryMajorVer + RiteBinaryMinorVer
	RiteCompilerName      = "MATZ"
	RiteCompilerVersion   = "0000"
	RiteVMVer             = "0300"
	RiteBinaryEOF         = "END\x00"
	RiteSectionIrepIdent  = "IREP"
	RiteSectionDebugIdent = "DBG\x00"
	RiteSectionLvIdent    = "LVAR"
)

// MrbDumpDefaultStrLen default str length
const MrbDumpDefaultStrLen = 128

// MrbDumpAlignment byte aligment for IREP dump
const MrbDumpAlignment = 4 //sizeof(uint32_t)

// RiteBinaryHeader RITE format binary header
type RiteBinaryHeader struct {
	BinaryIdent     [4]byte /* Binary Identifier */
	MajorVersion    [2]byte /* Binary Format Major Version */
	MinorVersion    [2]byte /* Binary Format Minor Version */
	BinarySize      [4]byte /* Binary Size */
	CompilerName    [4]byte /* Compiler name */
	CompilerVersion [4]byte
}

// RiteSectionHeader section header
type RiteSectionHeader struct {
	SectionIdent [4]byte
	SectionSize  [4]byte
}

// RiteSectionIrepHeader structure of IREP section header
type RiteSectionIrepHeader struct {
	RiteSectionHeader
	RiteVersion [4]byte /* Rite Instruction Specification Version */
}

// RiteSectionDebugHeader structure of debug header
type RiteSectionDebugHeader struct {
	RiteSectionHeader
}

// RiteSectionLvHeader structure of locals header
type RiteSectionLvHeader struct {
	RiteSectionHeader
}

// RiteLVNullMark NULL mark in RITE
const RiteLVNullMark = math.MaxUint16 // UINT16_MAX

// RiteBinaryFooter structure of rite binary footer
type RiteBinaryFooter struct {
	RiteSectionHeader
}

// DumpIrepBinary dumps IREP to IO writer
func (mrb *MrbState) DumpIrepBinary(irep MrbIrep, flags uint8, f io.Writer) (int, error) {
	return mrb.DumpIrep(irep, flags, f)
	// C.mrb_dump_irep_binary() is never called
}

// DumpIrepCFunc dumps IREP as C function
func (mrb *MrbState) DumpIrepCFunc(irep MrbIrep, flags uint8, f io.Writer, initName string) (int, error) {
	if initName == "" {
		return MrbDumpInvalidArgument, fmt.Errorf("invalid argument, missing init name")
	}

	data := make([]byte, 0, 512)
	buf := bytes.NewBuffer(data)

	result, err := mrb.DumpIrep(irep, flags, buf)
	if err != nil {
		return result, err
	}

	staticExtern := "#ifdef __cplusplus\nextern\n#endif"
	if (flags & MrbDumpStatic) != 0 {
		staticExtern = "static"
	}

	_, err = fmt.Fprintf(f,
		"#include <stdint.h>\n"+
			"%v\n"+
			"const uint8_t %v[] = {",
		staticExtern, initName)

	if err != nil {
		return MrbDumpWriteFault, err
	}

	for binIdx, b := range data {
		if (binIdx % 16) == 0 {
			if _, err := f.Write([]byte("\n")); err != nil {
				return MrbDumpWriteFault, err
			}
		}
		if _, err := fmt.Fprintf(f, "0x%02X,", b); err != nil {
			return MrbDumpWriteFault, err
		}
	}
	if _, err := f.Write([]byte("\n};\n")); err != nil {
		return MrbDumpWriteFault, err
	}

	return result, nil
}
