package thread

import (
	"bytes"
	"github.com/oruby/oruby"
	"strings"
)

func (c *Context) migrateState() error {
	var err error

	c.migrateAllSymbols()

	c.proc, err = c.migrateRProc(c.proc)
	if err != nil {
		return err
	}
	c.proc.SetTargetClass(c.mrb.ObjectClass())

	for i := 0; i < c.args.Len(); i++ {
		v, err := c.migrateValue(c.args.Item(i))
		if err != nil {
			return err
		}
		c.args.SetItem(i, v)
	}

	gv := c.mrbCaller.FGlobalVariables()
	for i := 0; i < gv.Len(); i++ {
		ai := c.mrbCaller.GCArenaSave()
		k := gv.Item(i).Symbol()
		o := c.mrbCaller.GVGet(k)
		if c.isSafeMigratableSimpleValue(o) {
			v, err := c.migrateValue(o)
			if err != nil {
				return err
			}
			c.mrb.GVSet(k, v)
		}
		c.mrbCaller.GCArenaRestore(ai)
	}
	return nil
}

func path2class(mrb *oruby.MrbState, classPath string) (oruby.RClass, error) {
	ret := mrb.ObjectClass()

	for _, name := range strings.Split(classPath, "::") {
		cls := mrb.Intern(name)

		if !mrb.ModCVDefined(ret, cls) {
			return oruby.RClass{}, oruby.EArgumentError("undefined class/module %v", name)
		}

		cnst := mrb.ModCVGet(ret, cls)
		if !cnst.IsClass() && !cnst.IsModule() {
			return oruby.RClass{}, oruby.ETypeError("%v does not refer to class/module", name)
		}
		ret = mrb.ClassPtr(cnst)
	}
	return ret, nil
}

// based on https://gist.github.com/3066997 and github.com/mattn/mruby-thread
func (c *Context) migrateValue(v oruby.Value) (oruby.MrbValue, error) {
	if c.mrb == c.mrbCaller {
		return v, nil
	}

	mrb  := c.mrbCaller
	mrb2 := c.mrb

	switch v.Type() {
	case oruby.MrbTTClass, oruby.MrbTTModule:
		path := mrb.ClassPtr(v).ClassPath()
		if path.IsNil() {
			return mrb.NilValue(), nil
		}
		return path2class(mrb2, path.String())
	case oruby.MrbTTObject, oruby.MrbTTException:
		path := mrb.ClassOf(v).ClassPath()
		if path.IsNil() {
			return mrb.NilValue(), nil
		}
		cls, err := path2class(mrb2, path.String())
		if err != nil {
			return mrb.NilValue(), err
		}
		nv := mrb.ObjAlloc(v.Type(), cls).Value()
		err = c.migrateSimpleIV(v, nv)
		if err != nil {
			return mrb.NilValue(), err
		}

		if v.Type() == oruby.MrbTTException {
			msg, err := c.migrateValue(mrb.IVGet(v, mrb.Intern( "mesg")))
			if err != nil {
				return mrb.NilValue(), err
			}
			err = mrb2.IVSet(nv, mrb2.Intern("mesg"), msg)
			if err != nil {
				return mrb.NilValue(), err
			}
		}
		return nv, nil
	case oruby.MrbTTProc:
		return c.migrateRProc(mrb.RProc(v))
	case oruby.MrbTTFalse, oruby.MrbTTTrue, oruby.MrbTTFixnum:
		return v, nil
	case oruby.MrbTTSymbol:
		return c.migrateSym(v.Symbol()), nil
	case oruby.MrbTTFloat:
		return mrb2.FloatValue(v.Float64()), nil
	case oruby.MrbTTString:
		return mrb2.StringValue(v.String()), nil
	case oruby.MrbTTRange:
		r := oruby.MrbRangePtr(v)
		beg, err := c.migrateValue(r.Begin())
		if err != nil {
			return mrb.NilValue(), err
		}
		end, err := c.migrateValue(r.End())
		if err != nil {
			return mrb.NilValue(), err
		}

		return mrb2.RangeNew(beg, end, r.Exclusive()), nil
	case oruby.MrbTTArray:
		nv := mrb2.AryNewCapa(v.Len())
		ai := mrb2.GCArenaSave()
		for i := 0; i < v.Len(); i++ {
			item, err := c.migrateValue(mrb.AryEntry(v, i))
			if err != nil {
				return mrb.NilValue(), err
			}
			nv.Push(item)
			mrb2.GCArenaRestore(ai)
		}
		return nv, nil
	case oruby.MrbTTHash:
		nv := mrb2.HashNew()
		ka := mrb.HashKeys(v)
		l  := ka.Len()
		for i := 0; i < l; i++ {
			ai := mrb2.GCArenaSave()
			k, err := c.migrateValue(mrb.AryEntry(ka, i))
			if err != nil {
				return mrb.NilValue(), err
			}
			o, err := c.migrateValue(mrb.HashGet(v, k))
			if err != nil {
				return mrb.NilValue(), err
			}
			nv.Set(k, o)
			mrb2.GCArenaRestore(ai)
		}
		err := c.migrateSimpleIV(v, nv.Value())
		return nv, err
	case oruby.MrbTTData:
		path := mrb.ClassPtr(v).ClassPath()
		c, err := path2class(mrb2, path.String())
		if err != nil {
			return mrb.NilValue(), err
		}

		nv := mrb.ObjAlloc(v.Type(), c).Value()
		mrb2.DataSetInterface(nv, mrb.Data(v))
		return nv, nil
	}

	return oruby.Nil, oruby.ETypeError("cannot migrateState object: %v (%v)", mrb.Inspect(v), mrb.TypeName(v))
}


func (c *Context) migrateSimpleIV(v, v2 oruby.Value) error {
	a := c.mrbCaller.ObjInstanceVariables(v)

	for i :=0; i < a.Len(); i++ {
		sym  := c.mrbCaller.Symbol(a.Item(i))
		sym2 := c.migrateSym(sym)
		iv := c.mrbCaller.IVGet(v, sym)
		v, err := c.migrateValue(iv)
		if err != nil {
			return err
		}
		err = c.mrb.IVSet(v2, sym2, v)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *Context) migrateSym(sym oruby.MrbSym) oruby.MrbSym {
	s := c.mrbCaller.SymString(sym)
	return c.mrb.Intern(s)
}

func (c *Context) migrateAllSymbols() {
	for i := 1; i < c.mrb.SymIdx() + 1; i++ {
		c.migrateSym(oruby.MrbSym(i))
	}
}

func (c *Context) isSafeMigratableSimpleValue(v oruby.Value) bool {
	mrb := c.mrbCaller
	switch v.Type() {
	case oruby.MrbTTObject, oruby.MrbTTException:
		path := mrb.ClassOf(v).ClassPath()
		if path.IsNil() || !mrb.ClassDefined(path.String()) {
			return false
		}
	case oruby.MrbTTProc, oruby.MrbTTFalse, oruby.MrbTTTrue, oruby.MrbTTFixnum,
		oruby.MrbTTSymbol, oruby.MrbTTFloat, oruby.MrbTTString:
		return true
	case oruby.MrbTTRange:
		r := oruby.MrbRangePtr(v)
		if !c.isSafeMigratableSimpleValue(r.Begin()) || !c.isSafeMigratableSimpleValue(r.End()) {
			return false
		}
	case oruby.MrbTTArray:
		for i :=0; i < v.Len(); i++ {
			if !c.isSafeMigratableSimpleValue(mrb.AryEntry(v, i)) {
				return false
			}
		}
	case oruby.MrbTTHash:
		ka := mrb.HashKeys(v)
		for i := 0; i < ka.Len(); i++ {
			k := mrb.AryEntry(ka, i)
			if !c.isSafeMigratableSimpleValue(k) ||	!c.isSafeMigratableSimpleValue(mrb.HashGet(v, k)) {
				return false
			}
		}
	case oruby.MrbTTData:
		return true
	}
	return false
}

func (c *Context) migrateIRepChild(ret oruby.MrbIrep) {
	// migrateState pool
	for i := 0; i < ret.PLen(); i++ {
		v := ret.Pool(i)
		if v.IsString() {
			s := c.mrb.EnsureStringType(v)
			if s.IsNoFree() && s.Len() > 0 {
				s.MigrateTo(c.mrb)
			}
		}
	}

	// migrateState iseq
	if (ret.Flags() & oruby.MrbIseqNoFree) != 0 {
		ret.CopyISeq(ret)
	}

	// migrateState sub ireps
	for i := 0; i < ret.RLen(); i++ {
		c.migrateIRepChild(ret.Reps(i));
	}
}

func (c *Context) migrateIRep(src oruby.MrbIrep) (oruby.MrbIrep, error) {
	var err error
	var irep bytes.Buffer

	_,err = c.mrbCaller.DumpIrep(src, oruby.DumpEndianNat, &irep)
	if err != nil {
		return oruby.MrbIrep{}, err
	}

	ret, err := c.mrb.ReadIrep(irep.Bytes())
	if err != nil {
		return oruby.MrbIrep{}, err
	}

	c.migrateIRepChild(ret)

	return ret, nil
}

func (c *Context) migrateRProc(proc oruby.RProc) (oruby.RProc, error) {
	irep, err := c.migrateIRep(proc.IRep())
	if err != nil {
		return oruby.RProc{}, err
	}
	newproc := c.mrb.ProcNew(irep)
	c.mrb.IrepDecref(newproc.IRep())

	if proc.HasEnv() {
		stack := make([]oruby.Value, proc.Env().Len())
		for i,_ := range stack {
			v := proc.Env().Stack(i)
			if v.IsProc() && v == proc.Value() {
				stack[i] = proc.Value()
				continue
			}
			nv, err := c.migrateValue(v)
			if err != nil {
				return oruby.RProc{}, err
			}
			stack[i] = nv.Value()
		}
		newproc.SetEnv(stack...)

		if !proc.Upper().IsNil() {
			upper, err := c.migrateRProc(proc.Upper())
			if err != nil {
				return newproc, err
			}
			newproc.SetUpper(upper)
		}
	}

	return newproc, nil
}

