package nio4r_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/gem/io"
	_ "github.com/oruby/oruby/gem/load"
	_ "github.com/oruby/oruby/gem/nio4r"
	_ "github.com/oruby/oruby/gem/socket"
	_ "github.com/oruby/oruby/gem/stdlib"
)

func TestNIOSelectorPipeAndWakeup(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	_, err := mrb.Eval(`
		require "nio"

		sel = NIO::Selector.new
		raise "not empty" unless sel.empty?
		raise "closed?" if sel.closed?
		raise "backends" unless NIO::Selector.backends.include?(sel.backend)

		r, w = IO.pipe
		mon = sel.register(r, :r)
		mon.value = "my-pipe"
		raise "registered?" unless sel.registered?(r)
		raise "empty?" if sel.empty?
		raise "interests" unless mon.interests == :r
		raise "value" unless mon.value == "my-pipe"

		# Timeout returns nil
		raise "expected nil on timeout" unless sel.select(0.01).nil?
		raise "expected nil on timeout with block" unless sel.select(0.01) { raise "should not yield" }.nil?

		# Wakeup returns [] (no block) or 0 (with block)
		sel.wakeup
		woken = sel.select(0.1)
		raise "wakeup should return [], got #{woken.inspect}" unless woken == []
		sel.wakeup
		wcount = sel.select(0.1) { raise "should not yield on wakeup" }
		raise "wakeup block should return 0, got #{wcount.inspect}" unless wcount == 0

		# Write triggers readability
		w.write("hello nio")
		ready = sel.select(0.5)
		raise "ready count" unless ready.length == 1 && ready[0].equal?(mon)
		raise "readable?" unless mon.readable? && mon.readiness == :r && !mon.writable?

		# Block form returns count
		yielded = nil
		cnt = sel.select(0.5) { |m| yielded = m }
		raise "block count" unless cnt == 1 && yielded.equal?(mon)
		raise "data" unless r.read(9) == "hello nio"

		# Interest mutations
		mon.add_interest(:w)
		raise "add_interest" unless mon.interests == :rw
		mon.remove_interest(:r)
		raise "remove_interest" unless mon.interests == :w

		# Deregister & close
		sel.deregister(r)
		raise "still registered" if sel.registered?(r)
		raise "mon not closed" unless mon.closed?
		raise "not empty" unless sel.empty?

		r.close
		w.close
		sel.close
		raise "not closed" unless sel.closed?
	`)
	if err != nil {
		t.Fatalf("NIO selector pipe test failed: %v", err)
	}
}

func TestNIOSelectorTCPAndByteBuffer(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	_, err := mrb.Eval(`
		require "nio"
		require "socket"

		server = TCPServer.new("127.0.0.1", 0)
		port = server.addr[1]

		sel = NIO::Selector.new
		srv_mon = sel.register(server, :r)

		client = TCPSocket.new("127.0.0.1", port)
		ready = sel.select(1.0)
		raise "server not readable for accept" unless ready && ready.include?(srv_mon)

		peer = server.accept
		peer_mon = sel.register(peer, :r)

		# Use ByteBuffer to send & receive over TCP
		wbuf = NIO::ByteBuffer.new(32)
		wbuf << "ping-nio4r"
		wbuf.flip
		written = wbuf.write_to(client)
		raise "write_to: #{written}" unless written == 10

		ready2 = sel.select(1.0)
		raise "peer not readable" unless ready2 && ready2.include?(peer_mon)

		rbuf = NIO::ByteBuffer.new(32)
		nread = rbuf.read_from(peer)
		raise "read_from: #{nread}" unless nread == 10
		rbuf.flip
		raise "get(4)" unless rbuf.get(4) == "ping"
		rbuf.mark
		raise "get(1)" unless rbuf.get(1) == "-"
		rbuf.reset
		raise "get rest" unless rbuf.get == "-nio4r"

		# Compact & Overflow/Underflow/MarkUnset errors
		bb = NIO::ByteBuffer.new(4)
		bb << "abcd"
		raise "full?" unless bb.full?
		raised_ov = false
		begin
			bb << "x"
		rescue NIO::ByteBuffer::OverflowError
			raised_ov = true
		end
		raise "expected OverflowError" unless raised_ov

		bb.flip
		raise "get 2" unless bb.get(2) == "ab"
		bb.compact
		raise "after compact pos=#{bb.position} lim=#{bb.limit}" unless bb.position == 2 && bb.limit == 4
		bb << "ef"
		bb.flip
		raise "compacted content" unless bb.get == "cdef"

		raised_uf = false
		begin
			bb.get(1)
		rescue NIO::ByteBuffer::UnderflowError
			raised_uf = true
		end
		raise "expected UnderflowError" unless raised_uf

		raised_mk = false
		begin
			bb.reset
		rescue NIO::ByteBuffer::MarkUnsetError
			raised_mk = true
		end
		raise "expected MarkUnsetError" unless raised_mk

		client.close
		peer.close
		server.close
		sel.close
	`)
	if err != nil {
		t.Fatalf("NIO TCP/ByteBuffer test failed: %v", err)
	}
}
