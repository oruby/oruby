package puma_http11_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/gem/io"
	_ "github.com/oruby/oruby/gem/load"
	_ "github.com/oruby/oruby/gem/nio4r"
	_ "github.com/oruby/oruby/gem/puma_http11"
	_ "github.com/oruby/oruby/gem/socket"
	_ "github.com/oruby/oruby/gem/stdlib"
)

func TestPumaHttpParserExactParity(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	_, err := mrb.Eval(`
		require "puma/puma_http11"

		hp = Puma::HttpParser.new
		env = {}
		raw = "GET /users/42?a=1&b=2#frag HTTP/1.1\r\nHost: example.com:3000\r\nX-Test: val1\r\nX-Test: val2\r\nContent-Type: text/plain\r\nContent-Length: 5\r\n\r\nhello"
		n = hp.execute(env, raw, 0)

		raise "nread: #{n}" unless n == 136 && hp.nread == 136
		raise "finished?" unless hp.finished?
		raise "error?" if hp.error?
		raise "body: #{hp.body.inspect}" unless hp.body == "hello"

		expected = {
			"REQUEST_METHOD" => "GET",
			"REQUEST_PATH" => "/users/42",
			"QUERY_STRING" => "a=1&b=2",
			"REQUEST_URI" => "/users/42?a=1&b=2",
			"FRAGMENT" => "frag",
			"SERVER_PROTOCOL" => "HTTP/1.1",
			"HTTP_HOST" => "example.com:3000",
			"HTTP_X_TEST" => "val1, val2",
			"CONTENT_TYPE" => "text/plain",
			"CONTENT_LENGTH" => "5",
		}
		raise "env mismatch:\ngot:  #{env.inspect}\nwant: #{expected.inspect}" unless env == expected

		# Reset & byte-by-byte streaming execute
		hp.reset
		raise "still finished after reset" if hp.finished?
		raise "nread after reset" unless hp.nread == 0

		env2 = {}
		full = "POST /api/items?x=1 HTTP/1.1\r\nHost: localhost\r\nContent-Length: 4\r\n\r\nbody"
		buf = +""
		nread = 0
		full.each_byte do |b|
			buf << b.chr
			nread = hp.execute(env2, buf, nread)
			break if hp.finished?
		end
		raise "stream finished" unless hp.finished?
		raise "stream nread=#{nread}" unless nread == 68
		raise "stream body=#{hp.body.inspect}" unless hp.body == ""
		raise "stream method" unless env2["REQUEST_METHOD"] == "POST"
		raise "stream path" unless env2["REQUEST_PATH"] == "/api/items"
		raise "stream query" unless env2["QUERY_STRING"] == "x=1"
		raise "stream host" unless env2["HTTP_HOST"] == "localhost"
		raise "stream cl" unless env2["CONTENT_LENGTH"] == "4"

		# Security / malformed inputs raise Puma::HttpParserError
		bad_inputs = [
			"GET / HTTP/1.1\nHost: x\n\n",                       # bare LF
			"GET / HTTP/1.1\r\nBad Header : val\r\n\r\n",        # space before colon
			"GET / BADPROTO\r\nHost: x\r\n\r\n",                 # invalid protocol
			"GET / SsUTF/1.1",                                   # partial invalid protocol
			"GET /path{here HTTP/1.1\r\n\r\n",                   # unescaped '{'
			"GET /path}here HTTP/1.1\r\n\r\n",                   # unescaped '}'
			"GET /path` + "`" + `here HTTP/1.1\r\n\r\n",                   # unescaped backtick
			"GET /path\\here HTTP/1.1\r\n\r\n",                  # unescaped backslash
			"GET / HTTP/1.1\r\nX-Test: bad\x01val\r\n\r\n",      # control char in header value
			"\x16\x03\x01\x00\x05hello".b,                       # TLS ClientHello
		]
		bad_inputs.each do |bad|
			p2 = Puma::HttpParser.new
			raised = false
			begin
				p2.execute({}, bad, 0)
			rescue Puma::HttpParserError
				raised = true
			end
			raise "expected HttpParserError for #{bad.inspect}" unless raised && p2.error?
		end

		# Absolute-form URI sets REQUEST_URI only (matching Puma's C parser)
		p3 = Puma::HttpParser.new
		env3 = {}
		abs = "GET http://192.168.1.96:3000/api/v1/matches/test?1=1 HTTP/1.1\r\n\r\n"
		p3.execute(env3, abs, 0)
		raise "abs finished" unless p3.finished?
		raise "abs uri" unless env3["REQUEST_URI"] == "http://192.168.1.96:3000/api/v1/matches/test?1=1"
		raise "abs path must be nil" unless env3["REQUEST_PATH"].nil? && env3["QUERY_STRING"].nil? && env3["FRAGMENT"].nil?
	`)
	if err != nil {
		t.Fatalf("Puma::HttpParser test failed: %v", err)
	}
}

func TestPumaReactorLoopWithNIO4R(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	_, err := mrb.Eval(`
		require "nio"
		require "socket"
		require "puma/puma_http11"

		# End-to-end Puma-style Reactor (NIO::Selector + Puma::HttpParser) over TCP!
		server = TCPServer.new("127.0.0.1", 0)
		port = server.addr[1]
		sel = NIO::Selector.new
		srv_mon = sel.register(server, :r)

		client = TCPSocket.new("127.0.0.1", port)
		client.write("POST /submit HTTP/1.1\r\nHost: 127.0.0.1\r\nContent-Length: 5\r\n\r\nworld")

		ready = sel.select(1.0)
		raise "accept ready" unless ready && ready.include?(srv_mon)
		conn = server.accept
		conn_mon = sel.register(conn, :r)

		hp = Puma::HttpParser.new
		env = {}
		buf = +""
		nread = 0

		until hp.finished?
			sel.select(1.0)
			chunk = conn.read_nonblock(4096)
			buf << chunk
			nread = hp.execute(env, buf, nread)
		end

		body = hp.body
		cl = env["CONTENT_LENGTH"].to_i
		while body.bytesize < cl
			sel.select(1.0)
			body << conn.read_nonblock(cl - body.bytesize)
		end

		raise "method" unless env["REQUEST_METHOD"] == "POST"
		raise "path" unless env["REQUEST_PATH"] == "/submit"
		raise "body" unless body == "world"

		conn.write("HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
		resp = client.read(40)
		raise "resp: #{resp.inspect}" unless resp.include?("200 OK") && resp.end_with?("ok")

		client.close
		conn.close
		server.close
		sel.close
	`)
	if err != nil {
		t.Fatalf("Puma Reactor + NIO4R test failed: %v", err)
	}
}
