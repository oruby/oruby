package thread

import (
	"bytes"
	"strings"

	"github.com/oruby/oruby"
)

/*
	if c.mrb == c.mrbCaller {
		return v, nil
	}

	mrb := c.mrbCaller
	mrb2 := c.mrb
*/

func (c *Context) migrateState() error {
	var err error

	c.migrateAllSymbols()

	c.proc, err = migrateRProc(c.mrbCaller, c.mrb, c.proc)
	if err != nil {
		return err
	}
	c.proc.SetTargetClass(c.mrb.ObjectClass())

	for i := 0; i < c.args.Len(); i++ {
		v, err := migrateValue(c.mrbCaller, c.mrb, c.args.Item(i))
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
		if isSafeMigratableSimpleValue(c.mrbCaller, o) {
			v, err := migrateValue(c.mrbCaller, c.mrb, o)
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
func migrateValue(mrb, mrb2 *oruby.MrbState, v oruby.Value) (oruby.MrbValue, error) {
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
		err = migrateSimpleIV(mrb, mrb2, v, nv)
		if err != nil {
			return mrb.NilValue(), err
		}

		if v.Type() == oruby.MrbTTException {
			msg, err := migrateValue(mrb, mrb2, mrb.IVGet(v, mrb.Intern("mesg")))
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
		return migrateRProc(mrb, mrb2, mrb.RProc(v))
	case oruby.MrbTTFalse, oruby.MrbTTTrue, oruby.MrbTTFixnum:
		return v, nil
	case oruby.MrbTTSymbol:
		return migrateSym(mrb, mrb2, v.Symbol()), nil
	case oruby.MrbTTFloat:
		return mrb2.FloatValue(v.Float64()), nil
	case oruby.MrbTTString:
		return mrb2.StringValue(v.String()), nil
	case oruby.MrbTTRange:
		r := oruby.MrbRangePtr(v)
		beg, err := migrateValue(mrb, mrb2, r.Begin())
		if err != nil {
			return mrb.NilValue(), err
		}
		end, err := migrateValue(mrb, mrb2, r.End())
		if err != nil {
			return mrb.NilValue(), err
		}

		return mrb2.RangeNew(beg, end, r.Exclusive()), nil
	case oruby.MrbTTArray:
		nv := mrb2.AryNewCapa(v.Len())
		ai := mrb2.GCArenaSave()
		for i := 0; i < v.Len(); i++ {
			item, err := migrateValue(mrb, mrb2, mrb.AryEntry(v, i))
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
		l := ka.Len()
		for i := 0; i < l; i++ {
			ai := mrb2.GCArenaSave()
			k, err := migrateValue(mrb, mrb2, mrb.AryEntry(ka, i))
			if err != nil {
				return mrb.NilValue(), err
			}
			o, err := migrateValue(mrb, mrb2, mrb.HashGet(v, k))
			if err != nil {
				return mrb.NilValue(), err
			}
			nv.Set(k, o)
			mrb2.GCArenaRestore(ai)
		}
		err := migrateSimpleIV(mrb, mrb2, v, nv.Value())
		return nv, err
	case oruby.MrbTTCData:
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

func migrateSimpleIV(mrb, mrb2 *oruby.MrbState, v, v2 oruby.Value) error {
	a := mrb.RValue(v).Call("instance_variables").RArray()

	for i := 0; i < a.Len(); i++ {
		sym := mrb.Symbol(a.Item(i))
		sym2 := migrateSym(mrb, mrb2, sym)
		iv := mrb.IVGet(v, sym)
		v, err := migrateValue(mrb, mrb2, iv)
		if err != nil {
			return err
		}
		err = mrb2.IVSet(v2, sym2, v)
		if err != nil {
			return err
		}
	}
	return nil
}

func migrateSym(mrb, mrb2 *oruby.MrbState, sym oruby.MrbSym) oruby.MrbSym {
	s := mrb.SymString(sym)
	return mrb2.Intern(s)
}

func (c *Context) migrateAllSymbols() {
	for i := 1; i < c.mrb.SymIdx()+1; i++ {
		migrateSym(c.mrbCaller, c.mrb, oruby.MrbSym(i))
	}
}

func isSafeMigratableSimpleValue(mrb *oruby.MrbState, v oruby.Value) bool {
	switch v.Type() {
	case oruby.MrbTTObject, oruby.MrbTTException:
		path := mrb.ClassOf(v).ClassPath()
		if path.IsNil() || !mrb.ClassDefined(path.String()) {
			return false
		}
	case oruby.MrbTTProc, oruby.MrbTTFalse, oruby.MrbTTTrue, oruby.MrbTTInteger,
		oruby.MrbTTSymbol, oruby.MrbTTFloat, oruby.MrbTTString:
		return true
	case oruby.MrbTTRange:
		r := oruby.MrbRangePtr(v)
		if !isSafeMigratableSimpleValue(mrb, r.Begin()) || !isSafeMigratableSimpleValue(mrb, r.End()) {
			return false
		}
	case oruby.MrbTTArray:
		for i := 0; i < v.Len(); i++ {
			if !isSafeMigratableSimpleValue(mrb, mrb.AryEntry(v, i)) {
				return false
			}
		}
	case oruby.MrbTTHash:
		ka := mrb.HashKeys(v)
		for i := 0; i < ka.Len(); i++ {
			k := mrb.AryEntry(ka, i)
			if !isSafeMigratableSimpleValue(mrb, k) || !isSafeMigratableSimpleValue(mrb, mrb.HashGet(v, k)) {
				return false
			}
		}
	case oruby.MrbTTCData:
		return true
	}
	return false
}

func migrateIRepChild(mrb, mrb2 *oruby.MrbState, ret oruby.MrbIrep) {
	// migrateState pool
	for i := 0; i < ret.PLen(); i++ {
		ret.Pool(i).Migrate()
	}

	// migrateState iseq
	if (ret.Flags() & oruby.MrbIseqNoFree) != 0 {
		ret.CopyISeq(ret)
	}

	// migrateState sub ireps
	for i := 0; i < ret.RLen(); i++ {
		migrateIRepChild(mrb, mrb2, ret.Reps(i))
	}
}

func migrateIRep(mrb, mrb2 *oruby.MrbState, src oruby.MrbIrep) (oruby.MrbIrep, error) {
	var err error
	var irep bytes.Buffer

	_, err = mrb.DumpIrep(src, 0, &irep)
	if err != nil {
		return oruby.MrbIrep{}, err
	}

	ret, err := mrb2.ReadIrep(irep.Bytes())
	if err != nil {
		return oruby.MrbIrep{}, err
	}

	migrateIRepChild(mrb, mrb2, ret)

	return ret, nil
}

func migrateRProc(mrb, mrb2 *oruby.MrbState, proc oruby.RProc) (oruby.RProc, error) {
	irep, err := migrateIRep(mrb, mrb2, proc.IRep())
	if err != nil {
		return oruby.RProc{}, err
	}
	newproc := mrb2.ProcNew(irep)
	mrb2.IrepDecref(newproc.IRep())

	if proc.HasEnv() {
		stack := make([]oruby.Value, proc.Env().Len())
		for i := range stack {
			v := proc.Env().Stack(i)
			if v.IsProc() && v == proc.Value() {
				stack[i] = proc.Value()
				continue
			}
			nv, err := migrateValue(mrb, mrb2, v)
			if err != nil {
				return oruby.RProc{}, err
			}
			stack[i] = nv.Value()
		}
		newproc.SetEnv(stack...)

		if !proc.Upper().IsNil() {
			upper, err := migrateRProc(mrb, mrb2, proc.Upper())
			if err != nil {
				return newproc, err
			}
			newproc.SetUpper(upper)
		}
	}

	return newproc, nil
}
