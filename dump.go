package oruby

// #include "go-mrb.h"
import "C"
import (
	"bytes"
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
	DumpDebugInfo  = uint8(1)
	DumpEndianBig  = uint8(2)
	DumpEndianLil  = uint8(4)
	DumpEndianNat  = uint8(6)
	DumpEndianMask = uint8(6)
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
	err = mrb.tryE(func() {
		bufLen := len(buffer)
		if bufLen == 0 {
			irep = MrbIrep{C.mrb_read_irep(mrb.p, nil)}
			return
		}
		irep = MrbIrep{C.mrb_read_irep_buf(mrb.p, unsafe.Pointer(&buffer[0]), C.size_t(bufLen))}
	})
	runtime.KeepAlive(buffer)
	return irep, err
}

// ReadIrepBuf reads irep from buffer, same as ReadIrep
func (mrb *MrbState) ReadIrepBuf(buffer []byte) (MrbIrep, error) {
	return mrb.ReadIrep(buffer)
}

// ReadIrepFile read irep from file
func (mrb *MrbState) ReadIrepFile(fileName string) (MrbIrep, error) {
	data, err := ioutil.ReadFile(fileName)
	if err != nil {
		return MrbIrep{}, err
	}

	return mrb.ReadIrep(data)
	// C.mrb_read_irep_file is never called
}

// dump/load error codes
//
// NOTE: MRB_DUMP_GENERAL_FAILURE is caused by
//unspecified issues like malloc failed.
const (
	MrbDumpOK                = 0
	MrbDumpGeneralFailure    = -1
	MrbDumpWriteFault        = -2
	MrbDumpReadFault         = -3
	MrbDumpCRCError          = -4
	MrbDumpInvalidFileHeader = -5
	MrbDumpInvalidIrep       = -6
	MrbDumpInvalidArgument   = -7
)

// MrbDumpNullSymLen is null symbol length
const MrbDumpNullSymLen = 0xFFFF

// Rite Binary File header
const (
	RiteBinaryIdent     = "RITE"
	RiteBinaryIdentLil  = "ETIR"
	RiteBinaryFormatVer = "0006"
	RiteCompilerName    = "MATZ"
	RiteCompilerVersion = "0000"

	RiteVMVer = "0002"

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
	BinaryVersion   [4]byte /* Binary Format Version */
	BinaryCrc       [2]byte /* Binary CRC */
	BinarySize      [4]byte /* Binary Size */
	CompilerName    [4]byte /* Compiler name */
	CompilerVersion [4]byte
}

// RiteSectionHeader section header
type RiteSectionHeader struct {
	SectionIdent [4]byte
	SectionSize  [4]byte
}

// RiteSectionIrepHeader stucture of IREP section header
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

// BigEndianP endian check, GO version borrowed from Go Tensorflow idea
func BigEndianP() bool {
	buf := [2]byte{}
	*(*uint16)(unsafe.Pointer(&buf[0])) = uint16(0xABCD)

	switch buf {
	case [2]byte{0xCD, 0xAB}:
		return false
	case [2]byte{0xAB, 0xCD}:
		return true
	default:
		panic("Could not determine endianness")
	}
}

func dumpBigendianP(flags uint8) bool {
	switch flags & DumpEndianNat {
	case DumpEndianBig:
		return true
	case DumpEndianLil:
		return false
	case DumpEndianNat:
		return BigEndianP()
	default:
		return BigEndianP()
	}
}

// Byte order enum
const (
	FlagByteOrderNative   = 2
	FlagByteOrderNoNative = 0
)

func dumpFlags(flags, native uint8) uint8 {
	if native == FlagByteOrderNative {
		if (flags & DumpEndianNat) == 0 {
			return (flags & DumpDebugInfo) | DumpEndianNat
		}
		return flags
	}
	if (flags & FlagByteOrderNative) == 0 {
		return (flags & DumpDebugInfo) | DumpEndianBig
	}
	return flags
}

// DumpIrepBinary dumps IREP to IO writer
func (mrb *MrbState) DumpIrepBinary(irep MrbIrep, flags uint8, f io.Writer) (int, error) {
	return mrb.DumpIrep(irep, dumpFlags(flags, FlagByteOrderNoNative), f)
	// C.mrb_dump_irep_binary() is neve called
}

// DumpIrepCFunc dumps IREP as C function
func (mrb *MrbState) DumpIrepCFunc(irep MrbIrep, flags uint8, f *os.File, initname string) (int, error) {
	cmode := C.CString("wb")
	defer C.free(unsafe.Pointer(cmode))
	init := C.CString(initname)
	defer C.free(unsafe.Pointer(init))
	file := C.fdopen(C.int(f.Fd()), cmode)
	defer C.fclose(file)

	ret := int(C.mrb_dump_irep_cfunc(mrb.p, irep.p, C.uchar(flags), file, init))
	if ret != MrbDumpOK {
		return ret, fmt.Errorf("dumping MrbIrep error. Code %v", ret)
	}

	return ret, nil
}

// DumpIrepCFunc2 dumps IREP as C function
func (mrb *MrbState) DumpIrepCFunc2(irep MrbIrep, flags uint8, f io.Writer, initname string) (int, error) {
	if initname == "" {
		return MrbDumpInvalidArgument, fmt.Errorf("invalid argument, missing init name")
	}

	data := make([]byte, 0, 512)
	buf := bytes.NewBuffer(data)

	result, err := mrb.DumpIrep(irep, dumpFlags(flags, FlagByteOrderNative), buf)
	if err != nil {
		return result, err
	}

	var warning string

	if !dumpBigendianP(flags) {
		warning = "/* dumped in little endian order.\n" +
			"   use `mrbc -E` option for big endian CPU. */\n"
	} else {
		warning = "/* dumped in big endian order.\n" +
			"   use `mrbc -e` option for better performance on little endian CPU. */\n"
	}

	_, err = fmt.Fprintf(f,
		"%v"+
			"#include <stdint.h>\n"+
			"#ifdef __cplusplus\n"+
			"extern const uint8_t %v[];\n"+
			"#endif\n"+
			"const uint8_t\n"+
			"#if defined __GNUC__\n"+
			"__attribute__((aligned(%v)))\n"+
			"#elif defined _MSC_VER\n"+
			"__declspec(align(%v))\n"+
			"#endif\n"+
			"%v[] = {",
		warning, initname, MrbDumpAlignment, MrbDumpAlignment, initname)

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
