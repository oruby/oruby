//go:build gc
// +build gc

package oruby

import (
	"testing"
)

// TestMrbState_FullGC this test is adopted from mitchellh/go-mruby
func TestMrbState_FullGC(t *testing.T) {
	//mrb := MrbOpen()
	mrb, _ := NewCore()
	defer mrb.Close()

	arenaIndex := mrb.GCArenaSave()

	v := mrb.StringValue("Test")
	Expect(t, !mrb.IsDead(v), "should be alive value")

	mrb.GCArenaRestore(arenaIndex)

	mrb.FullGC()

	Expect(t, mrb.IsDead(v), "should be dead value")
}

// TestMrbState_GCDisable this test is adopted from mitchellh/go-mruby
func TestMrbState_GCDisable(t *testing.T) {
	mrb := MrbOpen()
	defer mrb.Close()

	mrb.FullGC()
	mrb.GCDisable()

	// String should create three a objects, two of them will be overwritten and ready for GC
	_, err := mrb.LoadString("a = []; a = []; a = []")
	if err != nil {
		t.Fatal(err)
	}

	orig := mrb.GCLiveObjectCount()
	mrb.FullGC()

	// Since GC is disabled - all three objects should be alive
	if orig != mrb.GCLiveObjectCount() {
		t.Errorf("Object count was not what was expected after full GC: %d %d", orig, mrb.GCLiveObjectCount())
	}

	mrb.GCEnable()
	mrb.FullGC()

	// After enabling GC, two "a" objects should be garbage collected, and one "a" should be alive
	if mrb.GCLiveObjectCount() >= orig {
		t.Errorf("Object count was not what was expected after full GC: %d %d", orig, mrb.GCLiveObjectCount())
	}
}

func TestMrbState_ObjspaceEachObjects(t *testing.T) {
	mrb := MrbOpen()
	defer mrb.Close()

	v := mrb.StringValue("Test")
	found := false

	mrb.ObjspaceEachObjects(func(mrb *MrbState, obj RBasic) int {
		if v.Value().RBasic().p == obj.p {
			found = true
			return MrbEachObjBreak
		}
		return MrbEachObjOK
	})

	Expect(t, found, "Value shoud be found in obj space")
}
