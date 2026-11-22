// Package pg implements Ruby's pg gem (PostgreSQL client) in pure Go backed by
// github.com/jackc/pgx/v5/pgconn.
package pg

import (
	"context"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/internal/rb"
)

//go:embed pg.rb
var pgRb []byte

const (
	connectionOK  = 0
	connectionBad = 1

	pgresEmptyQuery    = 0
	pgresCommandOK     = 1
	pgresTuplesOK      = 2
	pgresCopyOut       = 3
	pgresCopyIn        = 4
	pgresBadResponse   = 5
	pgresNonfatalError = 6
	pgresFatalError    = 7

	pqtransIdle    = 0
	pqtransActive  = 1
	pqtransInTrans = 2
	pqtransInError = 3
	pqtransUnknown = 4

	pgDiagSeverity          = 'S'
	pgDiagSQLState          = 'C'
	pgDiagMessagePrimary    = 'M'
	pgDiagMessageDetail     = 'D'
	pgDiagMessageHint       = 'H'
	pgDiagStatementPosition = 'P'
	pgDiagSchemaName        = 's'
	pgDiagTableName         = 't'
	pgDiagColumnName        = 'c'
	pgDiagDatatypeName      = 'd'
	pgDiagConstraintName    = 'n'
)

func init() {
	oruby.LazyGem("pg", initPG)
	oruby.GemAlias("pg_ext", "pg")
	oruby.GemAlias("pg/constants", "pg")
	oruby.GemAlias("pg/exceptions", "pg")
	oruby.GemAlias("pg/connection", "pg")
	oruby.GemAlias("pg/result", "pg")
	oruby.GemAlias("pg/type_map", "pg")
	oruby.GemAlias("pg/basic_type_mapping", "pg")
}

func initPG(mrb *oruby.MrbState) any {
	mod := mrb.DefineModule("PG")
	mrb.ConstSet(mod, mrb.Intern("VERSION"), mrb.StrNew("1.5.9"))

	consts := mrb.DefineModuleUnder(mod, "Constants")
	for name, val := range map[string]int{
		"CONNECTION_OK":              connectionOK,
		"CONNECTION_BAD":             connectionBad,
		"PGRES_EMPTY_QUERY":          pgresEmptyQuery,
		"PGRES_COMMAND_OK":           pgresCommandOK,
		"PGRES_TUPLES_OK":            pgresTuplesOK,
		"PGRES_COPY_OUT":             pgresCopyOut,
		"PGRES_COPY_IN":              pgresCopyIn,
		"PGRES_BAD_RESPONSE":         pgresBadResponse,
		"PGRES_NONFATAL_ERROR":       pgresNonfatalError,
		"PGRES_FATAL_ERROR":          pgresFatalError,
		"PQTRANS_IDLE":               pqtransIdle,
		"PQTRANS_ACTIVE":             pqtransActive,
		"PQTRANS_INTRANS":            pqtransInTrans,
		"PQTRANS_INERROR":            pqtransInError,
		"PQTRANS_UNKNOWN":            pqtransUnknown,
		"PG_DIAG_SEVERITY":           pgDiagSeverity,
		"PG_DIAG_SQLSTATE":           pgDiagSQLState,
		"PG_DIAG_MESSAGE_PRIMARY":    pgDiagMessagePrimary,
		"PG_DIAG_MESSAGE_DETAIL":     pgDiagMessageDetail,
		"PG_DIAG_MESSAGE_HINT":       pgDiagMessageHint,
		"PG_DIAG_STATEMENT_POSITION": pgDiagStatementPosition,
		"PG_DIAG_SCHEMA_NAME":        pgDiagSchemaName,
		"PG_DIAG_TABLE_NAME":         pgDiagTableName,
		"PG_DIAG_COLUMN_NAME":        pgDiagColumnName,
		"PG_DIAG_DATATYPE_NAME":      pgDiagDatatypeName,
		"PG_DIAG_CONSTRAINT_NAME":    pgDiagConstraintName,
	} {
		mrb.ConstSet(consts, mrb.Intern(name), mrb.FixnumValue(val))
	}
	mrb.IncludeModule(mod, consts)

	mod.DefineClassMethod("library_version", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.FixnumValue(160000)
	}, mrb.ArgsNone())

	defineErrors(mrb, mod)
	defineConnection(mrb, mod, consts)
	defineResult(mrb, mod, consts)

	rb.LoadRuby(mrb, "<internal:pg>", pgRb)
	return nil
}

// ---------------------------------------------------------------------------
// Exceptions & SQLSTATE mapping
// ---------------------------------------------------------------------------

var sqlstateToClass = map[string]string{
	"0A000": "FeatureNotSupported",
	"22P02": "InvalidTextRepresentation",
	"23000": "IntegrityConstraintViolation",
	"23502": "NotNullViolation",
	"23503": "ForeignKeyViolation",
	"23505": "UniqueViolation",
	"23514": "CheckViolation",
	"28P01": "InvalidPassword",
	"3D000": "InvalidCatalogName",
	"40001": "SerializationFailure",
	"40P01": "DeadlockDetected",
	"42501": "InsufficientPrivilege",
	"42601": "SyntaxError",
	"42703": "UndefinedColumn",
	"42P01": "UndefinedTable",
	"57014": "QueryCanceled",
	"57P01": "AdminShutdown",
}

func defineErrors(mrb *oruby.MrbState, mod oruby.RClass) {
	stdErr := mrb.ExcGet("StandardError")
	pgErr := mrb.DefineClassUnder(mod, "Error", stdErr)

	for _, name := range []string{
		"ConnectionBad",
		"UnableToSend",
		"InvalidResultStatus",
		"ServerError",
		"FeatureNotSupported",
		"DataException",
		"InvalidTextRepresentation",
		"IntegrityConstraintViolation",
		"NotNullViolation",
		"ForeignKeyViolation",
		"UniqueViolation",
		"CheckViolation",
		"InvalidAuthorizationSpecification",
		"InvalidPassword",
		"InvalidCatalogName",
		"TransactionRollback",
		"SerializationFailure",
		"DeadlockDetected",
		"SyntaxErrorOrAccessRuleViolation",
		"InsufficientPrivilege",
		"SyntaxError",
		"UndefinedColumn",
		"UndefinedTable",
		"OperatorIntervention",
		"QueryCanceled",
		"AdminShutdown",
	} {
		parent := pgErr
		switch name {
		case "InvalidTextRepresentation":
			parent = rb.ClassPath(mrb, "PG::DataException")
		case "NotNullViolation", "ForeignKeyViolation", "UniqueViolation", "CheckViolation":
			parent = rb.ClassPath(mrb, "PG::IntegrityConstraintViolation")
		case "InvalidPassword":
			parent = rb.ClassPath(mrb, "PG::InvalidAuthorizationSpecification")
		case "SerializationFailure", "DeadlockDetected":
			parent = rb.ClassPath(mrb, "PG::TransactionRollback")
		case "InsufficientPrivilege", "SyntaxError", "UndefinedColumn", "UndefinedTable":
			parent = rb.ClassPath(mrb, "PG::SyntaxErrorOrAccessRuleViolation")
		case "QueryCanceled", "AdminShutdown":
			parent = rb.ClassPath(mrb, "PG::OperatorIntervention")
		}
		mrb.DefineClassUnder(mod, name, parent)
	}
}

func raisePGError(mrb *oruby.MrbState, connVal oruby.Value, err error) oruby.MrbValue {
	clsName := "Error"
	var pgErr *pgconn.PgError
	var resVal oruby.Value = mrb.NilValue()
	if errors.As(err, &pgErr) {
		if mapped, ok := sqlstateToClass[pgErr.Code]; ok {
			clsName = mapped
		} else if len(pgErr.Code) >= 2 {
			switch pgErr.Code[:2] {
			case "22":
				clsName = "DataException"
			case "23":
				clsName = "IntegrityConstraintViolation"
			case "40":
				clsName = "TransactionRollback"
			case "42":
				clsName = "SyntaxErrorOrAccessRuleViolation"
			case "57":
				clsName = "OperatorIntervention"
			}
		}
		resCls := rb.ClassPath(mrb, "PG::Result")
		r := &pgResult{
			status:  pgresFatalError,
			pgErr:   pgErr,
			errMsg:  pgErr.Error(),
			typeMap: mrb.NilValue(),
		}
		resVal = mrb.DataWrapInterface(resCls, r).Value()
	}

	excCls := rb.ClassPath(mrb, "PG::"+clsName)
	exc, callErr := mrb.Funcall(excCls, mrb.Intern("new"), mrb.StrNew(err.Error()))
	if callErr != nil {
		return mrb.Raise(excCls, err.Error())
	}
	mrb.IVSet(exc, mrb.Intern("@connection"), connVal)
	mrb.IVSet(exc, mrb.Intern("@result"), resVal)
	mrb.ExcRaise(exc)
	return mrb.NilValue()
}

// ---------------------------------------------------------------------------
// PG::Connection
// ---------------------------------------------------------------------------

type pgConn struct {
	conn           *pgconn.PgConn
	cfg            *pgconn.Config
	closed         bool
	notifCh        chan *pgconn.Notification
	typeMapResults oruby.Value
	typeMapQueries oruby.Value
}

func getConn(mrb *oruby.MrbState, self oruby.Value) (*pgConn, error) {
	c, ok := rb.Get[*pgConn](mrb, self)
	if !ok || c == nil {
		return nil, errors.New("PG::Connection is not initialized")
	}
	return c, nil
}

func (c *pgConn) checkOpen() error {
	if c.closed || c.conn == nil || c.conn.IsClosed() {
		return errors.New("connection is closed")
	}
	return nil
}

func parseConnConfig(mrb *oruby.MrbState, args oruby.RArgs, kw oruby.Value) (*pgconn.Config, error) {
	var connStr string
	var opts oruby.Value = mrb.NilValue()

	if args.Len() == 1 {
		a0 := args.Item(0)
		if a0.IsString() {
			connStr = a0.String()
		} else if a0.IsHash() {
			opts = a0
		}
	} else if args.Len() > 1 {
		// Legacy positional: (host, port, options, tty, dbname, login, password)
		parts := []string{}
		keys := []string{"host", "port", "options", "", "dbname", "user", "password"}
		for i := 0; i < args.Len() && i < len(keys); i++ {
			v := args.Item(i)
			if keys[i] == "" || v.IsNil() {
				continue
			}
			parts = append(parts, fmt.Sprintf("%s=%s", keys[i], quoteConninfoVal(mrb.String(v))))
		}
		connStr = strings.Join(parts, " ")
	}

	if !kw.IsNil() && kw.IsHash() {
		if opts.IsNil() {
			opts = kw
		} else if merged, err := mrb.Funcall(opts, mrb.Intern("merge"), kw); err == nil {
			opts = merged
		}
	}

	if !opts.IsNil() && opts.IsHash() {
		parts := []string{}
		if connStr != "" {
			parts = append(parts, connStr)
		}
		keys := mrb.HashKeys(opts)
		for i := 0; i < oruby.RArrayLen(keys); i++ {
			k := mrb.AryRef(keys, i)
			v, err := mrb.Funcall(opts, mrb.Intern("[]"), k)
			if err != nil || v.IsNil() {
				continue
			}
			ks := mrb.String(k)
			if ks == "hostaddr" {
				ks = "host"
			}
			parts = append(parts, fmt.Sprintf("%s=%s", ks, quoteConninfoVal(mrb.String(v))))
		}
		connStr = strings.Join(parts, " ")
	}

	cfg, err := pgconn.ParseConfig(connStr)
	if err != nil {
		// Fallback for URL forms with postgres://
		if strings.HasPrefix(connStr, "postgres://") || strings.HasPrefix(connStr, "postgresql://") {
			if _, uerr := url.Parse(connStr); uerr != nil {
				return nil, uerr
			}
		}
		return nil, err
	}
	return cfg, nil
}

func quoteConninfoVal(s string) string {
	if s == "" || strings.ContainsAny(s, " \t\n\r'\\") {
		escaped := strings.ReplaceAll(s, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `'`, `\'`)
		return "'" + escaped + "'"
	}
	return s
}

func escapePGString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

func escapePGLiteral(s string) string {
	if strings.ContainsRune(s, '\\') {
		e := strings.ReplaceAll(s, `\`, `\\`)
		e = strings.ReplaceAll(e, `'`, `''`)
		return " E'" + e + "'"
	}
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func escapePGIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func escapeBytea(b []byte) string {
	return `\x` + hex.EncodeToString(b)
}

func unescapeBytea(s string) ([]byte, error) {
	if strings.HasPrefix(s, `\x`) || strings.HasPrefix(s, `\X`) {
		return hex.DecodeString(s[2:])
	}
	// Legacy PostgreSQL octal escape format
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			if s[i+1] == '\\' {
				out = append(out, '\\')
				i++
				continue
			}
			if i+3 < len(s) && s[i+1] >= '0' && s[i+1] <= '3' && s[i+2] >= '0' && s[i+2] <= '7' && s[i+3] >= '0' && s[i+3] <= '7' {
				v := ((s[i+1] - '0') << 6) | ((s[i+2] - '0') << 3) | (s[i+3] - '0')
				out = append(out, v)
				i += 3
				continue
			}
		}
		out = append(out, s[i])
	}
	return out, nil
}

func extractParams(mrb *oruby.MrbState, paramsVal oruby.Value) ([][]byte, []int16, error) {
	if paramsVal.IsNil() {
		return nil, nil, nil
	}
	if !paramsVal.IsArray() {
		return nil, nil, oruby.ETypeError("params must be an Array")
	}
	n := oruby.RArrayLen(paramsVal)
	vals := make([][]byte, n)
	fmts := make([]int16, n)
	for i := 0; i < n; i++ {
		item := mrb.AryRef(paramsVal, i)
		if item.IsNil() {
			vals[i] = nil
			fmts[i] = 0
			continue
		}
		if item.IsHash() {
			v, _ := mrb.Funcall(item, mrb.Intern("[]"), mrb.Intern("value").Value())
			f, _ := mrb.Funcall(item, mrb.Intern("[]"), mrb.Intern("format").Value())
			if v.IsNil() {
				vals[i] = nil
			} else if v.IsString() {
				vals[i] = append([]byte(nil), v.Bytes()...)
			} else {
				vals[i] = []byte(mrb.String(v))
			}
			if !f.IsNil() {
				fmts[i] = int16(f.Int())
			}
			continue
		}
		switch item.Type() {
		case oruby.MrbTTTrue:
			vals[i] = []byte("t")
		case oruby.MrbTTFalse:
			vals[i] = []byte("f")
		case oruby.MrbTTInteger:
			vals[i] = []byte(strconv.FormatInt(item.Int64(), 10))
		case oruby.MrbTTFloat:
			vals[i] = []byte(strconv.FormatFloat(item.Float64(), 'g', -1, 64))
		case oruby.MrbTTString:
			vals[i] = append([]byte(nil), item.Bytes()...)
		default:
			vals[i] = []byte(mrb.String(item))
		}
	}
	return vals, fmts, nil
}

func wrapPGResult(mrb *oruby.MrbState, c *pgConn, r *pgconn.Result) oruby.Value {
	resCls := rb.ClassPath(mrb, "PG::Result")
	status := pgresCommandOK
	if len(r.FieldDescriptions) > 0 {
		status = pgresTuplesOK
	} else if r.CommandTag.String() == "" {
		status = pgresEmptyQuery
	}
	res := &pgResult{
		status:  status,
		fields:  r.FieldDescriptions,
		rows:    r.Rows,
		cmdTag:  r.CommandTag,
		typeMap: c.typeMapResults,
	}
	val := mrb.DataWrapInterface(resCls, res).Value()
	if !res.typeMap.IsNil() {
		mrb.IVSet(val, mrb.Intern("@__type_map"), res.typeMap)
	}
	return val
}

func defineConnection(mrb *oruby.MrbState, mod, consts oruby.RClass) {
	cls := mrb.DefineClassUnder(mod, "Connection", mrb.ObjectClass())
	rb.DataClass(cls)
	mrb.IncludeModule(cls, consts)

	// Class-level escape helpers
	cls.DefineClassMethod("escape_string", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.StrNew(escapePGString(mrb.GetArgsFirst().String()))
	}, mrb.ArgsReq(1))
	cls.DefineClassMethod("escape", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.StrNew(escapePGString(mrb.GetArgsFirst().String()))
	}, mrb.ArgsReq(1))
	cls.DefineClassMethod("quote_ident", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.StrNew(escapePGIdent(mrb.GetArgsFirst().String()))
	}, mrb.ArgsReq(1))
	cls.DefineClassMethod("escape_bytea", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.StrNew(escapeBytea(mrb.GetArgsFirst().Bytes()))
	}, mrb.ArgsReq(1))
	cls.DefineClassMethod("unescape_bytea", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		b, err := unescapeBytea(mrb.GetArgsFirst().String())
		if err != nil {
			return mrb.Raise(rb.ClassPath(mrb, "PG::Error"), err.Error())
		}
		return mrb.Call(mrb.BytesValue(b), "b")
	}, mrb.ArgsReq(1))

	cls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		kw := mrb.KeywordArgs().Value()
		cfg, err := parseConnConfig(mrb, args, kw)
		if err != nil {
			return rb.Raise(mrb, "PG::ConnectionBad", "%s", err.Error())
		}

		notifCh := make(chan *pgconn.Notification, 16)
		cfg.OnNotification = func(_ *pgconn.PgConn, n *pgconn.Notification) {
			select {
			case notifCh <- n:
			default:
			}
		}

		ctx := mrb.GoContext()
		conn, err := pgconn.ConnectConfig(ctx, cfg)
		if err != nil {
			return rb.Raise(mrb, "PG::ConnectionBad", "%s", err.Error())
		}

		c := &pgConn{
			conn:           conn,
			cfg:            cfg,
			notifCh:        notifCh,
			typeMapResults: mrb.NilValue(),
			typeMapQueries: mrb.NilValue(),
		}
		rb.Set(mrb, self, c)
		return self
	}, mrb.ArgsAny())

	closeFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil {
			return mrb.NilValue()
		}
		if !c.closed && c.conn != nil {
			c.closed = true
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = c.conn.Close(ctx)
		}
		return mrb.NilValue()
	}
	cls.DefineMethod("close", closeFn, mrb.ArgsNone())
	cls.DefineMethod("finish", closeFn, mrb.ArgsNone())

	finishedFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil {
			return mrb.TrueValue()
		}
		return mrb.BoolValue(c.closed || c.conn == nil || c.conn.IsClosed())
	}
	cls.DefineMethod("finished?", finishedFn, mrb.ArgsNone())
	cls.DefineMethod("closed?", finishedFn, mrb.ArgsNone())

	cls.DefineMethod("status", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return mrb.FixnumValue(connectionBad)
		}
		return mrb.FixnumValue(connectionOK)
	}, mrb.ArgsNone())

	cls.DefineMethod("reset", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil {
			return rb.Raise(mrb, "PG::ConnectionBad", "%s", err.Error())
		}
		if c.conn != nil {
			_ = c.conn.Close(context.Background())
		}
		conn, err := pgconn.ConnectConfig(mrb.GoContext(), c.cfg)
		if err != nil {
			c.closed = true
			return rb.Raise(mrb, "PG::ConnectionBad", "%s", err.Error())
		}
		c.conn = conn
		c.closed = false
		return mrb.NilValue()
	}, mrb.ArgsNone())

	cls.DefineMethod("db", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := getConn(mrb, self)
		return mrb.StrNew(c.cfg.Database)
	}, mrb.ArgsNone())
	cls.DefineMethod("user", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := getConn(mrb, self)
		return mrb.StrNew(c.cfg.User)
	}, mrb.ArgsNone())
	cls.DefineMethod("pass", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := getConn(mrb, self)
		return mrb.StrNew(c.cfg.Password)
	}, mrb.ArgsNone())
	cls.DefineMethod("host", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := getConn(mrb, self)
		return mrb.StrNew(c.cfg.Host)
	}, mrb.ArgsNone())
	cls.DefineMethod("port", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := getConn(mrb, self)
		return mrb.FixnumValue(int(c.cfg.Port))
	}, mrb.ArgsNone())

	cls.DefineMethod("backend_pid", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return mrb.FixnumValue(0)
		}
		return mrb.FixnumValue(int(c.conn.PID()))
	}, mrb.ArgsNone())

	cls.DefineMethod("parameter_status", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return mrb.NilValue()
		}
		v := c.conn.ParameterStatus(mrb.GetArgsFirst().String())
		if v == "" {
			return mrb.NilValue()
		}
		return mrb.StrNew(v)
	}, mrb.ArgsReq(1))

	cls.DefineMethod("server_version", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return mrb.FixnumValue(0)
		}
		sv := c.conn.ParameterStatus("server_version")
		if sv == "" {
			return mrb.FixnumValue(0)
		}
		// Parse "16.2" -> 160002 or "15.4 (Debian)" -> 150004
		fields := strings.Fields(sv)
		parts := strings.Split(fields[0], ".")
		major, _ := strconv.Atoi(parts[0])
		minor := 0
		if len(parts) > 1 {
			minor, _ = strconv.Atoi(parts[1])
		}
		return mrb.FixnumValue(major*10000 + minor)
	}, mrb.ArgsNone())

	cls.DefineMethod("transaction_status", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return mrb.FixnumValue(pqtransUnknown)
		}
		switch c.conn.TxStatus() {
		case 'I':
			return mrb.FixnumValue(pqtransIdle)
		case 'T':
			return mrb.FixnumValue(pqtransInTrans)
		case 'E':
			return mrb.FixnumValue(pqtransInError)
		default:
			return mrb.FixnumValue(pqtransUnknown)
		}
	}, mrb.ArgsNone())

	cls.DefineMethod("escape_string", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.StrNew(escapePGString(mrb.GetArgsFirst().String()))
	}, mrb.ArgsReq(1))
	cls.DefineMethod("escape", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.StrNew(escapePGString(mrb.GetArgsFirst().String()))
	}, mrb.ArgsReq(1))
	cls.DefineMethod("escape_literal", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.StrNew(escapePGLiteral(mrb.GetArgsFirst().String()))
	}, mrb.ArgsReq(1))
	cls.DefineMethod("escape_identifier", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.StrNew(escapePGIdent(mrb.GetArgsFirst().String()))
	}, mrb.ArgsReq(1))
	cls.DefineMethod("quote_ident", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.StrNew(escapePGIdent(mrb.GetArgsFirst().String()))
	}, mrb.ArgsReq(1))
	cls.DefineMethod("escape_bytea", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.StrNew(escapeBytea(mrb.GetArgsFirst().Bytes()))
	}, mrb.ArgsReq(1))
	cls.DefineMethod("unescape_bytea", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		b, err := unescapeBytea(mrb.GetArgsFirst().String())
		if err != nil {
			return rb.Raise(mrb, "PG::Error", "%s", err.Error())
		}
		return mrb.Call(mrb.BytesValue(b), "b")
	}, mrb.ArgsReq(1))

	cls.DefineMethod("type_map_for_results", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := getConn(mrb, self)
		return c.typeMapResults
	}, mrb.ArgsNone())
	cls.DefineMethod("type_map_for_results=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := getConn(mrb, self)
		v := mrb.GetArgsFirst()
		c.typeMapResults = v
		mrb.IVSet(self, mrb.Intern("@__tm_res"), v)
		return v
	}, mrb.ArgsReq(1))
	cls.DefineMethod("type_map_for_queries", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := getConn(mrb, self)
		return c.typeMapQueries
	}, mrb.ArgsNone())
	cls.DefineMethod("type_map_for_queries=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := getConn(mrb, self)
		v := mrb.GetArgsFirst()
		c.typeMapQueries = v
		mrb.IVSet(self, mrb.Intern("@__tm_qry"), v)
		return v
	}, mrb.ArgsReq(1))

	execFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return rb.Raise(mrb, "PG::ConnectionBad", "connection is closed")
		}
		args := mrb.GetArgs()
		blk := mrb.GetArgsBlock()
		sql := args.Item(0).String()

		// If params passed to exec/query(sql, params) -> delegate to ExecParams
		if args.Len() >= 2 && !args.Item(1).IsNil() {
			return execParamsImpl(mrb, self, c, sql, args.Item(1), blk)
		}

		ctx := mrb.GoContext()
		results, err := c.conn.Exec(ctx, sql).ReadAll()
		if err != nil {
			return raisePGError(mrb, self, err)
		}
		var resVal oruby.Value
		if len(results) == 0 {
			resVal = wrapPGResult(mrb, c, &pgconn.Result{})
		} else {
			resVal = wrapPGResult(mrb, c, results[len(results)-1])
		}
		return yieldAndClearResult(mrb, resVal, blk)
	}
	cls.DefineMethod("exec", execFn, mrb.ArgsReq(1)|mrb.ArgsOpt(2)|mrb.ArgsBlock())
	cls.DefineMethod("query", execFn, mrb.ArgsReq(1)|mrb.ArgsOpt(2)|mrb.ArgsBlock())

	cls.DefineMethod("exec_params", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return rb.Raise(mrb, "PG::ConnectionBad", "connection is closed")
		}
		args := mrb.GetArgs()
		blk := mrb.GetArgsBlock()
		sql := args.Item(0).String()
		params := mrb.NilValue()
		if args.Len() >= 2 {
			params = args.Item(1)
		}
		return execParamsImpl(mrb, self, c, sql, params, blk)
	}, mrb.ArgsReq(1)|mrb.ArgsOpt(2)|mrb.ArgsBlock())

	cls.DefineMethod("prepare", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return rb.Raise(mrb, "PG::ConnectionBad", "connection is closed")
		}
		args := mrb.GetArgs()
		name := args.Item(0).String()
		sql := args.Item(1).String()
		var oids []uint32
		if args.Len() >= 3 && args.Item(2).IsArray() {
			a := args.Item(2)
			oids = make([]uint32, oruby.RArrayLen(a))
			for i := 0; i < len(oids); i++ {
				oids[i] = uint32(mrb.AryRef(a, i).Int64())
			}
		}
		sd, err := c.conn.Prepare(mrb.GoContext(), name, sql, oids)
		if err != nil {
			return raisePGError(mrb, self, err)
		}
		return wrapPGResult(mrb, c, &pgconn.Result{FieldDescriptions: sd.Fields})
	}, mrb.ArgsReq(2)|mrb.ArgsOpt(1))

	cls.DefineMethod("exec_prepared", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return rb.Raise(mrb, "PG::ConnectionBad", "connection is closed")
		}
		args := mrb.GetArgs()
		blk := mrb.GetArgsBlock()
		name := args.Item(0).String()
		params := mrb.NilValue()
		if args.Len() >= 2 {
			params = args.Item(1)
		}
		vals, fmts, err := extractParams(mrb, params)
		if err != nil {
			return mrb.RaiseError(err)
		}
		res := c.conn.ExecPrepared(mrb.GoContext(), name, vals, fmts, nil).Read()
		if res.Err != nil {
			return raisePGError(mrb, self, res.Err)
		}
		resVal := wrapPGResult(mrb, c, res)
		return yieldAndClearResult(mrb, resVal, blk)
	}, mrb.ArgsReq(1)|mrb.ArgsOpt(2)|mrb.ArgsBlock())

	cls.DefineMethod("wait_for_notify", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return rb.Raise(mrb, "PG::ConnectionBad", "connection is closed")
		}
		args := mrb.GetArgs()
		blk := mrb.GetArgsBlock()

		var notif *pgconn.Notification
		select {
		case notif = <-c.notifCh:
		default:
			ctx := mrb.GoContext()
			var cancel context.CancelFunc
			if args.Len() >= 1 && !args.Item(0).IsNil() {
				var sec float64
				if args.Item(0).IsInt() {
					sec = float64(args.Item(0).Int64())
				} else {
					sec = args.Item(0).Float64()
				}
				ctx, cancel = context.WithTimeout(ctx, time.Duration(sec*float64(time.Second)))
				defer cancel()
			}
			if err := c.conn.WaitForNotification(ctx); err != nil {
				if errors.Is(err, context.DeadlineExceeded) {
					return mrb.NilValue()
				}
				return raisePGError(mrb, self, err)
			}
			select {
			case notif = <-c.notifCh:
			default:
				return mrb.NilValue()
			}
		}

		chanVal := mrb.StrNew(notif.Channel)
		if !blk.IsNil() {
			if _, yErr := mrb.YieldArgvE(blk, chanVal, mrb.FixnumValue(int(notif.PID)), mrb.StrNew(notif.Payload)); yErr != nil {
				return mrb.RaiseError(yErr)
			}
		}
		return chanVal
	}, mrb.ArgsOpt(1)|mrb.ArgsBlock())
}

func execParamsImpl(mrb *oruby.MrbState, self oruby.Value, c *pgConn, sql string, params oruby.Value, blk oruby.RProc) oruby.MrbValue {
	vals, fmts, err := extractParams(mrb, params)
	if err != nil {
		return mrb.RaiseError(err)
	}
	res := c.conn.ExecParams(mrb.GoContext(), sql, vals, nil, fmts, nil).Read()
	if res.Err != nil {
		return raisePGError(mrb, self, res.Err)
	}
	resVal := wrapPGResult(mrb, c, res)
	return yieldAndClearResult(mrb, resVal, blk)
}

func yieldAndClearResult(mrb *oruby.MrbState, resVal oruby.Value, blk oruby.RProc) oruby.MrbValue {
	if blk.IsNil() {
		return resVal
	}
	out, err := mrb.YieldE(blk, resVal)
	_, _ = mrb.Funcall(resVal, mrb.Intern("clear"))
	if err != nil {
		return mrb.RaiseError(err)
	}
	return out
}

// ---------------------------------------------------------------------------
// PG::Result
// ---------------------------------------------------------------------------

type pgResult struct {
	status  int
	fields  []pgconn.FieldDescription
	rows    [][][]byte
	cmdTag  pgconn.CommandTag
	pgErr   *pgconn.PgError
	errMsg  string
	cleared bool
	typeMap oruby.Value
}

func getRes(mrb *oruby.MrbState, self oruby.Value) (*pgResult, error) {
	r, ok := rb.Get[*pgResult](mrb, self)
	if !ok || r == nil {
		return nil, errors.New("PG::Result is not initialized")
	}
	return r, nil
}

func (r *pgResult) checkNotCleared() error {
	if r.cleared {
		return errors.New("result has been cleared")
	}
	return nil
}

func (r *pgResult) decodeCell(mrb *oruby.MrbState, row, col int) (oruby.Value, error) {
	cell := r.rows[row][col]
	if cell == nil {
		return mrb.NilValue(), nil
	}
	raw := mrb.BytesValue(cell)
	if !r.typeMap.IsNil() {
		oid := int(r.fields[col].DataTypeOID)
		fmtCode := int(r.fields[col].Format)
		decoded, err := mrb.Funcall(r.typeMap, mrb.Intern("__decode"), raw, mrb.FixnumValue(oid), mrb.FixnumValue(fmtCode))
		if err != nil {
			return mrb.NilValue(), err
		}
		return decoded, nil
	}
	return raw, nil
}

func defineResult(mrb *oruby.MrbState, mod, consts oruby.RClass) {
	cls := mrb.DefineClassUnder(mod, "Result", mrb.ObjectClass())
	rb.DataClass(cls)
	mrb.IncludeModule(cls, mrb.ModuleGet("Enumerable"))
	mrb.IncludeModule(cls, consts)

	cls.DefineMethod("clear", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, err := getRes(mrb, self)
		if err == nil {
			r.cleared = true
			r.rows = nil
			r.fields = nil
		}
		return mrb.NilValue()
	}, mrb.ArgsNone())

	cls.DefineMethod("cleared?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, err := getRes(mrb, self)
		return mrb.BoolValue(err != nil || r.cleared)
	}, mrb.ArgsNone())

	cls.DefineMethod("result_status", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := getRes(mrb, self)
		return mrb.FixnumValue(r.status)
	}, mrb.ArgsNone())

	cls.DefineMethod("cmd_status", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := getRes(mrb, self)
		return mrb.StrNew(r.cmdTag.String())
	}, mrb.ArgsNone())

	cmdTuplesFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := getRes(mrb, self)
		return mrb.Int64Value(r.cmdTag.RowsAffected())
	}
	cls.DefineMethod("cmd_tuples", cmdTuplesFn, mrb.ArgsNone())
	cls.DefineMethod("cmdtuples", cmdTuplesFn, mrb.ArgsNone())

	ntuplesFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := getRes(mrb, self)
		if err := r.checkNotCleared(); err != nil {
			return rb.Raise(mrb, "PG::Error", "%s", err.Error())
		}
		return mrb.FixnumValue(len(r.rows))
	}
	cls.DefineMethod("ntuples", ntuplesFn, mrb.ArgsNone())
	cls.DefineMethod("num_tuples", ntuplesFn, mrb.ArgsNone())

	nfieldsFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := getRes(mrb, self)
		if err := r.checkNotCleared(); err != nil {
			return rb.Raise(mrb, "PG::Error", "%s", err.Error())
		}
		return mrb.FixnumValue(len(r.fields))
	}
	cls.DefineMethod("nfields", nfieldsFn, mrb.ArgsNone())
	cls.DefineMethod("num_fields", nfieldsFn, mrb.ArgsNone())

	cls.DefineMethod("fields", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := getRes(mrb, self)
		if err := r.checkNotCleared(); err != nil {
			return rb.Raise(mrb, "PG::Error", "%s", err.Error())
		}
		ary := mrb.AryNewCapa(len(r.fields))
		for _, f := range r.fields {
			mrb.AryPush(ary, mrb.StrNew(f.Name))
		}
		return ary
	}, mrb.ArgsNone())

	cls.DefineMethod("fname", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := getRes(mrb, self)
		i := mrb.GetArgsFirst().Int()
		if i < 0 || i >= len(r.fields) {
			return mrb.EArgumentError().Raisef("column index %d out of range", i)
		}
		return mrb.StrNew(r.fields[i].Name)
	}, mrb.ArgsReq(1))

	cls.DefineMethod("fnumber", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := getRes(mrb, self)
		name := mrb.GetArgsFirst().String()
		for i, f := range r.fields {
			if f.Name == name {
				return mrb.FixnumValue(i)
			}
		}
		return mrb.FixnumValue(-1)
	}, mrb.ArgsReq(1))

	cls.DefineMethod("ftype", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := getRes(mrb, self)
		i := mrb.GetArgsFirst().Int()
		if i < 0 || i >= len(r.fields) {
			return mrb.EArgumentError().Raisef("column index %d out of range", i)
		}
		return mrb.Int64Value(int64(r.fields[i].DataTypeOID))
	}, mrb.ArgsReq(1))

	cls.DefineMethod("fsize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := getRes(mrb, self)
		i := mrb.GetArgsFirst().Int()
		if i < 0 || i >= len(r.fields) {
			return mrb.EArgumentError().Raisef("column index %d out of range", i)
		}
		return mrb.FixnumValue(int(r.fields[i].DataTypeSize))
	}, mrb.ArgsReq(1))

	cls.DefineMethod("fmod", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := getRes(mrb, self)
		i := mrb.GetArgsFirst().Int()
		if i < 0 || i >= len(r.fields) {
			return mrb.EArgumentError().Raisef("column index %d out of range", i)
		}
		return mrb.FixnumValue(int(r.fields[i].TypeModifier))
	}, mrb.ArgsReq(1))

	cls.DefineMethod("fformat", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := getRes(mrb, self)
		i := mrb.GetArgsFirst().Int()
		if i < 0 || i >= len(r.fields) {
			return mrb.EArgumentError().Raisef("column index %d out of range", i)
		}
		return mrb.FixnumValue(int(r.fields[i].Format))
	}, mrb.ArgsReq(1))

	cls.DefineMethod("getvalue", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := getRes(mrb, self)
		if err := r.checkNotCleared(); err != nil {
			return rb.Raise(mrb, "PG::Error", "%s", err.Error())
		}
		args := mrb.GetArgs()
		row, col := args.Item(0).Int(), args.Item(1).Int()
		if row < 0 || row >= len(r.rows) || col < 0 || col >= len(r.fields) {
			return mrb.EArgumentError().Raisef("index (%d, %d) out of range", row, col)
		}
		v, err := r.decodeCell(mrb, row, col)
		if err != nil {
			return mrb.RaiseError(err)
		}
		return v
	}, mrb.ArgsReq(2))

	cls.DefineMethod("getisnull", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := getRes(mrb, self)
		args := mrb.GetArgs()
		row, col := args.Item(0).Int(), args.Item(1).Int()
		if row < 0 || row >= len(r.rows) || col < 0 || col >= len(r.fields) {
			return mrb.EArgumentError().Raisef("index (%d, %d) out of range", row, col)
		}
		return mrb.BoolValue(r.rows[row][col] == nil)
	}, mrb.ArgsReq(2))

	cls.DefineMethod("getlength", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := getRes(mrb, self)
		args := mrb.GetArgs()
		row, col := args.Item(0).Int(), args.Item(1).Int()
		if row < 0 || row >= len(r.rows) || col < 0 || col >= len(r.fields) {
			return mrb.EArgumentError().Raisef("index (%d, %d) out of range", row, col)
		}
		return mrb.FixnumValue(len(r.rows[row][col]))
	}, mrb.ArgsReq(2))

	tupleFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := getRes(mrb, self)
		if err := r.checkNotCleared(); err != nil {
			return rb.Raise(mrb, "PG::Error", "%s", err.Error())
		}
		row := mrb.GetArgsFirst().Int()
		if row < 0 {
			row += len(r.rows)
		}
		if row < 0 || row >= len(r.rows) {
			return mrb.NilValue()
		}
		h := mrb.HashNewCapa(len(r.fields))
		for col, f := range r.fields {
			v, err := r.decodeCell(mrb, row, col)
			if err != nil {
				return mrb.RaiseError(err)
			}
			mrb.HashSet(h, mrb.StrNew(f.Name), v)
		}
		return h
	}
	cls.DefineMethod("[]", tupleFn, mrb.ArgsReq(1))
	cls.DefineMethod("tuple", tupleFn, mrb.ArgsReq(1))

	cls.DefineMethod("tuple_values", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := getRes(mrb, self)
		if err := r.checkNotCleared(); err != nil {
			return rb.Raise(mrb, "PG::Error", "%s", err.Error())
		}
		row := mrb.GetArgsFirst().Int()
		if row < 0 || row >= len(r.rows) {
			return mrb.EIndexError().Raisef("row %d out of range", row)
		}
		ary := mrb.AryNewCapa(len(r.fields))
		for col := range r.fields {
			v, err := r.decodeCell(mrb, row, col)
			if err != nil {
				return mrb.RaiseError(err)
			}
			mrb.AryPush(ary, v)
		}
		return ary
	}, mrb.ArgsReq(1))

	cls.DefineMethod("values", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := getRes(mrb, self)
		if err := r.checkNotCleared(); err != nil {
			return rb.Raise(mrb, "PG::Error", "%s", err.Error())
		}
		out := mrb.AryNewCapa(len(r.rows))
		for row := range r.rows {
			a := mrb.AryNewCapa(len(r.fields))
			for col := range r.fields {
				v, err := r.decodeCell(mrb, row, col)
				if err != nil {
					return mrb.RaiseError(err)
				}
				mrb.AryPush(a, v)
			}
			mrb.AryPush(out, a)
		}
		return out
	}, mrb.ArgsNone())

	cls.DefineMethod("column_values", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := getRes(mrb, self)
		col := mrb.GetArgsFirst().Int()
		if col < 0 || col >= len(r.fields) {
			return mrb.EArgumentError().Raisef("column index %d out of range", col)
		}
		out := mrb.AryNewCapa(len(r.rows))
		for row := range r.rows {
			v, err := r.decodeCell(mrb, row, col)
			if err != nil {
				return mrb.RaiseError(err)
			}
			mrb.AryPush(out, v)
		}
		return out
	}, mrb.ArgsReq(1))

	cls.DefineMethod("type_map", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := getRes(mrb, self)
		return r.typeMap
	}, mrb.ArgsNone())

	setTypeMap := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := getRes(mrb, self)
		v := mrb.GetArgsFirst()
		r.typeMap = v
		mrb.IVSet(self, mrb.Intern("@__type_map"), v)
		return self
	}
	cls.DefineMethod("type_map=", setTypeMap, mrb.ArgsReq(1))
	cls.DefineMethod("map_types!", setTypeMap, mrb.ArgsReq(1))

	errMsgFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := getRes(mrb, self)
		return mrb.StrNew(r.errMsg)
	}
	cls.DefineMethod("error_message", errMsgFn, mrb.ArgsNone())
	cls.DefineMethod("result_error_message", errMsgFn, mrb.ArgsNone())

	errFieldFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := getRes(mrb, self)
		if r.pgErr == nil {
			return mrb.NilValue()
		}
		code := mrb.GetArgsFirst().Int()
		var s string
		switch code {
		case pgDiagSeverity:
			s = r.pgErr.Severity
		case pgDiagSQLState:
			s = r.pgErr.Code
		case pgDiagMessagePrimary:
			s = r.pgErr.Message
		case pgDiagMessageDetail:
			s = r.pgErr.Detail
		case pgDiagMessageHint:
			s = r.pgErr.Hint
		case pgDiagStatementPosition:
			if r.pgErr.Position > 0 {
				s = strconv.Itoa(int(r.pgErr.Position))
			}
		case pgDiagSchemaName:
			s = r.pgErr.SchemaName
		case pgDiagTableName:
			s = r.pgErr.TableName
		case pgDiagColumnName:
			s = r.pgErr.ColumnName
		case pgDiagDatatypeName:
			s = r.pgErr.DataTypeName
		case pgDiagConstraintName:
			s = r.pgErr.ConstraintName
		}
		if s == "" {
			return mrb.NilValue()
		}
		return mrb.StrNew(s)
	}
	cls.DefineMethod("error_field", errFieldFn, mrb.ArgsReq(1))
	cls.DefineMethod("result_error_field", errFieldFn, mrb.ArgsReq(1))
}
