package rack_test

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/gem/io"
	_ "github.com/oruby/oruby/gem/load"
	_ "github.com/oruby/oruby/gem/rack"
	_ "github.com/oruby/oruby/gem/stdlib"
)

func TestORubyRackAdapter(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	portVal, err := mrb.Eval(`
		require "oruby/rack"

		app = lambda do |env|
			case env["PATH_INFO"]
			when "/echo"
				body = env["rack.input"].read
				[
					201,
					{
						"content-type" => "text/plain",
						"x-method" => env["REQUEST_METHOD"],
						"x-query" => env["QUERY_STRING"],
						"x-custom" => env["HTTP_X_CUSTOM"].to_s,
						"set-cookie" => ["a=1", "b=2"],
					},
					["echo:", body]
				]
			when "/stream"
				streamer = lambda do |stream|
					stream.write("chunk1-")
					stream.write("chunk2")
				end
				[200, { "content-type" => "text/plain" }, streamer]
			else
				closed = false
				enum_body = Object.new
				enum_body.define_singleton_method(:each) { |&b| b.call("hello rack") }
				enum_body.define_singleton_method(:close) { closed = true }
				[200, { "content-type" => "text/plain", "x-multi" => "v1\nv2" }, enum_body]
			end
		end

		$rack_srv = ORuby::Rack::Server.new(app, host: "127.0.0.1", port: 0)
		$rack_srv.start
		$rack_srv.port
	`)
	if err != nil {
		t.Fatalf("failed to start ORuby::Rack::Server: %v", err)
	}
	defer func() {
		_, _ = mrb.Eval(`$rack_srv.stop`)
	}()

	port := portVal.Int()
	base := fmt.Sprintf("http://127.0.0.1:%d", port)

	// 1. POST /echo with query, custom header, body, and Rack 3 multi-value array Set-Cookie
	req, _ := http.NewRequest("POST", base+"/echo?foo=bar", strings.NewReader("payload123"))
	req.Header.Set("X-Custom", "oruby-rocks")
	req.Header.Set("Content-Type", "text/plain")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /echo failed: %v", err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	if string(b) != "echo:payload123" {
		t.Fatalf("body mismatch: %q", string(b))
	}
	if resp.Header.Get("X-Method") != "POST" || resp.Header.Get("X-Query") != "foo=bar" || resp.Header.Get("X-Custom") != "oruby-rocks" {
		t.Fatalf("header mismatch: %+v", resp.Header)
	}
	cookies := resp.Header.Values("Set-Cookie")
	if len(cookies) != 2 || cookies[0] != "a=1" || cookies[1] != "b=2" {
		t.Fatalf("Set-Cookie mismatch: %+v", cookies)
	}

	// 2. GET /stream (Rack 3 callable streaming body)
	resp2, err := http.Get(base + "/stream")
	if err != nil {
		t.Fatalf("GET /stream failed: %v", err)
	}
	b2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if string(b2) != "chunk1-chunk2" {
		t.Fatalf("stream body mismatch: %q", string(b2))
	}

	// 3. GET / (Enumerable body + Rack 2 newline multi-header)
	resp3, err := http.Get(base + "/")
	if err != nil {
		t.Fatalf("GET / failed: %v", err)
	}
	b3, _ := io.ReadAll(resp3.Body)
	resp3.Body.Close()
	if string(b3) != "hello rack" {
		t.Fatalf("root body mismatch: %q", string(b3))
	}
	if vals := resp3.Header.Values("X-Multi"); len(vals) != 2 || vals[0] != "v1" || vals[1] != "v2" {
		t.Fatalf("X-Multi mismatch: %+v", vals)
	}
}
