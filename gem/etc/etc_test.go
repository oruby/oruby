package etc_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/gem/etc"
	_ "github.com/oruby/oruby/gem/load"
	_ "github.com/oruby/oruby/gem/process"
)

func TestEtcSystemQueries(t *testing.T) {
	mrb := oruby.MrbOpen()
	defer mrb.Close()

	_, err := mrb.Eval(`
require "etc"

raise "nprocessors" unless Etc.nprocessors.is_a?(Integer) && Etc.nprocessors >= 1

u = Etc.uname
raise "uname hash" unless u.is_a?(Hash) && u[:sysname].is_a?(String) && !u[:sysname].empty?
raise "uname machine" unless u[:machine].is_a?(String) && !u[:machine].empty?

raise "systmpdir" unless Etc.systmpdir.is_a?(String) && !Etc.systmpdir.empty?
raise "sysconfdir" unless Etc.sysconfdir.is_a?(String) && !Etc.sysconfdir.empty?

pw = Etc.getpwuid
raise "getpwuid" unless pw.is_a?(Etc::Passwd) && pw.uid == Process.uid && pw.name.is_a?(String)
pw2 = Etc.getpwnam(pw.name)
raise "getpwnam" unless pw2.uid == pw.uid

gr = Etc.getgrgid
raise "getgrgid" unless gr.is_a?(Etc::Group) && gr.gid == Process.gid && gr.name.is_a?(String)
gr2 = Etc.getgrnam(gr.name)
raise "getgrnam" unless gr2.gid == gr.gid

login = Etc.getlogin
raise "getlogin type" unless login.nil? || login.is_a?(String)
`)
	if err != nil {
		t.Fatalf("Etc test failed: %v", err)
	}
}
