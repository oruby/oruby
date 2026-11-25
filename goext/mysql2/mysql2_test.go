package mysql2_test

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/oruby/oruby"
	_ "github.com/oruby/oruby/gem/core_ext"
	_ "github.com/oruby/oruby/gem/load"
	_ "github.com/oruby/oruby/goext/mysql2"
)

// Minimal real MySQL v10 wire-protocol mock server for end-to-end TCP tests.
func startMockMySQLServer(t *testing.T) (string, int, func()) {
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
			go handleMySQLConn(c)
		}
	}()
	addr := ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", addr.Port, func() { _ = ln.Close() }
}

func writePacket(w io.Writer, seq byte, payload []byte) error {
	var hdr [4]byte
	n := len(payload)
	hdr[0] = byte(n)
	hdr[1] = byte(n >> 8)
	hdr[2] = byte(n >> 16)
	hdr[3] = seq
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

func readPacket(r io.Reader) (byte, []byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, err
	}
	n := int(hdr[0]) | int(hdr[1])<<8 | int(hdr[2])<<16
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return 0, nil, err
	}
	return hdr[3], buf, nil
}

func appendLenEncStr(b []byte, s string) []byte {
	b = append(b, byte(len(s)))
	return append(b, s...)
}

func makeColDef41(name string, colType byte, charset uint16) []byte {
	var b []byte
	b = appendLenEncStr(b, "def")
	b = appendLenEncStr(b, "")
	b = appendLenEncStr(b, "")
	b = appendLenEncStr(b, "")
	b = appendLenEncStr(b, name)
	b = appendLenEncStr(b, name)
	b = append(b, 0x0c) // length of fixed-length fields
	b = append(b, byte(charset), byte(charset>>8))
	b = append(b, 0xff, 0, 0, 0) // column length
	b = append(b, colType)
	b = append(b, 0, 0) // flags
	b = append(b, 0)    // decimals
	b = append(b, 0, 0) // filler
	return b
}

func makeOK(affected, lastID byte) []byte {
	return []byte{0x00, affected, lastID, 0x02, 0x00, 0x00, 0x00}
}

func makeEOF() []byte {
	return []byte{0xfe, 0x00, 0x00, 0x02, 0x00}
}

func makeERR(code uint16, state, msg string) []byte {
	b := []byte{0xff, byte(code), byte(code >> 8), '#'}
	b = append(b, []byte(state)...)
	b = append(b, []byte(msg)...)
	return b
}

func handleMySQLConn(conn net.Conn) {
	defer conn.Close()

	// 1. Send Initial Handshake Packet v10
	var hs []byte
	hs = append(hs, 10)                        // protocol version
	hs = append(hs, "8.0.36-mock"...)          // server version
	hs = append(hs, 0)                         // NUL
	hs = append(hs, 1, 0, 0, 0)                // connection id
	hs = append(hs, []byte("12345678")...)     // auth-plugin-data-part-1
	hs = append(hs, 0)                         // filler
	hs = append(hs, 0xff, 0xf7)                // capability flags lower (CLIENT_PROTOCOL_41 | SECURE_CONNECTION)
	hs = append(hs, 45)                        // charset utf8mb4
	hs = append(hs, 0x02, 0x00)                // status flags
	hs = append(hs, 0x0f, 0x80)                // capability flags upper
	hs = append(hs, 21)                        // auth plugin data len
	hs = append(hs, make([]byte, 10)...)       // reserved
	hs = append(hs, []byte("abcdefghijkl")...) // auth-plugin-data-part-2 (12 bytes)
	hs = append(hs, 0)
	hs = append(hs, "mysql_native_password"...)
	hs = append(hs, 0)

	if err := writePacket(conn, 0, hs); err != nil {
		return
	}
	// Read client HandshakeResponse41
	seq, _, err := readPacket(conn)
	if err != nil {
		return
	}
	if err := writePacket(conn, seq+1, makeOK(0, 0)); err != nil {
		return
	}

	// Command loop
	for {
		seq, pkt, err := readPacket(conn)
		if err != nil || len(pkt) == 0 {
			return
		}
		cmd := pkt[0]
		switch cmd {
		case 0x01: // COM_QUIT
			return
		case 0x0e: // COM_PING
			_ = writePacket(conn, seq+1, makeOK(0, 0))
		case 0x03: // COM_QUERY
			sqlStr := strings.ToLower(strings.TrimSpace(string(pkt[1:])))
			switch {
			case strings.HasPrefix(sqlStr, "insert"):
				_ = writePacket(conn, seq+1, makeOK(1, 55))
			case strings.Contains(sqlStr, "duplicate"):
				_ = writePacket(conn, seq+1, makeERR(1062, "23000", "Duplicate entry '1' for key 'PRIMARY'"))
			case strings.HasPrefix(sqlStr, "select"):
				// 4 columns: id (LONG=0x03), name (VAR_STRING=0xfd), active (TINY=0x01), score (DOUBLE=0x05)
				_ = writePacket(conn, seq+1, []byte{4})
				_ = writePacket(conn, seq+2, makeColDef41("id", 0x03, 63))
				_ = writePacket(conn, seq+3, makeColDef41("name", 0xfd, 45))
				_ = writePacket(conn, seq+4, makeColDef41("active", 0x01, 63))
				_ = writePacket(conn, seq+5, makeColDef41("score", 0x05, 63))
				_ = writePacket(conn, seq+6, makeEOF())
				var row []byte
				row = appendLenEncStr(row, "42")
				row = appendLenEncStr(row, "alice")
				row = appendLenEncStr(row, "1")
				row = appendLenEncStr(row, "9.5")
				_ = writePacket(conn, seq+7, row)
				_ = writePacket(conn, seq+8, makeEOF())
			default:
				_ = writePacket(conn, seq+1, makeOK(0, 0))
			}
		case 0x16: // COM_STMT_PREPARE
			// stmt_id=1, num_cols=1, num_params=1
			var okPkt [12]byte
			okPkt[0] = 0x00
			binary.LittleEndian.PutUint32(okPkt[1:5], 1)
			binary.LittleEndian.PutUint16(okPkt[5:7], 1)
			binary.LittleEndian.PutUint16(okPkt[7:9], 1)
			_ = writePacket(conn, seq+1, okPkt[:])
			_ = writePacket(conn, seq+2, makeColDef41("?", 0xfd, 45))
			_ = writePacket(conn, seq+3, makeEOF())
			_ = writePacket(conn, seq+4, makeColDef41("echo", 0xfd, 45))
			_ = writePacket(conn, seq+5, makeEOF())
		case 0x17: // COM_STMT_EXECUTE
			// Extract string param from binary execute packet (1 header + 4 stmt_id + 1 flags + 4 iter + 1 nullbitmap + 1 new_params + 2 type + lenenc string)
			val := "ok"
			if len(pkt) > 15 {
				slen := int(pkt[14])
				if 15+slen <= len(pkt) {
					val = string(pkt[15 : 15+slen])
				}
			}
			_ = writePacket(conn, seq+1, []byte{1})
			_ = writePacket(conn, seq+2, makeColDef41("echo", 0xfd, 45))
			_ = writePacket(conn, seq+3, makeEOF())
			// Binary result row: 0x00 header + 1-byte null bitmap (0x00) + lenenc string
			row := []byte{0x00, 0x00}
			row = appendLenEncStr(row, val)
			_ = writePacket(conn, seq+4, row)
			_ = writePacket(conn, seq+5, makeEOF())
		case 0x19: // COM_STMT_CLOSE
			// no response
		default:
			_ = writePacket(conn, seq+1, makeOK(0, 0))
		}
	}
}

func TestMySQL2WireProtocolAndAPI(t *testing.T) {
	host, port, cleanup := startMockMySQLServer(t)
	defer cleanup()

	mrb := oruby.MrbOpen()
	defer mrb.Close()

	_, err := mrb.Eval(fmt.Sprintf(`
		require "mysql2"

		# Static escape
		raise "escape" unless Mysql2::Client.escape("a'b\"c\\d\ne") == "a\\'b\\\"c\\\\d\\ne"

		client = Mysql2::Client.new(host: %q, port: %d, username: "root", database: "test")
		raise "ping" unless client.ping == true
		raise "closed?" if client.closed?

		# SELECT as hash (default) with type casting
		res = client.query("SELECT id, name, active, score FROM users")
		raise "fields" unless res.fields == ["id", "name", "active", "score"]
		raise "count" unless res.count == 1
		row = res.first
		raise "id" unless row["id"] == 42
		raise "name" unless row["name"] == "alice"
		raise "active int" unless row["active"] == 1
		raise "score" unless (row["score"] - 9.5).abs < 1e-9

		# Per-call symbolize_keys & cast_booleans
		sym_row = res.each(symbolize_keys: true, cast_booleans: true).first
		raise "sym id" unless sym_row[:id] == 42
		raise "sym active bool" unless sym_row[:active] == true

		# Per-call as: :array
		ary_row = res.each(as: :array).first
		raise "array row: #{ary_row.inspect}" unless ary_row == [42, "alice", 1, 9.5]

		# INSERT -> nil result, updates last_id & affected_rows
		ins = client.query("INSERT INTO users (name) VALUES ('bob')")
		raise "insert should return nil" unless ins.nil?
		raise "last_id" unless client.last_id == 55
		raise "affected_rows" unless client.affected_rows == 1

		# MySQL error code & SQLSTATE
		caught = nil
		begin
			client.query("SELECT duplicate")
		rescue Mysql2::Error => e
			caught = e
		end
		raise "expected Mysql2::Error" unless caught
		raise "errno: #{caught.error_number}" unless caught.error_number == 1062 && caught.errno == 1062
		raise "sql_state: #{caught.sql_state.inspect}" unless caught.sql_state == "23000"

		# Prepared statement (COM_STMT_PREPARE + COM_STMT_EXECUTE)
		stmt = client.prepare("SELECT ? AS echo")
		raise "param_count" unless stmt.param_count == 1
		sres = stmt.execute("prepared-hello")
		raise "stmt result: #{sres.first.inspect}" unless sres.first["echo"] == "prepared-hello"
		stmt.close
		raise "stmt closed?" unless stmt.closed?

		client.close
		raise "client closed?" unless client.closed?
	`, host, port))
	if err != nil {
		t.Fatalf("Mysql2 wire protocol test failed: %v", err)
	}
}
