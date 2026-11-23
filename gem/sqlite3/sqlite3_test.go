package sqlite3_test

import (
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/gem/load"
	_ "github.com/oruby/oruby/gem/sqlite3"
)

func evalTest(t *testing.T, code string) {
	t.Helper()
	mrb, err := oruby.New()
	if err != nil {
		t.Fatal(err)
	}
	defer mrb.Close()
	if _, err := mrb.Eval(code); err != nil {
		t.Fatalf("eval failed: %v", err)
	}
}

func TestSQLite3BasicAndCRUD(t *testing.T) {
	evalTest(t, `
		require "sqlite3"

		raise "version" unless SQLite3::SQLITE_VERSION.is_a?(String) && !SQLite3::SQLITE_VERSION.empty?
		raise "libversion" unless SQLite3.libversion > 3000000
		raise "quote" unless SQLite3::Database.quote("it's") == "it''s"

		db = SQLite3::Database.new(":memory:")
		raise "closed?" if db.closed?
		raise "readonly?" if db.readonly?

		db.execute_batch <<-SQL
			CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE, score REAL, data BLOB);
			INSERT INTO users (name, score, data) VALUES ('alice', 9.5, X'DEADBEEF');
		SQL

		raise "last_id" unless db.last_insert_row_id == 1
		raise "changes" unless db.changes == 1

		db.execute("INSERT INTO users (name, score, data) VALUES (?, ?, ?)", ["bob", 8.25, SQLite3::Blob.new("\x00\x01\x02")])
		raise "last_id 2" unless db.last_insert_row_id == 2
		raise "total_changes" unless db.total_changes == 2

		db.execute("INSERT INTO users (name, score) VALUES (:name, :score)", name: "carol", score: 10.0)
		raise "count" unless db.get_first_value("SELECT count(*) FROM users") == 3

		rows = db.execute("SELECT name, score FROM users ORDER BY id")
		raise "rows: #{rows.inspect}" unless rows == [["alice", 9.5], ["bob", 8.25], ["carol", 10.0]]

		# execute2 includes header row
		rows2 = db.execute2("SELECT name, score FROM users WHERE id = 1")
		raise "rows2: #{rows2.inspect}" unless rows2 == [["name", "score"], ["alice", 9.5]]

		# results_as_hash
		db.results_as_hash = true
		row = db.get_first_row("SELECT name, score FROM users WHERE id = 1")
		raise "hash row: #{row.inspect}" unless row == {"name" => "alice", "score" => 9.5}
		db.results_as_hash = false

		# Blob round-trip
		blob = db.get_first_value("SELECT data FROM users WHERE name = 'bob'")
		raise "blob: #{blob.inspect}" unless blob == "\x00\x01\x02".b && blob.encoding == Encoding::BINARY

		db.close
		raise "closed? after close" unless db.closed?
	`)
}

func TestSQLite3TransactionsAndExceptions(t *testing.T) {
	evalTest(t, `
		require "sqlite3"

		SQLite3::Database.open(":memory:") do |db|
			db.execute("CREATE TABLE items (id INTEGER PRIMARY KEY, val TEXT UNIQUE)")
			raise "tx active before" if db.transaction_active?

			res = db.transaction do
				raise "tx not active" unless db.transaction_active?
				db.execute("INSERT INTO items (val) VALUES (?)", "one")
				:committed
			end
			raise "tx return" unless res == :committed
			raise "tx active after" if db.transaction_active?
			raise "count after commit" unless db.get_first_value("SELECT count(*) FROM items") == 1

			begin
				db.transaction do
					db.execute("INSERT INTO items (val) VALUES (?)", "two")
					raise "boom"
				end
			rescue RuntimeError => e
				raise "wrong msg" unless e.message == "boom"
			end
			raise "rollback failed" unless db.get_first_value("SELECT count(*) FROM items") == 1

			# Constraint exception
			got_constraint = false
			begin
				db.execute("INSERT INTO items (val) VALUES (?)", "one")
			rescue SQLite3::ConstraintException => e
				got_constraint = true
				raise "code" unless e.code == 19
			end
			raise "expected ConstraintException" unless got_constraint

			# SQL syntax exception
			got_sql = false
			begin
				db.execute("SELECT * FROM nonexistent_table")
			rescue SQLite3::SQLException
				got_sql = true
			end
			raise "expected SQLException" unless got_sql
		end
	`)
}

func TestSQLite3StatementAndResultSetAndCustomFunc(t *testing.T) {
	evalTest(t, `
		require "sqlite3"

		SQLite3::Database.open(":memory:") do |db|
			db.execute("CREATE TABLE nums (n INTEGER)")
			db.prepare("INSERT INTO nums (n) VALUES (?)") do |stmt|
				[10, 20, 30].each do |v|
					stmt.execute!(v)
				end
			end

			# ResultSet via query
			collected = []
			db.query("SELECT n FROM nums ORDER BY n") do |rs|
				raise "cols" unless rs.columns == ["n"]
				rs.each { |r| collected << r[0] }
				raise "eof" unless rs.eof?
			end
			raise "collected: #{collected.inspect}" unless collected == [10, 20, 30]

			# Statement step + reset + bind_params
			stmt = db.prepare("SELECT n FROM nums WHERE n >= ? ORDER BY n")
			stmt.bind_params(20)
			a = stmt.step
			b = stmt.step
			c = stmt.step
			raise "step: #{[a,b,c].inspect}" unless a == [20] && b == [30] && c.nil?
			stmt.reset!
			stmt.bind_param(1, 30)
			raise "after reset" unless stmt.step == [30]
			stmt.close
			raise "stmt closed" unless stmt.closed?

			# Custom SQL function
			db.create_function("double_it", 1) { |func, x| func.result = x * 2 }
			raise "custom func" unless db.get_first_value("SELECT double_it(21)") == 42

			# ForkSafety, Constants::Open, Pragmas, Statement#column_count
			SQLite3::ForkSafety.suppress_warnings!
			raise "sharedcache" unless SQLite3::Constants::Open::SHAREDCACHE > 0
			raise "pragmas" unless SQLite3::Pragmas.method_defined?(:foreign_keys=)
			db.foreign_keys = true
			raise "fk pragma" unless db.foreign_keys == true

			db.results_as_hash = true
			s_ins = db.prepare("INSERT INTO nums (n) VALUES (?)")
			raise "insert column_count" unless s_ins.column_count == 0
			s_ins.bind_params(40)
			s_ins.step
			s_ins.close

			s_sel = db.prepare("SELECT n FROM nums WHERE n = ?")
			s_sel.bind_params(40)
			raise "select column_count" unless s_sel.column_count == 1
			raise "step returns array" unless s_sel.to_a == [[40]]
			s_sel.close
			db.results_as_hash = false
		end
	`)
}
