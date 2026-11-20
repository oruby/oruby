package oruby

// #include <stdlib.h>
// #include "go-prism.h"
import "C"

import (
	"strconv"
	"strings"
	"unsafe"
)

// CoverageRec is one record of the coverage scan (go-coverage.c, orb_cov_rec)
type CoverageRec struct {
	Kind, S, E, Line, T, F, M, Aux, Aux2 int
	L1, C1, L2, C2                       int
}

// Coverage record kinds
const (
	CovNone    = int(C.ORB_COV_NONE)
	CovPrefix  = int(C.ORB_COV_PREFIX)
	CovWrap    = int(C.ORB_COV_WRAP)
	CovCond    = int(C.ORB_COV_COND)
	CovNilCond = int(C.ORB_COV_NILCOND)
	CovBase    = int(C.ORB_COV_BASE)
	CovTarget  = int(C.ORB_COV_TARGET)
	CovDef     = int(C.ORB_COV_DEF)
)

// CoverageScan is what MRI's Coverage counts in a source, found with Prism as
// MRI's compiler does (go-coverage.c): the lines (Lines[i]: line i+1 is
// counted), and the records of the counters, branches and methods
type CoverageScan struct {
	Lines []bool
	Recs  []CoverageRec
	// what Instrument adds to the lines, branch counters and methods it
	// counts (eval'd code counting into its file's tables), and whether it
	// counts lines at all
	LineOff, CounterOff, MethodOff int
	NoLines                        bool
}

// ScanCoverage scans src; branches and methods add those records. A source
// that does not parse has no counted lines.
func ScanCoverage(src []byte, branches, methods bool) *CoverageScan {
	sc := &CoverageScan{}
	var p *C.char
	if len(src) > 0 {
		p = (*C.char)(unsafe.Pointer(&src[0]))
	}
	var n, nlines C.int
	var cover *C.int8_t
	prismMu.Lock()
	recs := C._oruby_prism_coverage(p, C.size_t(len(src)), C.int(btoi(branches)), C.int(btoi(methods)), &n, &cover, &nlines)
	prismMu.Unlock()
	if cover != nil {
		for _, v := range unsafe.Slice(cover, int(nlines)) {
			sc.Lines = append(sc.Lines, v != 0)
		}
		C.free(unsafe.Pointer(cover))
	}
	if recs != nil {
		for _, r := range unsafe.Slice(recs, int(n)) {
			sc.Recs = append(sc.Recs, CoverageRec{int(r.kind), int(r.s), int(r.e), int(r.line), int(r.t), int(r.f), int(r.m),
				int(r.aux), int(r.aux2), int(r.l1), int(r.c1), int(r.l2), int(r.c2)})
		}
		C.free(unsafe.Pointer(recs))
	}
	if sc.Lines == nil {
		sc.Lines = []bool{}
	}
	return sc
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Instrument is src with the scan's counters inserted, as calls to the
// Coverage module (gem/coverage) for file id fid. The inserts have no
// newlines: line numbers stay as in src.
func (sc *CoverageScan) Instrument(src []byte, fid int) []byte {
	r := &rewriter{src: src}
	f := strconv.Itoa(fid)
	off := func(v, by int) string {
		if v < 0 {
			return "-1"
		}
		return strconv.Itoa(v + by)
	}
	line := func(c CoverageRec) int {
		if c.Line <= 0 || sc.NoLines {
			return -1
		}
		return c.Line - 1 // the line's index
	}
	hit := func(c CoverageRec) string {
		call := "::Coverage.__oruby_hit(" + f + "," + off(line(c), sc.LineOff) + "," + off(c.T, sc.CounterOff) + "," +
			off(c.F, sc.CounterOff) + "," + off(c.M, sc.MethodOff)
		if c.M >= 0 {
			call += ",self" // the receiver: which copy of the method ran
		}
		return call + ")"
	}
	counts := func(c CoverageRec) bool { return line(c) >= 0 || c.T >= 0 || c.F >= 0 || c.M >= 0 }
	for _, c := range sc.Recs {
		c := c
		switch c.Kind {
		case CovPrefix:
			if counts(c) {
				r.add(c.S, c.S, func(*rewriter) string { return hit(c) + "; " })
			}
		case CovWrap:
			if counts(c) {
				r.addWrap(c.S, c.E, func(*rewriter) string { return "(" + hit(c) + "; " }, func(*rewriter) string { return ")" })
			}
		case CovCond, CovNilCond:
			fn := "__oruby_c"
			if c.Kind == CovNilCond {
				fn = "__oruby_n"
			}
			open := "::Coverage." + fn + "(" + f + "," + off(c.T, sc.CounterOff) + "," + off(c.F, sc.CounterOff) + ",("
			r.addWrap(c.S, c.E, func(*rewriter) string { return open }, func(*rewriter) string { return "))" })
		case CovDef:
			recv := "self"
			if c.F == 2 {
				recv = "(" + string(src[c.Line:c.M]) + ")"
			}
			open := "::Coverage.__oruby_md(" + f + "," + off(c.T, sc.MethodOff) + "," + strconv.Itoa(c.F) + "," + recv + ",("
			r.addWrap(c.S, c.E, func(*rewriter) string { return open }, func(*rewriter) string { return "))" })
		}
	}
	if len(r.edits) == 0 {
		return src
	}
	return r.output()
}

// CoverageBranchTypes and CoverageBranchLabels name ORB_COV_BASE's and
// ORB_COV_TARGET's aux
var (
	CoverageBranchTypes  = []string{"if", "unless", "while", "until", "case", "&."}
	CoverageBranchLabels = []string{"then", "else", "body", "when", "in"}
)

// MethodName is a CovDef record's method name
func (c CoverageRec) MethodName(src []byte) string {
	if c.Aux < 0 || c.Aux2 > len(src) || c.Aux2 < c.Aux {
		return ""
	}
	return strings.TrimSpace(string(src[c.Aux:c.Aux2]))
}
