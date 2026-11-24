package socket_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/gem/io"
	_ "github.com/oruby/oruby/gem/load"
	_ "github.com/oruby/oruby/gem/socket"
	_ "github.com/oruby/oruby/gem/thread"
)

func TestSocketTCPAndUDPAndAddrinfo(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	_, err := mrb.Eval(`
require "socket"

# Addrinfo & hostname
ai = Addrinfo.tcp("127.0.0.1", 8080)
raise "ip_address" unless ai.ip_address == "127.0.0.1"
raise "ip_port" unless ai.ip_port == 8080
raise "ipv4?" unless ai.ipv4? && ai.ip?
raise "hostname" unless Socket.gethostname.is_a?(String) && !Socket.gethostname.empty?

# Socket.pair
s1, s2 = Socket.pair(:UNIX, :STREAM, 0)
begin
  s1.write("ping")
  got = s2.read(4)
  raise "pair read: #{got.inspect}" unless got == "ping"
ensure
  s1.close
  s2.close
end

# TCPServer / TCPSocket loopback
srv = TCPServer.new("127.0.0.1", 0)
port = srv.addr[1]
t = Thread.new do
  c = srv.accept
  msg = c.gets
  c.puts("echo:#{msg}")
  c.close
end
cli = TCPSocket.new("127.0.0.1", port)
cli.puts("hello")
resp = cli.gets
cli.close
t.join
srv.close
raise "tcp echo: #{resp.inspect}" unless resp == "echo:hello\n"

# UDPSocket loopback
u1 = UDPSocket.new
u1.bind("127.0.0.1", 0)
uport = u1.addr[1]
u2 = UDPSocket.new
u2.send("udp-ok", 0, "127.0.0.1", uport)
data, _sender = u1.recvfrom(64)
u1.close
u2.close
raise "udp recv: #{data.inspect}" unless data == "udp-ok"
`)
	if err != nil {
		t.Fatalf("Socket test failed: %v", err)
	}
}
