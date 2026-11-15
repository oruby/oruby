package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseTally(t *testing.T) {
	tests := []struct {
		name, text string
		want       tally
		ok         bool
	}{
		{"normal", "text\n1 file, 3 examples, 3 expectations, 0 failures, 0 errors, 0 tagged\n", tally{1, 3, 3, 0, 0, 0}, true},
		{"failures", "2 files, 4 examples, 5 expectations, 1 failure, 2 errors, 3 tagged\n", tally{2, 4, 5, 1, 2, 3}, true},
		{"last", "1 file, 1 example, 1 expectation, 0 failures, 0 errors, 0 tagged\n2 files, 9 examples, 9 expectations, 0 failures, 0 errors, 0 tagged\n", tally{2, 9, 9, 0, 0, 0}, true},
		{"incomplete", "1 file, 3 examples, 0 failures, 0 errors", tally{}, false},
		{"overflow", strings.Repeat("9", 100) + " files, 1 example, 1 expectation, 0 failures, 0 errors, 0 tagged", tally{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseTally(tt.text)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("parseTally = %+v, %v; want %+v, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestReportTally(t *testing.T) {
	good := "1 file, 3 examples, 3 expectations, 0 failures, 0 errors, 0 tagged\n"
	if !reportTally(result{output: good, exitCode: 0}) {
		t.Fatal("valid result should pass")
	}
	for _, r := range []result{
		{output: "", exitCode: 0},
		{output: good, exitCode: 1},
		{output: good, exitCode: 0, timedOut: true},
		{output: strings.Replace(good, "0 failures", "1 failure", 1), exitCode: 0},
		{output: strings.Replace(good, "3 examples", "0 examples", 1), exitCode: 0},
	} {
		if reportTally(r) {
			t.Fatalf("unexpected pass: %+v", r)
		}
	}
	if !skippedDependencies("warning: skipped: missing\n") || skippedDependencies("no warnings") {
		t.Fatal("skipped dependency detection failed")
	}
}

func TestCommandHelper(t *testing.T) {
	if os.Getenv("SMOKE_HELPER") == "" {
		return
	}
	switch os.Getenv("SMOKE_HELPER") {
	case "exit":
		fmt.Print("failure output")
		os.Exit(7)
	case "timeout":
		time.Sleep(10 * time.Second)
	}
	os.Exit(0)
}

func TestRunCommand(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	r := runCommand("", []string{"SMOKE_HELPER=exit"}, time.Second*5, exe, "-test.run=TestCommandHelper")
	if r.exitCode != 7 || r.err == nil || r.timedOut || r.output != "failure output" {
		t.Fatalf("failure result: %+v", r)
	}
	r = runCommand("", []string{"SMOKE_HELPER=timeout"}, 100*time.Millisecond, exe, "-test.run=TestCommandHelper")
	if !r.timedOut || r.err == nil {
		t.Fatalf("timeout result: %+v", r)
	}
	r = runCommand("", nil, time.Second, filepath.Join(".", "missing-smoke-command"))
	if r.exitCode != -1 || r.err == nil || r.timedOut {
		t.Fatalf("missing command result: %+v", r)
	}
}

func TestHashRubyTree(t *testing.T) {
	dir, err := os.MkdirTemp(".", ".test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("b.rb", "b")
	write("a.rb", "a")
	write("ignored.txt", "x")
	h1, count, err := hashRubyTree(dir)
	if err != nil || count != 2 {
		t.Fatalf("hash: %s %d %v", h1, count, err)
	}
	write("ignored.txt", "changed")
	h2, _, err := hashRubyTree(dir)
	if err != nil || h1 != h2 {
		t.Fatalf("hash changed for non-Ruby input: %s %s %v", h1, h2, err)
	}
	write("a.rb", "changed")
	h3, _, err := hashRubyTree(dir)
	if err != nil || h1 == h3 {
		t.Fatalf("hash unchanged for changed Ruby input: %s %s %v", h1, h3, err)
	}
}

func TestHashFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifact")
	content := []byte{0, 1, 2, 255}
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("%x", sha256.Sum256(content))
	hash, size, err := hashFile(path)
	if err != nil || hash != want || size != int64(len(content)) {
		t.Fatalf("hashFile = %q, %d, %v; want %q, %d, nil", hash, size, err, want, len(content))
	}
	if _, _, err := hashFile(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("hashFile should report a missing artifact")
	}
}
