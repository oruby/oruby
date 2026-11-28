//go:build darwin || linux

package oruby

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMappedOrbLifetime(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "app.orb")
	write := func(source string) {
		t.Helper()
		b := NewOrbBuilder(0)
		if err := b.AddEntry("main.rb", OrbKindRuby, OrbFlagEntrypoint, 0644, []byte(source)); err != nil {
			t.Fatal(err)
		}
		if err := b.WriteFile(filename); err != nil {
			t.Fatal(err)
		}
	}
	write("'original'")

	first, second := MrbOpen(), MrbOpen()
	defer first.Close()
	defer second.Close()
	orb, err := first.MountOrbFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if orb.mapping == nil {
		t.Fatal("expected a memory mapping")
	}
	again, err := first.MountOrbFile(filename)
	if err != nil || again != orb {
		t.Fatalf("duplicate mount = %p, %v, want %p", again, err, orb)
	}
	shared, err := second.MountOrb(orb)
	if err != nil {
		t.Fatal(err)
	}
	mapping := orb.mapping
	first.Close()
	if mapping.refs.Load() != 1 || orb.raw != nil {
		t.Fatal("closing the first state should leave exactly one owner")
	}

	// Rebuilding the archive must leave the old mapping readable.
	write("'replacement'")
	run := func(state *MrbState, bundle *OrbFile, want string) {
		t.Helper()
		entry, ok := bundle.Entrypoint()
		if !ok {
			t.Fatal("missing entrypoint")
		}
		got, err := state.ExecuteOrbEntry(bundle, entry, nil)
		if err != nil || got.String() != want {
			t.Fatalf("entrypoint = %s, %v, want %q", got, err, want)
		}
	}
	run(second, shared, "original")
	second.Close()
	if mapping.refs.Load() != 0 || shared.raw != nil {
		t.Fatal("closing the last state should release the mapping")
	}

	third := MrbOpen()
	defer third.Close()
	file, err := os.Open(filename)
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := third.MountOpenOrbFile(file)
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	run(third, replacement, "replacement")
}
