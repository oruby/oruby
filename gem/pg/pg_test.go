package pg_test

import (
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgproto3"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/gem/load"
	_ "github.com/oruby/oruby/gem/pg"
)

// startMockPGServer speaks the real PostgreSQL v3 wire protocol over TCP.
func startMockPGServer(t *testing.T) (string, int, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go handlePGConn(c)
		}
	}()
	addr := ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", addr.Port, func() { _ = ln.Close() }
}

func handlePGConn(conn net.Conn) {
	defer conn.Close()
	be := pgproto3.NewBackend(conn, conn)

	msg, err := be.ReceiveStartupMessage()
	if err != nil {
		return
	}
	if _, ok := msg.(*pgproto3.SSLRequest); ok {
		_, _ = conn.Write([]byte{'N'})
		if _, err := be.ReceiveStartupMessage(); err != nil {
			return
		}
	}

	be.Send(&pgproto3.AuthenticationOk{})
	be.Send(&pgproto3.ParameterStatus{Name: "server_version", Value: "16.4"})
	be.Send(&pgproto3.ParameterStatus{Name: "client_encoding", Value: "UTF8"})
	be.Send(&pgproto3.BackendKeyData{ProcessID: 4242, SecretKey: []byte{0, 0, 0x27, 0x0f}})
	be.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
	if err := be.Flush(); err != nil {
		return
	}

	txStatus := byte('I')
	prepared := map[string]string{}
	var pendingSQL string
	var pendingParams [][]byte

	sendSelectResult := func(sql string, params [][]byte) {
		lower := strings.ToLower(strings.TrimSpace(sql))
		switch {
		case lower == "begin":
			txStatus = 'T'
			be.Send(&pgproto3.CommandComplete{CommandTag: []byte("BEGIN")})
		case lower == "commit":
			txStatus = 'I'
			be.Send(&pgproto3.CommandComplete{CommandTag: []byte("COMMIT")})
		case lower == "rollback":
			txStatus = 'I'
			be.Send(&pgproto3.CommandComplete{CommandTag: []byte("ROLLBACK")})
		case strings.Contains(lower, "duplicate"):
			be.Send(&pgproto3.ErrorResponse{
				Severity:       "ERROR",
				Code:           "23505",
				Message:        "duplicate key value violates unique constraint \"users_pkey\"",
				TableName:      "users",
				ConstraintName: "users_pkey",
			})
		case strings.Contains(lower, "select 1"):
			be.Send(&pgproto3.RowDescription{Fields: []pgproto3.FieldDescription{
				{Name: []byte("id"), DataTypeOID: 23, DataTypeSize: 4, Format: 0},
				{Name: []byte("name"), DataTypeOID: 25, DataTypeSize: -1, Format: 0},
				{Name: []byte("active"), DataTypeOID: 16, DataTypeSize: 1, Format: 0},
				{Name: []byte("score"), DataTypeOID: 701, DataTypeSize: 8, Format: 0},
				{Name: []byte("raw"), DataTypeOID: 17, DataTypeSize: -1, Format: 0},
				{Name: []byte("empty"), DataTypeOID: 25, DataTypeSize: -1, Format: 0},
			}})
			be.Send(&pgproto3.DataRow{Values: [][]byte{
				[]byte("42"),
				[]byte("alice"),
				[]byte("t"),
				[]byte("9.5"),
				[]byte(`\xdeadbeef`),
				nil,
			}})
			be.Send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT 1")})
		case strings.Contains(lower, "echo"):
			p0 := []byte("")
			p1 := []byte("")
			if len(params) > 0 && params[0] != nil {
				p0 = params[0]
			}
			if len(params) > 1 && params[1] != nil {
				p1 = params[1]
			}
			be.Send(&pgproto3.RowDescription{Fields: []pgproto3.FieldDescription{
				{Name: []byte("a"), DataTypeOID: 25, DataTypeSize: -1, Format: 0},
				{Name: []byte("b"), DataTypeOID: 23, DataTypeSize: 4, Format: 0},
			}})
			be.Send(&pgproto3.DataRow{Values: [][]byte{p0, p1}})
			be.Send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT 1")})
		default:
			be.Send(&pgproto3.CommandComplete{CommandTag: []byte("OK")})
		}
	}

	for {
		m, err := be.Receive()
		if err != nil {
			return
		}
		switch v := m.(type) {
		case *pgproto3.Terminate:
			return
		case *pgproto3.Query:
			sendSelectResult(v.String, nil)
			be.Send(&pgproto3.ReadyForQuery{TxStatus: txStatus})
			_ = be.Flush()
		case *pgproto3.Parse:
			prepared[v.Name] = v.Query
			pendingSQL = v.Query
			be.Send(&pgproto3.ParseComplete{})
		case *pgproto3.Bind:
			if q, ok := prepared[v.PreparedStatement]; ok {
				pendingSQL = q
			}
			pendingParams = v.Parameters
			be.Send(&pgproto3.BindComplete{})
		case *pgproto3.Describe:
			if v.ObjectType == 'S' {
				be.Send(&pgproto3.ParameterDescription{ParameterOIDs: []uint32{25, 23}})
			}
			be.Send(&pgproto3.RowDescription{Fields: []pgproto3.FieldDescription{
				{Name: []byte("a"), DataTypeOID: 25, DataTypeSize: -1, Format: 0},
				{Name: []byte("b"), DataTypeOID: 23, DataTypeSize: 4, Format: 0},
			}})
		case *pgproto3.Execute:
			sendSelectResult(pendingSQL, pendingParams)
		case *pgproto3.Sync:
			be.Send(&pgproto3.ReadyForQuery{TxStatus: txStatus})
			_ = be.Flush()
		}
	}
}

func TestPGWireProtocolAndAPI(t *testing.T) {
	host, port, cleanup := startMockPGServer(t)
	defer cleanup()

	mrb := oruby.MrbOpen()
	defer mrb.Close()

	_, err := mrb.Eval(fmt.Sprintf(`
		require "pg"

		# Escaping & bytea helpers (without needing a connection)
		raise "escape_string" unless PG::Connection.escape_string("o'reilly") == "o''reilly"
		raise "quote_ident" unless PG::Connection.quote_ident('a"b') == '"a""b"'
		esc = PG::Connection.escape_bytea("\xde\xad\xbe\xef".b)
		raise "escape_bytea: #{esc}" unless esc == "\\xdeadbeef"
		unesc = PG::Connection.unescape_bytea(esc)
		raise "unescape_bytea" unless unesc == "\xde\xad\xbe\xef".b && unesc.encoding == Encoding::BINARY

		# Connect over real PG v3 TCP wire protocol
		PG.connect(host: %q, port: %d, dbname: "testdb", user: "postgres", sslmode: "disable") do |conn|
			raise "status" unless conn.status == PG::CONNECTION_OK
			raise "pid" unless conn.backend_pid == 4242
			raise "server_version" unless conn.server_version == 160004
			raise "db" unless conn.db == "testdb"
			raise "tx idle" unless conn.transaction_status == PG::PQTRANS_IDLE

			# Simple query with default string results
			res = conn.exec("SELECT 1")
			raise "ntuples" unless res.ntuples == 1
			raise "nfields" unless res.nfields == 6
			raise "fields" unless res.fields == ["id", "name", "active", "score", "raw", "empty"]
			raise "default string id" unless res[0]["id"] == "42"
			raise "default string active" unless res[0]["active"] == "t"
			raise "null cell" unless res[0]["empty"].nil? && res.getisnull(0, 5)
			raise "cmd_tuples" unless res.cmd_tuples == 1

			# BasicTypeMapForResults
			conn.type_map_for_results = PG::BasicTypeMapForResults.new(conn)
			res2 = conn.exec("SELECT 1")
			row = res2[0]
			raise "typed id" unless row["id"] == 42
			raise "typed name" unless row["name"] == "alice"
			raise "typed active" unless row["active"] == true
			raise "typed score" unless (row["score"] - 9.5).abs < 1e-9
			raise "typed raw" unless row["raw"] == "\xde\xad\xbe\xef".b
			raise "typed null" unless row["empty"].nil?
			res2.clear
			raise "cleared?" unless res2.cleared?

			# Extended query with exec_params
			res3 = conn.exec_params("SELECT echo($1, $2)", ["hello", 77])
			raise "exec_params a" unless res3[0]["a"] == "hello"
			raise "exec_params b" unless res3[0]["b"] == 77

			# Prepared statement: prepare + exec_prepared
			conn.prepare("stmt_echo", "SELECT echo($1, $2)")
			res4 = conn.exec_prepared("stmt_echo", ["prep", 99])
			raise "exec_prepared" unless res4[0]["a"] == "prep" && res4[0]["b"] == 99

			# Transaction commit & rollback
			tx_val = conn.transaction do |c|
				raise "tx intrans" unless c.transaction_status == PG::PQTRANS_INTRANS
				"committed"
			end
			raise "tx return" unless tx_val == "committed"
			raise "tx back to idle" unless conn.transaction_status == PG::PQTRANS_IDLE

			# SQLSTATE 23505 -> PG::UniqueViolation with result diagnostics!
			caught = nil
			begin
				conn.exec("INSERT duplicate")
			rescue PG::UniqueViolation => e
				caught = e
			end
			raise "expected PG::UniqueViolation" unless caught
			raise "SQLSTATE" unless caught.result.error_field(PG::PG_DIAG_SQLSTATE) == "23505"
			raise "constraint" unless caught.result.error_field(PG::PG_DIAG_CONSTRAINT_NAME) == "users_pkey"
			raise "table" unless caught.result.error_field(PG::PG_DIAG_TABLE_NAME) == "users"
		end
	`, host, port))
	if err != nil {
		t.Fatalf("PG wire protocol test failed: %v", err)
	}
}
