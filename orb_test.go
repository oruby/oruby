package oruby

import (
	"bytes"
	"testing"
)

func TestOrbBuilderAndParser(t *testing.T) {
	mrb := MrbOpen()
	defer mrb.Close()

	compileRite := func(name, code string) []byte {
		c := mrb.MrbCcontextNew()
		defer c.Free()
		c.Filename(name)
		c.SetNoExec(true)
		v, err := c.LoadBytes([]byte(code))
		if err != nil {
			t.Fatalf("compile %s: %v", name, err)
		}
		var buf bytes.Buffer
		if st, err := mrb.DumpIrepBinary(mrb.RProc(v).IRep(), MrbDumpDebugInfo, &buf); err != nil || st != MrbDumpOK {
			t.Fatalf("dump %s: st=%d err=%v", name, st, err)
		}
		return buf.Bytes()
	}

	b := NewOrbBuilder(OrbHdrDebugInfo)
	if err := b.AddDir("lib", true); err != nil {
		t.Fatal(err)
	}
	if err := b.AddEntry("lib/greeter.rb", OrbKindMrb, 0, 0644, compileRite("lib/greeter.rb", `class Greeter; def hi = "orb_hi"; end`)); err != nil {
		t.Fatal(err)
	}
	if err := b.AddEntry("config/settings.yml", OrbKindAsset, 0, 0644, []byte("env: production\n")); err != nil {
		t.Fatal(err)
	}
	if err := b.AddEntry("main.rb", OrbKindMrb, 0, 0755, compileRite("main.rb", `Greeter.new.hi`)); err != nil {
		t.Fatal(err)
	}
	b.SetEntrypoint("main.rb")

	raw, err := b.Bytes()
	if err != nil {
		t.Fatalf("OrbBuilder.Bytes: %v", err)
	}
	if !IsOrbBinary(raw) {
		t.Fatal("IsOrbBinary returned false for valid .orb image")
	}

	orb, err := ParseOrb("test.orb", raw)
	if err != nil {
		t.Fatalf("ParseOrb: %v", err)
	}
	if orb.Header.Version != OrbVersion {
		t.Fatalf("Version = %d, want %d", orb.Header.Version, OrbVersion)
	}
	if orb.Header.EntryCount != 4 {
		t.Fatalf("EntryCount = %d, want 4", orb.Header.EntryCount)
	}
	if orb.Header.IndexOffset%uint64(OrbAlignment) != 0 {
		t.Fatalf("IndexOffset %d is not 4-byte aligned", orb.Header.IndexOffset)
	}

	for _, e := range orb.Entries {
		if e.DataSize > 0 && e.DataOffset%uint64(OrbAlignment) != 0 {
			t.Fatalf("entry %s DataOffset %d is not 4-byte aligned", e.Path, e.DataOffset)
		}
	}

	ep, ok := orb.Entrypoint()
	if !ok || ep.Path != "main.rb" {
		t.Fatalf("Entrypoint = %+v, %v; want main.rb", ep, ok)
	}

	// Lookup .mrb alias for .rb entry
	greeterEntry, ok := orb.Lookup("lib/greeter.mrb")
	if !ok || greeterEntry.Path != "lib/greeter.rb" {
		t.Fatalf("Lookup(lib/greeter.mrb) = %+v, %v", greeterEntry, ok)
	}

	// Mount and execute
	runState := MrbOpen()
	defer runState.Close()
	mounted, err := runState.MountOrb(orb)
	if err != nil {
		t.Fatalf("MountOrb: %v", err)
	}
	if asset, ok := runState.OrbReadAsset("config/settings.yml"); !ok || string(asset) != "env: production\n" {
		t.Fatalf("OrbReadAsset = %q, %v", string(asset), ok)
	}
	if _, err := runState.ExecuteOrbEntry(mounted, greeterEntry, nil); err != nil {
		t.Fatalf("ExecuteOrbEntry greeter: %v", err)
	}
	res, err := runState.ExecuteOrbEntry(mounted, ep, nil)
	if err != nil {
		t.Fatalf("ExecuteOrbEntry main: %v", err)
	}
	if got := res.String(); got != "orb_hi" {
		t.Fatalf("got %q, want %q", got, "orb_hi")
	}
}
