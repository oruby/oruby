package load

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/oruby/oruby"
)

func Test_fullName(t *testing.T) {
	//
}

func Test_require(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

}

func openLoadTestState(t *testing.T) *oruby.MrbState {
	t.Helper()
	mrb := oruby.MrbOpen()
	t.Cleanup(mrb.Close)
	if _, err := mrb.Resolve("load"); err != nil {
		t.Fatal(err)
	}
	return mrb
}

func writeAutoloadFile(t *testing.T, name, source string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAutoloadIsDeferredAndModuleScoped(t *testing.T) {
	mrb := openLoadTestState(t)
	alphaPath := writeAutoloadFile(t, "alpha.rb", `AutoloadAlpha::VALUE = "alpha"; $autoload_alpha_count = ($autoload_alpha_count || 0) + 1`)
	betaPath := writeAutoloadFile(t, "beta.rb", `AutoloadBeta::VALUE = "beta"; $autoload_beta_count = ($autoload_beta_count || 0) + 1`)
	globalPath := writeAutoloadFile(t, "global.rb", `TopAutoloadFixture = "global"; $autoload_global_count = ($autoload_global_count || 0) + 1`)
	setup := "module AutoloadAlpha; autoload(:VALUE, " + strconv.Quote(alphaPath) + "); end; " +
		"module AutoloadBeta; autoload(\"VALUE\", " + strconv.Quote(betaPath) + "); end; " +
		"autoload(:TopAutoloadFixture, " + strconv.Quote(globalPath) + ")"
	if _, err := mrb.Eval(setup); err != nil {
		t.Fatalf("register autoloads: %v", err)
	}
	for _, global := range []string{"$autoload_alpha_count", "$autoload_beta_count", "$autoload_global_count"} {
		got, err := mrb.Eval(global)
		if err != nil || !got.IsNil() {
			t.Fatalf("%s before lookup = %v, %v; want nil", global, got, err)
		}
	}
	for _, tc := range []struct {
		code string
		want string
	}{
		{"AutoloadAlpha.autoload?(:VALUE)", alphaPath},
		{`AutoloadBeta.autoload?("VALUE")`, betaPath},
	} {
		got, err := mrb.Eval(tc.code)
		if err != nil || mrb.String(got) != tc.want {
			t.Fatalf("%s = %q, %v; want %q", tc.code, mrb.String(got), err, tc.want)
		}
	}
	for _, tc := range []struct {
		code string
		want string
	}{
		{"AutoloadAlpha::VALUE", "alpha"},
		{"AutoloadBeta::VALUE", "beta"},
		{"AutoloadAlpha::VALUE", "alpha"},
		{"TopAutoloadFixture", "global"},
	} {
		got, err := mrb.Eval(tc.code)
		if err != nil || mrb.String(got) != tc.want {
			t.Fatalf("%s = %q, %v; want %q", tc.code, mrb.String(got), err, tc.want)
		}
	}
	for _, global := range []string{"$autoload_alpha_count", "$autoload_beta_count", "$autoload_global_count"} {
		got, err := mrb.Eval(global)
		if err != nil || got.Int() != 1 {
			t.Fatalf("%s after lookup = %v, %v; want 1", global, got, err)
		}
	}
	for _, code := range []string{"AutoloadAlpha.autoload?(:VALUE)", `AutoloadBeta.autoload?("VALUE")`} {
		got, err := mrb.Eval(code)
		if err != nil || !got.IsNil() {
			t.Fatalf("%s after loading = %v, %v; want nil", code, got, err)
		}
	}
}

func TestAutoloadFailureCanBeRetried(t *testing.T) {
	mrb := openLoadTestState(t)
	path := filepath.Join(t.TempDir(), "retry.rb")
	if err := os.WriteFile(path, []byte(`raise "autoload failed"`), 0600); err != nil {
		t.Fatal(err)
	}
	setup := "module RetryAutoload; autoload(:VALUE, " + strconv.Quote(path) + "); end"
	if _, err := mrb.Eval(setup); err != nil {
		t.Fatalf("register autoload: %v", err)
	}
	if _, err := mrb.Eval("RetryAutoload::VALUE"); err == nil {
		t.Fatal("first constant lookup should fail")
	}
	if got, err := mrb.Eval("RetryAutoload.autoload?(:VALUE)"); err != nil || got.IsNil() {
		t.Fatalf("autoload? after failed load = %v, %v; want retained path", got, err)
	}
	if err := os.WriteFile(path, []byte(`RetryAutoload::VALUE = 73`), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := mrb.Eval("RetryAutoload::VALUE")
	if err != nil || got.Int() != 73 {
		t.Fatalf("retried constant lookup = %v, %v; want 73", got, err)
	}
}

func TestAutoloadMissingFileCanBeAddedAndRetried(t *testing.T) {
	mrb := openLoadTestState(t)
	path := filepath.Join(t.TempDir(), "later.rb")
	setup := "module MissingAutoload; autoload(\"VALUE\", " + strconv.Quote(path) + "); end"
	if _, err := mrb.Eval(setup); err != nil {
		t.Fatalf("register missing-file autoload: %v", err)
	}
	if _, err := mrb.Eval("MissingAutoload::VALUE"); err == nil {
		t.Fatal("lookup with missing file should fail")
	}
	if err := os.WriteFile(path, []byte(`MissingAutoload::VALUE = 91`), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := mrb.Eval("MissingAutoload::VALUE")
	if err != nil || got.Int() != 91 {
		t.Fatalf("lookup after adding file = %v, %v; want 91", got, err)
	}
}

func TestAutoloadRecursiveLookupFailsAndCanBeRetried(t *testing.T) {
	mrb := openLoadTestState(t)
	path := writeAutoloadFile(t, "recursive.rb", `RecursiveAutoload::VALUE`)
	setup := "module RecursiveAutoload; autoload(:VALUE, " + strconv.Quote(path) + "); end"
	if _, err := mrb.Eval(setup); err != nil {
		t.Fatalf("register recursive autoload: %v", err)
	}
	if _, err := mrb.Eval("RecursiveAutoload::VALUE"); err == nil {
		t.Fatal("recursive lookup should fail instead of recursing indefinitely")
	}
	if err := os.WriteFile(path, []byte(`RecursiveAutoload::VALUE = 17`), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := mrb.Eval("RecursiveAutoload::VALUE")
	if err != nil || got.Int() != 17 {
		t.Fatalf("lookup after recursive failure = %v, %v; want 17", got, err)
	}
}
