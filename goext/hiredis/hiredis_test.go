package hiredis_test

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/goext/hiredis"
	_ "github.com/oruby/oruby/gem/load"
)

func TestHiredisReaderRESP2AndRESP3(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()
	_, err := mrb.Eval(`
require "hiredis"

r = Hiredis::Reader.new
raise "empty gets" unless r.gets == false

# Partial feed then complete
r.feed("+PO")
raise "partial" unless r.gets == false
r.feed("NG\r\n")
raise "simple string" unless r.gets == "PONG"

# Integer, bulk string, nil bulk, error, nested array
r.feed(":42\r\n$5\r\nhello\r\n$-1\r\n-ERR boom\r\n*2\r\n$3\r\nfoo\r\n:7\r\n")
raise "int" unless r.gets == 42
raise "bulk" unless r.gets == "hello"
raise "nil bulk" unless r.gets.nil?
err = r.gets
raise "error reply" unless err.is_a?(RuntimeError) && err.message == "ERR boom"
raise "array" unless r.gets == ["foo", 7]

# RESP3 types: null, bool, double, map, set, verbatim
r.feed("_\r\n#t\r\n#f\r\n,3.14\r\n%2\r\n+a\r\n:1\r\n+b\r\n:2\r\n~2\r\n+x\r\n+y\r\n=9\r\ntxt:hello\r\n")
raise "resp3 null" unless r.gets.nil?
raise "resp3 true" unless r.gets == true
raise "resp3 false" unless r.gets == false
raise "resp3 double" unless (r.gets - 3.14).abs < 1e-9
raise "resp3 map" unless r.gets == { "a" => 1, "b" => 2 }
raise "resp3 set" unless r.gets == ["x", "y"]
raise "resp3 verbatim" unless r.gets == "hello"
raise "drained" unless r.gets == false && !r.has_data?
`)
	if err != nil {
		t.Fatalf("hiredis reader test failed: %v", err)
	}
}

func TestHiredisConnectionPipeline(t *testing.T) {
	// Minimal mock Redis TCP server supporting PING, ECHO, SET, GET
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		br := bufio.NewReader(conn)
		kv := map[string]string{}
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "*") {
				continue
			}
			var n int
			fmt.Sscanf(line[1:], "%d", &n)
			args := make([]string, n)
			for i := 0; i < n; i++ {
				lenLine, err := br.ReadString('\n')
				if err != nil {
					return
				}
				var blen int
				fmt.Sscanf(strings.TrimSpace(lenLine)[1:], "%d", &blen)
				buf := make([]byte, blen+2)
				if _, err := br.Read(buf); err != nil {
					return
				}
				args[i] = string(buf[:blen])
			}
			cmd := strings.ToUpper(args[0])
			switch cmd {
			case "PING":
				_, _ = conn.Write([]byte("+PONG\r\n"))
			case "SET":
				kv[args[1]] = args[2]
				_, _ = conn.Write([]byte("+OK\r\n"))
			case "GET":
				if v, ok := kv[args[1]]; ok {
					_, _ = fmt.Fprintf(conn, "$%d\r\n%s\r\n", len(v), v)
				} else {
					_, _ = conn.Write([]byte("$-1\r\n"))
				}
			default:
				_, _ = fmt.Fprintf(conn, "-ERR unknown command %s\r\n", cmd)
			}
		}
	}()

	mrb := oruby.MrbOpen()
	defer mrb.Close()
	_, err = mrb.Eval(fmt.Sprintf(`
require "hiredis/connection"

c = Hiredis::Connection.new
raise "not connected yet" if c.connected?
c.connect("127.0.0.1", %d, 2_000_000)
raise "connected" unless c.connected?

# Pipelined commands
c.write(["PING"])
c.write(["SET", "k1", "val1"])
c.write(["GET", "k1"])
c.write(["GET", "missing"])
c.write(["NOPE"])

raise "ping" unless c.read == "PONG"
raise "set" unless c.read == "OK"
raise "get" unless c.read == "val1"
raise "missing" unless c.read.nil?
e = c.read
raise "err reply" unless e.is_a?(RuntimeError) && e.message.include?("unknown command")

c.disconnect
raise "disconnected" if c.connected?
`, port))
	if err != nil {
		t.Fatalf("hiredis connection test failed: %v", err)
	}
}
