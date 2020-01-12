package database

import (
	"github.com/oruby/oruby"

	"os"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func setup() (*oruby.MrbState, func()) {
	mrb, _ := oruby.New()

	_ = os.Remove("testdata/foo.db")

	return mrb, mrb.Close
}

func assert(t *testing.T, desc, code string) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Error(r)
		}
	}()

	mrb, _ := oruby.New()
	defer mrb.Close()

	result, err := mrb.Eval(code)
	if err != nil {
		t.Error(desc, ": ", err)
	}

	if !oruby.MrbBoolean(result) {
		t.Error(desc, ": result =", mrb.Intf(result))
	}
}

func TestOpen(t *testing.T) {
	mrb, closer := setup()
	defer closer()

	_, err := mrb.Eval(`db = Database::DB.new 'sqlite3', 'testdata/foo.db'`)
	if err != nil {
		t.Error(err)
	}
}

func TestSqlite(t *testing.T) {
	mrb, closer := setup()
	defer closer()

	_, err := mrb.Eval(`

  db = Database::DB.new 'sqlite3', 'testdata/foo.db'

  db.exec 'create table foo (id integer not null primary key, name text);'
  db.exec 'insert into foo (id, name) values(1, "v1")'
  db.exec 'insert into foo (id, name) values(?, ?);', 2, "v2"

  rows = db.query("select id, name from foo")

  # rows.each { |row| puts row }
  rows.close

  sve = "OK"
  `)

	if err != nil {
		t.Error(err)
	}
}
