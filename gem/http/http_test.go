package http

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oruby/oruby"
)

func TestHTTPServer_Basic(t *testing.T) {
	mrb, err := oruby.New()
	if err != nil {
		t.Fatalf("failed to create mrb: %v", err)
	}
	defer mrb.Close()

	script := `
		$server = HTTP::Server.new("127.0.0.1:0")
		$server.handle("/hello") do |req|
			"Hello from ORuby #{req.path}!"
		end

		$server.handle("/rack") do |req|
			[201, {"X-Custom" => "ORubyHeader"}, ["Rack Chunk 1, ", "Chunk 2"]]
		end

		$server.handle("/echo") do |req|
			HTTP::Response.new(200, {"Content-Type" => "text/plain"}, "Echo: #{req.body}")
		end

		$server.start_async
		$server.port
	`

	portVal, err := mrb.Eval(script)
	if err != nil {
		t.Fatalf("failed to start server in ruby: %v", err)
	}

	port := portVal.Int()
	if port <= 0 {
		t.Fatalf("expected valid port, got %d", port)
	}

	// Give server a moment to start
	time.Sleep(50 * time.Millisecond)

	// Test 1: /hello
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/hello", port))
	if err != nil {
		t.Fatalf("GET /hello failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if string(body) != "Hello from ORuby /hello!" {
		t.Errorf("unexpected body: %q", string(body))
	}

	// Test 2: /rack
	resp, err = http.Get(fmt.Sprintf("http://127.0.0.1:%d/rack", port))
	if err != nil {
		t.Fatalf("GET /rack failed: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Errorf("expected 201, got %d", resp.StatusCode)
	}
	if resp.Header.Get("X-Custom") != "ORubyHeader" {
		t.Errorf("expected header X-Custom: ORubyHeader, got %q", resp.Header.Get("X-Custom"))
	}
	if string(body) != "Rack Chunk 1, Chunk 2" {
		t.Errorf("unexpected rack body: %q", string(body))
	}

	// Test 3: Stop server
	_, err = mrb.Eval(`$server.stop`)
	if err != nil {
		t.Errorf("failed to stop server: %v", err)
	}
}

func TestHTTPServer_MountStatic(t *testing.T) {
	mrb, err := oruby.New()
	if err != nil {
		t.Fatalf("failed to create mrb: %v", err)
	}
	defer mrb.Close()

	tmpDir, err := os.MkdirTemp("", "oruby-http-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	testFilePath := filepath.Join(tmpDir, "test.txt")
	_ = os.WriteFile(testFilePath, []byte("Static file content"), 0644)

	script := fmt.Sprintf(`
		$static_server = HTTP::Server.new("127.0.0.1:0")
		$static_server.mount("/static/", %q)
		$static_server.start_async
		$static_server.port
	`, tmpDir)

	portVal, err := mrb.Eval(script)
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	port := portVal.Int()
	time.Sleep(50 * time.Millisecond)

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/static/test.txt", port))
	if err != nil {
		t.Fatalf("GET static file failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if string(body) != "Static file content" {
		t.Errorf("unexpected body: %q", string(body))
	}

	_, _ = mrb.Eval(`$static_server.stop`)
}

func TestHTTPClient(t *testing.T) {
	mrb, err := oruby.New()
	if err != nil {
		t.Fatalf("failed to create mrb: %v", err)
	}
	defer mrb.Close()

	script := `
		$srv = HTTP::Server.new("127.0.0.1:0")
		$srv.handle("/api/data") do |req|
			"Received #{req.method}: #{req.body}"
		end
		$srv.start_async
		$srv.port
	`

	portVal, err := mrb.Eval(script)
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	port := portVal.Int()
	time.Sleep(50 * time.Millisecond)

	clientScript := fmt.Sprintf(`
		res1 = HTTP.get("http://127.0.0.1:%d/api/data")
		res2 = HTTP.post("http://127.0.0.1:%d/api/data", "Hello Client", "text/plain")
		[res1.status, res1.body, res2.status, res2.body]
	`, port, port)

	res, err := mrb.Eval(clientScript)
	if err != nil {
		t.Fatalf("client eval error: %v", err)
	}

	s1 := mrb.AryRef(res, 0).Int()
	b1 := mrb.AryRef(res, 1).String()
	s2 := mrb.AryRef(res, 2).Int()
	b2 := mrb.AryRef(res, 3).String()

	if s1 != 200 || b1 != "Received GET: " {
		t.Errorf("unexpected GET response: %d, %q", s1, b1)
	}
	if s2 != 200 || b2 != "Received POST: Hello Client" {
		t.Errorf("unexpected POST response: %d, %q", s2, b2)
	}

	_, _ = mrb.Eval(`$srv.stop`)
}
