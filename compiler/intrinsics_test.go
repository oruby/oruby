package compiler_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/compiler"
)

func TestGoExtIntrinsicsCodegenAndExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles and runs an AOT Go binary with goext intrinsics")
	}
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	goExts, err := compiler.ResolveGoExtList([]string{"fast_blank", "escape_utils", "xxhash", "zstd", "brotli"})
	if err != nil {
		t.Fatal(err)
	}

	rubySrc := `
require "fast_blank"
require "escape_utils"
require "xxhash"
require "zstd-ruby"
require "brotli"

# 1. fast_blank (warm + hot loop, embedded & heap strings, unicode whitespace)
s_empty = ""
s_ws = "  \t\n\u3000  "
s_long_ws = " " * 128
s_non = "  hello  "
raise "blank empty" unless s_empty.blank? && s_empty.blank_as?
raise "blank ws" unless s_ws.blank? && s_ws.blank_as?
raise "blank long" unless s_long_ws.blank?
raise "non-blank" if s_non.blank? || s_non.blank_as?

# Singleton override must bypass intrinsic
s_over = "   "
s_over.define_singleton_method(:blank?) { :singleton_ok }
raise "singleton override failed" unless s_over.blank? == :singleton_ok

# Subclass override must bypass intrinsic
class SubStr < String
  def blank?; :subclass_ok; end
end
raise "subclass override failed" unless SubStr.new("   ").blank? == :subclass_ok

# 2. escape_utils (respects html_secure toggling)
raise "escape_html secure" unless EscapeUtils.escape_html("<a href='/'>&\"'</a>") == "&lt;a href=&#39;&#47;&#39;&gt;&amp;&quot;&#39;&lt;&#47;a&gt;"
EscapeUtils.html_secure = false
raise "escape_html insecure" unless EscapeUtils.escape_html("a/b") == "a/b"
EscapeUtils.html_secure = true
raise "escape_url" unless EscapeUtils.escape_url("a b+c") == "a+b%2Bc"

# 3. xxhash (1-arg and 2-arg, plus TypeError fallback)
h32_a = XXhash.xxh32("hello")
h32_b = XXhash.xxh32("hello", 42)
h64_a = XXhash.xxh64("hello")
h64_b = XXhash.xxh64("hello", 99)
raise "xxh32 seed diff" if h32_a == h32_b
raise "xxh64 seed diff" if h64_a == h64_b
# Verify intrinsic matches dynamic send (which bypasses intrinsic)
raise "xxh32 mismatch" unless h32_a == XXhash.send(:xxh32, "hello")
raise "xxh32 seed mismatch" unless h32_b == XXhash.send(:xxh32, "hello", 42)
raise "xxh64 mismatch" unless h64_a == XXhash.send(:xxh64, "hello")
raise "xxh64 seed mismatch" unless h64_b == XXhash.send(:xxh64, "hello", 99)
type_err = begin; XXhash.xxh32(123); false; rescue TypeError; true; end
raise "xxh32 TypeError fallback" unless type_err

# 4. zstd (roundtrip + binary encoding + error fallback)
plain = "ORuby AOT Zstd payload! " * 40
zc = Zstd.compress(plain)
raise "zstd encoding" unless zc.encoding == Encoding::BINARY
raise "zstd roundtrip" unless Zstd.decompress(zc) == plain
zerr = begin; Zstd.decompress("bad"); false; rescue RuntimeError; true; end
raise "zstd error fallback" unless zerr

# 5. brotli (roundtrip + binary encoding + error fallback)
bc = Brotli.deflate(plain)
raise "brotli encoding" unless bc.encoding == Encoding::BINARY
raise "brotli roundtrip" unless Brotli.inflate(bc) == plain
berr = begin; Brotli.inflate("\xff\xff\xff\xff".b); false; rescue Brotli::Error; true; end
raise "brotli error fallback" unless berr

# 6. Runtime monkey-patch invalidates mruby method cache -> falls back cleanly
class String
  def blank?; :patched_blank; end
end
raise "monkey-patch String#blank?" unless "   ".blank? == :patched_blank

module XXhash
  def self.xxh32(_s, _seed = 0); 777; end
end
raise "monkey-patch XXhash.xxh32" unless XXhash.xxh32("hello") == 777

puts "ALL_INTRINSICS_OK"
`

	work := t.TempDir()
	scriptPath := filepath.Join(work, "test.rb")
	if err := os.WriteFile(scriptPath, []byte(rubySrc), 0o644); err != nil {
		t.Fatal(err)
	}

	mrb := oruby.MrbOpen()
	defer mrb.Close()

	comp := compiler.New(compiler.Options{
		PackageName: "main",
		Standalone:  true,
		GoExts:      goExts,
	})
	mainDir := filepath.Join(work, "main")
	if _, err := comp.CompilePackageDir(mrb, scriptPath, nil, nil, mainDir); err != nil {
		t.Fatalf("CompilePackageDir: %v", err)
	}

	// Verify intrinsic calls were actually emitted in aot_mod_000.go
	mod0, err := os.ReadFile(filepath.Join(mainDir, "aot_mod_000.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"goextfastblank.IsBlankBytes(",
		"goextescapeutils.EscapeHTMLBytes(",
		"goextescapeutils.EscapeURLBytes(",
		"goextxxhash.XXH32(",
		"goextxxhash.XXH64(",
		"goextzstd.Compress(",
		"goextzstd.Decompress(",
		"goextbrotli.Deflate(",
		"goextbrotli.Inflate(",
	} {
		if !strings.Contains(string(mod0), want) {
			t.Errorf("aot_mod_000.go missing intrinsic call %q", want)
		}
	}

	// Write go.mod + merged go.sum and build + execute the compiled binary
	modText := fmt.Sprintf("module aotintrinsictest\n\ngo 1.27.0\n\nrequire github.com/oruby/oruby v0.0.0\nrequire %s v0.0.0\n\nreplace github.com/oruby/oruby => %s\nreplace %s => %s\n",
		compiler.GoExtModule, root, compiler.GoExtModule, filepath.Join(root, "goext"))
	if err := os.WriteFile(filepath.Join(work, "go.mod"), []byte(modText), 0o644); err != nil {
		t.Fatal(err)
	}
	var sumBuf strings.Builder
	for _, sf := range []string{filepath.Join(root, "go.sum"), filepath.Join(root, "goext", "go.sum")} {
		if data, err := os.ReadFile(sf); err == nil {
			sumBuf.Write(data)
			sumBuf.WriteByte('\n')
		}
	}
	if err := os.WriteFile(filepath.Join(work, "go.sum"), []byte(sumBuf.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	bin := filepath.Join(work, "aot_bin")
	build := exec.Command("go", "build", "-o", bin, "./main")
	build.Dir = work
	build.Env = append(os.Environ(), "CGO_ENABLED=1", "GOFLAGS=-mod=mod", "GOWORK=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}

	run := exec.Command(bin)
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("compiled binary failed: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "ALL_INTRINSICS_OK" {
		t.Fatalf("output = %q, want ALL_INTRINSICS_OK", got)
	}
}
