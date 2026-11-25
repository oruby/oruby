package compiler

import "testing"

func TestProgressFuncNilIsSilent(t *testing.T) {
	var f ProgressFunc
	f.report("resolve", 1, 2, "x") // must not panic
}

func TestProgressFuncReports(t *testing.T) {
	var got []string
	f := ProgressFunc(func(stage string, done, total int, item string) {
		got = append(got, stage)
	})
	f.report("emit", 1, 3, "a.rb")
	f.report("emit", 2, 3, "b.rb")
	if len(got) != 2 || got[0] != "emit" {
		t.Fatalf("got %v", got)
	}
}

func TestModFileNameAvoidsArchSuffix(t *testing.T) {
	if got := modFileName(7); got != "aot_mod_007.go" {
		t.Fatalf("got %q", got)
	}
	if got := modFileName(386); got == "aot_mod_386.go" {
		t.Fatalf("%q ends in an architecture suffix and is ignored by go build", got)
	}
}
