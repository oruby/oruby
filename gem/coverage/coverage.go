// Package coverage is MRI's Coverage (ext/coverage): line, oneshot line,
// branch and method coverage of the files required or loaded while it is set
// up. A file is instrumented as it loads (gem/load asks
// Coverage.__oruby_instrument): the counters go where MRI's compiler puts its
// coverage events, found with Prism by oruby.ScanCoverage, and count as the
// code runs. coverage.rb keeps the tables and answers Coverage's API.
package coverage

import (
	_ "embed"
	"os"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/rb"
)

//go:embed coverage.rb
var coverageRb []byte

func init() {
	oruby.LazyGem("coverage", initCoverage)
}

func initCoverage(mrb *oruby.MrbState) interface{} {
	cov := mrb.DefineModule("Coverage")
	// Coverage.__oruby_scan(src, fid, branches, methods, lines, line_off,
	// counter_off, method_off) -> [instrumented source, lines, bases, methods]
	// (the offsets and lines: eval'd code counting into its file's tables)
	// lines: per line 0 (counted) or nil; bases: [type, l1, c1, l2, c2,
	// [[label, l1, c1, l2, c2, counter], ...]] in MRI's order; methods:
	// [name, l1, c1, l2, c2] by method id
	cov.DefineClassMethod("__oruby_scan", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		src := args.Item(0).Bytes()
		fid := int(args.Item(1).Int64())
		sc := oruby.ScanCoverage(src, args.Item(2).Bool(), args.Item(3).Bool())
		sc.NoLines = !args.Item(4).Bool()
		sc.LineOff, sc.CounterOff, sc.MethodOff = int(args.Item(5).Int64()), int(args.Item(6).Int64()), int(args.Item(7).Int64())
		out := mrb.AryNew()
		out.Push(mrb.BytesValue(sc.Instrument(src, fid)))
		out.Push(lineStub(mrb, sc))
		bases := mrb.AryNew()
		baseAt := map[int]oruby.RArray{} // a base record's index -> its targets
		methods := mrb.AryNew()
		for i, c := range sc.Recs {
			switch c.Kind {
			case oruby.CovBase:
				targets := mrb.AryNew()
				b := mrb.AryNewFromValues(mrb.SymbolValue(mrb.Intern(oruby.CoverageBranchTypes[c.Aux])),
					mrb.FixnumValue(c.L1+sc.LineOff), mrb.FixnumValue(c.C1), mrb.FixnumValue(c.L2+sc.LineOff), mrb.FixnumValue(c.C2), targets.Value())
				bases.Push(b)
				baseAt[i] = targets
			case oruby.CovTarget:
				if targets, ok := baseAt[c.F]; ok {
					targets.Push(mrb.AryNewFromValues(mrb.SymbolValue(mrb.Intern(oruby.CoverageBranchLabels[c.Aux])),
						mrb.FixnumValue(c.L1+sc.LineOff), mrb.FixnumValue(c.C1), mrb.FixnumValue(c.L2+sc.LineOff), mrb.FixnumValue(c.C2), mrb.FixnumValue(c.T+sc.CounterOff)))
				}
			case oruby.CovDef:
				methods.Push(mrb.AryNewFromValues(mrb.SymbolValue(mrb.Intern(c.MethodName(src))),
					mrb.FixnumValue(c.L1+sc.LineOff), mrb.FixnumValue(c.C1), mrb.FixnumValue(c.L2+sc.LineOff), mrb.FixnumValue(c.C2)))
			}
		}
		out.Push(bases)
		out.Push(methods)
		return out
	}, mrb.ArgsReq(8))
	// Coverage.__oruby_stub(src): the lines line coverage counts (0) or not (nil)
	cov.DefineClassMethod("__oruby_stub", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		return lineStub(mrb, oruby.ScanCoverage(mrb.GetArgsFirst().Bytes(), false, false))
	}, mrb.ArgsReq(1))
	// Coverage.__oruby_read(path): the file's bytes (with no File class needed)
	cov.DefineClassMethod("__oruby_read", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		path := mrb.GetArgsFirst()
		if !path.IsString() {
			return mrb.Raise(mrb.ExcGet("TypeError"), "no implicit conversion into String")
		}
		data, err := os.ReadFile(mrb.String(path))
		if err != nil {
			return mrb.RaiseError(err)
		}
		return mrb.BytesValue(data)
	}, mrb.ArgsReq(1))
	rb.LoadRuby(mrb, "<internal:coverage>", coverageRb)
	return nil
}

func lineStub(mrb *oruby.MrbState, sc *oruby.CoverageScan) oruby.RArray {
	a := mrb.AryNewCapa(len(sc.Lines))
	for _, counted := range sc.Lines {
		if counted {
			a.Push(mrb.FixnumValue(0))
		} else {
			a.Push(mrb.NilValue())
		}
	}
	return a
}
