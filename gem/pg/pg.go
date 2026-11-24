// Package pg implements Ruby's pg gem (PostgreSQL client) in pure Go backed by
// github.com/jackc/pgx/v5/pgconn.
package pg

import (
	"context"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/internal/rb"
)

//go:embed pg.rb
var pgRb []byte

//go:embed types.rb
var typesRb []byte

const (
	connectionOK  = 0
	connectionBad = 1

	pgresEmptyQuery      = 0
	pgresCommandOK       = 1
	pgresTuplesOK        = 2
	pgresCopyOut         = 3
	pgresCopyIn          = 4
	pgresBadResponse     = 5
	pgresNonfatalError   = 6
	pgresFatalError      = 7
	pgresCopyBoth        = 8
	pgresSingleTuple     = 9
	pgresPipelineSync    = 10
	pgresPipelineAborted = 11

	pqtransIdle    = 0
	pqtransActive  = 1
	pqtransInTrans = 2
	pqtransInError = 3
	pqtransUnknown = 4

	pgDiagSeverity             = 'S'
	pgDiagSeverityNonlocalized = 'V'
	pgDiagSQLState             = 'C'
	pgDiagMessagePrimary       = 'M'
	pgDiagMessageDetail        = 'D'
	pgDiagMessageHint          = 'H'
	pgDiagStatementPosition    = 'P'
	pgDiagInternalPosition     = 'p'
	pgDiagInternalQuery        = 'q'
	pgDiagContext              = 'W'
	pgDiagSchemaName           = 's'
	pgDiagTableName            = 't'
	pgDiagColumnName           = 'c'
	pgDiagDatatypeName         = 'd'
	pgDiagConstraintName       = 'n'
	pgDiagSourceFile           = 'F'
	pgDiagSourceLine           = 'L'
	pgDiagSourceFunction       = 'R'

	pqErrorsTerse    = 0
	pqErrorsDefault  = 1
	pqErrorsVerbose  = 2
	pqErrorsSQLState = 3

	pqShowContextNever  = 0
	pqShowContextErrors = 1
	pqShowContextAlways = 2

	pqPingOK         = 0
	pqPingReject     = 1
	pqPingNoResponse = 2
	pqPingNoAttempt  = 3

	pqPipelineOff     = 0
	pqPipelineOn      = 1
	pqPipelineAborted = 2

	pgresPollingFailed  = 0
	pgresPollingReading = 1
	pgresPollingWriting = 2
	pgresPollingOK      = 3
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
		"CONNECTION_OK":                 connectionOK,
		"CONNECTION_BAD":                connectionBad,
		"PGRES_EMPTY_QUERY":             pgresEmptyQuery,
		"PGRES_COMMAND_OK":              pgresCommandOK,
		"PGRES_TUPLES_OK":               pgresTuplesOK,
		"PGRES_COPY_OUT":                pgresCopyOut,
		"PGRES_COPY_IN":                 pgresCopyIn,
		"PGRES_BAD_RESPONSE":            pgresBadResponse,
		"PGRES_NONFATAL_ERROR":          pgresNonfatalError,
		"PGRES_FATAL_ERROR":             pgresFatalError,
		"PGRES_COPY_BOTH":               pgresCopyBoth,
		"PGRES_SINGLE_TUPLE":            pgresSingleTuple,
		"PGRES_PIPELINE_SYNC":           pgresPipelineSync,
		"PGRES_PIPELINE_ABORTED":        pgresPipelineAborted,
		"PQTRANS_IDLE":                  pqtransIdle,
		"PQTRANS_ACTIVE":                pqtransActive,
		"PQTRANS_INTRANS":               pqtransInTrans,
		"PQTRANS_INERROR":               pqtransInError,
		"PQTRANS_UNKNOWN":               pqtransUnknown,
		"PG_DIAG_SEVERITY":              pgDiagSeverity,
		"PG_DIAG_SEVERITY_NONLOCALIZED": pgDiagSeverityNonlocalized,
		"PG_DIAG_SQLSTATE":              pgDiagSQLState,
		"PG_DIAG_MESSAGE_PRIMARY":       pgDiagMessagePrimary,
		"PG_DIAG_MESSAGE_DETAIL":        pgDiagMessageDetail,
		"PG_DIAG_MESSAGE_HINT":          pgDiagMessageHint,
		"PG_DIAG_STATEMENT_POSITION":    pgDiagStatementPosition,
		"PG_DIAG_INTERNAL_POSITION":     pgDiagInternalPosition,
		"PG_DIAG_INTERNAL_QUERY":        pgDiagInternalQuery,
		"PG_DIAG_CONTEXT":               pgDiagContext,
		"PG_DIAG_SCHEMA_NAME":           pgDiagSchemaName,
		"PG_DIAG_TABLE_NAME":            pgDiagTableName,
		"PG_DIAG_COLUMN_NAME":           pgDiagColumnName,
		"PG_DIAG_DATATYPE_NAME":         pgDiagDatatypeName,
		"PG_DIAG_CONSTRAINT_NAME":       pgDiagConstraintName,
		"PG_DIAG_SOURCE_FILE":           pgDiagSourceFile,
		"PG_DIAG_SOURCE_LINE":           pgDiagSourceLine,
		"PG_DIAG_SOURCE_FUNCTION":       pgDiagSourceFunction,
		"PQERRORS_TERSE":                pqErrorsTerse,
		"PQERRORS_DEFAULT":              pqErrorsDefault,
		"PQERRORS_VERBOSE":              pqErrorsVerbose,
		"PQERRORS_SQLSTATE":             pqErrorsSQLState,
		"PQSHOW_CONTEXT_NEVER":          pqShowContextNever,
		"PQSHOW_CONTEXT_ERRORS":         pqShowContextErrors,
		"PQSHOW_CONTEXT_ALWAYS":         pqShowContextAlways,
		"PQPING_OK":                     pqPingOK,
		"PQPING_REJECT":                 pqPingReject,
		"PQPING_NO_RESPONSE":            pqPingNoResponse,
		"PQPING_NO_ATTEMPT":             pqPingNoAttempt,
		"PQ_PIPELINE_OFF":               pqPipelineOff,
		"PQ_PIPELINE_ON":                pqPipelineOn,
		"PQ_PIPELINE_ABORTED":           pqPipelineAborted,
		"PGRES_POLLING_FAILED":          pgresPollingFailed,
		"PGRES_POLLING_READING":         pgresPollingReading,
		"PGRES_POLLING_WRITING":         pgresPollingWriting,
		"PGRES_POLLING_OK":              pgresPollingOK,
		"INVALID_OID":                   0,
	} {
		mrb.ConstSet(consts, mrb.Intern(name), mrb.FixnumValue(val))
	}
	mrb.IncludeModule(mod, consts)

	mod.DefineClassMethod("library_version", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.FixnumValue(160000)
	}, mrb.ArgsNone())
	mrb.ConstSet(mod, mrb.Intern("IS_BINARY_GEM"), mrb.FalseValue())
	mrb.ConstSet(mod, mrb.Intern("POSTGRESQL_LIB_PATH"), mrb.StrNew("/usr/lib"))

	defineErrors(mrb, mod)
	defineConnection(mrb, mod, consts)
	defineResult(mrb, mod, consts)

	rb.LoadRuby(mrb, "<internal:pg>", pgRb)
	rb.LoadRuby(mrb, "<internal:pg/types>", typesRb)
	return nil
}

// ---------------------------------------------------------------------------
// Exceptions & SQLSTATE mapping
// ---------------------------------------------------------------------------

var sqlstateToClass = map[string]string{
	"0A000": "FeatureNotSupported",
	"22012": "DivisionByZero",
	"22023": "InvalidParameterValue",
	"22P02": "InvalidTextRepresentation",
	"23000": "IntegrityConstraintViolation",
	"23502": "NotNullViolation",
	"23503": "ForeignKeyViolation",
	"23505": "UniqueViolation",
	"23514": "CheckViolation",
	"26000": "InvalidSqlStatementName",
	"28P01": "InvalidPassword",
	"3D000": "InvalidCatalogName",
	"3F000": "InvalidSchemaName",
	"40001": "SerializationFailure",
	"40P01": "DeadlockDetected",
	"42501": "InsufficientPrivilege",
	"42601": "SyntaxError",
	"42703": "UndefinedColumn",
	"42883": "UndefinedFunction",
	"42P01": "UndefinedTable",
	"42P05": "DuplicatePreparedStatement",
	"54000": "ProgramLimitExceeded",
	"54001": "ProgramLimitExceeded",
	"54011": "TooManyColumns",
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
		"NoResultError",
		"InvalidChangeOfResultFields",
		"NotAllCopyDataRetrieved",
		"NotInBlockingMode",
		"LostCopyState",
		"ServerError",
		"FeatureNotSupported",
		"DataException",
		"DivisionByZero",
		"InvalidTextRepresentation",
		"InvalidParameterValue",
		"IntegrityConstraintViolation",
		"NotNullViolation",
		"ForeignKeyViolation",
		"UniqueViolation",
		"CheckViolation",
		"InvalidAuthorizationSpecification",
		"InvalidPassword",
		"InvalidCatalogName",
		"InvalidSchemaName",
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
		"InvalidSqlStatementName",
		"UndefinedFunction",
		"DuplicatePreparedStatement",
		"ProgramLimitExceeded",
		"TooManyColumns",
	} {
		parent := pgErr
		switch name {
		case "FeatureNotSupported", "DataException", "IntegrityConstraintViolation",
			"InvalidAuthorizationSpecification", "InvalidCatalogName", "InvalidSchemaName",
			"TransactionRollback", "SyntaxErrorOrAccessRuleViolation", "OperatorIntervention",
			"ProgramLimitExceeded":
			parent = rb.ClassPath(mrb, "PG::ServerError")
		case "InvalidTextRepresentation", "InvalidParameterValue", "DivisionByZero":
			parent = rb.ClassPath(mrb, "PG::DataException")
		case "TooManyColumns":
			parent = rb.ClassPath(mrb, "PG::ProgramLimitExceeded")
		case "NotNullViolation", "ForeignKeyViolation", "UniqueViolation", "CheckViolation":
			parent = rb.ClassPath(mrb, "PG::IntegrityConstraintViolation")
		case "InvalidPassword":
			parent = rb.ClassPath(mrb, "PG::InvalidAuthorizationSpecification")
		case "SerializationFailure", "DeadlockDetected":
			parent = rb.ClassPath(mrb, "PG::TransactionRollback")
		case "InsufficientPrivilege", "SyntaxError", "UndefinedColumn", "UndefinedTable", "UndefinedFunction":
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

var errParamTooLarge = errors.New("parameter payload too large")

type connectRes struct {
	conn *pgconn.PgConn
	err  error
}

type asyncRes struct {
	res       *pgconn.Result
	paramOIDs []uint32
	prepName  string
	prepDesc  *pgconn.StatementDescription
}

type copyDone struct {
	tag pgconn.CommandTag
	err error
}

type chanWriter struct {
	ch chan []byte
}

func (w *chanWriter) Write(p []byte) (int, error) {
	cp := append([]byte(nil), p...)
	w.ch <- cp
	return len(p), nil
}

func detectCopySQL(sql string) (mode int, binary bool) {
	// mode: 0=not copy, pgresCopyIn, pgresCopyOut
	s := strings.TrimSpace(sql)
	for strings.HasPrefix(s, "--") || strings.HasPrefix(s, "/*") {
		if strings.HasPrefix(s, "--") {
			if idx := strings.IndexByte(s, '\n'); idx >= 0 {
				s = strings.TrimSpace(s[idx+1:])
			} else {
				return 0, false
			}
		} else {
			if idx := strings.Index(s, "*/"); idx >= 0 {
				s = strings.TrimSpace(s[idx+2:])
			} else {
				return 0, false
			}
		}
	}
	u := strings.ToUpper(s)
	if !strings.HasPrefix(u, "COPY ") && !strings.HasPrefix(u, "COPY\t") && !strings.HasPrefix(u, "COPY\n") {
		return 0, false
	}
	// Check for BINARY format option
	binary = strings.Contains(u, "FORMAT BINARY") || strings.Contains(u, "FORMAT 'BINARY'") ||
		strings.Contains(u, " BINARY") || strings.Contains(u, "(BINARY")
	// Determine FROM STDIN / FROM STDOUT vs TO STDOUT / TO STDIN
	// Note: PostgreSQL treats COPY ... FROM STDOUT as COPY FROM STDIN, and TO STDIN as TO STDOUT.
	if strings.Contains(u, " FROM STDIN") || strings.Contains(u, " FROM STDOUT") || strings.Contains(u, ")FROM STDIN") {
		return pgresCopyIn, binary
	}
	if strings.Contains(u, " TO STDOUT") || strings.Contains(u, " TO STDIN") || strings.Contains(u, ")TO STDOUT") {
		return pgresCopyOut, binary
	}
	return 0, false
}

type pgConn struct {
	conn           *pgconn.PgConn
	cfg            *pgconn.Config
	closed         bool
	notifCh        chan *pgconn.Notification
	noticeCh       chan struct{}
	noticeMu       sync.Mutex
	notices        []*pgconn.Notice
	lastErr        string
	typeMapResults oruby.Value
	typeMapQueries oruby.Value
	fieldNameSym   bool
	asyncDone      chan []asyncRes
	pendingRaw     []asyncRes
	prepared       map[string]*pgconn.StatementDescription
	connectDone    chan connectRes
	connectCancel  context.CancelFunc

	// Active COPY state
	copyMode    int // 0, pgresCopyIn, pgresCopyOut
	copyBinary  bool
	copyInW     *io.PipeWriter
	copyInDone  chan copyDone
	copyOutCh   chan []byte
	copyOutDone chan copyDone
	copyOutPeek []byte
	hasCopyPeek bool
	copyOutEOF  bool
}

func (c *pgConn) pollAsyncNonBlocking() {
	if c.asyncDone == nil {
		return
	}
	select {
	case raw := <-c.asyncDone:
		c.asyncDone = nil
		for _, ar := range raw {
			if ar.prepDesc != nil {
				c.prepared[ar.prepName] = ar.prepDesc
			}
			c.pendingRaw = append(c.pendingRaw, ar)
		}
	default:
	}
}

func (c *pgConn) waitAsync(mrb *oruby.MrbState, self oruby.Value) {
	for c.asyncDone != nil {
		ch := c.asyncDone
		var got bool
		var raw []asyncRes
		mrb.Blocking(func() {
			select {
			case raw = <-ch:
				got = true
			case <-c.noticeCh:
			}
		})
		c.flushNotices(mrb, self)
		if got {
			c.asyncDone = nil
			for _, ar := range raw {
				if ar.prepDesc != nil {
					c.prepared[ar.prepName] = ar.prepDesc
				}
				c.pendingRaw = append(c.pendingRaw, ar)
			}
		}
	}
}

func (c *pgConn) pollConnect(wait time.Duration) int {
	if c.closed {
		return pgresPollingFailed
	}
	if c.connectDone == nil {
		if c.conn != nil && !c.conn.IsClosed() {
			return pgresPollingOK
		}
		return pgresPollingFailed
	}
	if wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case res := <-c.connectDone:
			c.connectDone = nil
			if res.err != nil {
				c.lastErr = res.err.Error()
				c.closed = true
				return pgresPollingFailed
			}
			c.conn = res.conn
			return pgresPollingOK
		case <-timer.C:
			return pgresPollingReading
		}
	}
	select {
	case res := <-c.connectDone:
		c.connectDone = nil
		if res.err != nil {
			c.lastErr = res.err.Error()
			c.closed = true
			return pgresPollingFailed
		}
		c.conn = res.conn
		return pgresPollingOK
	default:
		return pgresPollingReading
	}
}

func (c *pgConn) cleanupCopyOnClose() {
	if c.copyInW != nil {
		_ = c.copyInW.CloseWithError(errors.New("connection closed"))
		c.copyInW = nil
	}
	c.copyMode = 0
	c.copyInDone = nil
	c.copyOutCh = nil
	c.copyOutDone = nil
	c.copyOutPeek = nil
	c.hasCopyPeek = false
	c.copyOutEOF = false
}

func (c *pgConn) Memsize() int {
	return 256
}

func sqlToUTF8(mrb *oruby.MrbState, v oruby.Value) string {
	if !v.IsString() {
		return mrb.String(v)
	}
	if encVal, err := mrb.Funcall(v, mrb.Intern("encoding")); err == nil && !encVal.IsNil() {
		if nameVal, nerr := mrb.Funcall(encVal, mrb.Intern("name")); nerr == nil {
			name := nameVal.String()
			if name != "UTF-8" && name != "US-ASCII" && name != "ASCII-8BIT" {
				if conv, cerr := mrb.Funcall(v, mrb.Intern("encode"), mrb.StrNew("UTF-8")); cerr == nil {
					return conv.String()
				}
			}
		}
	}
	return v.String()
}

func (c *pgConn) flushNotices(mrb *oruby.MrbState, self oruby.Value) {
	c.noticeMu.Lock()
	if len(c.notices) == 0 {
		c.noticeMu.Unlock()
		return
	}
	batch := c.notices
	c.notices = nil
	c.noticeMu.Unlock()

	proc := mrb.IVGet(self, mrb.Intern("@notice_processor"))
	if !proc.IsNil() {
		for _, n := range batch {
			msg := n.Severity + ":  " + n.Message + "\n"
			_, _ = mrb.Funcall(proc, mrb.Intern("call"), mrb.StrNew(msg))
		}
	}
	recv := mrb.IVGet(self, mrb.Intern("@notice_receiver"))
	if !recv.IsNil() {
		for _, n := range batch {
			resCls := rb.ClassPath(mrb, "PG::Result")
			r := &pgResult{
				status:  pgresNonfatalError,
				errMsg:  n.Severity + ":  " + n.Message + "\n",
				pgErr:   &pgconn.PgError{Severity: n.Severity, Code: n.Code, Message: n.Message, Detail: n.Detail, Hint: n.Hint},
				typeMap: mrb.NilValue(),
			}
			rv := mrb.DataWrapInterface(resCls, r).Value()
			_, _ = mrb.Funcall(recv, mrb.Intern("call"), rv)
		}
	}
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

func extractParams(mrb *oruby.MrbState, paramsVal, typeMap oruby.Value) ([][]byte, []uint32, []int16, error) {
	if paramsVal.IsNil() {
		return nil, nil, nil, nil
	}
	if !paramsVal.IsArray() {
		return nil, nil, nil, oruby.ETypeError("params must be an Array")
	}
	n := oruby.RArrayLen(paramsVal)
	vals := make([][]byte, n)
	oids := make([]uint32, n)
	fmts := make([]int16, n)
	hasOID := false
	useTypeMap := !typeMap.IsNil() && mrb.RespondTo(typeMap, mrb.Intern("__encode_param"))
	if useTypeMap && mrb.RespondTo(typeMap, mrb.Intern("fit_to_query")) {
		fitted, ferr := mrb.Funcall(typeMap, mrb.Intern("fit_to_query"), paramsVal)
		if ferr != nil {
			return nil, nil, nil, ferr
		}
		if !fitted.IsNil() {
			typeMap = fitted
		}
	}
	const maxTotalParamBytes = (1 << 30) - 1024
	var totalBytes int64
	for i := 0; i < n; i++ {
		if totalBytes > maxTotalParamBytes {
			return nil, nil, nil, errParamTooLarge
		}
		item := mrb.AryRef(paramsVal, i)
		if item.IsNil() {
			vals[i] = nil
			fmts[i] = 0
			continue
		}
		if item.IsHash() {
			// Check if this is a param spec hash ({value:, format:, type:}) or a plain Hash to be encoded by typeMap
			hasValKey, _ := mrb.Funcall(item, mrb.Intern("key?"), mrb.Intern("value").Value())
			hasFmtKey, _ := mrb.Funcall(item, mrb.Intern("key?"), mrb.Intern("format").Value())
			hasTypeKey, _ := mrb.Funcall(item, mrb.Intern("key?"), mrb.Intern("type").Value())
			if !useTypeMap || hasValKey.Bool() || hasFmtKey.Bool() || hasTypeKey.Bool() {
				v, _ := mrb.Funcall(item, mrb.Intern("[]"), mrb.Intern("value").Value())
				f, _ := mrb.Funcall(item, mrb.Intern("[]"), mrb.Intern("format").Value())
				t, _ := mrb.Funcall(item, mrb.Intern("[]"), mrb.Intern("type").Value())
				if v.IsNil() {
					vals[i] = nil
				} else if v.IsString() {
					b := v.Bytes()
					if int64(len(b))+totalBytes > maxTotalParamBytes {
						return nil, nil, nil, errParamTooLarge
					}
					vals[i] = append([]byte(nil), b...)
					totalBytes += int64(len(b))
				} else {
					s := mrb.String(v)
					if int64(len(s))+totalBytes > maxTotalParamBytes {
						return nil, nil, nil, errParamTooLarge
					}
					vals[i] = []byte(s)
					totalBytes += int64(len(s))
				}
				if !f.IsNil() {
					fmts[i] = int16(f.Int())
				}
				if !t.IsNil() && t.IsInt() {
					oids[i] = uint32(t.Int64())
					hasOID = true
				}
				continue
			}
		}
		if useTypeMap {
			enc, err := mrb.Funcall(typeMap, mrb.Intern("__encode_param"), item, mrb.FixnumValue(i))
			if err != nil {
				return nil, nil, nil, err
			}
			if enc.IsArray() && oruby.RArrayLen(enc) >= 3 {
				ev := mrb.AryRef(enc, 0)
				eo := mrb.AryRef(enc, 1)
				ef := mrb.AryRef(enc, 2)
				if ev.IsNil() {
					vals[i] = nil
				} else if ev.IsString() {
					b := ev.Bytes()
					if int64(len(b))+totalBytes > maxTotalParamBytes {
						return nil, nil, nil, errParamTooLarge
					}
					vals[i] = append([]byte(nil), b...)
					totalBytes += int64(len(b))
				} else {
					s := mrb.String(ev)
					if int64(len(s))+totalBytes > maxTotalParamBytes {
						return nil, nil, nil, errParamTooLarge
					}
					vals[i] = []byte(s)
					totalBytes += int64(len(s))
				}
				if !eo.IsNil() && eo.IsInt() && eo.Int64() != 0 {
					oids[i] = uint32(eo.Int64())
					hasOID = true
				}
				if !ef.IsNil() && ef.IsInt() {
					fmts[i] = int16(ef.Int())
				}
				continue
			}
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
			b := item.Bytes()
			if int64(len(b))+totalBytes > maxTotalParamBytes {
				return nil, nil, nil, errParamTooLarge
			}
			vals[i] = append([]byte(nil), b...)
			totalBytes += int64(len(b))
		default:
			s := mrb.String(item)
			if int64(len(s))+totalBytes > maxTotalParamBytes {
				return nil, nil, nil, errParamTooLarge
			}
			vals[i] = []byte(s)
			totalBytes += int64(len(s))
		}
	}
	if !hasOID {
		oids = nil
	}
	return vals, oids, fmts, nil
}

func wrapPGResult(mrb *oruby.MrbState, c *pgConn, r *pgconn.Result) oruby.Value {
	resCls := rb.ClassPath(mrb, "PG::Result")
	status := pgresCommandOK
	if r.Err != nil {
		status = pgresFatalError
	} else if len(r.FieldDescriptions) > 0 {
		status = pgresTuplesOK
	} else if r.CommandTag.String() == "" {
		status = pgresEmptyQuery
	}
	var pgErr *pgconn.PgError
	errMsg := ""
	if r.Err != nil {
		errors.As(r.Err, &pgErr)
		errMsg = r.Err.Error()
	}
	res := &pgResult{
		status:       status,
		fields:       r.FieldDescriptions,
		rows:         r.Rows,
		cmdTag:       r.CommandTag,
		pgErr:        pgErr,
		errMsg:       errMsg,
		typeMap:      c.typeMapResults,
		fieldNameSym: c.fieldNameSym,
	}
	val := mrb.DataWrapInterface(resCls, res).Value()
	if !res.typeMap.IsNil() {
		if mrb.RespondTo(res.typeMap, mrb.Intern("fit_to_result")) {
			if fitted, ferr := mrb.Funcall(res.typeMap, mrb.Intern("fit_to_result"), val); ferr == nil && !fitted.IsNil() {
				res.typeMap = fitted
			}
		}
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

		notifCh := make(chan *pgconn.Notification, 64)
		noticeCh := make(chan struct{}, 1)
		c := &pgConn{
			cfg:            cfg,
			notifCh:        notifCh,
			noticeCh:       noticeCh,
			typeMapResults: mrb.NilValue(),
			typeMapQueries: mrb.NilValue(),
			prepared:       map[string]*pgconn.StatementDescription{},
		}
		cfg.OnNotification = func(_ *pgconn.PgConn, n *pgconn.Notification) {
			select {
			case notifCh <- n:
			default:
			}
		}
		cfg.OnNotice = func(_ *pgconn.PgConn, n *pgconn.Notice) {
			c.noticeMu.Lock()
			c.notices = append(c.notices, n)
			c.noticeMu.Unlock()
			select {
			case noticeCh <- struct{}{}:
			default:
			}
		}

		ctx := mrb.GoContext()
		var conn *pgconn.PgConn
		mrb.Blocking(func() {
			conn, err = pgconn.ConnectConfig(ctx, cfg)
		})
		if err != nil {
			return rb.Raise(mrb, "PG::ConnectionBad", "%s", err.Error())
		}
		c.conn = conn
		rb.Set(mrb, self, c)
		return self
	}, mrb.ArgsAny())

	cls.DefineClassMethod("connect_start", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		kw := mrb.KeywordArgs().Value()
		blk := mrb.GetArgsBlock()
		cfg, err := parseConnConfig(mrb, args, kw)
		if err != nil {
			return rb.Raise(mrb, "PG::ConnectionBad", "%s", err.Error())
		}
		notifCh := make(chan *pgconn.Notification, 64)
		noticeCh := make(chan struct{}, 1)
		connectCh := make(chan connectRes, 1)
		ctx, cancel := context.WithCancel(context.Background())
		c := &pgConn{
			cfg:            cfg,
			notifCh:        notifCh,
			noticeCh:       noticeCh,
			connectDone:    connectCh,
			connectCancel:  cancel,
			typeMapResults: mrb.NilValue(),
			typeMapQueries: mrb.NilValue(),
			prepared:       map[string]*pgconn.StatementDescription{},
		}
		cfg.OnNotification = func(_ *pgconn.PgConn, n *pgconn.Notification) {
			select {
			case notifCh <- n:
			default:
			}
		}
		cfg.OnNotice = func(_ *pgconn.PgConn, n *pgconn.Notice) {
			c.noticeMu.Lock()
			c.notices = append(c.notices, n)
			c.noticeMu.Unlock()
			select {
			case noticeCh <- struct{}{}:
			default:
			}
		}
		go func() {
			conn, cerr := pgconn.ConnectConfig(ctx, cfg)
			connectCh <- connectRes{conn: conn, err: cerr}
			close(connectCh)
		}()
		connVal := mrb.DataWrapInterface(cls, c).Value()
		if !blk.IsNil() {
			out, yerr := mrb.YieldE(blk, connVal)
			_, _ = mrb.Funcall(connVal, mrb.Intern("finish"))
			if yerr != nil {
				return mrb.RaiseError(yerr)
			}
			return out
		}
		return connVal
	}, mrb.ArgsAny())

	cls.DefineMethod("connect_poll", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil {
			return mrb.FixnumValue(pgresPollingFailed)
		}
		var st int
		mrb.Blocking(func() {
			st = c.pollConnect(25 * time.Millisecond)
		})
		return mrb.FixnumValue(st)
	}, mrb.ArgsNone())

	closeFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil {
			return mrb.NilValue()
		}
		if c.connectCancel != nil {
			c.connectCancel()
			c.connectCancel = nil
		}
		c.connectDone = nil
		c.cleanupCopyOnClose()
		c.asyncDone = nil
		c.pendingRaw = nil
		if !c.closed && c.conn != nil {
			c.closed = true
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			mrb.Blocking(func() {
				_ = c.conn.Close(ctx)
			})
		} else {
			c.closed = true
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
		c.cleanupCopyOnClose()
		c.asyncDone = nil
		c.pendingRaw = nil
		ctx := mrb.GoContext()
		var conn *pgconn.PgConn
		mrb.Blocking(func() {
			if c.conn != nil {
				_ = c.conn.Close(context.Background())
			}
			conn, err = pgconn.ConnectConfig(ctx, c.cfg)
		})
		if err != nil {
			c.closed = true
			c.lastErr = err.Error()
			return rb.Raise(mrb, "PG::ConnectionBad", "%s", err.Error())
		}
		c.conn = conn
		c.closed = false
		c.prepared = map[string]*pgconn.StatementDescription{}
		return mrb.NilValue()
	}, mrb.ArgsNone())

	cls.DefineMethod("error_message", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil {
			return mrb.StrNew("")
		}
		return mrb.StrNew(c.lastErr)
	}, mrb.ArgsNone())

	cls.DefineMethod("cancel", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return mrb.StrNew("connection is closed")
		}
		ctx, cancel := context.WithTimeout(mrb.GoContext(), 5*time.Second)
		defer cancel()
		var cerr error
		mrb.Blocking(func() {
			cerr = c.conn.CancelRequest(ctx)
		})
		if cerr != nil {
			return mrb.StrNew(cerr.Error())
		}
		return mrb.NilValue()
	}, mrb.ArgsNone())

	cls.DefineMethod("field_name_type", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := getConn(mrb, self)
		if c != nil && c.fieldNameSym {
			return mrb.Intern("symbol").Value()
		}
		return mrb.Intern("string").Value()
	}, mrb.ArgsNone())

	cls.DefineMethod("field_name_type=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := getConn(mrb, self)
		v := mrb.GetArgsFirst()
		if !v.IsSymbol() {
			return mrb.ETypeError().Raise("expected Symbol (:string or :symbol)")
		}
		switch mrb.SymName(v.Symbol()) {
		case "symbol":
			c.fieldNameSym = true
		case "string":
			c.fieldNameSym = false
		default:
			return mrb.EArgumentError().Raisef("invalid field_name_type %q", mrb.SymName(v.Symbol()))
		}
		return v
	}, mrb.ArgsReq(1))

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
	cls.DefineMethod("hostaddr", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := getConn(mrb, self)
		if c.cfg.Host == "localhost" {
			return mrb.StrNew("127.0.0.1")
		}
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
			return rb.Raise(mrb, "PG::ConnectionBad", "connection is closed")
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

	cls.DefineMethod("protocol_version", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return rb.Raise(mrb, "PG::ConnectionBad", "connection is closed")
		}
		return mrb.FixnumValue(3)
	}, mrb.ArgsNone())

	cls.DefineMethod("full_protocol_version", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return rb.Raise(mrb, "PG::ConnectionBad", "connection is closed")
		}
		return mrb.FixnumValue(30000)
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
		if c.typeMapResults.IsNil() {
			tmCls := rb.ClassPath(mrb, "PG::TypeMapAllStrings")
			if v, err := mrb.Funcall(tmCls, mrb.Intern("new")); err == nil {
				return v
			}
		}
		return c.typeMapResults
	}, mrb.ArgsNone())
	cls.DefineMethod("type_map_for_results=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := getConn(mrb, self)
		v := mrb.GetArgsFirst()
		tmCls := rb.ClassPath(mrb, "PG::TypeMap")
		if !mrb.ObjIsKindOf(v, tmCls) {
			return mrb.ETypeError().Raise("wrong argument type (expected PG::TypeMap)")
		}
		c.typeMapResults = v
		mrb.IVSet(self, mrb.Intern("@__tm_res"), v)
		return v
	}, mrb.ArgsReq(1))
	cls.DefineMethod("type_map_for_queries", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := getConn(mrb, self)
		if c.typeMapQueries.IsNil() {
			tmCls := rb.ClassPath(mrb, "PG::TypeMapAllStrings")
			if v, err := mrb.Funcall(tmCls, mrb.Intern("new")); err == nil {
				return v
			}
		}
		return c.typeMapQueries
	}, mrb.ArgsNone())
	cls.DefineMethod("type_map_for_queries=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := getConn(mrb, self)
		v := mrb.GetArgsFirst()
		tmCls := rb.ClassPath(mrb, "PG::TypeMap")
		if !mrb.ObjIsKindOf(v, tmCls) {
			return mrb.ETypeError().Raise("wrong argument type (expected PG::TypeMap)")
		}
		c.typeMapQueries = v
		mrb.IVSet(self, mrb.Intern("@__tm_qry"), v)
		return v
	}, mrb.ArgsReq(1))

	execFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil {
			return rb.Raise(mrb, "PG::ConnectionBad", "connection is closed")
		}
		if c.connectDone != nil {
			if c.pollConnect(0) != pgresPollingOK {
				excCls := rb.ClassPath(mrb, "PG::UnableToSend")
				if exc, cerr := mrb.Funcall(excCls, mrb.Intern("new"), mrb.StrNew("no connection to the server")); cerr == nil {
					mrb.IVSet(exc, mrb.Intern("@connection"), self)
					mrb.ExcRaise(exc)
					return mrb.NilValue()
				}
				return rb.Raise(mrb, "PG::UnableToSend", "no connection to the server")
			}
		}
		if c.checkOpen() != nil {
			return rb.Raise(mrb, "PG::ConnectionBad", "connection is closed")
		}
		if c.asyncDone != nil {
			c.waitAsync(mrb, self)
		}
		c.pendingRaw = nil
		args := mrb.GetArgs()
		blk := mrb.GetArgsBlock()
		sql := sqlToUTF8(mrb, args.Item(0))

		// If params passed to exec/query(sql, params, result_format = 0, type_map = nil) -> delegate to ExecParams
		if args.Len() >= 2 && !args.Item(1).IsNil() {
			resFmt := 0
			if args.Len() >= 3 && !args.Item(2).IsNil() {
				resFmt = args.Item(2).Int()
			}
			tm := c.typeMapQueries
			if args.Len() >= 4 && !args.Item(3).IsNil() {
				tm = args.Item(3)
			}
			return execParamsImpl(mrb, self, c, sql, args.Item(1), resFmt, tm, blk)
		}

		// Handle COPY ... FROM STDIN / TO STDOUT
		if copyMode, copyBin := detectCopySQL(sql); copyMode != 0 {
			return startCopyExec(mrb, self, c, sql, copyMode, copyBin, blk)
		}

		ctx := mrb.GoContext()
		var results []*pgconn.Result
		mrb.Blocking(func() {
			results, err = c.conn.Exec(ctx, sql).ReadAll()
		})
		c.flushNotices(mrb, self)
		if err != nil {
			c.lastErr = err.Error()
			return raisePGError(mrb, self, err)
		}
		c.lastErr = ""
		var resVal oruby.Value
		if len(results) == 0 {
			resVal = wrapPGResult(mrb, c, &pgconn.Result{})
		} else {
			resVal = wrapPGResult(mrb, c, results[len(results)-1])
		}
		return yieldAndClearResult(mrb, resVal, blk)
	}
	cls.DefineMethod("exec", execFn, mrb.ArgsReq(1)|mrb.ArgsOpt(3)|mrb.ArgsBlock())
	cls.DefineMethod("query", execFn, mrb.ArgsReq(1)|mrb.ArgsOpt(3)|mrb.ArgsBlock())

	cls.DefineMethod("exec_params", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return rb.Raise(mrb, "PG::ConnectionBad", "connection is closed")
		}
		if c.asyncDone != nil {
			c.waitAsync(mrb, self)
		}
		c.pendingRaw = nil
		args := mrb.GetArgs()
		blk := mrb.GetArgsBlock()
		sql := sqlToUTF8(mrb, args.Item(0))
		params := mrb.NilValue()
		if args.Len() >= 2 {
			params = args.Item(1)
		}
		resFmt := 0
		if args.Len() >= 3 && !args.Item(2).IsNil() {
			resFmt = args.Item(2).Int()
		}
		tm := c.typeMapQueries
		if args.Len() >= 4 && !args.Item(3).IsNil() {
			tm = args.Item(3)
		}
		return execParamsImpl(mrb, self, c, sql, params, resFmt, tm, blk)
	}, mrb.ArgsReq(1)|mrb.ArgsOpt(3)|mrb.ArgsBlock())

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
		ctx := mrb.GoContext()
		var sd *pgconn.StatementDescription
		mrb.Blocking(func() {
			sd, err = c.conn.Prepare(ctx, name, sql, oids)
		})
		c.flushNotices(mrb, self)
		if err != nil {
			c.lastErr = err.Error()
			return raisePGError(mrb, self, err)
		}
		c.lastErr = ""
		c.prepared[name] = sd
		resVal := wrapPGResult(mrb, c, &pgconn.Result{FieldDescriptions: sd.Fields})
		if r, gerr := getRes(mrb, resVal); gerr == nil {
			r.paramOIDs = sd.ParamOIDs
		}
		return resVal
	}, mrb.ArgsReq(2)|mrb.ArgsOpt(1))

	cls.DefineMethod("describe_prepared", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return rb.Raise(mrb, "PG::ConnectionBad", "connection is closed")
		}
		name := mrb.GetArgsFirst().String()
		sd, ok := c.prepared[name]
		if !ok {
			return rb.Raise(mrb, "PG::InvalidSqlStatementName", "prepared statement %q does not exist", name)
		}
		resVal := wrapPGResult(mrb, c, &pgconn.Result{FieldDescriptions: sd.Fields})
		if r, gerr := getRes(mrb, resVal); gerr == nil {
			r.paramOIDs = sd.ParamOIDs
		}
		return resVal
	}, mrb.ArgsReq(1))

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
		resFmt := 0
		if args.Len() >= 3 && !args.Item(2).IsNil() {
			resFmt = args.Item(2).Int()
		}
		tm := c.typeMapQueries
		if args.Len() >= 4 && !args.Item(3).IsNil() {
			tm = args.Item(3)
		}
		vals, _, fmts, err := extractParams(mrb, params, tm)
		if err != nil {
			return mrb.RaiseError(err)
		}
		var resFmts []int16
		if resFmt != 0 {
			resFmts = []int16{int16(resFmt)}
		}
		ctx := mrb.GoContext()
		var res *pgconn.Result
		mrb.Blocking(func() {
			res = c.conn.ExecPrepared(ctx, name, vals, fmts, resFmts).Read()
		})
		c.flushNotices(mrb, self)
		if res.Err != nil {
			c.lastErr = res.Err.Error()
			return raisePGError(mrb, self, res.Err)
		}
		c.lastErr = ""
		resVal := wrapPGResult(mrb, c, res)
		return yieldAndClearResult(mrb, resVal, blk)
	}, mrb.ArgsReq(1)|mrb.ArgsOpt(3)|mrb.ArgsBlock())

	// Truly asynchronous query methods (send_query* run in a Go goroutine so
	// callers can cancel, wait_for_notify, or check is_busy while the query runs).
	cls.DefineMethod("send_query", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return rb.Raise(mrb, "PG::ConnectionBad", "connection is closed")
		}
		c.pollAsyncNonBlocking()
		if c.asyncDone != nil {
			return rb.Raise(mrb, "PG::Error", "another command is already in progress")
		}
		args := mrb.GetArgs()
		sql := args.Item(0).String()
		if args.Len() >= 2 && !args.Item(1).IsNil() {
			resFmt := 0
			if args.Len() >= 3 && !args.Item(2).IsNil() {
				resFmt = args.Item(2).Int()
			}
			tm := c.typeMapQueries
			if args.Len() >= 4 && !args.Item(3).IsNil() {
				tm = args.Item(3)
			}
			vals, oids, fmts, perr := extractParams(mrb, args.Item(1), tm)
			if perr != nil {
				return mrb.RaiseError(perr)
			}
			var resFmts []int16
			if resFmt != 0 {
				resFmts = []int16{int16(resFmt)}
			}
			ctx := mrb.GoContext()
			ch := make(chan []asyncRes, 1)
			c.asyncDone = ch
			go func() {
				res := c.conn.ExecParams(ctx, sql, vals, oids, fmts, resFmts).Read()
				ch <- []asyncRes{{res: res}}
				close(ch)
			}()
			return mrb.NilValue()
		}
		ctx := mrb.GoContext()
		ch := make(chan []asyncRes, 1)
		c.asyncDone = ch
		go func() {
			var out []asyncRes
			mrr := c.conn.Exec(ctx, sql)
			for mrr.NextResult() {
				rr := mrr.ResultReader()
				r := rr.Read()
				out = append(out, asyncRes{res: r})
			}
			qerr := mrr.Close()
			if len(out) == 0 {
				if qerr != nil {
					out = append(out, asyncRes{res: &pgconn.Result{Err: qerr}})
				} else {
					out = append(out, asyncRes{res: &pgconn.Result{}})
				}
			}
			ch <- out
			close(ch)
		}()
		return mrb.NilValue()
	}, mrb.ArgsReq(1)|mrb.ArgsOpt(3))

	cls.DefineMethod("send_query_params", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return rb.Raise(mrb, "PG::ConnectionBad", "connection is closed")
		}
		c.pollAsyncNonBlocking()
		if c.asyncDone != nil {
			return rb.Raise(mrb, "PG::Error", "another command is already in progress")
		}
		args := mrb.GetArgs()
		sql := args.Item(0).String()
		params := mrb.NilValue()
		if args.Len() >= 2 {
			params = args.Item(1)
		}
		resFmt := 0
		if args.Len() >= 3 && !args.Item(2).IsNil() {
			resFmt = args.Item(2).Int()
		}
		tm := c.typeMapQueries
		if args.Len() >= 4 && !args.Item(3).IsNil() {
			tm = args.Item(3)
		}
		vals, oids, fmts, perr := extractParams(mrb, params, tm)
		if perr != nil {
			return mrb.RaiseError(perr)
		}
		var resFmts []int16
		if resFmt != 0 {
			resFmts = []int16{int16(resFmt)}
		}
		ctx := mrb.GoContext()
		ch := make(chan []asyncRes, 1)
		c.asyncDone = ch
		go func() {
			res := c.conn.ExecParams(ctx, sql, vals, oids, fmts, resFmts).Read()
			ch <- []asyncRes{{res: res}}
			close(ch)
		}()
		return mrb.NilValue()
	}, mrb.ArgsReq(1)|mrb.ArgsOpt(3))

	cls.DefineMethod("send_prepare", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return rb.Raise(mrb, "PG::ConnectionBad", "connection is closed")
		}
		c.pollAsyncNonBlocking()
		if c.asyncDone != nil {
			return rb.Raise(mrb, "PG::Error", "another command is already in progress")
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
		ctx := mrb.GoContext()
		ch := make(chan []asyncRes, 1)
		c.asyncDone = ch
		go func() {
			sd, perr := c.conn.Prepare(ctx, name, sql, oids)
			if perr != nil {
				ch <- []asyncRes{{res: &pgconn.Result{Err: perr}}}
			} else {
				ch <- []asyncRes{{
					res:       &pgconn.Result{FieldDescriptions: sd.Fields},
					paramOIDs: sd.ParamOIDs,
					prepName:  name,
					prepDesc:  sd,
				}}
			}
			close(ch)
		}()
		return mrb.NilValue()
	}, mrb.ArgsReq(2)|mrb.ArgsOpt(1))

	cls.DefineMethod("send_query_prepared", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return rb.Raise(mrb, "PG::ConnectionBad", "connection is closed")
		}
		c.pollAsyncNonBlocking()
		if c.asyncDone != nil {
			return rb.Raise(mrb, "PG::Error", "another command is already in progress")
		}
		args := mrb.GetArgs()
		name := args.Item(0).String()
		params := mrb.NilValue()
		if args.Len() >= 2 {
			params = args.Item(1)
		}
		resFmt := 0
		if args.Len() >= 3 && !args.Item(2).IsNil() {
			resFmt = args.Item(2).Int()
		}
		tm := c.typeMapQueries
		if args.Len() >= 4 && !args.Item(3).IsNil() {
			tm = args.Item(3)
		}
		vals, _, fmts, perr := extractParams(mrb, params, tm)
		if perr != nil {
			return mrb.RaiseError(perr)
		}
		var resFmts []int16
		if resFmt != 0 {
			resFmts = []int16{int16(resFmt)}
		}
		ctx := mrb.GoContext()
		ch := make(chan []asyncRes, 1)
		c.asyncDone = ch
		go func() {
			res := c.conn.ExecPrepared(ctx, name, vals, fmts, resFmts).Read()
			ch <- []asyncRes{{res: res}}
			close(ch)
		}()
		return mrb.NilValue()
	}, mrb.ArgsReq(1)|mrb.ArgsOpt(3))

	cls.DefineMethod("is_busy", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return rb.Raise(mrb, "PG::ConnectionBad", "connection is closed")
		}
		c.pollAsyncNonBlocking()
		c.flushNotices(mrb, self)
		return mrb.BoolValue(c.asyncDone != nil)
	}, mrb.ArgsNone())

	cls.DefineMethod("consume_input", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return rb.Raise(mrb, "PG::ConnectionBad", "connection is closed")
		}
		c.pollAsyncNonBlocking()
		c.flushNotices(mrb, self)
		return mrb.NilValue()
	}, mrb.ArgsNone())

	cls.DefineMethod("block", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return rb.Raise(mrb, "PG::ConnectionBad", "connection is closed")
		}
		args := mrb.GetArgs()
		var hasTimeout bool
		var dur time.Duration
		if args.Len() >= 1 && !args.Item(0).IsNil() {
			hasTimeout = true
			if args.Item(0).IsInt() {
				dur = time.Duration(args.Item(0).Int64()) * time.Second
			} else {
				dur = time.Duration(args.Item(0).Float64() * float64(time.Second))
			}
		}
		if c.asyncDone != nil {
			ch := c.asyncDone
			var timedOut bool
			if hasTimeout {
				deadline := time.Now().Add(dur)
				for c.asyncDone != nil {
					rem := time.Until(deadline)
					if rem <= 0 {
						timedOut = true
						break
					}
					timer := time.NewTimer(rem)
					var got bool
					var raw []asyncRes
					mrb.Blocking(func() {
						select {
						case raw = <-ch:
							got = true
						case <-c.noticeCh:
						case <-timer.C:
							timedOut = true
						}
					})
					timer.Stop()
					c.flushNotices(mrb, self)
					if got {
						c.asyncDone = nil
						for _, ar := range raw {
							if ar.prepDesc != nil {
								c.prepared[ar.prepName] = ar.prepDesc
							}
							c.pendingRaw = append(c.pendingRaw, ar)
						}
						break
					}
					if timedOut {
						break
					}
				}
			} else {
				c.waitAsync(mrb, self)
			}
			return mrb.BoolValue(!timedOut)
		}
		c.flushNotices(mrb, self)
		return mrb.TrueValue()
	}, mrb.ArgsOpt(1))

	wrapAsyncRes := func(mrb *oruby.MrbState, c *pgConn, ar asyncRes) oruby.Value {
		rv := wrapPGResult(mrb, c, ar.res)
		if len(ar.paramOIDs) > 0 {
			if r, gerr := getRes(mrb, rv); gerr == nil {
				r.paramOIDs = ar.paramOIDs
			}
		}
		return rv
	}

	cls.DefineMethod("get_result", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil {
			return mrb.NilValue()
		}
		if c.copyMode == pgresCopyOut && c.copyOutDone != nil {
			if c.copyOutEOF {
				var done copyDone
				mrb.Blocking(func() {
					done = <-c.copyOutDone
				})
				c.copyMode = 0
				c.copyOutCh = nil
				c.copyOutDone = nil
				c.copyOutEOF = false
				c.flushNotices(mrb, self)
				tag := done.tag
				if done.err == nil && tag.String() == "" {
					tag = pgconn.NewCommandTag("COPY 0")
				}
				c.pendingRaw = append(c.pendingRaw, asyncRes{res: &pgconn.Result{CommandTag: tag, Err: done.err}})
			} else {
				resCls := rb.ClassPath(mrb, "PG::Result")
				r := &pgResult{status: pgresCopyOut, typeMap: mrb.NilValue()}
				return mrb.DataWrapInterface(resCls, r).Value()
			}
		}
		if len(c.pendingRaw) == 0 && c.asyncDone != nil {
			c.waitAsync(mrb, self)
		}
		if len(c.pendingRaw) == 0 {
			return mrb.NilValue()
		}
		ar := c.pendingRaw[0]
		c.pendingRaw = c.pendingRaw[1:]
		rv := wrapAsyncRes(mrb, c, ar)
		blk := mrb.GetArgsBlock()
		return yieldAndClearResult(mrb, rv, blk)
	}, mrb.ArgsBlock())

	cls.DefineMethod("get_last_result", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil {
			return mrb.NilValue()
		}
		if c.copyMode == pgresCopyOut && c.copyOutDone != nil {
			if c.copyOutEOF {
				var done copyDone
				mrb.Blocking(func() {
					done = <-c.copyOutDone
				})
				c.copyMode = 0
				c.copyOutCh = nil
				c.copyOutDone = nil
				c.copyOutEOF = false
				c.flushNotices(mrb, self)
				tag := done.tag
				if done.err == nil && tag.String() == "" {
					tag = pgconn.NewCommandTag("COPY 0")
				}
				c.pendingRaw = []asyncRes{{res: &pgconn.Result{CommandTag: tag, Err: done.err}}}
			} else {
				resCls := rb.ClassPath(mrb, "PG::Result")
				r := &pgResult{status: pgresCopyOut, typeMap: mrb.NilValue()}
				return mrb.DataWrapInterface(resCls, r).Value()
			}
		}
		if c.asyncDone != nil {
			c.waitAsync(mrb, self)
		}
		if len(c.pendingRaw) == 0 {
			return mrb.NilValue()
		}
		ar := c.pendingRaw[len(c.pendingRaw)-1]
		c.pendingRaw = nil
		if ar.res != nil && ar.res.Err != nil {
			c.lastErr = ar.res.Err.Error()
			return raisePGError(mrb, self, ar.res.Err)
		}
		rv := wrapAsyncRes(mrb, c, ar)
		blk := mrb.GetArgsBlock()
		return yieldAndClearResult(mrb, rv, blk)
	}, mrb.ArgsBlock())

	cls.DefineMethod("discard_results", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil {
			return mrb.NilValue()
		}
		if c.asyncDone != nil {
			c.waitAsync(mrb, self)
		}
		if c.copyMode == pgresCopyOut {
			ch := c.copyOutCh
			doneCh := c.copyOutDone
			mrb.Blocking(func() {
				if ch != nil {
					for range ch {
					}
				}
				if doneCh != nil {
					<-doneCh
				}
			})
			c.copyMode = 0
			c.copyOutCh = nil
			c.copyOutDone = nil
			c.copyOutPeek = nil
			c.hasCopyPeek = false
			c.copyOutEOF = false
		}
		if c.copyMode == pgresCopyIn {
			if c.copyInW != nil {
				_ = c.copyInW.CloseWithError(errors.New("discarded"))
				c.copyInW = nil
			}
			if c.copyInDone != nil {
				doneCh := c.copyInDone
				mrb.Blocking(func() {
					<-doneCh
				})
				c.copyInDone = nil
			}
			c.copyMode = 0
		}
		c.pendingRaw = nil
		c.flushNotices(mrb, self)
		return mrb.NilValue()
	}, mrb.ArgsNone())

	// COPY protocol methods
	putCopyDataFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return rb.Raise(mrb, "PG::ConnectionBad", "connection is closed")
		}
		if c.copyMode != pgresCopyIn || c.copyInW == nil {
			return rb.Raise(mrb, "PG::Error", "no COPY in progress")
		}
		args := mrb.GetArgs()
		data := args.Item(0)
		var coder oruby.Value = mrb.NilValue()
		if args.Len() >= 2 && !args.Item(1).IsNil() {
			coder = args.Item(1)
		} else {
			coder = mrb.IVGet(self, mrb.Intern("@encoder_for_put_copy_data"))
		}
		if !coder.IsNil() {
			enc, cerr := mrb.Funcall(coder, mrb.Intern("encode"), data)
			if cerr != nil {
				return mrb.RaiseError(cerr)
			}
			data = enc
		}
		var buf []byte
		if data.IsString() {
			buf = data.Bytes()
		} else {
			buf = []byte(mrb.String(data))
		}
		var werr error
		mrb.Blocking(func() {
			_, werr = c.copyInW.Write(buf)
		})
		if werr != nil {
			return rb.Raise(mrb, "PG::Error", "%s", werr.Error())
		}
		return mrb.TrueValue()
	}
	cls.DefineMethod("put_copy_data", putCopyDataFn, mrb.ArgsReq(1)|mrb.ArgsOpt(1))
	cls.DefineMethod("sync_put_copy_data", putCopyDataFn, mrb.ArgsReq(1)|mrb.ArgsOpt(1))
	cls.DefineMethod("async_put_copy_data", putCopyDataFn, mrb.ArgsReq(1)|mrb.ArgsOpt(1))

	putCopyEndFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return rb.Raise(mrb, "PG::ConnectionBad", "connection is closed")
		}
		if c.copyMode != pgresCopyIn || c.copyInW == nil {
			return rb.Raise(mrb, "PG::Error", "no COPY in progress")
		}
		args := mrb.GetArgs()
		var errMsg string
		if args.Len() >= 1 && !args.Item(0).IsNil() {
			errMsg = args.Item(0).String()
		}
		pw := c.copyInW
		doneCh := c.copyInDone
		c.copyInW = nil
		c.copyInDone = nil
		c.copyMode = 0
		var done copyDone
		mrb.Blocking(func() {
			if errMsg != "" {
				_ = pw.CloseWithError(errors.New(errMsg))
			} else {
				_ = pw.Close()
			}
			if doneCh != nil {
				done = <-doneCh
			}
		})
		c.flushNotices(mrb, self)
		c.pendingRaw = append(c.pendingRaw, asyncRes{res: &pgconn.Result{CommandTag: done.tag, Err: done.err}})
		return mrb.TrueValue()
	}
	cls.DefineMethod("put_copy_end", putCopyEndFn, mrb.ArgsOpt(1))
	cls.DefineMethod("sync_put_copy_end", putCopyEndFn, mrb.ArgsOpt(1))
	cls.DefineMethod("async_put_copy_end", putCopyEndFn, mrb.ArgsOpt(1))

	getCopyDataFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return rb.Raise(mrb, "PG::ConnectionBad", "connection is closed")
		}
		if c.copyMode != pgresCopyOut {
			return rb.Raise(mrb, "PG::Error", "no COPY in progress")
		}
		args := mrb.GetArgs()
		asyncMode := false
		if args.Len() >= 1 && args.Item(0).Bool() {
			asyncMode = true
		}
		var coder oruby.Value = mrb.NilValue()
		if args.Len() >= 2 && !args.Item(1).IsNil() {
			coder = args.Item(1)
		} else {
			coder = mrb.IVGet(self, mrb.Intern("@decoder_for_get_copy_data"))
		}
		var chunk []byte
		var ok bool
		if c.hasCopyPeek {
			chunk = c.copyOutPeek
			c.copyOutPeek = nil
			c.hasCopyPeek = false
			ok = true
		} else if c.copyOutEOF {
			return mrb.NilValue()
		} else if asyncMode {
			select {
			case chunk, ok = <-c.copyOutCh:
				if !ok {
					c.copyOutEOF = true
					return mrb.NilValue()
				}
			default:
				return mrb.FalseValue()
			}
		} else {
			ch := c.copyOutCh
			mrb.Blocking(func() {
				chunk, ok = <-ch
			})
			if !ok {
				c.copyOutEOF = true
				return mrb.NilValue()
			}
		}
		var strVal oruby.Value
		if c.copyBinary {
			strVal = mrb.Call(mrb.BytesValue(chunk), "b").Value()
		} else {
			strVal = mrb.BytesValue(chunk)
		}
		if !coder.IsNil() {
			dec, derr := mrb.Funcall(coder, mrb.Intern("decode"), strVal)
			if derr != nil {
				return mrb.RaiseError(derr)
			}
			return dec
		}
		return strVal
	}
	cls.DefineMethod("get_copy_data", getCopyDataFn, mrb.ArgsOpt(2))
	cls.DefineMethod("sync_get_copy_data", getCopyDataFn, mrb.ArgsOpt(2))
	cls.DefineMethod("async_get_copy_data", getCopyDataFn, mrb.ArgsOpt(2))

	cls.DefineMethod("notifies", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil {
			return mrb.NilValue()
		}
		select {
		case notif := <-c.notifCh:
			h := mrb.HashNewCapa(3)
			mrb.HashSet(h, mrb.StrNew("relname"), mrb.StrNew(notif.Channel))
			mrb.HashSet(h, mrb.StrNew("be_pid"), mrb.FixnumValue(int(notif.PID)))
			mrb.HashSet(h, mrb.StrNew("extra"), mrb.StrNew(notif.Payload))
			return h
		default:
			return mrb.NilValue()
		}
	}, mrb.ArgsNone())

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
			hasTimeout := false
			var deadline time.Time
			if args.Len() >= 1 && !args.Item(0).IsNil() {
				hasTimeout = true
				var sec float64
				if args.Item(0).IsInt() {
					sec = float64(args.Item(0).Int64())
				} else {
					sec = args.Item(0).Float64()
				}
				deadline = time.Now().Add(time.Duration(sec * float64(time.Second)))
			}

			// If an async query is running on this connection, wait on notifCh/noticeCh/asyncDone
			// without racing c.conn.WaitForNotification.
			for c.asyncDone != nil && notif == nil {
				ch := c.asyncDone
				var timedOut bool
				var gotAsync bool
				var raw []asyncRes
				var timer *time.Timer
				var timerC <-chan time.Time
				if hasTimeout {
					rem := time.Until(deadline)
					if rem <= 0 {
						c.flushNotices(mrb, self)
						return mrb.NilValue()
					}
					timer = time.NewTimer(rem)
					timerC = timer.C
				}
				mrb.Blocking(func() {
					select {
					case notif = <-c.notifCh:
					case <-c.noticeCh:
					case raw = <-ch:
						gotAsync = true
					case <-timerC:
						timedOut = true
					}
				})
				if timer != nil {
					timer.Stop()
				}
				c.flushNotices(mrb, self)
				if gotAsync {
					c.asyncDone = nil
					for _, ar := range raw {
						if ar.prepDesc != nil {
							c.prepared[ar.prepName] = ar.prepDesc
						}
						c.pendingRaw = append(c.pendingRaw, ar)
					}
				}
				if timedOut {
					return mrb.NilValue()
				}
			}

			if notif == nil {
				ctx := mrb.GoContext()
				var cancel context.CancelFunc
				if hasTimeout {
					rem := time.Until(deadline)
					if rem <= 0 {
						c.flushNotices(mrb, self)
						return mrb.NilValue()
					}
					ctx, cancel = context.WithTimeout(ctx, rem)
					defer cancel()
				}
				var werr error
				mrb.Blocking(func() {
					werr = c.conn.WaitForNotification(ctx)
				})
				c.flushNotices(mrb, self)
				if werr != nil {
					if errors.Is(werr, context.DeadlineExceeded) {
						return mrb.NilValue()
					}
					return raisePGError(mrb, self, werr)
				}
				select {
				case notif = <-c.notifCh:
				default:
					return mrb.NilValue()
				}
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

func startCopyExec(mrb *oruby.MrbState, self oruby.Value, c *pgConn, sql string, mode int, binary bool, blk oruby.RProc) oruby.MrbValue {
	ctx := mrb.GoContext()
	if mode == pgresCopyIn {
		pr, pw := io.Pipe()
		doneCh := make(chan copyDone, 1)
		c.copyMode = pgresCopyIn
		c.copyBinary = binary
		c.copyInW = pw
		c.copyInDone = doneCh
		go func() {
			tag, err := c.conn.CopyFrom(ctx, pr, sql)
			_ = pr.Close()
			doneCh <- copyDone{tag: tag, err: err}
			close(doneCh)
		}()
		resCls := rb.ClassPath(mrb, "PG::Result")
		var fields []pgconn.FieldDescription
		if binary {
			fields = []pgconn.FieldDescription{{Format: 1}}
		}
		r := &pgResult{status: pgresCopyIn, fields: fields, typeMap: mrb.NilValue()}
		rv := mrb.DataWrapInterface(resCls, r).Value()
		return yieldAndClearResult(mrb, rv, blk)
	}

	// pgresCopyOut
	outCh := make(chan []byte, 64)
	doneCh := make(chan copyDone, 1)
	go func() {
		tag, err := c.conn.CopyTo(ctx, &chanWriter{ch: outCh}, sql)
		close(outCh)
		doneCh <- copyDone{tag: tag, err: err}
		close(doneCh)
	}()

	// Peek first chunk so immediate SQL errors raise before entering COPY_OUT state
	// and so we can determine nfields / binary_tuples accurately.
	var first []byte
	var hasFirst bool
	mrb.Blocking(func() {
		first, hasFirst = <-outCh
	})
	if !hasFirst {
		var done copyDone
		mrb.Blocking(func() {
			done = <-doneCh
		})
		c.flushNotices(mrb, self)
		if done.err != nil {
			c.lastErr = done.err.Error()
			return raisePGError(mrb, self, done.err)
		}
		// 0 rows copied out: still enter COPY_OUT state with EOF already reached
		c.copyMode = pgresCopyOut
		c.copyBinary = binary
		c.copyOutCh = outCh
		c.copyOutDone = make(chan copyDone, 1)
		c.copyOutDone <- done
		close(c.copyOutDone)
		c.copyOutEOF = true
	} else {
		c.copyMode = pgresCopyOut
		c.copyBinary = binary
		c.copyOutCh = outCh
		c.copyOutDone = doneCh
		c.copyOutPeek = first
		c.hasCopyPeek = true
		c.copyOutEOF = false
	}

	nfields := 1
	if hasFirst {
		if binary {
			// PGCOPY header is 19 bytes, then 2-byte big-endian field count for first tuple (if present)
			if len(first) >= 21 && string(first[:11]) == "PGCOPY\n\xff\r\n\x00" {
				fc := int(int16(uint16(first[19])<<8 | uint16(first[20])))
				if fc > 0 {
					nfields = fc
				}
			}
		} else {
			nfields = strings.Count(string(first), "\t") + 1
		}
	}
	fields := make([]pgconn.FieldDescription, nfields)
	if binary {
		for i := range fields {
			fields[i].Format = 1
		}
	}
	resCls := rb.ClassPath(mrb, "PG::Result")
	r := &pgResult{status: pgresCopyOut, fields: fields, typeMap: mrb.NilValue()}
	rv := mrb.DataWrapInterface(resCls, r).Value()
	return yieldAndClearResult(mrb, rv, blk)
}

func execParamsImpl(mrb *oruby.MrbState, self oruby.Value, c *pgConn, sql string, params oruby.Value, resFmt int, typeMap oruby.Value, blk oruby.RProc) oruby.MrbValue {
	vals, oids, fmts, err := extractParams(mrb, params, typeMap)
	if err != nil {
		if errors.Is(err, errParamTooLarge) {
			excCls := rb.ClassPath(mrb, "PG::UnableToSend")
			if exc, cerr := mrb.Funcall(excCls, mrb.Intern("new"), mrb.StrNew("cannot allocate memory for output buffer")); cerr == nil {
				mrb.IVSet(exc, mrb.Intern("@connection"), self)
				mrb.ExcRaise(exc)
				return mrb.NilValue()
			}
			return rb.Raise(mrb, "PG::UnableToSend", "cannot allocate memory for output buffer")
		}
		return mrb.RaiseError(err)
	}
	var resFmts []int16
	if resFmt != 0 {
		resFmts = []int16{int16(resFmt)}
	}
	ctx := mrb.GoContext()
	var res *pgconn.Result
	mrb.Blocking(func() {
		res = c.conn.ExecParams(ctx, sql, vals, oids, fmts, resFmts).Read()
	})
	c.flushNotices(mrb, self)
	if res.Err != nil {
		c.lastErr = res.Err.Error()
		return raisePGError(mrb, self, res.Err)
	}
	c.lastErr = ""
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
	status       int
	fields       []pgconn.FieldDescription
	rows         [][][]byte
	cmdTag       pgconn.CommandTag
	pgErr        *pgconn.PgError
	errMsg       string
	cleared      bool
	typeMap      oruby.Value
	fieldNameSym bool
	fieldsCalled bool
	paramOIDs    []uint32
}

func (r *pgResult) Memsize() int {
	return 128 + len(r.fields)*32 + len(r.rows)*64
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

func (r *pgResult) rawCell(mrb *oruby.MrbState, row, col int) oruby.Value {
	cell := r.rows[row][col]
	if cell == nil {
		return mrb.NilValue()
	}
	if col < len(r.fields) && r.fields[col].Format != 0 {
		return mrb.Call(mrb.BytesValue(cell), "b").Value()
	}
	return mrb.BytesValue(cell)
}

func (r *pgResult) decodeCell(mrb *oruby.MrbState, self oruby.Value, row, col int) (oruby.Value, error) {
	cell := r.rows[row][col]
	if cell == nil {
		return mrb.NilValue(), nil
	}
	raw := r.rawCell(mrb, row, col)
	if !r.typeMap.IsNil() {
		oid := int(r.fields[col].DataTypeOID)
		fmtCode := int(r.fields[col].Format)
		decoded, err := mrb.Funcall(r.typeMap, mrb.Intern("__decode_cell"), self, mrb.FixnumValue(row), mrb.FixnumValue(col), raw, mrb.FixnumValue(oid), mrb.FixnumValue(fmtCode))
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
		if self.Frozen() {
			return mrb.EFrozenError().Raise("can't modify frozen PG::Result")
		}
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
		r.fieldsCalled = true
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
		if err := r.checkNotCleared(); err != nil {
			return rb.Raise(mrb, "PG::Error", "%s", err.Error())
		}
		v := mrb.GetArgsFirst()
		var name string
		if v.IsSymbol() {
			name = mrb.SymName(v.Symbol())
		} else if v.IsString() {
			name = v.String()
		} else if mrb.RespondTo(v, mrb.Intern("to_str")) {
			sv, err := mrb.Funcall(v, mrb.Intern("to_str"))
			if err != nil {
				return mrb.RaiseError(err)
			}
			name = sv.String()
		} else {
			return mrb.ETypeError().Raise("expected String or Symbol")
		}
		for i, f := range r.fields {
			if f.Name == name {
				return mrb.FixnumValue(i)
			}
		}
		return mrb.FixnumValue(-1)
	}, mrb.ArgsReq(1))

	cls.DefineMethod("ftable", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := getRes(mrb, self)
		i := mrb.GetArgsFirst().Int()
		if i < 0 || i >= len(r.fields) {
			return mrb.EArgumentError().Raisef("column index %d out of range", i)
		}
		return mrb.Int64Value(int64(r.fields[i].TableOID))
	}, mrb.ArgsReq(1))

	cls.DefineMethod("ftablecol", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := getRes(mrb, self)
		i := mrb.GetArgsFirst().Int()
		if i < 0 || i >= len(r.fields) {
			return mrb.EArgumentError().Raisef("column index %d out of range", i)
		}
		return mrb.FixnumValue(int(r.fields[i].TableAttributeNumber))
	}, mrb.ArgsReq(1))

	cls.DefineMethod("field_name_type", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := getRes(mrb, self)
		if r.fieldNameSym {
			return mrb.Intern("symbol").Value()
		}
		return mrb.Intern("string").Value()
	}, mrb.ArgsNone())

	cls.DefineMethod("field_name_type=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		if self.Frozen() {
			return mrb.EFrozenError().Raise("can't modify frozen PG::Result")
		}
		r, _ := getRes(mrb, self)
		if r.fieldsCalled {
			return mrb.EArgumentError().Raise("field names are already materialized")
		}
		v := mrb.GetArgsFirst()
		if !v.IsSymbol() {
			return mrb.EArgumentError().Raisef("invalid argument %s (expected :string or :symbol)", mrb.Inspect(v))
		}
		switch mrb.SymName(v.Symbol()) {
		case "symbol":
			r.fieldNameSym = true
		case "string":
			r.fieldNameSym = false
		default:
			return mrb.EArgumentError().Raisef("invalid argument :%s (expected :string or :symbol)", mrb.SymName(v.Symbol()))
		}
		return v
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

	cls.DefineMethod("__raw_value", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := getRes(mrb, self)
		if err := r.checkNotCleared(); err != nil {
			return rb.Raise(mrb, "PG::Error", "%s", err.Error())
		}
		args := mrb.GetArgs()
		row, col := args.Item(0).Int(), args.Item(1).Int()
		if row < 0 || row >= len(r.rows) || col < 0 || col >= len(r.fields) {
			return mrb.EArgumentError().Raisef("index (%d, %d) out of range", row, col)
		}
		return r.rawCell(mrb, row, col)
	}, mrb.ArgsReq(2))

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
		v, err := r.decodeCell(mrb, self, row, col)
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
			v, err := r.decodeCell(mrb, self, row, col)
			if err != nil {
				return mrb.RaiseError(err)
			}
			var key oruby.Value
			if r.fieldNameSym {
				key = mrb.SymbolValue(mrb.Intern(f.Name))
			} else {
				key = mrb.StrNew(f.Name)
			}
			mrb.HashSet(h, key, v)
		}
		return h
	}
	cls.DefineMethod("nparams", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := getRes(mrb, self)
		if err := r.checkNotCleared(); err != nil {
			return rb.Raise(mrb, "PG::Error", "%s", err.Error())
		}
		return mrb.FixnumValue(len(r.paramOIDs))
	}, mrb.ArgsNone())

	cls.DefineMethod("paramtype", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := getRes(mrb, self)
		if err := r.checkNotCleared(); err != nil {
			return rb.Raise(mrb, "PG::Error", "%s", err.Error())
		}
		i := mrb.GetArgsFirst().Int()
		if i < 0 || i >= len(r.paramOIDs) {
			return mrb.EArgumentError().Raisef("param index %d out of range", i)
		}
		return mrb.Int64Value(int64(r.paramOIDs[i]))
	}, mrb.ArgsReq(1))

	cls.DefineMethod("binary_tuples", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := getRes(mrb, self)
		for _, f := range r.fields {
			if f.Format != 0 {
				return mrb.FixnumValue(1)
			}
		}
		return mrb.FixnumValue(0)
	}, mrb.ArgsNone())

	cls.DefineMethod("[]", tupleFn, mrb.ArgsReq(1))

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
			v, err := r.decodeCell(mrb, self, row, col)
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
				v, err := r.decodeCell(mrb, self, row, col)
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
			v, err := r.decodeCell(mrb, self, row, col)
			if err != nil {
				return mrb.RaiseError(err)
			}
			mrb.AryPush(out, v)
		}
		return out
	}, mrb.ArgsReq(1))

	cls.DefineMethod("type_map", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := getRes(mrb, self)
		if r.typeMap.IsNil() {
			tmCls := rb.ClassPath(mrb, "PG::TypeMapAllStrings")
			if v, err := mrb.Funcall(tmCls, mrb.Intern("new")); err == nil {
				return v
			}
		}
		return r.typeMap
	}, mrb.ArgsNone())

	setTypeMap := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		if self.Frozen() {
			return mrb.EFrozenError().Raise("can't modify frozen PG::Result")
		}
		r, _ := getRes(mrb, self)
		if err := r.checkNotCleared(); err != nil {
			return rb.Raise(mrb, "PG::Error", "%s", err.Error())
		}
		v := mrb.GetArgsFirst()
		tmCls := rb.ClassPath(mrb, "PG::TypeMap")
		if !mrb.ObjIsKindOf(v, tmCls) {
			return mrb.ETypeError().Raise("wrong argument type (expected PG::TypeMap)")
		}
		if mrb.RespondTo(v, mrb.Intern("fit_to_result")) {
			fitted, ferr := mrb.Funcall(v, mrb.Intern("fit_to_result"), self)
			if ferr != nil {
				return mrb.RaiseError(ferr)
			}
			if !mrb.ObjIsKindOf(fitted, tmCls) {
				return mrb.ETypeError().Raise("fit_to_result must return a PG::TypeMap")
			}
			v = fitted
		}
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
		case pgDiagSeverityNonlocalized:
			s = r.pgErr.SeverityUnlocalized
			if s == "" {
				s = r.pgErr.Severity
			}
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
		case pgDiagInternalPosition:
			if r.pgErr.InternalPosition > 0 {
				s = strconv.Itoa(int(r.pgErr.InternalPosition))
			}
		case pgDiagInternalQuery:
			s = r.pgErr.InternalQuery
		case pgDiagContext:
			s = r.pgErr.Where
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
		case pgDiagSourceFile:
			s = r.pgErr.File
		case pgDiagSourceLine:
			if r.pgErr.Line > 0 {
				s = strconv.Itoa(int(r.pgErr.Line))
			}
		case pgDiagSourceFunction:
			s = r.pgErr.Routine
		}
		if s == "" {
			return mrb.NilValue()
		}
		return mrb.StrNew(s)
	}
	cls.DefineMethod("error_field", errFieldFn, mrb.ArgsReq(1))
	cls.DefineMethod("result_error_field", errFieldFn, mrb.ArgsReq(1))

	checkFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := getRes(mrb, self)
		if r.status == pgresFatalError || r.status == pgresBadResponse {
			if r.pgErr != nil {
				return raisePGError(mrb, mrb.NilValue(), r.pgErr)
			}
			return rb.Raise(mrb, "PG::Error", "%s", r.errMsg)
		}
		return self
	}
	cls.DefineMethod("check", checkFn, mrb.ArgsNone())
	cls.DefineMethod("check_result", checkFn, mrb.ArgsNone())
}
