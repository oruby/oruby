package oruby

// #include "go-mrb.h"
import "C"
import (
	"errors"
	"os"
	"runtime"
	"unsafe"
)

// MrbcContext struct
type MrbcContext struct {
	p   *C.struct_mrbc_context
	mrb *MrbState
}

// set option helper
func iifmb(v bool) C.mrb_bool {
	if v {
		return C.mrb_bool(true)
	}
	return C.mrb_bool(false)
}

// CaptureErrors returns if errors are captured
func (c *MrbcContext) CaptureErrors() bool { return C._mrbc_capture_errors(c.p) != 0 }

// SetCaptureErrors turns error capturing on or off
func (c *MrbcContext) SetCaptureErrors(b bool) { C._mrbc_set_capture_errors(c.p, iifmb(b)) }

// DumpResult returns if result is dumped
func (c *MrbcContext) DumpResult() bool { return C._mrbc_dump_result(c.p) != 0 }

// SetDumpResult turns  result dump on or off
func (c *MrbcContext) SetDumpResult(b bool) { C._mrbc_set_dump_result(c.p, iifmb(b)) }

// NoExec returns if NoExec is turend on
func (c *MrbcContext) NoExec() bool { return C._mrbc_no_exec(c.p) != 0 }

// SetNoExec returns if NoExec is turned on or off
func (c *MrbcContext) SetNoExec(b bool) { C._mrbc_set_no_exec(c.p, iifmb(b)) }

// MrbcContextNew create new context
func (mrb *MrbState) MrbcContextNew() *MrbcContext {
	return &MrbcContext{C.mrbc_context_new(mrb.p), mrb}
}

// LineNo from context
func (c *MrbcContext) LineNo() int { return int(c.p.lineno) }

// SetLineNo sets lineno in context
func (c *MrbcContext) SetLineNo(lineno int) { c.p.lineno = C.uint16_t(lineno) }

// Free MrbcContext
func (c *MrbcContext) Free() { C.mrbc_context_free(c.mrb.p, c.p) }

// Filename set for MrbcContext
func (c *MrbcContext) Filename(filename string) string {
	cfn := C.CString(filename)
	defer C.free(unsafe.Pointer(cfn))
	return C.GoString(C.mrbc_filename(c.mrb.p, c.p, cfn))
}

// PartialHook set parser hook
func (c *MrbcContext) PartialHook(partialHook PartialHookF) {
	C.mrbc_context_free(c.mrb.p, c.p)
}

// CleanupLocalVariables clears local variables
func (c *MrbcContext) CleanupLocalVariables() {
	C.mrbc_cleanup_local_variables(c.mrb.p, c.p)
}

// LoadFile loads file into oruby context
func (c *MrbcContext) LoadFile(filename string) (Value, error) {
	return c.mrb.LoadFileCxt(filename, c)
}

// LoadString loads string into oruby context
func (c *MrbcContext) LoadString(s string) (Value, error) {
	return c.mrb.LoadStringCxt(s, c)
}

// LoadBytes loads string into oruby context
func (c *MrbcContext) LoadBytes(buf []byte) (Value, error) {
	return c.mrb.LoadBytesCxt(buf, c)
}

// MrbcContextFree free context
func (mrb *MrbState) MrbcContextFree(context *MrbcContext) {
	C.mrbc_context_free(mrb.p, context.p)
}

// MrbcFilename return filename
func (mrb *MrbState) MrbcFilename(context *MrbcContext, filename string) string {
	cfn := C.CString(filename)
	defer C.free(unsafe.Pointer(cfn))
	return C.GoString(C.mrbc_filename(mrb.p, context.p, cfn))
}

// PartialHookF type of function for hook
type PartialHookF func(MrbParserState) int

//export go_partial_hook_callback
func go_partial_hook_callback(p *C.struct_mrb_parser_state) C.int {
	mrb := getMrbState(p.mrb)

	f, ok := mrb.getHook(unsafe.Pointer(p)).(PartialHookF)
	if !ok {
		return -1
	}
	return C.int(f(MrbParserState{p}))
}

// MrbcContextPartialHook set parser hook
func (mrb *MrbState) MrbcContextPartialHook(cxt *MrbcContext, partialHook PartialHookF) {
	mrb.setHook(unsafe.Pointer(cxt.p), partialHook)
}

// MrbcContextCleanupLocalVariables clear local variables
func (mrb *MrbState) MrbcContextCleanupLocalVariables(cxt *MrbcContext) {
	C.mrbc_cleanup_local_variables(mrb.p, cxt.p)
}

// MrbAstNode AST node structure
type MrbAstNode struct{ p *C.struct_mrb_ast_node }

func astNode(node *C.struct_mrb_ast_node) *MrbAstNode {
	if node == nil {
		return nil
	}
	return &MrbAstNode{node}
}

// Car from AST node
func (n *MrbAstNode) Car() *MrbAstNode { return astNode(n.p.car) }

// Cdr form AST node
func (n *MrbAstNode) Cdr() *MrbAstNode { return astNode(n.p.cdr) }

// LineNo from AST node
func (n *MrbAstNode) LineNo() int { return int(n.p.lineno) }

// FilenameIndex from AST node
func (n *MrbAstNode) FilenameIndex() int { return int(n.p.filename_index) }

// mrb_lex_state_enum
const (
	ExprBeg    = iota // ignore newline, +/- is a sign.
	ExprEnd           // newline significant, +/- is an operator.
	ExprEndarg        // ditto, and unbound braces.
	ExprEndfn         // ditto, and unbound braces.
	ExprArg           // newline significant, +/- is an operator.
	ExprCmdarg        // newline significant, +/- is an operator.
	ExprMid           // newline significant, +/- is an operator.
	ExprFname         // ignore newline, no reserved words.
	ExprDot           // right after `.' or `::', no reserved words.
	ExprClass         // immediate after `class', no here document.
	ExprValue         // alike ExprBeg but label is disallowed.
	ExprMaxState
)

// str func constants
const (
	StrFuncParsing = 0x01
	StrFuncExpand  = 0x02
	StrFuncRegexp  = 0x04
	StrFuncWord    = 0x08
	StrFuncSymbol  = 0x10
	StrFuncArray   = 0x20
	StrFuncHeredoc = 0x40
	StrFuncXquote  = 0x80
)

// mrb_string_type enum
const (
	StrNotParsing = 0
	StrSquote     = StrFuncParsing
	StrDquote     = StrFuncParsing | StrFuncExpand
	StrRegexp     = StrFuncParsing | StrFuncRegexp | StrFuncExpand
	StrSword      = StrFuncParsing | StrFuncWord | StrFuncArray
	StrDword      = StrFuncParsing | StrFuncWord | StrFuncArray | StrFuncExpand
	StrSsym       = StrFuncParsing | StrFuncSymbol
	StrSsymbols   = StrFuncParsing | StrFuncSymbol | StrFuncArray
	StrDsymbols   = StrFuncParsing | StrFuncSymbol | StrFuncArray | StrFuncExpand
	StrHeredoc    = StrFuncParsing | StrFuncHeredoc
	StrXquote     = StrFuncParsing | StrFuncXquote | StrFuncExpand
)

// MrbParserBufSize default
const MrbParserBufSize = 1024

// MrbParserState struct
type MrbParserState struct{ p *C.struct_mrb_parser_state }

// Filename returns filename
func (p MrbParserState) Filename() MrbSym {
	return MrbSym(p.p.filename_sym)
}

// LineNo returns line number
func (p MrbParserState) LineNo() int { return int(p.p.lineno) }

// SetLineNo sets line number in parser state
func (p MrbParserState) SetLineNo(lineno int) { p.p.lineno = C.uint16_t(lineno) }

// Column returns column
func (p MrbParserState) Column() int { return int(p.p.column) }

// NErr returns number of errors
func (p MrbParserState) NErr() int { return int(p.p.nerr) }

// NWarn returns number of warnings
func (p MrbParserState) NWarn() int { return int(p.p.nwarn) }

// Context returns mrb context from parser state
func (p MrbParserState) Context() *MrbcContext {
	return &MrbcContext{p.p.cxt, p.State()}
}

// State returns parsers mrb state
func (p MrbParserState) State() *MrbState {
	return getMrbState(p.p.mrb)
}

// MrbParserMessage struct
type MrbParserMessage struct {
	LineNo  int
	Column  int
	Message string
}

// ErrorBuffer creates error parser message
func (p MrbParserState) ErrorBuffer(i int) MrbParserMessage {
	return MrbParserMessage{
		int(p.p.error_buffer[i].lineno),
		int(p.p.error_buffer[i].column),
		C.GoString(p.p.error_buffer[i].message)}
}

// WarnBuffer creates warning parser message
func (p MrbParserState) WarnBuffer(i int) MrbParserMessage {
	return MrbParserMessage{
		int(p.p.warn_buffer[i].lineno),
		int(p.p.warn_buffer[i].column),
		C.GoString(p.p.warn_buffer[i].message)}
}

// LexStrTerm check for unterminated string
func (p MrbParserState) LexStrTerm() bool {
	return p.p.lex_strterm != nil
}

// HeredocsFromNextline node
func (p MrbParserState) HeredocsFromNextline() *MrbAstNode {
	return astNode(p.p.heredocs_from_nextline)
}

// ParsingHeredoc node
func (p MrbParserState) ParsingHeredoc() *MrbAstNode { return astNode(p.p.parsing_heredoc) }

// Locals node from paser state
func (p MrbParserState) Locals() *MrbAstNode { return astNode(p.p.locals) }

// Tree node from parser state
func (p MrbParserState) Tree() *MrbAstNode { return astNode(p.p.tree) }

// LState lexer state constant from lexer state enum (ExprBeg..ExprMaxState)
func (p MrbParserState) LState() int { return int(p.p.lstate) }

// IsNil checks if parser state exists
func (p MrbParserState) IsNil() bool { return p.p == nil }

// SetS sets parser->s, and parser->send, as used in orbi
func (p MrbParserState) SetS(s string) func() {
	if s == "" {
		C._set_parser_s(p.p, nil)
		return func() {}
	}
	cs := C.CString(s)
	C._set_parser_s(p.p, cs)

	// Caller must free string after use
	return func() {
		C.free(unsafe.Pointer(cs))
	}
}

// SetB sets parser->s, and parser->send as bytes, used in orbi
// Caller must free string after use, returns closure which frees buffer
func (p MrbParserState) SetB(buf []byte) func() {
	if len(buf) == 0 {
		C._set_parser_s(p.p, nil)
		return func() {}
	}
	cBuf := C.CBytes(buf)
	C._set_parser_s(p.p, (*C.char)(cBuf))

	// Caller must free string after use
	return func() {
		C.free(unsafe.Pointer(cBuf))
	}
}

// ParserNew creates new oruby parser state
func (mrb *MrbState) ParserNew() MrbParserState {
	return MrbParserState{C.mrb_parser_new(mrb.p)}
}

// Free releases parser state
func (p MrbParserState) Free() { C.mrb_parser_free(p.p) }

// Parse parses oruby context
func (p MrbParserState) Parse(context *MrbcContext) {
	C.mrb_parser_parse(p.p, context.p)
}

// SetFilename sets filename to oruby parser state
func (p MrbParserState) SetFilename(filename string) {
	cfn := C.CString(filename)
	defer C.free(unsafe.Pointer(cfn))
	C.mrb_parser_set_filename(p.p, cfn)
}

// GetFilename returns filename from parser state
func (p MrbParserState) GetFilename(idx uint16) MrbSym {
	return MrbSym(C.mrb_parser_get_filename(p.p, C.uint16_t(idx)))
}

func loadFile(fileName string) (string, error) {
	data, err := os.ReadFile(fileName)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

// ParseFile to oruby parser state
func (mrb *MrbState) ParseFile(fileName string, context *MrbcContext) (MrbParserState, error) {
	s, err := loadFile(fileName)
	if err != nil {
		return MrbParserState{nil}, err
	}

	return mrb.ParseString(s, context)
	// C.mrb_parse_file is never called
}

// ParseString to oruby parser state
func (mrb *MrbState) ParseString(s string, context *MrbcContext) (MrbParserState, error) {
	cs := C.CString(s)
	defer C.free(unsafe.Pointer(cs))
	p := C.mrb_parse_nstring(mrb.p, cs, C.size_t(len(s)), context.p)
	if p == nil {
		return MrbParserState{nil}, errors.New("create parser state error")
	}
	return MrbParserState{p}, nil
	// pure C.mrb_parse_string() is never called
}

// GenerateCode generates RPros
func (mrb *MrbState) GenerateCode(parser MrbParserState) (RProc, error) {
	p := C.mrb_generate_code(mrb.p, parser.p)

	if p == nil {
		return RProc{nil, mrb}, errors.New("error generating parser code")
	}

	return RProc{p, mrb}, nil
}

// LoadExec loads and executes parser context, returning Value
func (mrb *MrbState) LoadExec(parser MrbParserState, context *MrbcContext) Value {
	return Value{C.mrb_load_exec(mrb.p, parser.p, context.p)}
}

// LoadFile loads file to oruby value
func (mrb *MrbState) LoadFile(fileName string) (Value, error) {
	s, err := loadFile(fileName)
	if err != nil {
		return mrb.NilValue(), err
	}
	return mrb.LoadString(s)
	// pure C.mrb_load_file() is never called - *C.FILE unsupported
}

// LoadString loads string to oruby value
func (mrb *MrbState) LoadString(s string) (Value, error) {
	cs := C.CString(s)
	defer C.free(unsafe.Pointer(cs))

	return mrb.try(func() C.mrb_value {
		return C.mrb_load_nstring(mrb.p, cs, C.size_t(len(s)))
	})

	// pure C.mrb_load_string() is never called
}

// LoadFileCxt loads file into oruby context
func (mrb *MrbState) LoadFileCxt(fileName string, context *MrbcContext) (Value, error) {
	data, err := os.ReadFile(fileName)
	if err != nil {
		return mrb.NilValue(), err
	}

	return mrb.LoadBytesCxt(data, context)
	// pure C.mrb_load_file_cxt() is never called
}

// LoadStringCxt loads string into oruby context
func (mrb *MrbState) LoadStringCxt(s string, context *MrbcContext) (Value, error) {
	return mrb.LoadBytesCxt([]byte(s), context)
}

// LoadBytesCxt loads bytes into oruby context
func (mrb *MrbState) LoadBytesCxt(buf []byte, context *MrbcContext) (Value, error) {
	if len(buf) == 0 {
		return mrb.nilValue, errors.New("empty buffer")
	}

	v, err := mrb.try(func() C.mrb_value {
		if len(buf) == 0 {
			return C.mrb_load_string_cxt(mrb.p, nil, context.p)
		}
		ret := C.mrb_load_nstring_cxt(mrb.p, (*C.char)(unsafe.Pointer(&buf[0])), C.size_t(len(buf)), context.p)
		runtime.KeepAlive(buf)

		return ret
	})

	if err != nil {
		return v, err
	}
	return v, mrb.Err()
}

// CodedumpAll helper for oruby cmd
func (mrb *MrbState) CodedumpAll(proc RProc) {
	C.mrb_codedump_all(mrb.p, proc.p)
}

// SetLastStackValue helper for orbi cmd
func (mrb *MrbState) SetLastStackValue(v Value) {
	C._set_last_stack_value(mrb.p, v.v)
}
