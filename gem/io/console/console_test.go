package console_test

import (
	"testing"

	"github.com/creack/pty"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/gem/io"
	_ "github.com/oruby/oruby/gem/io/console"
	_ "github.com/oruby/oruby/gem/io/file"
	_ "github.com/oruby/oruby/gem/load"
	_ "github.com/oruby/oruby/gem/stdlib"
)

func TestIOConsoleOnRealPTY(t *testing.T) {
	ptmx, tty, err := pty.Open()
	if err != nil {
		t.Skipf("cannot open pty: %v", err)
	}
	defer ptmx.Close()
	defer tty.Close()

	mrb := oruby.MrbOpen()
	defer mrb.Close()

	cFile := mrb.ClassGet("File")
	masterVal := mrb.DataWrapInterface(cFile, ptmx).Value()
	slaveVal := mrb.DataWrapInterface(cFile, tty).Value()
	mrb.GVSet(mrb.Intern("$master"), masterVal)
	mrb.GVSet(mrb.Intern("$slave"), slaveVal)

	_, err = mrb.Eval(`
		require "io/console"

		raise "slave not tty" unless $slave.tty?

		# winsize / winsize=
		$slave.winsize = [32, 120]
		raise "winsize mismatch: #{$slave.winsize.inspect}" unless $slave.winsize == [32, 120]
		raise "master winsize mismatch" unless $master.winsize == [32, 120]

		changed = $slave.check_winsize_changed { $slave.winsize = [40, 100] }
		raise "check_winsize_changed false" unless changed == true
		raise "winsize after change" unless $slave.winsize == [40, 100]

		# echo? / echo= / noecho
		$slave.echo = true
		raise "echo? should be true" unless $slave.echo? == true
		$slave.noecho do |io|
			raise "echo? inside noecho should be false" unless io.echo? == false
		end
		raise "echo? not restored after noecho" unless $slave.echo? == true

		# console_mode / ConsoleMode snapshot & restore
		saved = $slave.console_mode
		raise "ConsoleMode class" unless saved.is_a?(IO::ConsoleMode)
		raw_mode = saved.raw(min: 2, time: 5)
		raise "raw_mode echo" unless raw_mode.echo? == false
		raise "raw_mode min" unless raw_mode.min == 2
		raise "raw_mode time" unless raw_mode.time == 5
		raise "saved still has echo" unless saved.echo? == true

		$slave.console_mode = raw_mode
		raise "applied raw_mode echo" unless $slave.echo? == false
		$slave.console_mode = saved
		raise "restored saved echo" unless $slave.echo? == true

		# raw / cooked block restore
		$slave.raw do |io|
			raise "raw echo false" unless io.echo? == false
			io.cooked do |c|
				raise "cooked echo true" unless c.echo? == true
			end
			raise "back to raw echo false" unless io.echo? == false
		end
		raise "restored after raw" unless $slave.echo? == true

		# getch over real PTY (single char without newline!)
		$master.write("X")
		ch = $slave.getch
		raise "getch mismatch: #{ch.inspect}" unless ch == "X"

		# ttyname
		name = $slave.ttyname
		raise "ttyname empty: #{name.inspect}" unless name.is_a?(String) && name.start_with?("/dev/")

		# iflush / oflush / ioflush
		$slave.iflush
		$slave.oflush
		$slave.ioflush

		# StringIO getch & getpass
		sio = StringIO.new("abc\n")
		raise "StringIO#getch" unless sio.getch == "a"
		raise "StringIO#getpass" unless sio.getpass == "bc"

		# IO.default_console_size / IO.console_size
		raise "default_console_size" unless IO.default_console_size == [24, 80]
		sz = IO.console_size
		raise "console_size" unless sz.is_a?(Array) && sz.length == 2
	`)
	if err != nil {
		t.Fatalf("io/console test failed: %v", err)
	}
}
