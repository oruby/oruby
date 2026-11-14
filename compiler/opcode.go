package compiler

import "fmt"

// Opcode represents an mruby virtual machine instruction opcode.
type Opcode uint8

// OpFormat represents the operand layout of an instruction.
type OpFormat uint8

const (
	FormatZ   OpFormat = iota // No operand
	FormatB                   // 8-bit operand (a)
	FormatBB                  // 8-bit + 8-bit operands (a, b)
	FormatBBB                 // 8-bit + 8-bit + 8-bit operands (a, b, c)
	FormatBS                  // 8-bit + 16-bit operands (a, b)
	FormatBSS                 // 8-bit + 16-bit + 16-bit operands (a, b, c)
	FormatS                   // 16-bit operand (a)
	FormatW                   // 24-bit operand (a)
)

// Opcodes corresponding to mruby 3.x RiteVM bytecode
const (
	OpNOP        Opcode = 0
	OpMOVE       Opcode = 1
	OpLOADL      Opcode = 2
	OpLOADI8     Opcode = 3
	OpLOADINEG   Opcode = 4
	OpLOADI__1   Opcode = 5
	OpLOADI_0    Opcode = 6
	OpLOADI_1    Opcode = 7
	OpLOADI_2    Opcode = 8
	OpLOADI_3    Opcode = 9
	OpLOADI_4    Opcode = 10
	OpLOADI_5    Opcode = 11
	OpLOADI_6    Opcode = 12
	OpLOADI_7    Opcode = 13
	OpLOADI16    Opcode = 14
	OpLOADI32    Opcode = 15
	OpLOADSYM    Opcode = 16
	OpLOADNIL    Opcode = 17
	OpLOADSELF   Opcode = 18
	OpLOADTRUE   Opcode = 19
	OpLOADFALSE  Opcode = 20
	OpGETGV      Opcode = 21
	OpSETGV      Opcode = 22
	OpGETSV      Opcode = 23
	OpSETSV      Opcode = 24
	OpGETIV      Opcode = 25
	OpSETIV      Opcode = 26
	OpGETCV      Opcode = 27
	OpSETCV      Opcode = 28
	OpGETCONST   Opcode = 29
	OpSETCONST   Opcode = 30
	OpGETMCNST   Opcode = 31
	OpSETMCNST   Opcode = 32
	OpGETUPVAR   Opcode = 33
	OpSETUPVAR   Opcode = 34
	OpGETIDX     Opcode = 35
	OpGETIDX0    Opcode = 36
	OpSETIDX     Opcode = 37
	OpJMP        Opcode = 38
	OpJMPIF      Opcode = 39
	OpJMPNOT     Opcode = 40
	OpJMPNIL     Opcode = 41
	OpJMPUW      Opcode = 42
	OpEXCEPT     Opcode = 43
	OpRESCUE     Opcode = 44
	OpRAISEIF    Opcode = 45
	OpMATCHERR   Opcode = 46
	OpSSEND      Opcode = 47
	OpSSEND0     Opcode = 48
	OpSSENDB     Opcode = 49
	OpSEND       Opcode = 50
	OpSEND0      Opcode = 51
	OpSENDB      Opcode = 52
	OpCALL       Opcode = 53
	OpBLKCALL    Opcode = 54
	OpSUPER      Opcode = 55
	OpARGARY     Opcode = 56
	OpENTER      Opcode = 57
	OpKEY_P      Opcode = 58
	OpKEYEND     Opcode = 59
	OpKARG       Opcode = 60
	OpRETURN     Opcode = 61
	OpRETURN_BLK Opcode = 62
	OpRETSELF    Opcode = 63
	OpRETNIL     Opcode = 64
	OpRETTRUE    Opcode = 65
	OpRETFALSE   Opcode = 66
	OpBREAK      Opcode = 67
	OpBLKPUSH    Opcode = 68
	OpADD        Opcode = 69
	OpADDI       Opcode = 70
	OpSUB        Opcode = 71
	OpSUBI       Opcode = 72
	OpADDILV     Opcode = 73
	OpSUBILV     Opcode = 74
	OpMUL        Opcode = 75
	OpDIV        Opcode = 76
	OpEQ         Opcode = 77
	OpLT         Opcode = 78
	OpLE         Opcode = 79
	OpGT         Opcode = 80
	OpGE         Opcode = 81
	OpARRAY      Opcode = 82
	OpARRAY2     Opcode = 83
	OpARYCAT     Opcode = 84
	OpARYPUSH    Opcode = 85
	OpARYSPLAT   Opcode = 86
	OpAREF       Opcode = 87
	OpASET       Opcode = 88
	OpAPOST      Opcode = 89
	OpINTERN     Opcode = 90
	OpSYMBOL     Opcode = 91
	OpSTRING     Opcode = 92
	OpSTRCAT     Opcode = 93
	OpHASH       Opcode = 94
	OpHASHADD    Opcode = 95
	OpHASHCAT    Opcode = 96
	OpLAMBDA     Opcode = 97
	OpBLOCK      Opcode = 98
	OpMETHOD     Opcode = 99
	OpRANGE_INC  Opcode = 100
	OpRANGE_EXC  Opcode = 101
	OpOCLASS     Opcode = 102
	OpCLASS      Opcode = 103
	OpMODULE     Opcode = 104
	OpEXEC       Opcode = 105
	OpDEF        Opcode = 106
	OpTDEF       Opcode = 107
	OpSDEF       Opcode = 108
	OpALIAS      Opcode = 109
	OpUNDEF      Opcode = 110
	OpSCLASS     Opcode = 111
	OpTCLASS     Opcode = 112
	OpDEBUG      Opcode = 113
	OpERR        Opcode = 114
	OpEXT1       Opcode = 115
	OpEXT2       Opcode = 116
	OpEXT3       Opcode = 117
	OpSTOP       Opcode = 118
)

// OpInfo holds metadata about an opcode.
type OpInfo struct {
	Name   string
	Format OpFormat
}

var opInfos = map[Opcode]OpInfo{
	OpNOP:        {"NOP", FormatZ},
	OpMOVE:       {"MOVE", FormatBB},
	OpLOADL:      {"LOADL", FormatBB},
	OpLOADI8:     {"LOADI8", FormatBB},
	OpLOADINEG:   {"LOADINEG", FormatBB},
	OpLOADI__1:   {"LOADI__1", FormatB},
	OpLOADI_0:    {"LOADI_0", FormatB},
	OpLOADI_1:    {"LOADI_1", FormatB},
	OpLOADI_2:    {"LOADI_2", FormatB},
	OpLOADI_3:    {"LOADI_3", FormatB},
	OpLOADI_4:    {"LOADI_4", FormatB},
	OpLOADI_5:    {"LOADI_5", FormatB},
	OpLOADI_6:    {"LOADI_6", FormatB},
	OpLOADI_7:    {"LOADI_7", FormatB},
	OpLOADI16:    {"LOADI16", FormatBS},
	OpLOADI32:    {"LOADI32", FormatBSS},
	OpLOADSYM:    {"LOADSYM", FormatBB},
	OpLOADNIL:    {"LOADNIL", FormatB},
	OpLOADSELF:   {"LOADSELF", FormatB},
	OpLOADTRUE:   {"LOADTRUE", FormatB},
	OpLOADFALSE:  {"LOADFALSE", FormatB},
	OpGETGV:      {"GETGV", FormatBB},
	OpSETGV:      {"SETGV", FormatBB},
	OpGETSV:      {"GETSV", FormatBB},
	OpSETSV:      {"SETSV", FormatBB},
	OpGETIV:      {"GETIV", FormatBB},
	OpSETIV:      {"SETIV", FormatBB},
	OpGETCV:      {"GETCV", FormatBB},
	OpSETCV:      {"SETCV", FormatBB},
	OpGETCONST:   {"GETCONST", FormatBB},
	OpSETCONST:   {"SETCONST", FormatBB},
	OpGETMCNST:   {"GETMCNST", FormatBB},
	OpSETMCNST:   {"SETMCNST", FormatBB},
	OpGETUPVAR:   {"GETUPVAR", FormatBBB},
	OpSETUPVAR:   {"SETUPVAR", FormatBBB},
	OpGETIDX:     {"GETIDX", FormatB},
	OpGETIDX0:    {"GETIDX0", FormatBB},
	OpSETIDX:     {"SETIDX", FormatB},
	OpJMP:        {"JMP", FormatS},
	OpJMPIF:      {"JMPIF", FormatBS},
	OpJMPNOT:     {"JMPNOT", FormatBS},
	OpJMPNIL:     {"JMPNIL", FormatBS},
	OpJMPUW:      {"JMPUW", FormatS},
	OpEXCEPT:     {"EXCEPT", FormatB},
	OpRESCUE:     {"RESCUE", FormatBB},
	OpRAISEIF:    {"RAISEIF", FormatB},
	OpMATCHERR:   {"MATCHERR", FormatB},
	OpSSEND:      {"SSEND", FormatBBB},
	OpSSEND0:     {"SSEND0", FormatBB},
	OpSSENDB:     {"SSENDB", FormatBBB},
	OpSEND:       {"SEND", FormatBBB},
	OpSEND0:      {"SEND0", FormatBB},
	OpSENDB:      {"SENDB", FormatBBB},
	OpCALL:       {"CALL", FormatZ},
	OpBLKCALL:    {"BLKCALL", FormatBB},
	OpSUPER:      {"SUPER", FormatBB},
	OpARGARY:     {"ARGARY", FormatBS},
	OpENTER:      {"ENTER", FormatW},
	OpKEY_P:      {"KEY_P", FormatBB},
	OpKEYEND:     {"KEYEND", FormatZ},
	OpKARG:       {"KARG", FormatBB},
	OpRETURN:     {"RETURN", FormatB},
	OpRETURN_BLK: {"RETURN_BLK", FormatB},
	OpRETSELF:    {"RETSELF", FormatZ},
	OpRETNIL:     {"RETNIL", FormatZ},
	OpRETTRUE:    {"RETTRUE", FormatZ},
	OpRETFALSE:   {"RETFALSE", FormatZ},
	OpBREAK:      {"BREAK", FormatB},
	OpBLKPUSH:    {"BLKPUSH", FormatBS},
	OpADD:        {"ADD", FormatB},
	OpADDI:       {"ADDI", FormatBB},
	OpSUB:        {"SUB", FormatB},
	OpSUBI:       {"SUBI", FormatBB},
	OpADDILV:     {"ADDILV", FormatBBB},
	OpSUBILV:     {"SUBILV", FormatBBB},
	OpMUL:        {"MUL", FormatB},
	OpDIV:        {"DIV", FormatB},
	OpEQ:         {"EQ", FormatB},
	OpLT:         {"LT", FormatB},
	OpLE:         {"LE", FormatB},
	OpGT:         {"GT", FormatB},
	OpGE:         {"GE", FormatB},
	OpARRAY:      {"ARRAY", FormatBB},
	OpARRAY2:     {"ARRAY2", FormatBBB},
	OpARYCAT:     {"ARYCAT", FormatB},
	OpARYPUSH:    {"ARYPUSH", FormatBB},
	OpARYSPLAT:   {"ARYSPLAT", FormatB},
	OpAREF:       {"AREF", FormatBBB},
	OpASET:       {"ASET", FormatBBB},
	OpAPOST:      {"APOST", FormatBBB},
	OpINTERN:     {"INTERN", FormatB},
	OpSYMBOL:     {"SYMBOL", FormatBB},
	OpSTRING:     {"STRING", FormatBB},
	OpSTRCAT:     {"STRCAT", FormatB},
	OpHASH:       {"HASH", FormatBB},
	OpHASHADD:    {"HASHADD", FormatBB},
	OpHASHCAT:    {"HASHCAT", FormatB},
	OpLAMBDA:     {"LAMBDA", FormatBB},
	OpBLOCK:      {"BLOCK", FormatBB},
	OpMETHOD:     {"METHOD", FormatBB},
	OpRANGE_INC:  {"RANGE_INC", FormatB},
	OpRANGE_EXC:  {"RANGE_EXC", FormatB},
	OpOCLASS:     {"OCLASS", FormatB},
	OpCLASS:      {"CLASS", FormatBB},
	OpMODULE:     {"MODULE", FormatBB},
	OpEXEC:       {"EXEC", FormatBB},
	OpDEF:        {"DEF", FormatBB},
	OpTDEF:       {"TDEF", FormatBBB},
	OpSDEF:       {"SDEF", FormatBBB},
	OpALIAS:      {"ALIAS", FormatBB},
	OpUNDEF:      {"UNDEF", FormatB},
	OpSCLASS:     {"SCLASS", FormatB},
	OpTCLASS:     {"TCLASS", FormatB},
	OpDEBUG:      {"DEBUG", FormatBBB},
	OpERR:        {"ERR", FormatB},
	OpEXT1:       {"EXT1", FormatZ},
	OpEXT2:       {"EXT2", FormatZ},
	OpEXT3:       {"EXT3", FormatZ},
	OpSTOP:       {"STOP", FormatZ},
}

// Name returns the mnemonic name for the opcode.
func (op Opcode) Name() string {
	if info, ok := opInfos[op]; ok {
		return info.Name
	}
	return fmt.Sprintf("OP_UNKNOWN_%d", op)
}

// Format returns the operand layout format for the opcode.
func (op Opcode) Format() OpFormat {
	if info, ok := opInfos[op]; ok {
		return info.Format
	}
	return FormatZ
}

// String implements fmt.Stringer.
func (op Opcode) String() string {
	return op.Name()
}
