// Package mysql2 implements Ruby's mysql2 gem in pure Go backed by
// github.com/go-sql-driver/mysql.
package mysql2

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	gomysql "github.com/go-sql-driver/mysql"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/internal/rb"
)

//go:embed mysql2.rb
var mysql2Rb []byte

const (
	flagFoundRows       = 2
	flagMultiStatements = 65536
)

func init() {
	oruby.LazyGem("mysql2", initMySQL2)
	for _, sub := range []string{
		"mysql2/mysql2",
		"mysql2/client",
		"mysql2/result",
		"mysql2/statement",
		"mysql2/error",
		"mysql2/version",
	} {
		oruby.GemAlias(sub, "mysql2")
	}
}

func initMySQL2(mrb *oruby.MrbState) any {
	mod := mrb.DefineModule("Mysql2")
	mrb.ConstSet(mod, mrb.Intern("VERSION"), mrb.StrNew("0.5.6"))

	stdErr := mrb.ExcGet("StandardError")
	errCls := mrb.DefineClassUnder(mod, "Error", stdErr)
	mrb.DefineClassUnder(errCls, "ConnectionError", errCls)
	mrb.DefineClassUnder(errCls, "TimeoutError", errCls)

	defineClient(mrb, mod)
	defineResult(mrb, mod)
	defineStatement(mrb, mod)

	rb.LoadRuby(mrb, "<internal:mysql2>", mysql2Rb)
	return nil
}

func raiseMySQLError(mrb *oruby.MrbState, err error, connErr bool) oruby.MrbValue {
	clsPath := "Mysql2::Error"
	if connErr {
		clsPath = "Mysql2::Error::ConnectionError"
	} else if errors.Is(err, context.DeadlineExceeded) {
		clsPath = "Mysql2::Error::TimeoutError"
	}
	cls := rb.ClassPath(mrb, clsPath)
	var errno int
	var sqlState string
	var me *gomysql.MySQLError
	if errors.As(err, &me) {
		errno = int(me.Number)
		sqlState = string(me.SQLState[:])
	}
	exc, callErr := mrb.Funcall(cls, mrb.Intern("new"), mrb.StrNew(err.Error()))
	if callErr != nil {
		return mrb.Raise(cls, err.Error())
	}
	mrb.IVSet(exc, mrb.Intern("@error_number"), mrb.FixnumValue(errno))
	mrb.IVSet(exc, mrb.Intern("@sql_state"), mrb.StrNew(sqlState))
	mrb.ExcRaise(exc)
	return mrb.NilValue()
}

// escapeMySQL implements mysql_real_escape_string semantics.
func escapeMySQL(s string) string {
	var sb strings.Builder
	sb.Grow(len(s) + 8)
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case 0:
			sb.WriteString(`\0`)
		case '\n':
			sb.WriteString(`\n`)
		case '\r':
			sb.WriteString(`\r`)
		case '\\':
			sb.WriteString(`\\`)
		case '\'':
			sb.WriteString(`\'`)
		case '"':
			sb.WriteString(`\"`)
		case '\x1a':
			sb.WriteString(`\Z`)
		default:
			sb.WriteByte(s[i])
		}
	}
	return sb.String()
}

// ---------------------------------------------------------------------------
// Options helpers
// ---------------------------------------------------------------------------

func hashGetOpt(mrb *oruby.MrbState, h oruby.Value, names ...string) oruby.Value {
	if h.IsNil() || !h.IsHash() {
		return mrb.NilValue()
	}
	for _, name := range names {
		if v, err := mrb.Funcall(h, mrb.Intern("[]"), mrb.Intern(name).Value()); err == nil && !v.IsNil() {
			return v
		}
		if v, err := mrb.Funcall(h, mrb.Intern("[]"), mrb.StrNew(name)); err == nil && !v.IsNil() {
			return v
		}
	}
	return mrb.NilValue()
}

func mergeOpts(mrb *oruby.MrbState, pos oruby.Value, kw oruby.Value) oruby.Value {
	if !pos.IsNil() && pos.IsHash() {
		if !kw.IsNil() && kw.IsHash() {
			if merged, err := mrb.Funcall(pos, mrb.Intern("merge"), kw); err == nil {
				return merged
			}
		}
		return pos
	}
	if !kw.IsNil() && kw.IsHash() {
		return kw
	}
	return mrb.NilValue()
}

// ---------------------------------------------------------------------------
// Mysql2::Client
// ---------------------------------------------------------------------------

type mysqlClient struct {
	db            *sql.DB
	conn          *sql.Conn
	cfg           *gomysql.Config
	closed        bool
	lastID        int64
	affectedRows  int64
	asArray       bool
	symbolizeKeys bool
	cast          bool
	castBooleans  bool
	encoding      string
}

func getClient(mrb *oruby.MrbState, self oruby.Value) (*mysqlClient, error) {
	c, ok := rb.Get[*mysqlClient](mrb, self)
	if !ok || c == nil {
		return nil, errors.New("Mysql2::Client is not initialized")
	}
	return c, nil
}

func (c *mysqlClient) checkOpen() error {
	if c.closed || c.conn == nil {
		return errors.New("closed MySQL connection")
	}
	return nil
}

func buildMySQLConfig(mrb *oruby.MrbState, opts oruby.Value) (*gomysql.Config, bool, bool, bool, bool, string) {
	cfg := gomysql.NewConfig()
	cfg.Net = "tcp"
	cfg.Addr = "127.0.0.1:3306"
	cfg.AllowNativePasswords = true

	host := "127.0.0.1"
	port := 3306
	encoding := "utf8mb4"
	asArray := false
	symbolizeKeys := false
	cast := true
	castBooleans := false

	if !opts.IsNil() && opts.IsHash() {
		if v := hashGetOpt(mrb, opts, "host"); !v.IsNil() {
			host = v.String()
		}
		if v := hashGetOpt(mrb, opts, "port"); !v.IsNil() {
			if v.IsInt() {
				port = v.Int()
			} else if n, err := strconv.Atoi(v.String()); err == nil {
				port = n
			}
		}
		if v := hashGetOpt(mrb, opts, "socket"); !v.IsNil() {
			cfg.Net = "unix"
			cfg.Addr = v.String()
		} else {
			cfg.Addr = fmt.Sprintf("%s:%d", host, port)
		}
		if v := hashGetOpt(mrb, opts, "username", "user"); !v.IsNil() {
			cfg.User = v.String()
		}
		if v := hashGetOpt(mrb, opts, "password", "pass"); !v.IsNil() {
			cfg.Passwd = v.String()
		}
		if v := hashGetOpt(mrb, opts, "database", "db"); !v.IsNil() {
			cfg.DBName = v.String()
		}
		if v := hashGetOpt(mrb, opts, "encoding", "charset"); !v.IsNil() {
			encoding = v.String()
		}
		if v := hashGetOpt(mrb, opts, "connect_timeout"); !v.IsNil() {
			cfg.Timeout = time.Duration(v.Float64() * float64(time.Second))
		}
		if v := hashGetOpt(mrb, opts, "read_timeout"); !v.IsNil() {
			cfg.ReadTimeout = time.Duration(v.Float64() * float64(time.Second))
		}
		if v := hashGetOpt(mrb, opts, "write_timeout"); !v.IsNil() {
			cfg.WriteTimeout = time.Duration(v.Float64() * float64(time.Second))
		}
		if v := hashGetOpt(mrb, opts, "flags"); !v.IsNil() && v.IsInt() {
			fl := v.Int()
			if fl&flagFoundRows != 0 {
				cfg.ClientFoundRows = true
			}
			if fl&flagMultiStatements != 0 {
				cfg.MultiStatements = true
			}
		}
		if v := hashGetOpt(mrb, opts, "as"); !v.IsNil() {
			if (v.IsSymbol() && mrb.SymName(v.Symbol()) == "array") || (v.IsString() && v.String() == "array") {
				asArray = true
			}
		}
		if v := hashGetOpt(mrb, opts, "symbolize_keys"); !v.IsNil() {
			symbolizeKeys = v.Bool()
		}
		if v := hashGetOpt(mrb, opts, "cast"); !v.IsNil() {
			cast = v.Bool()
		}
		if v := hashGetOpt(mrb, opts, "cast_booleans"); !v.IsNil() {
			castBooleans = v.Bool()
		}
	}

	if cfg.Timeout == 0 {
		cfg.Timeout = 10 * time.Second
	}
	return cfg, asArray, symbolizeKeys, cast, castBooleans, encoding
}

func defineClient(mrb *oruby.MrbState, mod oruby.RClass) {
	cls := mrb.DefineClassUnder(mod, "Client", mrb.ObjectClass())
	rb.DataClass(cls)

	mrb.ConstSet(cls, mrb.Intern("FOUND_ROWS"), mrb.FixnumValue(flagFoundRows))
	mrb.ConstSet(cls, mrb.Intern("MULTI_STATEMENTS"), mrb.FixnumValue(flagMultiStatements))
	mrb.ConstSet(cls, mrb.Intern("SSL_MODE_DISABLED"), mrb.StrNew("DISABLED"))
	mrb.ConstSet(cls, mrb.Intern("SSL_MODE_PREFERRED"), mrb.StrNew("PREFERRED"))
	mrb.ConstSet(cls, mrb.Intern("SSL_MODE_REQUIRED"), mrb.StrNew("REQUIRED"))

	cls.DefineClassMethod("escape", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.StrNew(escapeMySQL(mrb.GetArgsFirst().String()))
	}, mrb.ArgsReq(1))

	cls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		pos := mrb.NilValue()
		if args.Len() >= 1 {
			pos = args.Item(0)
		}
		opts := mergeOpts(mrb, pos, mrb.KeywordArgs().Value())
		cfg, asArray, symKeys, cast, castBools, enc := buildMySQLConfig(mrb, opts)

		db, err := sql.Open("mysql", cfg.FormatDSN())
		if err != nil {
			return raiseMySQLError(mrb, err, true)
		}
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)

		ctx := mrb.GoContext()
		conn, err := db.Conn(ctx)
		if err != nil {
			_ = db.Close()
			return raiseMySQLError(mrb, err, true)
		}
		if err := conn.PingContext(ctx); err != nil {
			_ = conn.Close()
			_ = db.Close()
			return raiseMySQLError(mrb, err, true)
		}

		c := &mysqlClient{
			db:            db,
			conn:          conn,
			cfg:           cfg,
			asArray:       asArray,
			symbolizeKeys: symKeys,
			cast:          cast,
			castBooleans:  castBools,
			encoding:      enc,
		}
		rb.Set(mrb, self, c)
		return self
	}, mrb.ArgsAny())

	cls.DefineMethod("close", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getClient(mrb, self)
		if err == nil && !c.closed {
			c.closed = true
			if c.conn != nil {
				_ = c.conn.Close()
				c.conn = nil
			}
			if c.db != nil {
				_ = c.db.Close()
				c.db = nil
			}
		}
		return mrb.NilValue()
	}, mrb.ArgsNone())

	cls.DefineMethod("closed?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getClient(mrb, self)
		return mrb.BoolValue(err != nil || c.closed)
	}, mrb.ArgsNone())

	cls.DefineMethod("ping", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getClient(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return mrb.FalseValue()
		}
		return mrb.BoolValue(c.conn.PingContext(mrb.GoContext()) == nil)
	}, mrb.ArgsNone())

	cls.DefineMethod("escape", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.StrNew(escapeMySQL(mrb.GetArgsFirst().String()))
	}, mrb.ArgsReq(1))

	cls.DefineMethod("last_id", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := getClient(mrb, self)
		return mrb.Int64Value(c.lastID)
	}, mrb.ArgsNone())

	cls.DefineMethod("affected_rows", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := getClient(mrb, self)
		return mrb.Int64Value(c.affectedRows)
	}, mrb.ArgsNone())

	cls.DefineMethod("select_db", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getClient(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return raiseMySQLError(mrb, errors.New("closed MySQL connection"), true)
		}
		dbName := mrb.GetArgsFirst().String()
		if _, err := c.conn.ExecContext(mrb.GoContext(), "USE `"+strings.ReplaceAll(dbName, "`", "``")+"`"); err != nil {
			return raiseMySQLError(mrb, err, false)
		}
		return mrb.StrNew(dbName)
	}, mrb.ArgsReq(1))

	cls.DefineMethod("query", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getClient(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return raiseMySQLError(mrb, errors.New("closed MySQL connection"), true)
		}
		args := mrb.GetArgs()
		sqlStr := args.Item(0).String()
		posOpt := mrb.NilValue()
		if args.Len() >= 2 {
			posOpt = args.Item(1)
		}
		opts := mergeOpts(mrb, posOpt, mrb.KeywordArgs().Value())
		asArray, symKeys, cast, castBools := c.resolveQueryOpts(mrb, opts)

		ctx := mrb.GoContext()
		if isNonRowQuery(sqlStr) {
			res, err := c.conn.ExecContext(ctx, sqlStr)
			if err != nil {
				return raiseMySQLError(mrb, err, false)
			}
			if id, err := res.LastInsertId(); err == nil {
				c.lastID = id
			}
			if aff, err := res.RowsAffected(); err == nil {
				c.affectedRows = aff
			}
			return mrb.NilValue()
		}

		rows, err := c.conn.QueryContext(ctx, sqlStr)
		if err != nil {
			return raiseMySQLError(mrb, err, false)
		}
		defer rows.Close()

		res, err := readMySQLRows(rows, asArray, symKeys, cast, castBools)
		if err != nil {
			return raiseMySQLError(mrb, err, false)
		}
		c.affectedRows = int64(len(res.rows))
		resCls := rb.ClassPath(mrb, "Mysql2::Result")
		return mrb.DataWrapInterface(resCls, res).Value()
	}, mrb.ArgsReq(1)|mrb.ArgsOpt(1))

	cls.DefineMethod("prepare", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getClient(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return raiseMySQLError(mrb, errors.New("closed MySQL connection"), true)
		}
		sqlStr := mrb.GetArgsFirst().String()
		stmt, err := c.conn.PrepareContext(mrb.GoContext(), sqlStr)
		if err != nil {
			return raiseMySQLError(mrb, err, false)
		}
		st := &mysqlStmt{
			client: c,
			stmt:   stmt,
			sql:    sqlStr,
			params: countPlaceholders(sqlStr),
		}
		stCls := rb.ClassPath(mrb, "Mysql2::Statement")
		v := mrb.DataWrapInterface(stCls, st).Value()
		mrb.IVSet(v, mrb.Intern("@__client"), self)
		return v
	}, mrb.ArgsReq(1))
}

func (c *mysqlClient) resolveQueryOpts(mrb *oruby.MrbState, opts oruby.Value) (bool, bool, bool, bool) {
	asArray, symKeys, cast, castBools := c.asArray, c.symbolizeKeys, c.cast, c.castBooleans
	if opts.IsNil() || !opts.IsHash() {
		return asArray, symKeys, cast, castBools
	}
	if v := hashGetOpt(mrb, opts, "as"); !v.IsNil() {
		s := ""
		if v.IsSymbol() {
			s = mrb.SymName(v.Symbol())
		} else if v.IsString() {
			s = v.String()
		}
		if s == "array" {
			asArray = true
		} else if s == "hash" {
			asArray = false
		}
	}
	if v := hashGetOpt(mrb, opts, "symbolize_keys"); !v.IsNil() {
		symKeys = v.Bool()
	}
	if v := hashGetOpt(mrb, opts, "cast"); !v.IsNil() {
		cast = v.Bool()
	}
	if v := hashGetOpt(mrb, opts, "cast_booleans"); !v.IsNil() {
		castBools = v.Bool()
	}
	return asArray, symKeys, cast, castBools
}

func isNonRowQuery(sqlStr string) bool {
	s := strings.TrimSpace(sqlStr)
	upper := strings.ToUpper(s)
	for _, prefix := range []string{
		"INSERT", "UPDATE", "DELETE", "REPLACE", "CREATE", "DROP", "ALTER",
		"TRUNCATE", "BEGIN", "START TRANSACTION", "COMMIT", "ROLLBACK",
		"SAVEPOINT", "RELEASE SAVEPOINT", "SET ", "USE ", "LOCK ", "UNLOCK ",
	} {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	return false
}

func countPlaceholders(sqlStr string) int {
	n := 0
	inSingle := false
	inDouble := false
	for i := 0; i < len(sqlStr); i++ {
		c := sqlStr[i]
		if c == '\\' && (inSingle || inDouble) {
			i++
			continue
		}
		if c == '\'' && !inDouble {
			inSingle = !inSingle
		} else if c == '"' && !inSingle {
			inDouble = !inDouble
		} else if c == '?' && !inSingle && !inDouble {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// Mysql2::Result
// ---------------------------------------------------------------------------

type mysqlColMeta struct {
	name     string
	dbType   string
	isBinary bool
	isInt    bool
	isFloat  bool
	isBool   bool
}

type mysqlResult struct {
	cols          []mysqlColMeta
	rows          [][]any
	asArray       bool
	symbolizeKeys bool
	cast          bool
	castBooleans  bool
}

func readMySQLRows(rows *sql.Rows, asArray, symKeys, cast, castBools bool) (*mysqlResult, error) {
	colNames, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	colTypes, _ := rows.ColumnTypes()
	cols := make([]mysqlColMeta, len(colNames))
	for i, name := range colNames {
		m := mysqlColMeta{name: name}
		if i < len(colTypes) && colTypes[i] != nil {
			dt := strings.ToUpper(colTypes[i].DatabaseTypeName())
			m.dbType = dt
			switch {
			case dt == "TINYINT" || dt == "BOOL" || dt == "BOOLEAN" || dt == "BIT":
				m.isInt = true
				m.isBool = true
			case strings.Contains(dt, "INT"):
				m.isInt = true
			case dt == "FLOAT" || dt == "DOUBLE" || dt == "REAL":
				m.isFloat = true
			case strings.Contains(dt, "BLOB") || strings.Contains(dt, "BINARY"):
				m.isBinary = true
			}
		}
		cols[i] = m
	}

	var outRows [][]any
	rawVals := make([]any, len(cols))
	scanDest := make([]any, len(cols))
	for i := range rawVals {
		scanDest[i] = &rawVals[i]
	}

	for rows.Next() {
		if err := rows.Scan(scanDest...); err != nil {
			return nil, err
		}
		row := make([]any, len(cols))
		for i, v := range rawVals {
			if b, ok := v.([]byte); ok {
				cp := make([]byte, len(b))
				copy(cp, b)
				row[i] = cp
			} else {
				row[i] = v
			}
		}
		outRows = append(outRows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return &mysqlResult{
		cols:          cols,
		rows:          outRows,
		asArray:       asArray,
		symbolizeKeys: symKeys,
		cast:          cast,
		castBooleans:  castBools,
	}, nil
}

func mysqlValToRuby(mrb *oruby.MrbState, col mysqlColMeta, v any, cast, castBools bool) oruby.Value {
	if v == nil {
		return mrb.NilValue()
	}
	switch x := v.(type) {
	case int64:
		if cast && castBools && col.isBool {
			return mrb.BoolValue(x != 0)
		}
		if !cast {
			return mrb.StrNew(strconv.FormatInt(x, 10))
		}
		return mrb.Int64Value(x)
	case uint64:
		if !cast {
			return mrb.StrNew(strconv.FormatUint(x, 10))
		}
		return mrb.Uint64Value(x)
	case float64:
		if !cast {
			return mrb.StrNew(strconv.FormatFloat(x, 'g', -1, 64))
		}
		return mrb.FloatValue(x)
	case float32:
		if !cast {
			return mrb.StrNew(strconv.FormatFloat(float64(x), 'g', -1, 32))
		}
		return mrb.FloatValue(float64(x))
	case bool:
		return mrb.BoolValue(x)
	case string:
		return mrb.StrNew(x)
	case []byte:
		if !cast {
			return mrb.BytesValue(x)
		}
		if col.isBinary {
			return mrb.Call(mrb.BytesValue(x), "b")
		}
		if col.isInt {
			s := string(x)
			if n, err := strconv.ParseInt(s, 10, 64); err == nil {
				if castBools && col.isBool {
					return mrb.BoolValue(n != 0)
				}
				return mrb.Int64Value(n)
			}
			if u, err := strconv.ParseUint(s, 10, 64); err == nil {
				return mrb.Uint64Value(u)
			}
		}
		if col.isFloat {
			if f, err := strconv.ParseFloat(string(x), 64); err == nil {
				return mrb.FloatValue(f)
			}
		}
		return mrb.BytesValue(x)
	case time.Time:
		return mrb.StrNew(x.Format("2006-01-02 15:04:05"))
	default:
		return mrb.StrNew(fmt.Sprintf("%v", x))
	}
}

func getRes(mrb *oruby.MrbState, self oruby.Value) *mysqlResult {
	r, _ := rb.Get[*mysqlResult](mrb, self)
	return r
}

func defineResult(mrb *oruby.MrbState, mod oruby.RClass) {
	cls := mrb.DefineClassUnder(mod, "Result", mrb.ObjectClass())
	rb.DataClass(cls)
	mrb.IncludeModule(cls, mrb.ModuleGet("Enumerable"))

	countFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.FixnumValue(len(getRes(mrb, self).rows))
	}
	cls.DefineMethod("count", countFn, mrb.ArgsNone())
	cls.DefineMethod("size", countFn, mrb.ArgsNone())

	cls.DefineMethod("fields", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r := getRes(mrb, self)
		ary := mrb.AryNewCapa(len(r.cols))
		for _, c := range r.cols {
			mrb.AryPush(ary, mrb.StrNew(c.name))
		}
		return ary
	}, mrb.ArgsNone())

	cls.DefineMethod("free", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r := getRes(mrb, self)
		r.rows = nil
		return mrb.NilValue()
	}, mrb.ArgsNone())

	cls.DefineMethod("each", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r := getRes(mrb, self)
		args := mrb.GetArgs()
		blk := mrb.GetArgsBlock()
		posOpt := mrb.NilValue()
		if args.Len() >= 1 {
			posOpt = args.Item(0)
		}
		opts := mergeOpts(mrb, posOpt, mrb.KeywordArgs().Value())
		if blk.IsNil() {
			if !opts.IsNil() {
				return mrb.Call(self, "to_enum", mrb.Intern("each").Value(), opts)
			}
			return mrb.Call(self, "to_enum", mrb.Intern("each").Value())
		}

		asArray := r.asArray
		symKeys := r.symbolizeKeys
		cast := r.cast
		castBools := r.castBooleans
		if !opts.IsNil() && opts.IsHash() {
			if v := hashGetOpt(mrb, opts, "as"); !v.IsNil() {
				s := ""
				if v.IsSymbol() {
					s = mrb.SymName(v.Symbol())
				} else if v.IsString() {
					s = v.String()
				}
				if s == "array" {
					asArray = true
				} else if s == "hash" {
					asArray = false
				}
			}
			if v := hashGetOpt(mrb, opts, "symbolize_keys"); !v.IsNil() {
				symKeys = v.Bool()
			}
			if v := hashGetOpt(mrb, opts, "cast"); !v.IsNil() {
				cast = v.Bool()
			}
			if v := hashGetOpt(mrb, opts, "cast_booleans"); !v.IsNil() {
				castBools = v.Bool()
			}
		}

		for _, row := range r.rows {
			var rowVal oruby.Value
			if asArray {
				ary := mrb.AryNewCapa(len(r.cols))
				for i, col := range r.cols {
					mrb.AryPush(ary, mysqlValToRuby(mrb, col, row[i], cast, castBools))
				}
				rowVal = ary.Value()
			} else {
				h := mrb.HashNewCapa(len(r.cols))
				for i, col := range r.cols {
					var k oruby.Value
					if symKeys {
						k = mrb.Intern(col.name).Value()
					} else {
						k = mrb.StrNew(col.name)
					}
					mrb.HashSet(h, k, mysqlValToRuby(mrb, col, row[i], cast, castBools))
				}
				rowVal = h.Value()
			}
			if _, err := mrb.YieldE(blk, rowVal); err != nil {
				return mrb.RaiseError(err)
			}
		}
		return self
	}, mrb.ArgsOpt(1)|mrb.ArgsBlock())
}

// ---------------------------------------------------------------------------
// Mysql2::Statement
// ---------------------------------------------------------------------------

type mysqlStmt struct {
	client       *mysqlClient
	stmt         *sql.Stmt
	sql          string
	params       int
	closed       bool
	lastID       int64
	affectedRows int64
}

func getStmt(mrb *oruby.MrbState, self oruby.Value) *mysqlStmt {
	s, _ := rb.Get[*mysqlStmt](mrb, self)
	return s
}

func rubyToGoArg(mrb *oruby.MrbState, v oruby.Value) any {
	switch v.Type() {
	case oruby.MrbTTFalse:
		if v.IsNil() {
			return nil
		}
		return false
	case oruby.MrbTTTrue:
		return true
	case oruby.MrbTTInteger:
		return v.Int64()
	case oruby.MrbTTFloat:
		return v.Float64()
	case oruby.MrbTTString:
		if oruby.StrBinary(v) {
			return append([]byte(nil), v.Bytes()...)
		}
		return v.String()
	default:
		return mrb.String(v)
	}
}

func defineStatement(mrb *oruby.MrbState, mod oruby.RClass) {
	cls := mrb.DefineClassUnder(mod, "Statement", mrb.ObjectClass())
	rb.DataClass(cls)

	cls.DefineMethod("param_count", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.FixnumValue(getStmt(mrb, self).params)
	}, mrb.ArgsNone())

	cls.DefineMethod("last_id", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.Int64Value(getStmt(mrb, self).lastID)
	}, mrb.ArgsNone())

	cls.DefineMethod("affected_rows", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.Int64Value(getStmt(mrb, self).affectedRows)
	}, mrb.ArgsNone())

	cls.DefineMethod("close", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		st := getStmt(mrb, self)
		if !st.closed && st.stmt != nil {
			st.closed = true
			_ = st.stmt.Close()
		}
		return mrb.NilValue()
	}, mrb.ArgsNone())

	cls.DefineMethod("closed?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.BoolValue(getStmt(mrb, self).closed)
	}, mrb.ArgsNone())

	cls.DefineMethod("execute", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		st := getStmt(mrb, self)
		if st.closed || st.stmt == nil {
			return raiseMySQLError(mrb, errors.New("statement is closed"), false)
		}
		args := mrb.GetArgs()
		kw := mrb.KeywordArgs().Value()

		n := args.Len()
		goArgs := make([]any, n)
		for i := 0; i < n; i++ {
			goArgs[i] = rubyToGoArg(mrb, args.Item(i))
		}
		asArray, symKeys, cast, castBools := st.client.resolveQueryOpts(mrb, kw)
		ctx := mrb.GoContext()

		if isNonRowQuery(st.sql) {
			res, err := st.stmt.ExecContext(ctx, goArgs...)
			if err != nil {
				return raiseMySQLError(mrb, err, false)
			}
			if id, err := res.LastInsertId(); err == nil {
				st.lastID = id
				st.client.lastID = id
			}
			if aff, err := res.RowsAffected(); err == nil {
				st.affectedRows = aff
				st.client.affectedRows = aff
			}
			return mrb.NilValue()
		}

		rows, err := st.stmt.QueryContext(ctx, goArgs...)
		if err != nil {
			return raiseMySQLError(mrb, err, false)
		}
		defer rows.Close()

		res, err := readMySQLRows(rows, asArray, symKeys, cast, castBools)
		if err != nil {
			return raiseMySQLError(mrb, err, false)
		}
		st.affectedRows = int64(len(res.rows))
		st.client.affectedRows = st.affectedRows
		resCls := rb.ClassPath(mrb, "Mysql2::Result")
		return mrb.DataWrapInterface(resCls, res).Value()
	}, mrb.ArgsAny())
}
