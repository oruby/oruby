package oruby

// #include "go-mrb.h"
import "C"
import (
	"errors"
	"os"
	"runtime"
	"unsafe"
)

// MrbCcontext struct
type MrbCcontext struct {
	p   *C.struct_mrb_ccontext
	mrb *MrbState
}

// set option helper
func iifmb(v bool) C.mrb_bool {
	if v {
		return C.mrb_bool(true)
	}
	return C.mrb_bool(false)
}

// LineNo from context
func (c *MrbCcontext) LineNo() int { return int(c.p.lineno) }

// SetLineNo sets lineno in context
func (c *MrbCcontext) SetLineNo(lineno int) { c.p.lineno = C.uint16_t(lineno) }

// CaptureErrors returns if errors are captured
func (c *MrbCcontext) CaptureErrors() bool { return C._mrbc_capture_errors(c.p) != 0 }

// SetCaptureErrors sets error capturing on or off
func (c *MrbCcontext) SetCaptureErrors(b bool) { C._mrbc_set_capture_errors(c.p, iifmb(b)) }

// DumpResult returns if result is dumped
func (c *MrbCcontext) DumpResult() bool { return C._mrbc_dump_result(c.p) != 0 }

// SetDumpResult sets result dump on or off
func (c *MrbCcontext) SetDumpResult(b bool) { C._mrbc_set_dump_result(c.p, iifmb(b)) }

// NoExec returns if NoExec is turend on
func (c *MrbCcontext) NoExec() bool { return C._mrbc_no_exec(c.p) != 0 }

// SetNoExec sets NoExec on or off
func (c *MrbCcontext) SetNoExec(b bool) { C._mrbc_set_no_exec(c.p, iifmb(b)) }

// KeepLv returns if KeepLv is turned on
func (c *MrbCcontext) KeepLv() bool { return C._mrbc_keep_lv(c.p) != 0 }

// SetKeepLv sets KeepLv on or off
func (c *MrbCcontext) SetKeepLv(b bool) { C._mrbc_set_keep_lv(c.p, iifmb(b)) }

// SetNoOptimize sets NoOptimize on or off
func (c *MrbCcontext) SetNoOptimize(b bool) { C._mrbc_set_no_optimize(c.p, iifmb(b)) }

// SetNoExtOps sets NoExtOps on or off
func (c *MrbCcontext) SetNoExtOps(b bool) { C._mrbc_set_no_ext_ops(c.p, iifmb(b)) }

// MrbCcontextNew create new context
func (mrb *MrbState) MrbCcontextNew() *MrbCcontext {
	return &MrbCcontext{C.mrb_ccontext_new(mrb.p), mrb}
}

// Free MrbCcontext
func (c *MrbCcontext) Free() { C.mrb_ccontext_free(c.mrb.p, c.p) }

// Filename set for MrbCcontext
func (c *MrbCcontext) Filename(filename string) string {
	cfn := C.CString(filename)
	defer C.free(unsafe.Pointer(cfn))
	return C.GoString(C.mrb_ccontext_filename(c.mrb.p, c.p, cfn))
}

// PartialHook set parser hook
func (c *MrbCcontext) PartialHook(partialHook PartialHookF) {
	c.mrb.setHook(unsafe.Pointer(c.p), partialHook)
}

// CleanupLocalVariables clears local variables
func (c *MrbCcontext) CleanupLocalVariables() {
	C.mrbc_cleanup_local_variables(c.p)
}

// LoadFile loads file into oruby context
func (c *MrbCcontext) LoadFile(filename string) (Value, error) {
	return c.mrb.LoadFileCxt(filename, c)
}

// LoadString loads string into oruby context
func (c *MrbCcontext) LoadString(s string) (Value, error) {
	return c.mrb.LoadStringCxt(s, c)
}

// LoadBytes loads string into oruby context
func (c *MrbCcontext) LoadBytes(buf []byte) (Value, error) {
	return c.mrb.LoadBytesCxt(buf, c)
}

// MrbCcontextFree free context
func (mrb *MrbState) MrbCcontextFree(context *MrbCcontext) {
	C.mrb_ccontext_free(mrb.p, context.p)
}

// MrbcFilename return filename
func (mrb *MrbState) MrbcFilename(context *MrbCcontext, filename string) string {
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

// MrbCcontextPartialHook set parser hook
func (mrb *MrbState) MrbCcontextPartialHook(cxt *MrbCcontext, partialHook PartialHookF) {
	mrb.setHook(unsafe.Pointer(cxt.p), partialHook)
}

// MrbCcontextCleanupLocalVariables clear local variables
func (mrb *MrbState) MrbCcontextCleanupLocalVariables(cxt *MrbCcontext) {
	C.mrbc_cleanup_local_variables(cxt.p)
}

// MrbAstNode AST node structure
type MrbAstNode struct{ p *C.struct_mrb_ast_node }

func astNode(node *C.struct_mrb_ast_node) *MrbAstNode {
	if node == nil {
		return nil
	}
	return &MrbAstNode{node}
}

// LineNo from AST node
// func (n *MrbAstNode) LineNo() int { return int(n.p.lineno) }

// FilenameIndex from AST node
// func (n *MrbAstNode) FilenameIndex() int { return int(n.p.filename_index) }

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
func (p MrbParserState) Context() *MrbCcontext {
	return &MrbCcontext{p.p.cxt, p.State()}
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

// SetS fills both parser->s, and parser->send, as used in orbi
// Caller must free string after use, returns closure which frees buffer
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

// ParserNew creates new parser state
func (mrb *MrbState) ParserNew() MrbParserState {
	return MrbParserState{C.mrb_parser_new(mrb.p)}
}

// Free releases parser state
func (p MrbParserState) Free() { C.mrb_parser_free(p.p) }

// Parse parses oruby context
func (p MrbParserState) Parse(context *MrbCcontext) {
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
func (mrb *MrbState) ParseFile(fileName string, context *MrbCcontext) (MrbParserState, error) {
	s, err := loadFile(fileName)
	if err != nil {
		return MrbParserState{nil}, err
	}

	return mrb.ParseString(s, context)
	// C.mrb_parse_file() is never called
}

// ParseString to oruby parser state
func (mrb *MrbState) ParseString(s string, context *MrbCcontext) (MrbParserState, error) {
	s = PreprocessRuby(s)
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
func (mrb *MrbState) LoadExec(parser MrbParserState, context *MrbCcontext) Value {
	v, err := mrb.LoadExecE(parser, context)
	if err != nil {
		return mrb.RaiseError(err)
	}
	return v
}

// LoadExecE loads and executes parser context, returning any Ruby failure as an error.
func (mrb *MrbState) LoadExecE(parser MrbParserState, context *MrbCcontext) (Value, error) {
	var result C.mrb_value
	status := C._mrb_load_exec(mrb.p, parser.p, context.p, &result)
	v := Value{result}
	return mrb.protectedResult(v, status)
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
	s = PreprocessRuby(s)
	cs := C.CString(s)
	defer C.free(unsafe.Pointer(cs))

	var result C.mrb_value
	status := C._mrb_load_nstring_cxt(mrb.p, cs, C.size_t(len(s)), nil, &result)
	v := Value{result}
	return mrb.protectedResult(v, status)

	// pure C.mrb_load_string() is never called
}

// LoadFileCxt loads file into oruby context
func (mrb *MrbState) LoadFileCxt(fileName string, context *MrbCcontext) (Value, error) {
	data, err := os.ReadFile(fileName)
	if err != nil {
		return mrb.NilValue(), err
	}

	return mrb.LoadBytesCxt(data, context)
	// pure C.mrb_load_file_cxt() is never called
}

// LoadStringCxt loads string into oruby context
func (mrb *MrbState) LoadStringCxt(s string, context *MrbCcontext) (Value, error) {
	s = PreprocessRuby(s)
	return mrb.LoadBytesCxt([]byte(s), context)
}

// LoadBytesCxt loads bytes into oruby context
func (mrb *MrbState) LoadBytesCxt(buf []byte, context *MrbCcontext) (Value, error) {
	if len(buf) == 0 {
		return nilValue, errors.New("empty buffer")
	}
	buf = PreprocessRubyBytes(buf)
	var result C.mrb_value
	status := C._mrb_load_nstring_cxt(mrb.p, (*C.char)(unsafe.Pointer(&buf[0])), C.size_t(len(buf)), context.p, &result)
	runtime.KeepAlive(buf)
	v := Value{result}
	return mrb.protectedResult(v, status)
}

// CodedumpAll helper for oruby cmd
func (mrb *MrbState) CodedumpAll(proc RProc) {
	C.mrb_codedump_all(mrb.p, proc.p)
}

// SetLastStackValue helper for orbi cmd
func (mrb *MrbState) SetLastStackValue(v Value) {
	C._set_last_stack_value(mrb.p, v.v)
}
