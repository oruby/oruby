package pty_test

import (
	"runtime"
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/gem/io"
	_ "github.com/oruby/oruby/gem/io/console"
	_ "github.com/oruby/oruby/gem/io/file"
	_ "github.com/oruby/oruby/gem/load"
	_ "github.com/oruby/oruby/gem/process"
	_ "github.com/oruby/oruby/gem/pty"
	_ "github.com/oruby/oruby/gem/stdlib"
)

func TestPTYOpenAndSpawn(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTY not supported on Windows")
	}
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	_, err := mrb.Eval(`
		require "pty"
		require "io/console"

		# PTY.open with block + io/console integration
		PTY.open do |master, slave|
			raise "slave not tty" unless slave.tty?
			slave.winsize = [25, 90]
			raise "winsize" unless master.winsize == [25, 90]
			slave.raw!
			master.write("ping\n")
			line = slave.gets
			raise "open read mismatch: #{line.inspect}" unless line == "ping\n"
		end

		# PTY.spawn block form with shell command & env
		out = nil
		PTY.spawn({ "ORUBY_PTY_VAR" => "hello_pty" }, "echo $ORUBY_PTY_VAR") do |r, w, pid|
			raise "pid invalid" unless pid > 0
			out = r.gets
		end
		raise "spawn env output: #{out.inspect}" unless out && out.strip == "hello_pty"

		# PTY.spawn multi-arg interactive child (cat)
		r, w, pid = PTY.spawn("sh", "-c", "read x; echo got:$x")
		w.puts("oruby")
		lines = ""
		while (line = r.gets)
			lines << line
			break if line.include?("got:oruby")
		end
		r.close
		w.close
		raise "interactive pty failed: #{lines.inspect}" unless lines.include?("got:oruby")

		# PTY.check (wait briefly for exit)
		st = nil
		50.times do
			st = PTY.check(pid)
			break if st
			sleep 0.01
		end
		raise "PTY.check returned nil" unless st
		raise "PTY.check exitstatus: #{st.exitstatus}" unless st.exitstatus == 0

		# PTY.check with raise_err = true -> PTY::ChildExited
		r2, w2, pid2 = PTY.spawn("sh", "-c", "echo bye; exit 7")
		line2 = r2.gets
		raise "expected bye, got #{line2.inspect}" unless line2 && line2.strip == "bye"
		raised_exc = nil
		50.times do
			begin
				PTY.check(pid2, true)
			rescue PTY::ChildExited => e
				raised_exc = e
				break
			end
			sleep 0.01
		end
		r2.close
		w2.close
		raise "expected PTY::ChildExited" unless raised_exc
		raise "ChildExited status: #{raised_exc.status.inspect}" unless raised_exc.status.exitstatus == 7
	`)
	if err != nil {
		t.Fatalf("PTY test failed: %v", err)
	}
}
