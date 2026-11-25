// Package racc implements Racc's native racc/cparse LALR(1) runtime engine in Go
// (along with standalone racc/info and racc/parser fallbacks).
package racc

import (
	_ "embed"
	"errors"
	"fmt"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/rb"
)

//go:embed cparse.rb
var cparseRb []byte

//go:embed parser.rb
var parserRb []byte

const (
	raccVersion = "1.8.1"
	errorToken  = 1
	finalToken  = 0

	finNone    = 0
	finAccept  = 1
	finEOT     = 2
	finCantPop = 3
)

func init() {
	oruby.LazyGem("racc/cparse", initCparse)
	oruby.LazyGem("racc/info", initInfo)
	oruby.LazyGem("racc/parser", initParser)
	oruby.GemAlias("racc", "racc/parser")
}

func initInfo(mrb *oruby.MrbState) any {
	mod := mrb.DefineModule("Racc")
	mrb.ConstSet(mod, mrb.Intern("VERSION"), mrb.StrNew(raccVersion))
	mrb.ConstSet(mod, mrb.Intern("Version"), mrb.StrNew(raccVersion))
	mrb.ConstSet(mod, mrb.Intern("COPYRIGHT"), mrb.StrNew("Copyright (c) 1999-2006 Minero Aoki"))
	return nil
}

func initParser(mrb *oruby.MrbState) any {
	initInfo(mrb)
	initCparse(mrb)
	rb.LoadRuby(mrb, "<internal:racc/parser>", parserRb)
	return nil
}

func initCparse(mrb *oruby.MrbState) any {
	mod := mrb.DefineModule("Racc")
	parserCls := mrb.DefineClassUnder(mod, "Parser", mrb.ObjectClass())

	mrb.ConstSet(parserCls, mrb.Intern("Racc_Runtime_Core_Version_C"), mrb.StrNew(raccVersion))
	mrb.ConstSet(parserCls, mrb.Intern("Racc_Runtime_Core_Id_C"), mrb.StrNew("$originalId: cparse.c,v 1.8 2006/07/06 11:39:46 aamine Exp $"))

	paramsCls := mrb.DefineClassUnder(mod, "CparseParams", mrb.ObjectClass())
	rb.DataClass(paramsCls)

	paramsCls.DefineClassMethod("__new", newParams, mrb.ArgsReq(5))
	paramsCls.DefineMethod("__parse_all", paramsParseAll, mrb.ArgsNone())
	paramsCls.DefineMethod("__parse_init", paramsParseInit, mrb.ArgsNone())
	paramsCls.DefineMethod("__feed_token", paramsFeedToken, mrb.ArgsReq(1))
	paramsCls.DefineMethod("__should_break?", paramsShouldBreak, mrb.ArgsNone())
	paramsCls.DefineMethod("__finished?", paramsFinished, mrb.ArgsNone())
	paramsCls.DefineMethod("__retval", paramsRetval, mrb.ArgsNone())

	rb.LoadRuby(mrb, "<internal:racc/cparse>", cparseRb)
	return nil
}

// ---------------------------------------------------------------------------
// cparseParams: Go representation of struct cparse_params
// ---------------------------------------------------------------------------

type cparseParams struct {
	parser        oruby.Value
	lexIsIterator bool
	lexMidName    string

	// Immutable tables from Racc_arg
	actionTable   []optInt
	actionCheck   []optInt
	actionDefault []int64
	actionPointer []optInt

	gotoTable   []optInt
	gotoCheck   []optInt
	gotoDefault []int64
	gotoPointer []optInt

	ntBase      int64
	reduceTable oruby.Value
	tokenTable  oruby.Value
	shiftN      int64
	reduceN     int64
	useResVar   bool

	// Parser stacks & state
	state     oruby.Value
	curstate  int64
	vstack    oruby.Value
	tstack    oruby.Value
	t         int64
	errstatus int64
	nerr      int64

	retval   oruby.Value
	fin      int
	debug    bool
	sysDebug bool
	savedI   int64
}

type optInt struct {
	valid bool
	val   int64
}

func extractOptIntSlice(mrb *oruby.MrbState, ary oruby.Value) ([]optInt, error) {
	if !ary.IsArray() {
		return nil, oruby.ETypeError("expected Array, got %s", mrb.ClassOf(ary).Name())
	}
	n := oruby.RArrayLen(ary)
	out := make([]optInt, n)
	for i := 0; i < n; i++ {
		e := mrb.AryRef(ary, i)
		if !e.IsNil() {
			out[i] = optInt{valid: true, val: e.Int64()}
		}
	}
	return out, nil
}

func extractIntSlice(mrb *oruby.MrbState, ary oruby.Value) ([]int64, error) {
	if !ary.IsArray() {
		return nil, oruby.ETypeError("expected Array, got %s", mrb.ClassOf(ary).Name())
	}
	n := oruby.RArrayLen(ary)
	out := make([]int64, n)
	for i := 0; i < n; i++ {
		e := mrb.AryRef(ary, i)
		if !e.IsNil() {
			out[i] = e.Int64()
		}
	}
	return out, nil
}

func optAt(s []optInt, idx int64) (int64, bool) {
	if idx < 0 || int(idx) >= len(s) {
		return 0, false
	}
	e := s[idx]
	return e.val, e.valid
}

func intAt(s []int64, idx int64) int64 {
	if idx < 0 || int(idx) >= len(s) {
		return 0
	}
	return s[idx]
}

func newParams(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	parser := args.Item(0)
	arg := args.Item(1)
	sysdebug := args.Item(2).Bool()
	lexIsIter := args.Item(3).Bool()
	lexMid := args.Item(4)

	if !arg.IsArray() {
		return mrb.ETypeError().Raise("Racc_arg must be an Array")
	}
	argLen := oruby.RArrayLen(arg)
	if argLen < 13 || argLen > 14 {
		return mrb.ERuntimeError().Raisef("[Racc Bug] wrong arg.size %d", argLen)
	}

	actionTable, err := extractOptIntSlice(mrb, mrb.AryRef(arg, 0))
	if err != nil {
		return mrb.RaiseError(err)
	}
	actionCheck, err := extractOptIntSlice(mrb, mrb.AryRef(arg, 1))
	if err != nil {
		return mrb.RaiseError(err)
	}
	actionDefault, err := extractIntSlice(mrb, mrb.AryRef(arg, 2))
	if err != nil {
		return mrb.RaiseError(err)
	}
	actionPointer, err := extractOptIntSlice(mrb, mrb.AryRef(arg, 3))
	if err != nil {
		return mrb.RaiseError(err)
	}
	gotoTable, err := extractOptIntSlice(mrb, mrb.AryRef(arg, 4))
	if err != nil {
		return mrb.RaiseError(err)
	}
	gotoCheck, err := extractOptIntSlice(mrb, mrb.AryRef(arg, 5))
	if err != nil {
		return mrb.RaiseError(err)
	}
	gotoDefault, err := extractIntSlice(mrb, mrb.AryRef(arg, 6))
	if err != nil {
		return mrb.RaiseError(err)
	}
	gotoPointer, err := extractOptIntSlice(mrb, mrb.AryRef(arg, 7))
	if err != nil {
		return mrb.RaiseError(err)
	}

	ntBase := mrb.AryRef(arg, 8).Int64()
	reduceTable := mrb.AryRef(arg, 9)
	tokenTable := mrb.AryRef(arg, 10)
	if !reduceTable.IsArray() || !tokenTable.IsHash() {
		return mrb.ETypeError().Raise("invalid Racc_arg reduce_table/token_table")
	}
	shiftN := mrb.AryRef(arg, 11).Int64()
	reduceN := mrb.AryRef(arg, 12).Int64()
	useResVar := true
	if argLen > 13 {
		useResVar = mrb.AryRef(arg, 13).Bool()
	}

	debug := mrb.IVGet(parser, mrb.Intern("@yydebug")).Bool()
	vstack := mrb.AryNewCapa(64).Value()
	state := mrb.AryNewCapa(64).Value()
	mrb.AryPush(state, mrb.FixnumValue(0))
	tstack := mrb.NilValue()
	if debug {
		tstack = mrb.AryNewCapa(64).Value()
	}

	mrb.IVSet(parser, mrb.Intern("@racc_error_status"), mrb.FixnumValue(0))
	mrb.IVSet(parser, mrb.Intern("@vstack"), vstack)
	mrb.IVSet(parser, mrb.Intern("@tstack"), tstack)

	lexName := "next_token"
	if !lexMid.IsNil() {
		if lexMid.IsSymbol() {
			lexName = mrb.SymName(lexMid.Symbol())
		} else {
			lexName = lexMid.String()
		}
	}

	p := &cparseParams{
		parser:        parser,
		lexIsIterator: lexIsIter,
		lexMidName:    lexName,
		actionTable:   actionTable,
		actionCheck:   actionCheck,
		actionDefault: actionDefault,
		actionPointer: actionPointer,
		gotoTable:     gotoTable,
		gotoCheck:     gotoCheck,
		gotoDefault:   gotoDefault,
		gotoPointer:   gotoPointer,
		ntBase:        ntBase,
		reduceTable:   reduceTable,
		tokenTable:    tokenTable,
		shiftN:        shiftN,
		reduceN:       reduceN,
		useResVar:     useResVar,
		state:         state,
		curstate:      0,
		vstack:        vstack,
		tstack:        tstack,
		t:             finalToken + 1,
		errstatus:     0,
		nerr:          0,
		retval:        mrb.NilValue(),
		fin:           finNone,
		debug:         debug,
		sysDebug:      sysdebug,
	}

	cls := rb.ClassPath(mrb, "Racc::CparseParams")
	obj := mrb.DataWrapInterface(cls, p).Value()
	// Keep Ruby objects GC-marked via ivars
	mrb.IVSet(obj, mrb.Intern("@__parser"), parser)
	mrb.IVSet(obj, mrb.Intern("@__arg"), arg)
	mrb.IVSet(obj, mrb.Intern("@__state"), state)
	mrb.IVSet(obj, mrb.Intern("@__vstack"), vstack)
	mrb.IVSet(obj, mrb.Intern("@__tstack"), tstack)
	return obj
}

func getParams(mrb *oruby.MrbState, self oruby.Value) *cparseParams {
	p, _ := rb.Get[*cparseParams](mrb, self)
	return p
}

func paramsParseAll(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	p := getParams(mrb, self)
	if err := p.parseMain(mrb, mrb.NilValue(), mrb.NilValue(), false); err != nil {
		return mrb.RaiseError(err)
	}
	return p.retval
}

func paramsParseInit(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	p := getParams(mrb, self)
	if err := p.parseMain(mrb, mrb.NilValue(), mrb.NilValue(), false); err != nil {
		return mrb.RaiseError(err)
	}
	return mrb.NilValue()
}

func paramsFeedToken(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	p := getParams(mrb, self)
	if p.fin != finNone {
		return mrb.EArgumentError().Raise("extra token after EndOfToken")
	}
	tok, val, err := p.extractUserToken(mrb, mrb.GetArgsFirst())
	if err != nil {
		return mrb.RaiseError(err)
	}
	if err := p.parseMain(mrb, tok, val, true); err != nil {
		return mrb.RaiseError(err)
	}
	return mrb.NilValue()
}

func paramsShouldBreak(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	p := getParams(mrb, self)
	return mrb.BoolValue(p.fin != finNone && p.fin != finAccept)
}

func paramsFinished(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.BoolValue(getParams(mrb, self).fin != finNone)
}

func paramsRetval(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return getParams(mrb, self).retval
}

func (p *cparseParams) extractUserToken(mrb *oruby.MrbState, raw oruby.Value) (oruby.Value, oruby.Value, error) {
	if raw.IsNil() {
		return mrb.FalseValue(), mrb.StrNew("$"), nil
	}
	action := "returned"
	if p.lexIsIterator {
		action = "yielded"
	}
	if !raw.IsArray() {
		return mrb.NilValue(), mrb.NilValue(), oruby.ETypeError("%s() %s %s (must be Array[2])", p.lexMidName, action, mrb.ClassOf(raw).Name())
	}
	n := oruby.RArrayLen(raw)
	if n != 2 {
		return mrb.NilValue(), mrb.NilValue(), oruby.EArgumentError("%s() %s wrong size of array (%d for 2)", p.lexMidName, action, n)
	}
	return mrb.AryRef(raw, 0), mrb.AryRef(raw, 1), nil
}

func (p *cparseParams) shift(mrb *oruby.MrbState, act int64, tok int64, val oruby.Value) error {
	mrb.AryPush(p.vstack, val)
	if p.debug {
		mrb.AryPush(p.tstack, mrb.Int64Value(tok))
		if _, err := mrb.Funcall(p.parser, mrb.Intern("racc_shift"), mrb.Int64Value(tok), p.tstack, p.vstack); err != nil {
			return err
		}
	}
	p.curstate = act
	mrb.AryPush(p.state, mrb.Int64Value(act))
	return nil
}

func (p *cparseParams) reduce(mrb *oruby.MrbState, act int64) (int, error) {
	ruleno := int(-act * 3)
	reduceLen := mrb.AryRef(p.reduceTable, ruleno).Int()
	reduceTo := mrb.AryRef(p.reduceTable, ruleno+1).Int64()
	methodID := mrb.AryRef(p.reduceTable, ruleno+2)
	if !methodID.IsSymbol() {
		return 0, oruby.ETypeError("not symbol")
	}
	midSym := methodID.Symbol()
	noReduce := midSym == mrb.Intern("_reduce_none")

	var tmp oruby.Value
	var tmpV, tmpT oruby.Value

	if reduceLen == 0 {
		tmp = mrb.NilValue()
		if !noReduce {
			tmpV = mrb.AryNew().Value()
		}
		if p.debug {
			tmpT = mrb.AryNew().Value()
		}
	} else {
		vLen := oruby.RArrayLen(p.vstack)
		start := vLen - reduceLen
		if start < 0 {
			start = 0
		}
		if !noReduce {
			tv := mrb.AryNewCapa(reduceLen)
			for i := start; i < vLen; i++ {
				mrb.AryPush(tv, mrb.AryRef(p.vstack, i))
			}
			tmpV = tv.Value()
			tmp = mrb.AryRef(tmpV, 0)
		} else {
			tmp = mrb.AryRef(p.vstack, start)
		}
		for i := 0; i < reduceLen; i++ {
			mrb.AryPop(p.vstack)
		}
		if p.debug {
			tLen := oruby.RArrayLen(p.tstack)
			tStart := tLen - reduceLen
			if tStart < 0 {
				tStart = 0
			}
			tt := mrb.AryNewCapa(reduceLen)
			for i := tStart; i < tLen; i++ {
				mrb.AryPush(tt, mrb.AryRef(p.tstack, i))
			}
			tmpT = tt.Value()
			for i := 0; i < reduceLen; i++ {
				mrb.AryPop(p.tstack)
			}
		}
		for i := 0; i < reduceLen; i++ {
			mrb.AryPop(p.state)
		}
	}

	if !noReduce {
		paramsCls := rb.ClassPath(mrb, "Racc::CparseParams")
		resPair, err := mrb.Funcall(
			paramsCls,
			mrb.Intern("__call_action"),
			p.parser,
			methodID,
			mrb.BoolValue(p.useResVar),
			tmpV,
			p.vstack,
			tmp,
		)
		if err != nil {
			return 0, err
		}
		p.errstatus = mrb.IVGet(p.parser, mrb.Intern("@racc_error_status")).Int64()
		if resPair.IsInt() {
			// Thrown :racc_jump code (1 = yyerror, 2 = yyaccept)
			return resPair.Int(), nil
		}
		if resPair.IsArray() && oruby.RArrayLen(resPair) >= 2 {
			code := mrb.AryRef(resPair, 0).Int()
			if code != 0 {
				return code, nil
			}
			tmp = mrb.AryRef(resPair, 1)
		} else {
			tmp = resPair
		}
	}

	mrb.AryPush(p.vstack, tmp)
	if p.debug {
		mrb.AryPush(p.tstack, mrb.Int64Value(reduceTo))
		if _, err := mrb.Funcall(p.parser, mrb.Intern("racc_reduce"), tmpT, mrb.Int64Value(reduceTo), p.tstack, p.vstack); err != nil {
			return 0, err
		}
	}

	sLen := oruby.RArrayLen(p.state)
	if sLen == 0 {
		return 0, errors.New("state stack unexpectedly empty")
	}
	k2 := mrb.AryRef(p.state, sLen-1).Int64()
	k1 := reduceTo - p.ntBase

	var gotoState int64
	found := false
	if ptr, ok := optAt(p.gotoPointer, k1); ok {
		i := ptr + k2
		if gs, ok := optAt(p.gotoTable, i); ok {
			if chk, ok := optAt(p.gotoCheck, i); ok && chk == k1 {
				gotoState = gs
				found = true
			}
		}
	}
	if !found {
		gotoState = intAt(p.gotoDefault, k1)
	}

	mrb.AryPush(p.state, mrb.Int64Value(gotoState))
	p.curstate = gotoState
	p.errstatus = mrb.IVGet(p.parser, mrb.Intern("@racc_error_status")).Int64()
	return 0, nil
}

func (p *cparseParams) parseMain(mrb *oruby.MrbState, tok, val oruby.Value, resume bool) error {
	var i int64
	var act int64
	readNext := true

	for {
		if resume {
			resume = false
			i = p.savedI
			goto convertToken
		}

		{
			ptr, ok := optAt(p.actionPointer, p.curstate)
			if !ok {
				goto notFound
			}
			i = ptr
		}

		if readNext && p.t != finalToken {
			if p.lexIsIterator {
				if p.fin != finNone {
					return oruby.EArgumentError("token given after EOF")
				}
				p.savedI = i
				return nil
			}
			raw, err := mrb.Funcall(p.parser, mrb.Intern("next_token"))
			if err != nil {
				return err
			}
			tok, val, err = p.extractUserToken(mrb, raw)
			if err != nil {
				return err
			}
			goto convertToken
		}
		goto afterToken

	convertToken:
		{
			// Use Ruby Hash#[] to avoid MRB_NO_BOXING symbol padding issues in HashGet
			tv, err := mrb.Funcall(p.tokenTable, mrb.Intern("[]"), tok)
			if err != nil {
				return err
			}
			if tv.IsNil() {
				p.t = errorToken
			} else {
				p.t = tv.Int64()
			}
			if p.debug {
				if _, err := mrb.Funcall(p.parser, mrb.Intern("racc_read_token"), mrb.Int64Value(p.t), tok, val); err != nil {
					return err
				}
			}
		}

	afterToken:
		readNext = false
		i += p.t
		if i < 0 {
			goto notFound
		}
		{
			av, ok1 := optAt(p.actionTable, i)
			if !ok1 {
				goto notFound
			}
			chk, ok2 := optAt(p.actionCheck, i)
			if !ok2 || chk != p.curstate {
				goto notFound
			}
			act = av
			goto handleAct
		}

	notFound:
		act = intAt(p.actionDefault, p.curstate)

	handleAct:
		if act > 0 && act < p.shiftN {
			if p.errstatus > 0 {
				p.errstatus--
				mrb.IVSet(p.parser, mrb.Intern("@racc_error_status"), mrb.Int64Value(p.errstatus))
			}
			if err := p.shift(mrb, act, p.t, val); err != nil {
				return err
			}
			readNext = true
		} else if act < 0 && act > -p.reduceN {
			code, err := p.reduce(mrb, act)
			if err != nil {
				return err
			}
			switch code {
			case 1:
				goto userYYError
			case 2:
				goto acceptLabel
			}
		} else if act == -p.reduceN {
			goto errorLabel
		} else if act == p.shiftN {
			goto acceptLabel
		} else {
			return fmt.Errorf("[Racc Bug] unknown act value %d", act)
		}

	errorRecovered:
		if p.debug {
			if _, err := mrb.Funcall(p.parser, mrb.Intern("racc_next_state"), mrb.Int64Value(p.curstate), p.state); err != nil {
				return err
			}
		}
		continue

	errorLabel:
		if p.errstatus == 0 {
			p.nerr++
			if _, err := mrb.Funcall(p.parser, mrb.Intern("on_error"), mrb.Int64Value(p.t), val, p.vstack); err != nil {
				return err
			}
		}

	userYYError:
		if p.errstatus == 3 {
			if p.t == finalToken {
				p.retval = mrb.NilValue()
				p.fin = finEOT
				return nil
			}
			readNext = true
		}
		p.errstatus = 3
		mrb.IVSet(p.parser, mrb.Intern("@racc_error_status"), mrb.Int64Value(p.errstatus))

		for {
			ep, ok := optAt(p.actionPointer, p.curstate)
			if ok {
				ei := ep + errorToken
				if ei >= 0 {
					if av, ok1 := optAt(p.actionTable, ei); ok1 {
						if chk, ok2 := optAt(p.actionCheck, ei); ok2 && chk == p.curstate {
							act = av
							break
						}
					}
				}
			}
			if oruby.RArrayLen(p.state) <= 1 {
				p.retval = mrb.NilValue()
				p.fin = finCantPop
				return nil
			}
			mrb.AryPop(p.state)
			mrb.AryPop(p.vstack)
			p.curstate = mrb.AryRef(p.state, oruby.RArrayLen(p.state)-1).Int64()
			if p.debug {
				mrb.AryPop(p.tstack)
				if _, err := mrb.Funcall(p.parser, mrb.Intern("racc_e_pop"), p.state, p.tstack, p.vstack); err != nil {
					return err
				}
			}
		}

		if act > 0 && act < p.shiftN {
			if err := p.shift(mrb, act, errorToken, val); err != nil {
				return err
			}
		} else if act < 0 && act > -p.reduceN {
			code, err := p.reduce(mrb, act)
			if err != nil {
				return err
			}
			switch code {
			case 1:
				goto userYYError
			case 2:
				goto acceptLabel
			}
		} else if act == p.shiftN {
			goto acceptLabel
		} else {
			return fmt.Errorf("[Racc Bug] unknown act value %d", act)
		}
		goto errorRecovered
	}

acceptLabel:
	if p.debug {
		if _, err := mrb.Funcall(p.parser, mrb.Intern("racc_accept")); err != nil {
			return err
		}
	}
	if oruby.RArrayLen(p.vstack) > 0 {
		p.retval = mrb.AryRef(p.vstack, 0)
	} else {
		p.retval = mrb.NilValue()
	}
	p.fin = finAccept
	return nil
}
