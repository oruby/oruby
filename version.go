package oruby

// #include "go-mrb.h"
import "C"

func MRubyCopyright() string {
	return C.GoString(C._MRUBY_COPYRIGHT())
}

func MRubyDescription() string {
	return C.GoString(C._MRUBY_DESCRIPTION())
}

func MRubyRelease() (int, int, int) {
	return int(C.MRUBY_RELEASE_MAJOR), int(C.MRUBY_RELEASE_MINOR), int(C.MRUBY_RELEASE_TEENY)
}
