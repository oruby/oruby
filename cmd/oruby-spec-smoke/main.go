// oruby-spec-smoke runs a small set of real ruby/spec files against ORuby.
package main

import (
	"context"
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type tally struct {
	files, examples, expectations, failures, errors, tagged int
}

var tallyPattern = regexp.MustCompile(`(?m)^\s*(\d+) files?,\s*(\d+) examples?,\s*(\d+) expectations?,\s*(\d+) failures?,\s*(\d+) errors?,\s*(\d+) tagged\s*$`)

func parseTally(output string) (tally, bool) {
	matches := tallyPattern.FindAllStringSubmatch(output, -1)
	if len(matches) == 0 {
		return tally{}, false
	}
	m := matches[len(matches)-1]
	var n [6]int
	for i := range n {
		v, err := strconv.Atoi(m[i+1])
		if err != nil {
			return tally{}, false
		}
		n[i] = v
	}
	return tally{n[0], n[1], n[2], n[3], n[4], n[5]}, true
}

type result struct {
	output   string
	exitCode int // -1 when no exit status is available
	timedOut bool
	err      error
}

func runCommand(dir string, env []string, timeout time.Duration, name string, args ...string) result {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	configureProcess(cmd)
	cmd.WaitDelay = 3 * time.Second
	out, err := cmd.CombinedOutput()
	r := result{output: string(out), exitCode: -1, err: err, timedOut: ctx.Err() == context.DeadlineExceeded}
	if cmd.ProcessState != nil {
		r.exitCode = cmd.ProcessState.ExitCode()
	}
	return r
}

func reportCommand(label, dir string, env []string, timeout time.Duration, name string, args ...string) result {
	quoted := make([]string, 0, len(args)+1)
	for _, arg := range append([]string{name}, args...) {
		quoted = append(quoted, strconv.Quote(arg))
	}
	fmt.Printf("\n[%s] cwd=%s\ncommand: %s\n", label, dir, strings.Join(quoted, " "))
	r := runCommand(dir, env, timeout, name, args...)
	if r.output != "" {
		fmt.Print(r.output)
		if !strings.HasSuffix(r.output, "\n") {
			fmt.Println()
		}
	}
	fmt.Printf("exit=%d timeout=%t", r.exitCode, r.timedOut)
	if r.err != nil {
		fmt.Printf(" error=%v", r.err)
	}
	fmt.Println()
	return r
}

func reportTally(r result) bool {
	t, ok := parseTally(r.output)
	if !ok {
		fmt.Println("tally: unknown (no complete mspec tally)")
		return false
	}
	fmt.Printf("tally: files=%d examples=%d expectations=%d failures=%d errors=%d tagged=%d\n", t.files, t.examples, t.expectations, t.failures, t.errors, t.tagged)
	return r.err == nil && !r.timedOut && r.exitCode == 0 && t.files > 0 && t.examples > 0 && t.failures == 0 && t.errors == 0
}

func skippedDependencies(output string) bool {
	return strings.Contains(output, "warning: skipped:")
}

// hashRubyTree hashes sorted relative paths and file content hashes, including untracked files.
func hashRubyTree(root string) (string, int, error) {
	h := sha256.New()
	count := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".rb" {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("non-regular Ruby file: %s", path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		fileHash := sha256.New()
		_, copyErr := io.Copy(fileHash, f)
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		fmt.Fprintf(h, "%x  %s\n", fileHash.Sum(nil), filepath.ToSlash(rel))
		count++
		return nil
	})
	if err != nil {
		return "", count, err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), count, nil
}

func reportIdentity(name, root string) error {
	hash, count, err := hashRubyTree(root)
	if err != nil {
		return fmt.Errorf("hash %s: %w", name, err)
	}
	fmt.Printf("%s Ruby manifest sha256=%s files=%d root=%s\n", name, hash, count, root)
	top := runCommand(root, nil, 10*time.Second, "git", "rev-parse", "--show-toplevel")
	if top.err != nil || filepath.Clean(strings.TrimSpace(top.output)) != filepath.Clean(root) {
		fmt.Printf("%s git revision=unknown (not a checkout root)\n", name)
		return nil
	}
	rev := runCommand(root, nil, 10*time.Second, "git", "rev-parse", "HEAD")
	status := runCommand(root, nil, 10*time.Second, "git", "status", "--porcelain", "--untracked-files=all")
	if rev.err != nil {
		fmt.Printf("%s git revision=unknown (%v)\n", name, rev.err)
	} else {
		fmt.Printf("%s git revision=%s\n", name, strings.TrimSpace(rev.output))
	}
	if status.err != nil {
		fmt.Printf("%s git status=unknown (%v)\n", name, status.err)
	} else if status.output == "" {
		fmt.Printf("%s git status=clean\n", name)
	} else {
		fmt.Printf("%s git status=dirty:\n%s", name, status.output)
	}
	return nil
}

func projectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go-mrb.h")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("run from the ORuby repository")
		}
		dir = parent
	}
}

func main() { os.Exit(run()) }

func run() int {
	suiteFlag := flag.String("suite", "../oruby_test/spec", "directory containing ruby/ and mspec/")
	timeout := flag.Duration("timeout", 60*time.Second, "timeout for each spec execution")
	buildTimeout := flag.Duration("build-timeout", 3*time.Minute, "timeout for each build")
	flag.Parse()
	if *timeout <= 0 || *buildTimeout <= 0 {
		fmt.Fprintln(os.Stderr, "timeouts must be positive")
		return 2
	}
	root, err := projectRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	suite, err := filepath.Abs(*suiteFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	rubyRoot, mspecRoot := filepath.Join(suite, "ruby"), filepath.Join(suite, "mspec")
	mspec := filepath.Join(mspecRoot, "bin", "mspec")
	if s, err := os.Stat(mspec); err != nil || !s.Mode().IsRegular() {
		fmt.Fprintf(os.Stderr, "missing mspec launcher: %s\n", mspec)
		return 2
	}
	for _, script := range []string{"mspec", "mspec-run"} {
		path := filepath.Join(mspecRoot, "bin", script)
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read %s: %v\n", path, err)
			return 2
		}
		fmt.Printf("mspec/bin/%s sha256=%x\n", script, sha256.Sum256(data))
	}
	samples := flag.Args()
	if len(samples) == 0 {
		samples = []string{"library/base64/strict_encode64_spec.rb", "language/while_spec.rb"}
		yaml := "library/yaml/dump_spec.rb"
		if _, err := os.Stat(filepath.Join(rubyRoot, yaml)); err == nil {
			samples = append(samples, yaml)
		} else if os.IsNotExist(err) {
			fmt.Printf("optional sample missing: %s\n", yaml)
		} else {
			fmt.Fprintf(os.Stderr, "check optional sample %s: %v\n", yaml, err)
			return 2
		}
	}
	for _, sample := range samples {
		clean := filepath.Clean(sample)
		if filepath.IsAbs(sample) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			fmt.Fprintf(os.Stderr, "sample must be relative to ruby/: %s\n", sample)
			return 2
		}
		if s, err := os.Stat(filepath.Join(rubyRoot, clean)); err != nil || !s.Mode().IsRegular() {
			fmt.Fprintf(os.Stderr, "missing spec: %s\n", sample)
			return 2
		}
	}
	for _, entry := range []struct{ name, root string }{{"ruby/spec", rubyRoot}, {"mspec", mspecRoot}} {
		if err := reportIdentity(entry.name, entry.root); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	}
	tmp, err := os.MkdirTemp(filepath.Join(root, "cmd", "oruby-spec-smoke"), ".run-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer os.RemoveAll(tmp)
	env := []string{"TMPDIR=" + tmp, "ORUBY_PATH=" + root}
	for _, tool := range []struct{ name, versionArg string }{{"go", "version"}, {"ruby", "--version"}} {
		r := reportCommand("tool version", tmp, env, 10*time.Second, tool.name, tool.versionArg)
		if r.err != nil || r.timedOut || r.exitCode != 0 {
			return 2
		}
	}
	oruby, compiler := filepath.Join(tmp, "oruby"), filepath.Join(tmp, "oruby-compiler")
	for _, b := range []struct{ binary, pkg string }{{oruby, "./cmd/oruby"}, {compiler, "./cmd/oruby-compiler"}} {
		if r := reportCommand("build", root, env, *buildTimeout, "go", "build", "-o", b.binary, b.pkg); r.err != nil || r.timedOut || r.exitCode != 0 {
			return 1
		}
	}
	env = append(env, "RUBY_EXE="+oruby)
	fmt.Printf("RUBY_EXE=%s ORUBY_PATH=%s TMPDIR=%s\n", oruby, root, tmp)
	passed := true
	for i, sample := range samples {
		path := filepath.Join(rubyRoot, sample)
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read %s: %v\n", path, err)
			passed = false
			continue
		}
		fmt.Printf("\n=== %s sha256=%x ===\n", sample, sha256.Sum256(data))
		r := reportCommand("mspec interpreter", tmp, env, *timeout, "ruby", mspec, "-t", oruby, path)
		if !reportTally(r) {
			passed = false
		}
		bin := filepath.Join(tmp, fmt.Sprintf("sample-%d", i))
		r = reportCommand("AOT compile", tmp, env, *buildTimeout, compiler, "-build", bin, "-bundle", path)
		if r.err != nil || r.timedOut || r.exitCode != 0 {
			fmt.Println("AOT execution: not run (compile failed); tally: unknown")
			passed = false
			continue
		}
		if skippedDependencies(r.output) {
			fmt.Println("AOT bundle incomplete: skipped dependencies; result cannot pass")
			passed = false
		}
		r = reportCommand("AOT entry execution (not mspec launcher)", tmp, env, *timeout, bin)
		if !reportTally(r) {
			passed = false
		}
	}
	if !passed {
		fmt.Println("\nresult: FAIL or UNKNOWN")
		return 1
	}
	fmt.Println("\nresult: PASS")
	return 0
}
