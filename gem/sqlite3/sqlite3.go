// Package sqlite3 implements Ruby's standard sqlite3 gem API backed by
// github.com/mattn/go-sqlite3.
package sqlite3

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	gosqlite3 "github.com/mattn/go-sqlite3"
	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/internal/rb"
)

func init() {
	oruby.LazyGem("sqlite3", initSQLite3)
	oruby.GemAlias("sqlite3/sqlite3_native", "sqlite3")
}

type sqliteDB struct {
	sqlDB         *sql.DB
	conn          *sql.Conn
	rawConn       *gosqlite3.SQLiteConn
	filename      string
	readonly      bool
	resultsAsHash bool
	busyTimeout   int
	closed        bool
	lastErrCode   int
	lastErrMsg    string
}

type sqliteStmt struct {
	db          *sqliteDB
	query       string
	stmt        *sql.Stmt
	closed      bool
	boundPos    map[int]any    // 1-based positional binds
	boundNamed  map[string]any // named binds (without prefix)
	activeRows  *sql.Rows
	activeCols  []string
	activeTypes []string
	done        bool
}

type sqliteResultSet struct {
	db     *sqliteDB
	stmt   *sqliteStmt
	rows   *sql.Rows
	cols   []string
	types  []string
	closed bool
	eof    bool
	ownRS  bool
	nexted bool
	curRow []any
}

var errCodeToClass = map[gosqlite3.ErrNo]string{
	gosqlite3.ErrError:      "SQLException",
	gosqlite3.ErrInternal:   "InternalException",
	gosqlite3.ErrPerm:       "PermissionException",
	gosqlite3.ErrAbort:      "AbortException",
	gosqlite3.ErrBusy:       "BusyException",
	gosqlite3.ErrLocked:     "LockedException",
	gosqlite3.ErrNomem:      "MemoryException",
	gosqlite3.ErrReadonly:   "ReadOnlyException",
	gosqlite3.ErrInterrupt:  "InterruptException",
	gosqlite3.ErrIoErr:      "IOException",
	gosqlite3.ErrCorrupt:    "CorruptException",
	gosqlite3.ErrNotFound:   "NotFoundException",
	gosqlite3.ErrFull:       "FullException",
	gosqlite3.ErrCantOpen:   "CantOpenException",
	gosqlite3.ErrProtocol:   "ProtocolException",
	gosqlite3.ErrEmpty:      "EmptyException",
	gosqlite3.ErrSchema:     "SchemaChangedException",
	gosqlite3.ErrTooBig:     "TooBigException",
	gosqlite3.ErrConstraint: "ConstraintException",
	gosqlite3.ErrMismatch:   "MismatchException",
	gosqlite3.ErrMisuse:     "MisuseException",
	gosqlite3.ErrNoLFS:      "UnsupportedException",
	gosqlite3.ErrAuth:       "AuthorizationException",
	gosqlite3.ErrFormat:     "FormatException",
	gosqlite3.ErrRange:      "RangeException",
	gosqlite3.ErrNotADB:     "NotADatabaseException",
}

func initSQLite3(mrb *oruby.MrbState) any {
	mod := mrb.DefineModule("SQLite3")
	libVer, libVerNum, _ := gosqlite3.Version()
	mrb.ConstSet(mod, mrb.Intern("VERSION"), mrb.StrNew("2.6.0"))
	mrb.ConstSet(mod, mrb.Intern("SQLITE_VERSION"), mrb.StrNew(libVer))
	mrb.ConstSet(mod, mrb.Intern("SQLITE_VERSION_NUMBER"), mrb.FixnumValue(libVerNum))
	mrb.ConstSet(mod, mrb.Intern("SQLITE_LOADED_VERSION"), mrb.StrNew(libVer))

	mod.DefineClassMethod("libversion", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.FixnumValue(libVerNum)
	}, mrb.ArgsNone())
	mod.DefineClassMethod("sqlcipher?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.FalseValue()
	}, mrb.ArgsNone())
	mod.DefineClassMethod("threadsafe?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.TrueValue()
	}, mrb.ArgsNone())
	mod.DefineClassMethod("threadsafe", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.FixnumValue(1)
	}, mrb.ArgsNone())

	// Exception hierarchy
	excBase := mrb.DefineClassUnder(mod, "Exception", mrb.ExcGet("StandardError"))
	excBase.DefineMethod("code", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		v := mrb.IVGet(self, mrb.Intern("@code"))
		if v.IsNil() {
			return mrb.FixnumValue(0)
		}
		return v
	}, mrb.ArgsNone())

	for _, name := range []string{
		"SQLException", "InternalException", "PermissionException", "AbortException",
		"BusyException", "LockedException", "MemoryException", "ReadOnlyException",
		"InterruptException", "IOException", "CorruptException", "NotFoundException",
		"FullException", "CantOpenException", "ProtocolException", "EmptyException",
		"SchemaChangedException", "TooBigException", "ConstraintException",
		"MismatchException", "MisuseException", "UnsupportedException",
		"AuthorizationException", "FormatException", "RangeException",
		"NotADatabaseException", "BusyTimeoutException",
	} {
		mrb.DefineClassUnder(mod, name, excBase)
	}

	// SQLite3::Blob < String
	mrb.DefineClassUnder(mod, "Blob", mrb.StringClass())

	// SQLite3::Database
	dbCls := rb.DataClass(mrb.DefineClassUnder(mod, "Database", mrb.ObjectClass()))
	dbCls.DefineClassMethod("open", dbOpen, mrb.ArgsArg(1, 1)|mrb.ArgsBlock())
	dbCls.DefineClassMethod("quote", dbQuote, mrb.ArgsReq(1))
	dbCls.DefineMethod("initialize", dbInitialize, mrb.ArgsArg(1, 1)|mrb.ArgsBlock())
	dbCls.DefineMethod("close", dbClose, mrb.ArgsNone())
	dbCls.DefineMethod("closed?", dbClosedP, mrb.ArgsNone())
	dbCls.DefineMethod("readonly?", dbReadonlyP, mrb.ArgsNone())
	dbCls.DefineMethod("filename", dbFilename, mrb.ArgsOpt(1))
	dbCls.DefineMethod("encoding", dbEncoding, mrb.ArgsNone())
	dbCls.DefineMethod("results_as_hash", dbResultsAsHash, mrb.ArgsNone())
	dbCls.DefineMethod("results_as_hash=", dbSetResultsAsHash, mrb.ArgsReq(1))
	dbCls.DefineMethod("busy_timeout", dbBusyTimeout, mrb.ArgsNone())
	dbCls.DefineMethod("busy_timeout=", dbSetBusyTimeout, mrb.ArgsReq(1))
	dbCls.DefineMethod("execute", dbExecute, mrb.ArgsReq(1)|mrb.ArgsRest()|mrb.ArgsBlock())
	dbCls.DefineMethod("execute2", dbExecute2, mrb.ArgsReq(1)|mrb.ArgsRest()|mrb.ArgsBlock())
	dbCls.DefineMethod("execute_batch", dbExecuteBatch, mrb.ArgsReq(1)|mrb.ArgsRest())
	dbCls.DefineMethod("execute_batch2", dbExecuteBatch2, mrb.ArgsReq(1)|mrb.ArgsRest()|mrb.ArgsBlock())
	dbCls.DefineMethod("query", dbQuery, mrb.ArgsReq(1)|mrb.ArgsRest()|mrb.ArgsBlock())
	dbCls.DefineMethod("get_first_row", dbGetFirstRow, mrb.ArgsReq(1)|mrb.ArgsRest())
	dbCls.DefineMethod("get_first_value", dbGetFirstValue, mrb.ArgsReq(1)|mrb.ArgsRest())
	dbCls.DefineMethod("prepare", dbPrepare, mrb.ArgsReq(1)|mrb.ArgsBlock())
	dbCls.DefineMethod("transaction", dbTransaction, mrb.ArgsOpt(1)|mrb.ArgsBlock())
	dbCls.DefineMethod("transaction_active?", dbTransactionActiveP, mrb.ArgsNone())
	dbCls.DefineMethod("last_insert_row_id", dbLastInsertRowID, mrb.ArgsNone())
	dbCls.DefineMethod("changes", dbChanges, mrb.ArgsNone())
	dbCls.DefineMethod("total_changes", dbTotalChanges, mrb.ArgsNone())
	dbCls.DefineMethod("errcode", dbErrcode, mrb.ArgsNone())
	dbCls.DefineMethod("errmsg", dbErrmsg, mrb.ArgsNone())
	dbCls.DefineMethod("create_function", dbCreateFunction, mrb.ArgsArg(2, 1)|mrb.ArgsBlock())

	// SQLite3::Statement
	stmtCls := rb.DataClass(mrb.DefineClassUnder(mod, "Statement", mrb.ObjectClass()))
	mrb.IncludeModule(stmtCls, mrb.ModuleGet("Enumerable"))
	stmtCls.DefineMethod("bind_params", stmtBindParams, mrb.ArgsAny())
	stmtCls.DefineMethod("bind_param", stmtBindParam, mrb.ArgsReq(2))
	stmtCls.DefineMethod("clear_bindings!", stmtClearBindings, mrb.ArgsNone())
	stmtCls.DefineMethod("execute", stmtExecute, mrb.ArgsAny()|mrb.ArgsBlock())
	stmtCls.DefineMethod("execute!", stmtExecuteBang, mrb.ArgsAny()|mrb.ArgsBlock())
	stmtCls.DefineMethod("step", stmtStep, mrb.ArgsNone())
	stmtCls.DefineMethod("each", stmtEach, mrb.ArgsBlock())
	stmtCls.DefineMethod("reset!", stmtReset, mrb.ArgsNone())
	stmtCls.DefineMethod("close", stmtClose, mrb.ArgsNone())
	stmtCls.DefineMethod("closed?", stmtClosedP, mrb.ArgsNone())
	stmtCls.DefineMethod("active?", stmtActiveP, mrb.ArgsNone())
	stmtCls.DefineMethod("done?", stmtDoneP, mrb.ArgsNone())
	stmtCls.DefineMethod("columns", stmtColumns, mrb.ArgsNone())
	stmtCls.DefineMethod("types", stmtTypes, mrb.ArgsNone())

	// SQLite3::ResultSet
	rsCls := rb.DataClass(mrb.DefineClassUnder(mod, "ResultSet", mrb.ObjectClass()))
	mrb.IncludeModule(rsCls, mrb.ModuleGet("Enumerable"))
	rsCls.DefineMethod("each", rsEach, mrb.ArgsBlock())
	rsCls.DefineMethod("each_hash", rsEachHash, mrb.ArgsBlock())
	rsCls.DefineMethod("next", rsNext, mrb.ArgsNone())
	rsCls.DefineMethod("next_hash", rsNextHash, mrb.ArgsNone())
	rsCls.DefineMethod("columns", rsColumns, mrb.ArgsNone())
	rsCls.DefineMethod("types", rsTypes, mrb.ArgsNone())
	rsCls.DefineMethod("eof?", rsEofP, mrb.ArgsNone())
	rsCls.DefineMethod("close", rsClose, mrb.ArgsNone())
	rsCls.DefineMethod("closed?", rsClosedP, mrb.ArgsNone())

	return nil
}

func raiseSQLiteError(mrb *oruby.MrbState, db *sqliteDB, err error) oruby.MrbValue {
	clsName := "Exception"
	code := 0
	var se gosqlite3.Error
	if errors.As(err, &se) {
		code = int(se.Code)
		if name, ok := errCodeToClass[se.Code]; ok {
			clsName = name
		}
	}
	if db != nil {
		db.lastErrCode = code
		db.lastErrMsg = err.Error()
	}
	cls := rb.ClassPath(mrb, "SQLite3::"+clsName)
	exc := mrb.ExcNew(cls, err.Error())
	mrb.IVSet(exc, mrb.Intern("@code"), mrb.FixnumValue(code))
	mrb.ExcRaise(exc)
	return mrb.NilValue()
}

func getDB(mrb *oruby.MrbState, self oruby.Value) *sqliteDB {
	d, ok := rb.Get[*sqliteDB](mrb, self)
	if !ok || d == nil {
		rb.Raise(mrb, "SQLite3::Exception", "uninitialized SQLite3::Database")
		return nil
	}
	return d
}

func requireOpenDB(mrb *oruby.MrbState, self oruby.Value) *sqliteDB {
	d := getDB(mrb, self)
	if d == nil {
		return nil
	}
	if d.closed {
		rb.Raise(mrb, "SQLite3::Exception", "cannot use a closed database")
		return nil
	}
	return d
}

func dbQuote(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s := mrb.String(mrb.GetArgsFirst())
	return mrb.StrNew(strings.ReplaceAll(s, "'", "''"))
}

func dbOpen(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args, blk := mrb.GetArgsWithBlock()
	argv := make([]any, 0, args.Len()+1)
	for i := 0; i < args.Len(); i++ {
		argv = append(argv, args.Item(i))
	}
	if kw := mrb.KeywordArgs().Value(); !kw.IsNil() && kw.IsHash() && mrb.HashKeys(kw).Len() > 0 {
		argv = append(argv, kw)
	}
	dbVal := mrb.Call(self, "new", argv...)
	if mrb.HasExc() {
		return dbVal
	}
	if blk.IsNil() {
		return dbVal
	}
	res := mrb.Yield(blk, dbVal)
	exc := mrb.Exc()
	if exc != nil && !exc.Value().IsNil() {
		mrb.ExcClear()
	}
	if d, ok := rb.Get[*sqliteDB](mrb, dbVal); ok && !d.closed {
		_ = d.close()
	}
	if exc != nil && !exc.Value().IsNil() {
		mrb.ExcRaise(exc.Value())
		return mrb.NilValue()
	}
	return res
}

func (d *sqliteDB) close() error {
	if d.closed {
		return nil
	}
	d.closed = true
	var err1, err2 error
	if d.conn != nil {
		err1 = d.conn.Close()
		d.conn = nil
		d.rawConn = nil
	}
	if d.sqlDB != nil {
		err2 = d.sqlDB.Close()
		d.sqlDB = nil
	}
	if err1 != nil {
		return err1
	}
	return err2
}

func dbInitialize(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args, blk := mrb.GetArgsWithBlock()
	if args.Len() < 1 {
		return mrb.Raise(mrb.EArgumentError(), "wrong number of arguments (given 0, expected 1..2)")
	}
	fnameVal := args.Item(0)
	if !fnameVal.IsString() {
		if mrb.RespondTo(fnameVal, mrb.Intern("to_path")) {
			fnameVal = mrb.Call(fnameVal, "to_path")
			if mrb.HasExc() {
				return mrb.NilValue()
			}
		} else if mrb.RespondTo(fnameVal, oruby.Sym_to_str) {
			fnameVal = mrb.Call(fnameVal, "to_str")
			if mrb.HasExc() {
				return mrb.NilValue()
			}
		} else {
			return mrb.ETypeError().Raisef("no implicit conversion of %v into String", mrb.ClassOf(fnameVal).Name())
		}
	}
	filename := mrb.String(fnameVal)

	readonly := false
	resultsAsHash := false
	busyTimeout := 0

	if args.Len() >= 2 && args.Item(1).IsHash() {
		opts := args.Item(1)
		if v := hashOpt(mrb, opts, "readonly"); !v.IsNil() {
			readonly = v.Bool()
		}
		if v := hashOpt(mrb, opts, "results_as_hash"); !v.IsNil() {
			resultsAsHash = v.Bool()
		}
		if v := hashOpt(mrb, opts, "busy_timeout"); !v.IsNil() {
			busyTimeout = v.Int()
		}
	}
	if kw := mrb.KeywordArgs().Value(); !kw.IsNil() && kw.IsHash() {
		if v := hashOpt(mrb, kw, "readonly"); !v.IsNil() {
			readonly = v.Bool()
		}
		if v := hashOpt(mrb, kw, "results_as_hash"); !v.IsNil() {
			resultsAsHash = v.Bool()
		}
		if v := hashOpt(mrb, kw, "busy_timeout"); !v.IsNil() {
			busyTimeout = v.Int()
		}
	}

	dsn := buildDSN(filename, readonly, busyTimeout)
	sdb, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return raiseSQLiteError(mrb, nil, err)
	}
	sdb.SetMaxOpenConns(1)
	sdb.SetMaxIdleConns(1)

	ctx := context.Background()
	conn, err := sdb.Conn(ctx)
	if err != nil {
		_ = sdb.Close()
		return raiseSQLiteError(mrb, nil, err)
	}

	var rawConn *gosqlite3.SQLiteConn
	err = conn.Raw(func(driverConn any) error {
		if sc, ok := driverConn.(*gosqlite3.SQLiteConn); ok {
			rawConn = sc
		}
		return nil
	})
	if err != nil {
		_ = conn.Close()
		_ = sdb.Close()
		return raiseSQLiteError(mrb, nil, err)
	}

	d := &sqliteDB{
		sqlDB:         sdb,
		conn:          conn,
		rawConn:       rawConn,
		filename:      filename,
		readonly:      readonly,
		resultsAsHash: resultsAsHash,
		busyTimeout:   busyTimeout,
	}
	rb.Set(mrb, self, d)

	if !blk.IsNil() {
		mrb.Yield(blk, self)
		exc := mrb.Exc()
		if exc != nil && !exc.Value().IsNil() {
			mrb.ExcClear()
		}
		_ = d.close()
		if exc != nil && !exc.Value().IsNil() {
			mrb.ExcRaise(exc.Value())
			return mrb.NilValue()
		}
	}
	return self
}

func hashOpt(mrb *oruby.MrbState, h oruby.Value, name string) oruby.Value {
	if v, err := mrb.Funcall(h, mrb.Intern("[]"), mrb.SymbolValue(mrb.Intern(name))); err == nil && !v.IsNil() {
		return v
	}
	if v, err := mrb.Funcall(h, mrb.Intern("[]"), mrb.StrNew(name)); err == nil {
		return v
	}
	return mrb.NilValue()
}

func buildDSN(filename string, readonly bool, busyTimeout int) string {
	if filename == ":memory:" || filename == "" {
		if busyTimeout > 0 {
			return fmt.Sprintf(":memory:?_busy_timeout=%d", busyTimeout)
		}
		return ":memory:"
	}
	q := url.Values{}
	if readonly {
		q.Set("mode", "ro")
	}
	if busyTimeout > 0 {
		q.Set("_busy_timeout", strconv.Itoa(busyTimeout))
	}
	if len(q) == 0 {
		return filename
	}
	if strings.HasPrefix(filename, "file:") {
		if strings.Contains(filename, "?") {
			return filename + "&" + q.Encode()
		}
		return filename + "?" + q.Encode()
	}
	return "file:" + filename + "?" + q.Encode()
}

func dbClose(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	d := getDB(mrb, self)
	if d == nil {
		return mrb.NilValue()
	}
	if err := d.close(); err != nil {
		return raiseSQLiteError(mrb, d, err)
	}
	return mrb.NilValue()
}

func dbClosedP(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	d := getDB(mrb, self)
	if d == nil {
		return mrb.TrueValue()
	}
	return mrb.BoolValue(d.closed)
}

func dbReadonlyP(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	d := requireOpenDB(mrb, self)
	if d == nil {
		return mrb.NilValue()
	}
	return mrb.BoolValue(d.readonly)
}

func dbFilename(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	d := requireOpenDB(mrb, self)
	if d == nil {
		return mrb.NilValue()
	}
	schema := "main"
	args := mrb.GetArgs()
	if args.Len() > 0 && !args.Item(0).IsNil() {
		schema = mrb.String(args.Item(0))
	}
	if d.rawConn != nil {
		fn := d.rawConn.GetFilename(schema)
		if fn != "" {
			return mrb.StrNew(fn)
		}
	}
	if d.filename == ":memory:" {
		return mrb.StrNew("")
	}
	return mrb.StrNew(d.filename)
}

func dbEncoding(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	if encCls := mrb.ClassGet("Encoding"); !encCls.IsNil() {
		return mrb.ConstGet(encCls, mrb.Intern("UTF_8"))
	}
	return mrb.StrNew("UTF-8")
}

func dbResultsAsHash(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	d := getDB(mrb, self)
	if d == nil {
		return mrb.NilValue()
	}
	return mrb.BoolValue(d.resultsAsHash)
}

func dbSetResultsAsHash(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	d := getDB(mrb, self)
	if d == nil {
		return mrb.NilValue()
	}
	v := mrb.GetArgsFirst()
	d.resultsAsHash = v.Bool()
	return v
}

func dbBusyTimeout(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	d := getDB(mrb, self)
	if d == nil {
		return mrb.NilValue()
	}
	return mrb.FixnumValue(d.busyTimeout)
}

func dbSetBusyTimeout(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	d := requireOpenDB(mrb, self)
	if d == nil {
		return mrb.NilValue()
	}
	ms := mrb.GetArgsFirst().Int()
	if _, err := d.conn.ExecContext(context.Background(), fmt.Sprintf("PRAGMA busy_timeout = %d", ms)); err != nil {
		return raiseSQLiteError(mrb, d, err)
	}
	d.busyTimeout = ms
	return mrb.FixnumValue(ms)
}

func dbTransactionActiveP(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	d := requireOpenDB(mrb, self)
	if d == nil {
		return mrb.NilValue()
	}
	if d.rawConn != nil {
		return mrb.BoolValue(!d.rawConn.AutoCommit())
	}
	return mrb.FalseValue()
}

func dbLastInsertRowID(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	d := requireOpenDB(mrb, self)
	if d == nil {
		return mrb.NilValue()
	}
	var id int64
	if err := d.conn.QueryRowContext(context.Background(), "SELECT last_insert_rowid()").Scan(&id); err != nil {
		return raiseSQLiteError(mrb, d, err)
	}
	return mrb.Int64Value(id)
}

func dbChanges(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	d := requireOpenDB(mrb, self)
	if d == nil {
		return mrb.NilValue()
	}
	var n int64
	if err := d.conn.QueryRowContext(context.Background(), "SELECT changes()").Scan(&n); err != nil {
		return raiseSQLiteError(mrb, d, err)
	}
	return mrb.Int64Value(n)
}

func dbTotalChanges(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	d := requireOpenDB(mrb, self)
	if d == nil {
		return mrb.NilValue()
	}
	var n int64
	if err := d.conn.QueryRowContext(context.Background(), "SELECT total_changes()").Scan(&n); err != nil {
		return raiseSQLiteError(mrb, d, err)
	}
	return mrb.Int64Value(n)
}

func dbErrcode(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	d := getDB(mrb, self)
	if d == nil {
		return mrb.FixnumValue(0)
	}
	return mrb.FixnumValue(d.lastErrCode)
}

func dbErrmsg(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	d := getDB(mrb, self)
	if d == nil {
		return mrb.StrNew("")
	}
	return mrb.StrNew(d.lastErrMsg)
}

func rubyToSQL(mrb *oruby.MrbState, v oruby.Value) (any, error) {
	if v.IsNil() {
		return nil, nil
	}
	switch v.Type() {
	case oruby.MrbTTInteger:
		return v.Int64(), nil
	case oruby.MrbTTBigInt:
		bi, err := mrb.BigInt(v)
		if err != nil {
			return nil, err
		}
		if bi.IsInt64() {
			return bi.Int64(), nil
		}
		return bi.String(), nil
	case oruby.MrbTTFloat:
		return v.Float64(), nil
	case oruby.MrbTTTrue:
		return int64(1), nil
	case oruby.MrbTTFalse:
		return int64(0), nil
	case oruby.MrbTTString:
		blobCls := rb.ClassPath(mrb, "SQLite3::Blob")
		if !blobCls.IsNil() && mrb.ObjIsKindOf(v, blobCls) {
			return append([]byte(nil), v.Bytes()...), nil
		}
		enc := mrb.Call(v, "encoding")
		if !mrb.HasExc() && !enc.IsNil() {
			name := mrb.String(mrb.Call(enc, "name"))
			if name == "ASCII-8BIT" {
				return append([]byte(nil), v.Bytes()...), nil
			}
		}
		return mrb.String(v), nil
	case oruby.MrbTTSymbol:
		return mrb.SymString(v.Symbol()), nil
	default:
		return nil, fmt.Errorf("can't prepare %s", mrb.ClassOf(v).Name())
	}
}

func sqlToRuby(mrb *oruby.MrbState, v any) oruby.Value {
	if v == nil {
		return mrb.NilValue()
	}
	switch x := v.(type) {
	case int64:
		return mrb.Int64Value(x)
	case float64:
		return mrb.FloatValue(x)
	case bool:
		if x {
			return mrb.FixnumValue(1)
		}
		return mrb.FixnumValue(0)
	case string:
		return mrb.StrNew(x)
	case []byte:
		s := mrb.BytesValue(x)
		return mrb.Call(s, "b")
	default:
		return mrb.StrNew(fmt.Sprint(x))
	}
}

func extractBindArgs(mrb *oruby.MrbState, args oruby.RArgs, startIdx int) ([]any, error) {
	n := args.Len() - startIdx
	kw := mrb.KeywordArgs().Value()
	hasKw := !kw.IsNil() && kw.IsHash() && mrb.HashKeys(kw).Len() > 0
	if n <= 0 && !hasKw {
		return nil, nil
	}
	var out []any
	if n == 1 {
		v := args.Item(startIdx)
		if v.IsArray() {
			alen := oruby.RArrayLen(v)
			out = make([]any, alen)
			for i := 0; i < alen; i++ {
				sv, err := rubyToSQL(mrb, mrb.AryRef(v, i))
				if err != nil {
					return nil, err
				}
				out[i] = sv
			}
		} else if v.IsHash() {
			named, err := extractNamedHash(mrb, v)
			if err != nil {
				return nil, err
			}
			out = append(out, named...)
		} else {
			sv, err := rubyToSQL(mrb, v)
			if err != nil {
				return nil, err
			}
			out = append(out, sv)
		}
	} else if n > 1 {
		out = make([]any, 0, n)
		for i := startIdx; i < args.Len(); i++ {
			v := args.Item(i)
			if i == args.Len()-1 && v.IsHash() {
				named, err := extractNamedHash(mrb, v)
				if err != nil {
					return nil, err
				}
				out = append(out, named...)
				continue
			}
			sv, err := rubyToSQL(mrb, v)
			if err != nil {
				return nil, err
			}
			out = append(out, sv)
		}
	}
	if hasKw {
		named, err := extractNamedHash(mrb, kw)
		if err != nil {
			return nil, err
		}
		out = append(out, named...)
	}
	return out, nil
}

func extractNamedHash(mrb *oruby.MrbState, h oruby.Value) ([]any, error) {
	keys := mrb.HashKeys(h)
	out := make([]any, 0, keys.Len())
	for i := 0; i < keys.Len(); i++ {
		k := keys.Item(i)
		var name string
		if k.IsSymbol() {
			name = mrb.SymString(k.Symbol())
		} else {
			name = mrb.String(k)
		}
		name = strings.TrimLeft(name, ":@$")
		sv, err := rubyToSQL(mrb, mrb.HashGet(h, k))
		if err != nil {
			return nil, err
		}
		out = append(out, sql.Named(name, sv))
	}
	return out, nil
}

func rowToRuby(mrb *oruby.MrbState, cols []string, vals []any, asHash bool) oruby.Value {
	if asHash {
		h := mrb.HashNewCapa(len(cols))
		for i, c := range cols {
			mrb.HashSet(h, mrb.StrNew(c), sqlToRuby(mrb, vals[i]))
		}
		return h.Value()
	}
	ary := mrb.AryNewCapa(len(cols))
	for _, v := range vals {
		ary.Push(sqlToRuby(mrb, v))
	}
	return ary.Value()
}

func scanRowValues(rows *sql.Rows, n int) ([]any, error) {
	vals := make([]any, n)
	ptrs := make([]any, n)
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return nil, err
	}
	return vals, nil
}

func dbExecute(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return dbExecuteCommon(mrb, self, false)
}

func dbExecute2(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return dbExecuteCommon(mrb, self, true)
}

func dbExecuteCommon(mrb *oruby.MrbState, self oruby.Value, includeHeader bool) oruby.MrbValue {
	d := requireOpenDB(mrb, self)
	if d == nil {
		return mrb.NilValue()
	}
	args, blk := mrb.GetArgsWithBlock()
	query := mrb.String(args.Item(0))
	bindArgs, err := extractBindArgs(mrb, args, 1)
	if err != nil {
		return mrb.Raise(mrb.ETypeError(), err.Error())
	}

	rows, err := d.conn.QueryContext(context.Background(), query, bindArgs...)
	if err != nil {
		return raiseSQLiteError(mrb, d, err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return raiseSQLiteError(mrb, d, err)
	}

	var result oruby.RArray
	if blk.IsNil() {
		result = mrb.AryNew()
	}

	if includeHeader && len(cols) > 0 {
		hdr := mrb.AryNewCapa(len(cols))
		for _, c := range cols {
			hdr.Push(mrb.StrNew(c))
		}
		if blk.IsNil() {
			result.Push(hdr)
		} else {
			mrb.Yield(blk, hdr)
			if mrb.HasExc() {
				return mrb.NilValue()
			}
		}
	}

	for rows.Next() {
		ai := mrb.GCArenaSave()
		vals, err := scanRowValues(rows, len(cols))
		if err != nil {
			mrb.GCArenaRestore(ai)
			return raiseSQLiteError(mrb, d, err)
		}
		row := rowToRuby(mrb, cols, vals, d.resultsAsHash)
		if blk.IsNil() {
			result.Push(row)
		} else {
			mrb.Yield(blk, row)
			if mrb.HasExc() {
				mrb.GCArenaRestore(ai)
				return mrb.NilValue()
			}
		}
		mrb.GCArenaRestore(ai)
	}
	if err := rows.Err(); err != nil {
		return raiseSQLiteError(mrb, d, err)
	}
	if blk.IsNil() {
		return result
	}
	return mrb.NilValue()
}

func dbExecuteBatch(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	d := requireOpenDB(mrb, self)
	if d == nil {
		return mrb.NilValue()
	}
	args := mrb.GetArgs()
	query := mrb.String(args.Item(0))
	bindArgs, err := extractBindArgs(mrb, args, 1)
	if err != nil {
		return mrb.Raise(mrb.ETypeError(), err.Error())
	}
	if _, err := d.conn.ExecContext(context.Background(), query, bindArgs...); err != nil {
		return raiseSQLiteError(mrb, d, err)
	}
	return mrb.NilValue()
}

func dbExecuteBatch2(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	d := requireOpenDB(mrb, self)
	if d == nil {
		return mrb.NilValue()
	}
	args, blk := mrb.GetArgsWithBlock()
	query := mrb.String(args.Item(0))
	if _, err := d.conn.ExecContext(context.Background(), query); err != nil {
		return raiseSQLiteError(mrb, d, err)
	}
	if blk.IsNil() {
		return mrb.AryNew()
	}
	return mrb.NilValue()
}

func dbQuery(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	d := requireOpenDB(mrb, self)
	if d == nil {
		return mrb.NilValue()
	}
	args, blk := mrb.GetArgsWithBlock()
	query := mrb.String(args.Item(0))
	bindArgs, err := extractBindArgs(mrb, args, 1)
	if err != nil {
		return mrb.Raise(mrb.ETypeError(), err.Error())
	}
	rows, err := d.conn.QueryContext(context.Background(), query, bindArgs...)
	if err != nil {
		return raiseSQLiteError(mrb, d, err)
	}
	rsVal, err := newResultSetValue(mrb, d, nil, rows, false)
	if err != nil {
		_ = rows.Close()
		return raiseSQLiteError(mrb, d, err)
	}
	if blk.IsNil() {
		return rsVal
	}
	res := mrb.Yield(blk, rsVal)
	exc := mrb.Exc()
	if exc != nil && !exc.Value().IsNil() {
		mrb.ExcClear()
	}
	if rs, ok := rb.Get[*sqliteResultSet](mrb, rsVal); ok && !rs.closed {
		rs.close()
	}
	if exc != nil && !exc.Value().IsNil() {
		mrb.ExcRaise(exc.Value())
		return mrb.NilValue()
	}
	return res
}

func dbGetFirstRow(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	d := requireOpenDB(mrb, self)
	if d == nil {
		return mrb.NilValue()
	}
	args := mrb.GetArgs()
	query := mrb.String(args.Item(0))
	bindArgs, err := extractBindArgs(mrb, args, 1)
	if err != nil {
		return mrb.Raise(mrb.ETypeError(), err.Error())
	}
	rows, err := d.conn.QueryContext(context.Background(), query, bindArgs...)
	if err != nil {
		return raiseSQLiteError(mrb, d, err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return raiseSQLiteError(mrb, d, err)
	}
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return raiseSQLiteError(mrb, d, err)
		}
		return mrb.NilValue()
	}
	vals, err := scanRowValues(rows, len(cols))
	if err != nil {
		return raiseSQLiteError(mrb, d, err)
	}
	return rowToRuby(mrb, cols, vals, d.resultsAsHash)
}

func dbGetFirstValue(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	d := requireOpenDB(mrb, self)
	if d == nil {
		return mrb.NilValue()
	}
	args := mrb.GetArgs()
	query := mrb.String(args.Item(0))
	bindArgs, err := extractBindArgs(mrb, args, 1)
	if err != nil {
		return mrb.Raise(mrb.ETypeError(), err.Error())
	}
	rows, err := d.conn.QueryContext(context.Background(), query, bindArgs...)
	if err != nil {
		return raiseSQLiteError(mrb, d, err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return raiseSQLiteError(mrb, d, err)
	}
	if !rows.Next() || len(cols) == 0 {
		if err := rows.Err(); err != nil {
			return raiseSQLiteError(mrb, d, err)
		}
		return mrb.NilValue()
	}
	vals, err := scanRowValues(rows, len(cols))
	if err != nil {
		return raiseSQLiteError(mrb, d, err)
	}
	return sqlToRuby(mrb, vals[0])
}

func dbPrepare(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	d := requireOpenDB(mrb, self)
	if d == nil {
		return mrb.NilValue()
	}
	args, blk := mrb.GetArgsWithBlock()
	query := mrb.String(args.Item(0))
	st, err := d.conn.PrepareContext(context.Background(), query)
	if err != nil {
		return raiseSQLiteError(mrb, d, err)
	}
	s := &sqliteStmt{
		db:         d,
		query:      query,
		stmt:       st,
		boundPos:   make(map[int]any),
		boundNamed: make(map[string]any),
	}
	stmtCls := rb.ClassPath(mrb, "SQLite3::Statement")
	obj := mrb.DataWrapInterface(stmtCls, s).Value()
	if blk.IsNil() {
		return obj
	}
	res := mrb.Yield(blk, obj)
	exc := mrb.Exc()
	if exc != nil && !exc.Value().IsNil() {
		mrb.ExcClear()
	}
	s.close()
	if exc != nil && !exc.Value().IsNil() {
		mrb.ExcRaise(exc.Value())
		return mrb.NilValue()
	}
	return res
}

func dbTransaction(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	d := requireOpenDB(mrb, self)
	if d == nil {
		return mrb.NilValue()
	}
	args, blk := mrb.GetArgsWithBlock()
	mode := "deferred"
	if args.Len() > 0 && !args.Item(0).IsNil() {
		v := args.Item(0)
		if v.IsSymbol() {
			mode = mrb.SymString(v.Symbol())
		} else {
			mode = mrb.String(v)
		}
	}
	beginSQL := "BEGIN " + strings.ToUpper(mode) + " TRANSACTION"
	if _, err := d.conn.ExecContext(context.Background(), beginSQL); err != nil {
		return raiseSQLiteError(mrb, d, err)
	}
	if blk.IsNil() {
		return mrb.TrueValue()
	}
	res := mrb.Yield(blk, self)
	exc := mrb.Exc()
	if exc != nil && !exc.Value().IsNil() {
		mrb.ExcClear()
		_, _ = d.conn.ExecContext(context.Background(), "ROLLBACK TRANSACTION")
		mrb.ExcRaise(exc.Value())
		return mrb.NilValue()
	}
	if _, err := d.conn.ExecContext(context.Background(), "COMMIT TRANSACTION"); err != nil {
		return raiseSQLiteError(mrb, d, err)
	}
	return res
}

func dbCreateFunction(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	d := requireOpenDB(mrb, self)
	if d == nil {
		return mrb.NilValue()
	}
	args, blk := mrb.GetArgsWithBlock()
	if blk.IsNil() {
		return mrb.Raise(mrb.EArgumentError(), "create_function requires a block")
	}
	name := mrb.String(args.Item(0))
	arity := args.Item(1).Int()
	if d.rawConn == nil {
		return raiseSQLiteError(mrb, d, errors.New("raw SQLite connection unavailable"))
	}

	// Keep the Ruby block reachable on the database object.
	funcs := mrb.IVGet(self, mrb.Intern("@__functions"))
	if funcs.IsNil() {
		funcs = mrb.AryNew().Value()
		mrb.IVSet(self, mrb.Intern("@__functions"), funcs)
	}
	mrb.AryPush(funcs, blk)

	callRuby := func(goArgs ...any) (any, error) {
		rbArgs := make([]any, len(goArgs))
		for i, a := range goArgs {
			rbArgs[i] = sqlToRuby(mrb, a)
		}
		proxyObj, err := mrb.Eval(`Object.new.tap { |o|
			def o.result=(v); @__set = true; @__res = v; end
			def o.result; @__res; end
			def o.__set?; @__set; end
		}`)
		if err != nil {
			return nil, err
		}
		allArgs := append([]any{proxyObj}, rbArgs...)
		ret := mrb.YieldArgv(blk, allArgs...)
		if mrb.HasExc() {
			exc := mrb.Exc()
			mrb.ExcClear()
			return nil, errors.New(mrb.String(mrb.Call(exc.Value(), "message")))
		}
		if mrb.Call(proxyObj, "__set?").Bool() {
			ret = mrb.Call(proxyObj, "result")
		}
		return rubyToSQL(mrb, ret)
	}

	var fn any
	switch arity {
	case 0:
		fn = func() (any, error) { return callRuby() }
	case 1:
		fn = func(a any) (any, error) { return callRuby(a) }
	case 2:
		fn = func(a, b any) (any, error) { return callRuby(a, b) }
	case 3:
		fn = func(a, b, c any) (any, error) { return callRuby(a, b, c) }
	case 4:
		fn = func(a, b, c, e any) (any, error) { return callRuby(a, b, c, e) }
	default:
		fn = func(a ...any) (any, error) { return callRuby(a...) }
	}
	if err := d.rawConn.RegisterFunc(name, fn, false); err != nil {
		return raiseSQLiteError(mrb, d, err)
	}
	return self
}

// ---- Statement ----

func getStmt(mrb *oruby.MrbState, self oruby.Value) *sqliteStmt {
	s, ok := rb.Get[*sqliteStmt](mrb, self)
	if !ok || s == nil {
		rb.Raise(mrb, "SQLite3::Exception", "uninitialized SQLite3::Statement")
		return nil
	}
	return s
}

func requireOpenStmt(mrb *oruby.MrbState, self oruby.Value) *sqliteStmt {
	s := getStmt(mrb, self)
	if s == nil {
		return nil
	}
	if s.closed {
		rb.Raise(mrb, "SQLite3::Exception", "cannot use a closed statement")
		return nil
	}
	return s
}

func (s *sqliteStmt) close() {
	if s.closed {
		return
	}
	s.closed = true
	if s.activeRows != nil {
		_ = s.activeRows.Close()
		s.activeRows = nil
	}
	if s.stmt != nil {
		_ = s.stmt.Close()
		s.stmt = nil
	}
}

func (s *sqliteStmt) collectBoundArgs() []any {
	maxPos := 0
	for k := range s.boundPos {
		if k > maxPos {
			maxPos = k
		}
	}
	out := make([]any, 0, maxPos+len(s.boundNamed))
	for i := 1; i <= maxPos; i++ {
		out = append(out, s.boundPos[i])
	}
	for k, v := range s.boundNamed {
		out = append(out, sql.Named(k, v))
	}
	return out
}

func stmtBindParams(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s := requireOpenStmt(mrb, self)
	if s == nil {
		return mrb.NilValue()
	}
	args := mrb.GetArgs()
	idx := 1
	for i := 0; i < args.Len(); i++ {
		v := args.Item(i)
		if v.IsHash() {
			keys := mrb.HashKeys(v)
			for j := 0; j < keys.Len(); j++ {
				k := keys.Item(j)
				var name string
				if k.IsSymbol() {
					name = mrb.SymString(k.Symbol())
				} else {
					name = mrb.String(k)
				}
				sv, err := rubyToSQL(mrb, mrb.HashGet(v, k))
				if err != nil {
					return mrb.Raise(mrb.ETypeError(), err.Error())
				}
				s.boundNamed[strings.TrimLeft(name, ":@$")] = sv
			}
			continue
		}
		if v.IsArray() && args.Len() == 1 {
			alen := oruby.RArrayLen(v)
			for j := 0; j < alen; j++ {
				sv, err := rubyToSQL(mrb, mrb.AryRef(v, j))
				if err != nil {
					return mrb.Raise(mrb.ETypeError(), err.Error())
				}
				s.boundPos[idx] = sv
				idx++
			}
			continue
		}
		sv, err := rubyToSQL(mrb, v)
		if err != nil {
			return mrb.Raise(mrb.ETypeError(), err.Error())
		}
		s.boundPos[idx] = sv
		idx++
	}
	if kw := mrb.KeywordArgs().Value(); !kw.IsNil() && kw.IsHash() {
		keys := mrb.HashKeys(kw)
		for j := 0; j < keys.Len(); j++ {
			k := keys.Item(j)
			var name string
			if k.IsSymbol() {
				name = mrb.SymString(k.Symbol())
			} else {
				name = mrb.String(k)
			}
			sv, err := rubyToSQL(mrb, mrb.HashGet(kw, k))
			if err != nil {
				return mrb.Raise(mrb.ETypeError(), err.Error())
			}
			s.boundNamed[strings.TrimLeft(name, ":@$")] = sv
		}
	}
	return self
}

func stmtBindParam(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s := requireOpenStmt(mrb, self)
	if s == nil {
		return mrb.NilValue()
	}
	args := mrb.GetArgs()
	param := args.Item(0)
	sv, err := rubyToSQL(mrb, args.Item(1))
	if err != nil {
		return mrb.Raise(mrb.ETypeError(), err.Error())
	}
	if param.Type() == oruby.MrbTTInteger {
		s.boundPos[param.Int()] = sv
	} else {
		var name string
		if param.IsSymbol() {
			name = mrb.SymString(param.Symbol())
		} else {
			name = mrb.String(param)
		}
		s.boundNamed[strings.TrimLeft(name, ":@$")] = sv
	}
	return self
}

func stmtClearBindings(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s := requireOpenStmt(mrb, self)
	if s == nil {
		return mrb.NilValue()
	}
	s.boundPos = make(map[int]any)
	s.boundNamed = make(map[string]any)
	return self
}

func stmtExecute(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s := requireOpenStmt(mrb, self)
	if s == nil {
		return mrb.NilValue()
	}
	args, blk := mrb.GetArgsWithBlock()
	var bindArgs []any
	if args.Len() > 0 {
		var err error
		bindArgs, err = extractBindArgs(mrb, args, 0)
		if err != nil {
			return mrb.Raise(mrb.ETypeError(), err.Error())
		}
	} else {
		bindArgs = s.collectBoundArgs()
	}
	if s.activeRows != nil {
		_ = s.activeRows.Close()
		s.activeRows = nil
	}
	s.done = false
	rows, err := s.stmt.QueryContext(context.Background(), bindArgs...)
	if err != nil {
		return raiseSQLiteError(mrb, s.db, err)
	}
	rsVal, err := newResultSetValue(mrb, s.db, s, rows, false)
	if err != nil {
		_ = rows.Close()
		return raiseSQLiteError(mrb, s.db, err)
	}
	if blk.IsNil() {
		return rsVal
	}
	res := mrb.Yield(blk, rsVal)
	exc := mrb.Exc()
	if exc != nil && !exc.Value().IsNil() {
		mrb.ExcClear()
	}
	if rs, ok := rb.Get[*sqliteResultSet](mrb, rsVal); ok && !rs.closed {
		rs.close()
	}
	if exc != nil && !exc.Value().IsNil() {
		mrb.ExcRaise(exc.Value())
		return mrb.NilValue()
	}
	return res
}

func stmtExecuteBang(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s := requireOpenStmt(mrb, self)
	if s == nil {
		return mrb.NilValue()
	}
	args, blk := mrb.GetArgsWithBlock()
	var bindArgs []any
	if args.Len() > 0 {
		var err error
		bindArgs, err = extractBindArgs(mrb, args, 0)
		if err != nil {
			return mrb.Raise(mrb.ETypeError(), err.Error())
		}
	} else {
		bindArgs = s.collectBoundArgs()
	}
	rows, err := s.stmt.QueryContext(context.Background(), bindArgs...)
	if err != nil {
		return raiseSQLiteError(mrb, s.db, err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return raiseSQLiteError(mrb, s.db, err)
	}
	var out oruby.RArray
	if blk.IsNil() {
		out = mrb.AryNew()
	}
	for rows.Next() {
		vals, err := scanRowValues(rows, len(cols))
		if err != nil {
			return raiseSQLiteError(mrb, s.db, err)
		}
		row := rowToRuby(mrb, cols, vals, s.db.resultsAsHash)
		if blk.IsNil() {
			out.Push(row)
		} else {
			mrb.Yield(blk, row)
			if mrb.HasExc() {
				return mrb.NilValue()
			}
		}
	}
	if err := rows.Err(); err != nil {
		return raiseSQLiteError(mrb, s.db, err)
	}
	if blk.IsNil() {
		return out
	}
	return mrb.NilValue()
}

func stmtStep(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s := requireOpenStmt(mrb, self)
	if s == nil {
		return mrb.NilValue()
	}
	if s.done {
		return mrb.NilValue()
	}
	if s.activeRows == nil {
		rows, err := s.stmt.QueryContext(context.Background(), s.collectBoundArgs()...)
		if err != nil {
			return raiseSQLiteError(mrb, s.db, err)
		}
		cols, err := rows.Columns()
		if err != nil {
			_ = rows.Close()
			return raiseSQLiteError(mrb, s.db, err)
		}
		s.activeRows = rows
		s.activeCols = cols
		s.activeTypes = columnTypeNames(rows)
	}
	if !s.activeRows.Next() {
		err := s.activeRows.Err()
		_ = s.activeRows.Close()
		s.activeRows = nil
		s.done = true
		if err != nil {
			return raiseSQLiteError(mrb, s.db, err)
		}
		return mrb.NilValue()
	}
	vals, err := scanRowValues(s.activeRows, len(s.activeCols))
	if err != nil {
		return raiseSQLiteError(mrb, s.db, err)
	}
	return rowToRuby(mrb, s.activeCols, vals, s.db.resultsAsHash)
}

func stmtEach(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	blk := mrb.GetArgsBlock()
	if blk.IsNil() {
		return mrb.Call(self, "to_enum", mrb.SymbolValue(mrb.Intern("each")))
	}
	for {
		row := stmtStep(mrb, self)
		if mrb.HasExc() {
			return mrb.NilValue()
		}
		if row.Value().IsNil() {
			break
		}
		mrb.Yield(blk, row)
		if mrb.HasExc() {
			return mrb.NilValue()
		}
	}
	return self
}

func stmtReset(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s := requireOpenStmt(mrb, self)
	if s == nil {
		return mrb.NilValue()
	}
	if s.activeRows != nil {
		_ = s.activeRows.Close()
		s.activeRows = nil
	}
	s.done = false
	return self
}

func stmtClose(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s := getStmt(mrb, self)
	if s == nil {
		return mrb.NilValue()
	}
	s.close()
	return mrb.NilValue()
}

func stmtClosedP(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s := getStmt(mrb, self)
	if s == nil {
		return mrb.TrueValue()
	}
	return mrb.BoolValue(s.closed)
}

func stmtActiveP(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s := getStmt(mrb, self)
	if s == nil {
		return mrb.FalseValue()
	}
	return mrb.BoolValue(!s.closed && s.activeRows != nil && !s.done)
}

func stmtDoneP(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s := getStmt(mrb, self)
	if s == nil {
		return mrb.TrueValue()
	}
	return mrb.BoolValue(s.done)
}

func stmtColumns(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s := requireOpenStmt(mrb, self)
	if s == nil {
		return mrb.NilValue()
	}
	if s.activeCols != nil {
		return stringsToAry(mrb, s.activeCols)
	}
	return mrb.AryNew()
}

func stmtTypes(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s := requireOpenStmt(mrb, self)
	if s == nil {
		return mrb.NilValue()
	}
	if s.activeTypes != nil {
		return stringsToAry(mrb, s.activeTypes)
	}
	return mrb.AryNew()
}

// ---- ResultSet ----

func columnTypeNames(rows *sql.Rows) []string {
	cts, err := rows.ColumnTypes()
	if err != nil {
		return nil
	}
	out := make([]string, len(cts))
	for i, ct := range cts {
		out[i] = ct.DatabaseTypeName()
	}
	return out
}

func stringsToAry(mrb *oruby.MrbState, ss []string) oruby.Value {
	ary := mrb.AryNewCapa(len(ss))
	for _, s := range ss {
		ary.Push(mrb.StrNew(s))
	}
	return ary.Value()
}

func newResultSetValue(mrb *oruby.MrbState, db *sqliteDB, stmt *sqliteStmt, rows *sql.Rows, ownStmt bool) (oruby.Value, error) {
	cols, err := rows.Columns()
	if err != nil {
		return mrb.NilValue(), err
	}
	types := columnTypeNames(rows)
	if stmt != nil {
		stmt.activeCols = cols
		stmt.activeTypes = types
	}
	rs := &sqliteResultSet{
		db:    db,
		stmt:  stmt,
		rows:  rows,
		cols:  cols,
		types: types,
		ownRS: ownStmt,
	}
	rs.advance()
	rsCls := rb.ClassPath(mrb, "SQLite3::ResultSet")
	return mrb.DataWrapInterface(rsCls, rs).Value(), nil
}

func (rs *sqliteResultSet) advance() {
	if rs.closed || rs.eof || rs.rows == nil {
		rs.eof = true
		return
	}
	if rs.rows.Next() {
		vals, err := scanRowValues(rs.rows, len(rs.cols))
		if err != nil {
			rs.eof = true
			return
		}
		rs.curRow = vals
		rs.nexted = true
	} else {
		rs.eof = true
		rs.curRow = nil
		rs.nexted = false
	}
}

func (rs *sqliteResultSet) close() {
	if rs.closed {
		return
	}
	rs.closed = true
	rs.eof = true
	if rs.rows != nil {
		_ = rs.rows.Close()
		rs.rows = nil
	}
	if rs.ownRS && rs.stmt != nil {
		rs.stmt.close()
	}
}

func getRS(mrb *oruby.MrbState, self oruby.Value) *sqliteResultSet {
	rs, ok := rb.Get[*sqliteResultSet](mrb, self)
	if !ok || rs == nil {
		rb.Raise(mrb, "SQLite3::Exception", "uninitialized SQLite3::ResultSet")
		return nil
	}
	return rs
}

func rsNextRow(mrb *oruby.MrbState, self oruby.Value, asHash bool) oruby.MrbValue {
	rs := getRS(mrb, self)
	if rs == nil || rs.closed || rs.eof || !rs.nexted {
		return mrb.NilValue()
	}
	row := rowToRuby(mrb, rs.cols, rs.curRow, asHash)
	rs.advance()
	return row
}

func rsNext(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	rs := getRS(mrb, self)
	if rs == nil {
		return mrb.NilValue()
	}
	return rsNextRow(mrb, self, rs.db.resultsAsHash)
}

func rsNextHash(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return rsNextRow(mrb, self, true)
}

func rsEach(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	rs := getRS(mrb, self)
	if rs == nil {
		return mrb.NilValue()
	}
	blk := mrb.GetArgsBlock()
	if blk.IsNil() {
		return mrb.Call(self, "to_enum", mrb.SymbolValue(mrb.Intern("each")))
	}
	for !rs.eof && !rs.closed {
		row := rsNextRow(mrb, self, rs.db.resultsAsHash)
		mrb.Yield(blk, row)
		if mrb.HasExc() {
			return mrb.NilValue()
		}
	}
	return self
}

func rsEachHash(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	rs := getRS(mrb, self)
	if rs == nil {
		return mrb.NilValue()
	}
	blk := mrb.GetArgsBlock()
	if blk.IsNil() {
		return mrb.Call(self, "to_enum", mrb.SymbolValue(mrb.Intern("each_hash")))
	}
	for !rs.eof && !rs.closed {
		row := rsNextRow(mrb, self, true)
		mrb.Yield(blk, row)
		if mrb.HasExc() {
			return mrb.NilValue()
		}
	}
	return self
}

func rsColumns(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	rs := getRS(mrb, self)
	if rs == nil {
		return mrb.NilValue()
	}
	return stringsToAry(mrb, rs.cols)
}

func rsTypes(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	rs := getRS(mrb, self)
	if rs == nil {
		return mrb.NilValue()
	}
	return stringsToAry(mrb, rs.types)
}

func rsEofP(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	rs := getRS(mrb, self)
	if rs == nil {
		return mrb.TrueValue()
	}
	return mrb.BoolValue(rs.eof || rs.closed)
}

func rsClose(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	rs := getRS(mrb, self)
	if rs == nil {
		return mrb.NilValue()
	}
	rs.close()
	return mrb.NilValue()
}

func rsClosedP(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	rs := getRS(mrb, self)
	if rs == nil {
		return mrb.TrueValue()
	}
	return mrb.BoolValue(rs.closed)
}
