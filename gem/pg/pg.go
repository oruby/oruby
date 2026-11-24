// Package pg implements Ruby's pg gem (PostgreSQL client) in pure Go backed by
// github.com/jackc/pgx/v5/pgconn.
package pg

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"syscall"
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

	connectionStarted       = 2
	connectionMade          = 3
	connectionAwaitingResp  = 4
	connectionAuthOK        = 5
	connectionSetEnv        = 6
	connectionSSLStartup    = 7
	connectionNeeded        = 8
	connectionCheckWritable = 9
	connectionConsume       = 10
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
		"CONNECTION_STARTED":            connectionStarted,
		"CONNECTION_MADE":               connectionMade,
		"CONNECTION_AWAITING_RESPONSE":  connectionAwaitingResp,
		"CONNECTION_AUTH_OK":            connectionAuthOK,
		"CONNECTION_SETENV":             connectionSetEnv,
		"CONNECTION_SSL_STARTUP":        connectionSSLStartup,
		"CONNECTION_NEEDED":             connectionNeeded,
		"CONNECTION_CHECK_WRITABLE":     connectionCheckWritable,
		"CONNECTION_CONSUME":            connectionConsume,
		"DEF_PGPORT":                    5432,
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
	errClasses := mrb.HashNewCapa(len(sqlstateToClass) + 12)
	for code, clsName := range sqlstateToClass {
		mrb.HashSet(errClasses, mrb.StrNew(code), rb.ClassPath(mrb, "PG::"+clsName).Value())
	}
	for code, clsName := range map[string]string{
		"0A": "FeatureNotSupported",
		"22": "DataException",
		"23": "IntegrityConstraintViolation",
		"28": "InvalidAuthorizationSpecification",
		"3D": "InvalidCatalogName",
		"3F": "InvalidSchemaName",
		"40": "TransactionRollback",
		"42": "SyntaxErrorOrAccessRuleViolation",
		"54": "ProgramLimitExceeded",
		"57": "OperatorIntervention",
	} {
		mrb.HashSet(errClasses, mrb.StrNew(code), rb.ClassPath(mrb, "PG::"+clsName).Value())
	}
	mrb.ConstSet(mod, mrb.Intern("ERROR_CLASSES"), errClasses)
}

func raiseConnError(mrb *oruby.MrbState, connVal oruby.Value, clsName, msg string) oruby.MrbValue {
	return raiseConnErrorWithResult(mrb, connVal, mrb.NilValue(), clsName, msg)
}

func raiseConnErrorWithResult(mrb *oruby.MrbState, connVal, resVal oruby.Value, clsName, msg string) oruby.MrbValue {
	excCls := rb.ClassPath(mrb, clsName)
	msgVal := mrb.StrNew(msg).Value()
	if clsName == "PG::ConnectionBad" {
		msgVal = mrb.Call(msgVal, "b").Value()
	} else if !connVal.IsNil() {
		if enc := connInternalEncoding(mrb, connVal); !enc.IsNil() {
			_, _ = mrb.Funcall(msgVal, mrb.Intern("force_encoding"), enc)
		}
	}
	exc, callErr := mrb.Funcall(excCls, mrb.Intern("new"), msgVal)
	if callErr != nil {
		return mrb.Raise(excCls, msg)
	}
	mrb.IVSet(exc, mrb.Intern("@connection"), connVal)
	mrb.IVSet(exc, mrb.Intern("@result"), resVal)
	mrb.ExcRaise(exc)
	return mrb.NilValue()
}

func raisePGError(mrb *oruby.MrbState, connVal oruby.Value, err error) oruby.MrbValue {
	var pgErr *pgconn.PgError
	var resVal oruby.Value = mrb.NilValue()
	excCls := rb.ClassPath(mrb, "PG::Error")
	if errors.As(err, &pgErr) {
		// Look up dynamically in PG::ERROR_CLASSES (respecting user modifications)
		pgMod := mrb.ModuleGet("PG")
		if errClasses := mrb.ConstGet(pgMod, mrb.Intern("ERROR_CLASSES")); errClasses.IsHash() {
			if c, cerr := mrb.Funcall(errClasses, mrb.Intern("[]"), mrb.StrNew(pgErr.Code)); cerr == nil && c.IsClass() {
				excCls = mrb.ClassPtr(c)
			} else if len(pgErr.Code) >= 2 {
				if c2, cerr2 := mrb.Funcall(errClasses, mrb.Intern("[]"), mrb.StrNew(pgErr.Code[:2])); cerr2 == nil && c2.IsClass() {
					excCls = mrb.ClassPtr(c2)
				} else {
					excCls = rb.ClassPath(mrb, "PG::ServerError")
				}
			} else {
				excCls = rb.ClassPath(mrb, "PG::ServerError")
			}
		}
		resCls := rb.ClassPath(mrb, "PG::Result")
		r := &pgResult{
			status:  pgresFatalError,
			pgErr:   pgErr,
			errMsg:  pgErr.Error(),
			typeMap: mrb.NilValue(),
			connVal: connVal,
		}
		resVal = mrb.DataWrapInterface(resCls, r).Value()
	}

	msgVal := mrb.StrNew(err.Error()).Value()
	if !connVal.IsNil() {
		if enc := connInternalEncoding(mrb, connVal); !enc.IsNil() {
			_, _ = mrb.Funcall(msgVal, mrb.Intern("force_encoding"), enc)
		}
	}
	exc, callErr := mrb.Funcall(excCls, mrb.Intern("new"), msgVal)
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
	res          *pgconn.Result
	paramOIDs    []uint32
	prepName     string
	prepDesc     *pgconn.StatementDescription
	forcedStatus int
	isSentinel   bool
}

type streamChunk struct {
	fields  []pgconn.FieldDescription
	row     [][]byte
	isRow   bool
	isEnd   bool
	cmdTag  pgconn.CommandTag
	err     error
	allDone bool
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

func estimateCopyInFields(sql string) int {
	u := strings.ToUpper(sql)
	fromIdx := strings.Index(u, " FROM ")
	if fromIdx < 0 {
		return 1
	}
	head := sql[:fromIdx]
	lp := strings.IndexByte(head, '(')
	rp := strings.LastIndexByte(head, ')')
	if lp >= 0 && rp > lp {
		cols := strings.TrimSpace(head[lp+1 : rp])
		if cols != "" {
			return strings.Count(cols, ",") + 1
		}
	}
	return 1
}

type pipelineCmd struct {
	kind    int // 1=query_params, 2=prepare, 3=query_prepared
	sql     string
	name    string
	vals    [][]byte
	oids    []uint32
	fmts    []int16
	resFmts []int16
}

type pgConn struct {
	conn              *pgconn.PgConn
	cfg               *pgconn.Config
	closed            bool
	activeMu          sync.Mutex
	activeHost        string
	activeHostAddr    string
	activePort        uint16
	serviceName       string
	socketIO          oruby.Value
	wakeMu            sync.Mutex
	sockFd            int
	wakeFd            int
	notifCh           chan *pgconn.Notification
	noticeCh          chan struct{}
	noticeMu          sync.Mutex
	notices           []*pgconn.Notice
	lastErr           string
	typeMapResults    oruby.Value
	typeMapQueries    oruby.Value
	fieldNameSym      bool
	asyncDone         chan []asyncRes
	pendingRaw        []asyncRes
	prepared          map[string]*pgconn.StatementDescription
	connectDone       chan connectRes
	connectCancel     context.CancelFunc
	polledOnce        bool
	reportedClosedErr bool
	singleRowMode     bool
	streamCh          chan streamChunk
	streamPeek        streamChunk
	hasStreamPeek     bool
	streamCancel      context.CancelFunc
	pipelineStatus    int
	pipelineQueue     []pipelineCmd
	pipelineAborted   bool

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

func (c *pgConn) signalWake() {
	c.wakeMu.Lock()
	fd := c.wakeFd
	c.wakeMu.Unlock()
	if fd >= 0 {
		_, _ = syscall.Write(fd, []byte{1})
	}
}

func (c *pgConn) drainWake() {
	c.wakeMu.Lock()
	fd := c.sockFd
	c.wakeMu.Unlock()
	if fd >= 0 {
		var buf [64]byte
		for {
			n, _ := syscall.Read(fd, buf[:])
			if n <= 0 {
				break
			}
		}
	}
}

func (c *pgConn) closeSocketIO(mrb *oruby.MrbState) {
	c.wakeMu.Lock()
	if c.wakeFd >= 0 {
		_ = syscall.Close(c.wakeFd)
		c.wakeFd = -1
	}
	c.sockFd = -1
	c.wakeMu.Unlock()
	if !c.socketIO.IsNil() {
		_, _ = mrb.Funcall(c.socketIO, mrb.Intern("close"))
		c.socketIO = mrb.NilValue()
	}
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

func (c *pgConn) nextStreamChunk(mrb *oruby.MrbState, self oruby.Value) (streamChunk, bool) {
	if c.hasStreamPeek {
		ch := c.streamPeek
		c.hasStreamPeek = false
		return ch, true
	}
	if c.streamCh == nil {
		return streamChunk{}, false
	}
	for {
		ch := c.streamCh
		var chunk streamChunk
		var ok, got bool
		mrb.Blocking(func() {
			select {
			case chunk, ok = <-ch:
				got = true
			case <-c.noticeCh:
			}
		})
		c.flushNotices(mrb, self)
		if got {
			if !ok {
				c.streamCh = nil
				c.singleRowMode = false
				return streamChunk{}, false
			}
			return chunk, true
		}
	}
}

func (c *pgConn) drainStreamToPending(mrb *oruby.MrbState, self oruby.Value, singleStmt bool) {
	if c.streamCh == nil && !c.hasStreamPeek {
		return
	}
	var fields []pgconn.FieldDescription
	var rows [][][]byte
	for {
		chunk, ok := c.nextStreamChunk(mrb, self)
		if !ok || chunk.allDone {
			c.streamCh = nil
			c.hasStreamPeek = false
			c.singleRowMode = false
			if c.streamCancel != nil {
				c.streamCancel()
				c.streamCancel = nil
			}
			break
		}
		if chunk.isRow {
			if len(fields) == 0 && len(chunk.fields) > 0 {
				fields = chunk.fields
			}
			rows = append(rows, chunk.row)
		} else if chunk.isEnd {
			if len(fields) == 0 && len(chunk.fields) > 0 {
				fields = chunk.fields
			}
			r := &pgconn.Result{
				FieldDescriptions: fields,
				Rows:              rows,
				CommandTag:        chunk.cmdTag,
				Err:               chunk.err,
			}
			c.pendingRaw = append(c.pendingRaw, asyncRes{res: r})
			fields = nil
			rows = nil
			if singleStmt {
				break
			}
		}
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
	if c.streamCh != nil || c.hasStreamPeek {
		c.drainStreamToPending(mrb, self, false)
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
			if !c.polledOnce {
				c.polledOnce = true
				return pgresPollingWriting
			}
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
		if !c.polledOnce {
			c.polledOnce = true
			return pgresPollingWriting
		}
		return pgresPollingReading
	}
}

func (c *pgConn) flushPipeline(mrb *oruby.MrbState, self oruby.Value, addSync bool) {
	if len(c.pipelineQueue) == 0 && !addSync {
		return
	}
	queue := c.pipelineQueue
	c.pipelineQueue = nil
	ctx := mrb.GoContext()
	for _, cmd := range queue {
		if c.pipelineAborted {
			c.pendingRaw = append(c.pendingRaw,
				asyncRes{res: &pgconn.Result{Err: errors.New("pipeline aborted")}, forcedStatus: pgresPipelineAborted},
				asyncRes{isSentinel: true},
			)
			continue
		}
		switch cmd.kind {
		case 1: // query_params
			var res *pgconn.Result
			mrb.Blocking(func() {
				res = readResultWithFields(c.conn.ExecParams(ctx, cmd.sql, cmd.vals, cmd.oids, cmd.fmts, cmd.resFmts))
			})
			c.flushNotices(mrb, self)
			if res.Err != nil {
				c.pipelineAborted = true
			}
			c.pendingRaw = append(c.pendingRaw, asyncRes{res: res}, asyncRes{isSentinel: true})
		case 2: // prepare
			var sd *pgconn.StatementDescription
			var perr error
			mrb.Blocking(func() {
				sd, perr = c.conn.Prepare(ctx, cmd.name, cmd.sql, cmd.oids)
			})
			c.flushNotices(mrb, self)
			if perr != nil {
				c.pipelineAborted = true
				c.pendingRaw = append(c.pendingRaw, asyncRes{res: &pgconn.Result{Err: perr}}, asyncRes{isSentinel: true})
			} else {
				c.prepared[cmd.name] = sd
				c.pendingRaw = append(c.pendingRaw,
					asyncRes{
						res:       &pgconn.Result{FieldDescriptions: sd.Fields},
						paramOIDs: sd.ParamOIDs,
						prepName:  cmd.name,
						prepDesc:  sd,
					},
					asyncRes{isSentinel: true},
				)
			}
		case 3: // query_prepared
			var res *pgconn.Result
			mrb.Blocking(func() {
				res = readResultWithFields(c.conn.ExecPrepared(ctx, cmd.name, cmd.vals, cmd.fmts, cmd.resFmts))
			})
			c.flushNotices(mrb, self)
			if res.Err != nil {
				c.pipelineAborted = true
			}
			c.pendingRaw = append(c.pendingRaw, asyncRes{res: res}, asyncRes{isSentinel: true})
		}
	}
	if addSync {
		c.pipelineAborted = false
		c.pendingRaw = append(c.pendingRaw, asyncRes{res: &pgconn.Result{}, forcedStatus: pgresPipelineSync})
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

func (c *pgConn) abortActiveCopy(mrb *oruby.MrbState) {
	if c.copyMode == pgresCopyIn {
		if c.copyInW != nil {
			_ = c.copyInW.CloseWithError(errors.New("aborted by another SQL query"))
			c.copyInW = nil
		}
		if c.copyInDone != nil {
			doneCh := c.copyInDone
			c.copyInDone = nil
			mrb.Blocking(func() {
				<-doneCh
			})
		}
		c.copyMode = 0
	} else if c.copyMode == pgresCopyOut {
		if c.conn != nil && !c.conn.IsClosed() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			mrb.Blocking(func() {
				_ = c.conn.CancelRequest(ctx)
			})
			cancel()
		}
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
}

func (c *pgConn) Memsize() int {
	return 256
}

func copyRowBytes(vals [][]byte) [][]byte {
	row := make([][]byte, len(vals))
	for i, cell := range vals {
		if cell != nil {
			row[i] = append([]byte(nil), cell...)
		}
	}
	return row
}

func readResultWithFields(rr *pgconn.ResultReader) *pgconn.Result {
	res := rr.Read()
	if len(res.FieldDescriptions) == 0 {
		if fds := rr.FieldDescriptions(); len(fds) > 0 {
			res.FieldDescriptions = append([]pgconn.FieldDescription(nil), fds...)
		}
	}
	return res
}

func readAllWithFields(mrr *pgconn.MultiResultReader) ([]*pgconn.Result, error) {
	var out []*pgconn.Result
	for mrr.NextResult() {
		out = append(out, readResultWithFields(mrr.ResultReader()))
	}
	return out, mrr.Close()
}

func checkNoNullByte(s string) error {
	if strings.IndexByte(s, 0) >= 0 {
		return oruby.EArgumentError("string contains null byte")
	}
	return nil
}

func encodeToConn(mrb *oruby.MrbState, connVal, v oruby.Value) (string, error) {
	if !v.IsString() {
		s := mrb.String(v)
		return s, checkNoNullByte(s)
	}
	targetEnc := connInternalEncoding(mrb, connVal)
	if !targetEnc.IsNil() {
		targetName := ""
		if tn, err := mrb.Funcall(targetEnc, mrb.Intern("name")); err == nil {
			targetName = tn.String()
		}
		if targetName != "" && targetName != "ASCII-8BIT" && targetName != "BINARY" {
			if encVal, err := mrb.Funcall(v, mrb.Intern("encoding")); err == nil && !encVal.IsNil() {
				if nameVal, nerr := mrb.Funcall(encVal, mrb.Intern("name")); nerr == nil {
					srcName := nameVal.String()
					if srcName != targetName && srcName != "ASCII-8BIT" && srcName != "BINARY" {
						if conv, cerr := mrb.Funcall(v, mrb.Intern("encode"), targetEnc); cerr == nil {
							s := conv.String()
							return s, checkNoNullByte(s)
						}
					}
				}
			}
		}
	}
	s := v.String()
	return s, checkNoNullByte(s)
}

func connTargetEncoding(mrb *oruby.MrbState, connVal oruby.Value) oruby.Value {
	if connVal.IsNil() {
		return mrb.NilValue()
	}
	intEnc, _ := mrb.Funcall(connVal, mrb.Intern("internal_encoding"))
	return intEnc
}

func applyResultEncoding(mrb *oruby.MrbState, strVal, intEnc oruby.Value) oruby.Value {
	if !intEnc.IsNil() {
		_, _ = mrb.Funcall(strVal, mrb.Intern("force_encoding"), intEnc)
	}
	return strVal
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

	intEnc := connTargetEncoding(mrb, self)
	proc := mrb.IVGet(self, mrb.Intern("@notice_processor"))
	if !proc.IsNil() {
		for _, n := range batch {
			msg := n.Severity + ":  " + n.Message + "\n"
			mv := applyResultEncoding(mrb, mrb.StrNew(msg).Value(), intEnc)
			_, _ = mrb.Funcall(proc, mrb.Intern("call"), mv)
		}
	}
	recv := mrb.IVGet(self, mrb.Intern("@notice_receiver"))
	if !recv.IsNil() {
		for _, n := range batch {
			resCls := rb.ClassPath(mrb, "PG::Result")
			r := &pgResult{
				status:    pgresNonfatalError,
				errMsg:    n.Severity + ":  " + n.Message + "\n",
				pgErr:     &pgconn.PgError{Severity: n.Severity, Code: n.Code, Message: n.Message, Detail: n.Detail, Hint: n.Hint},
				typeMap:   mrb.NilValue(),
				intEnc:    intEnc,
				autoclear: true,
				connVal:   self,
			}
			rv := mrb.DataWrapInterface(resCls, r).Value()
			_, _ = mrb.Funcall(recv, mrb.Intern("call"), rv)
			r.cleared = true
			r.rows = nil
			r.fields = nil
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

var libpqNonGUCParams = map[string]bool{
	"keepalives":                true,
	"keepalives_idle":           true,
	"keepalives_interval":       true,
	"keepalives_count":          true,
	"tcp_user_timeout":          true,
	"gssencmode":                true,
	"requiressl":                true,
	"sslcompression":            true,
	"krbsrvname":                true,
	"gsslib":                    true,
	"gssdelegation":             true,
	"service":                   true,
	"hostaddr":                  true,
	"fallback_application_name": true,
	"sslnegotiation":            true,
	"sslcertmode":               true,
	"ssl_min_protocol_version":  true,
	"ssl_max_protocol_version":  true,
	"load_balance_hosts":        true,
	"tty":                       true,
}

func parseConnConfig(mrb *oruby.MrbState, args oruby.RArgs, kw oruby.Value) (*pgconn.Config, string, error) {
	var connStr string
	var opts oruby.Value = mrb.NilValue()
	var serviceName string

	// Separate optional trailing Hash from positional arguments
	posLen := args.Len()
	if posLen > 0 && args.Item(posLen-1).IsHash() {
		opts = args.Item(posLen - 1)
		posLen--
	}
	if posLen > 7 {
		return nil, "", oruby.EArgumentError("extra positional parameter %d: %s", posLen, mrb.Inspect(args.Item(7)))
	}

	if posLen == 1 {
		a0 := args.Item(0)
		if a0.IsString() {
			connStr = a0.String()
			if err := checkNoNullByte(connStr); err != nil {
				return nil, "", err
			}
		} else if !a0.IsNil() {
			// Handle URI objects or other stringable objects
			connStr = mrb.String(a0)
			if err := checkNoNullByte(connStr); err != nil {
				return nil, "", err
			}
		}
	} else if posLen > 1 {
		// Legacy positional: (host, port, options, tty, dbname, login, password)
		parts := []string{}
		keys := []string{"host", "port", "options", "", "dbname", "user", "password"}
		for i := 0; i < posLen && i < len(keys); i++ {
			v := args.Item(i)
			if keys[i] == "" || v.IsNil() {
				continue
			}
			sv := mrb.String(v)
			if err := checkNoNullByte(sv); err != nil {
				return nil, "", err
			}
			parts = append(parts, fmt.Sprintf("%s=%s", keys[i], quoteConninfoVal(sv)))
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

	// If connStr is a URI and we also have hash options, convert URI through parse_connect_args
	if !opts.IsNil() && opts.IsHash() {
		if strings.HasPrefix(connStr, "postgres://") || strings.HasPrefix(connStr, "postgresql://") {
			connCls := rb.ClassPath(mrb, "PG::Connection")
			if parsed, perr := mrb.Funcall(connCls, mrb.Intern("parse_connect_args"), mrb.StrNew(connStr), opts); perr == nil && parsed.IsString() {
				connStr = parsed.String()
				opts = mrb.NilValue()
			}
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
			vs := mrb.String(v)
			if err := checkNoNullByte(vs); err != nil {
				return nil, "", err
			}
			if ks == "service" {
				serviceName = vs
			}
			if ks == "hostaddr" {
				// Only map hostaddr->host if host is not explicitly provided
				hasHost := false
				if hv, herr := mrb.Funcall(opts, mrb.Intern("key?"), mrb.Intern("host").Value()); herr == nil && hv.Bool() {
					hasHost = true
				}
				if hv, herr := mrb.Funcall(opts, mrb.Intern("key?"), mrb.StrNew("host")); herr == nil && hv.Bool() {
					hasHost = true
				}
				if !hasHost {
					ks = "host"
				} else {
					continue
				}
			}
			parts = append(parts, fmt.Sprintf("%s=%s", ks, quoteConninfoVal(vs)))
		}
		connStr = strings.Join(parts, " ")
	}

	// Extract service name if present in connStr
	if serviceName == "" && strings.Contains(connStr, "service=") {
		connCls := rb.ClassPath(mrb, "PG::Connection")
		if parsed, perr := mrb.Funcall(connCls, mrb.Intern("conninfo_parse"), mrb.StrNew(connStr)); perr == nil && parsed.IsArray() {
			for i := 0; i < oruby.RArrayLen(parsed); i++ {
				h := mrb.AryRef(parsed, i)
				if kv, _ := mrb.Funcall(h, mrb.Intern("[]"), mrb.Intern("keyword").Value()); kv.IsString() && kv.String() == "service" {
					if vv, _ := mrb.Funcall(h, mrb.Intern("[]"), mrb.Intern("val").Value()); vv.IsString() {
						serviceName = vv.String()
					}
					break
				}
			}
		}
	}

	cfg, err := pgconn.ParseConfig(connStr)
	if err != nil {
		// Fallback for URL forms with postgres://
		if strings.HasPrefix(connStr, "postgres://") || strings.HasPrefix(connStr, "postgresql://") {
			if _, uerr := url.Parse(connStr); uerr != nil {
				return nil, serviceName, uerr
			}
		}
		return nil, serviceName, err
	}

	// Strip libpq-only non-GUC parameters from RuntimeParams so PostgreSQL doesn't reject them
	if cfg.RuntimeParams != nil {
		if fb, ok := cfg.RuntimeParams["fallback_application_name"]; ok {
			if _, hasApp := cfg.RuntimeParams["application_name"]; !hasApp && fb != "" {
				cfg.RuntimeParams["application_name"] = fb
			}
		}
		for k := range libpqNonGUCParams {
			delete(cfg.RuntimeParams, k)
		}
	}
	return cfg, serviceName, nil
}

func pbkdf2SHA256(password, salt []byte, iter, keyLen int) []byte {
	hashLen := sha256.Size
	numBlocks := (keyLen + hashLen - 1) / hashLen
	dk := make([]byte, 0, numBlocks*hashLen)
	var blockBuf [4]byte
	for block := 1; block <= numBlocks; block++ {
		blockBuf[0] = byte(block >> 24)
		blockBuf[1] = byte(block >> 16)
		blockBuf[2] = byte(block >> 8)
		blockBuf[3] = byte(block)
		mac := hmac.New(sha256.New, password)
		mac.Write(salt)
		mac.Write(blockBuf[:])
		u := mac.Sum(nil)
		t := make([]byte, len(u))
		copy(t, u)
		for i := 1; i < iter; i++ {
			mac.Reset()
			mac.Write(u)
			u = mac.Sum(u[:0])
			for j := range t {
				t[j] ^= u[j]
			}
		}
		dk = append(dk, t...)
	}
	return dk[:keyLen]
}

func scramSHA256Hash(password string) string {
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	const iterations = 4096
	saltedPassword := pbkdf2SHA256([]byte(password), salt, iterations, 32)

	macClient := hmac.New(sha256.New, saltedPassword)
	macClient.Write([]byte("Client Key"))
	clientKey := macClient.Sum(nil)
	storedKey := sha256.Sum256(clientKey)

	macServer := hmac.New(sha256.New, saltedPassword)
	macServer.Write([]byte("Server Key"))
	serverKey := macServer.Sum(nil)

	return fmt.Sprintf("SCRAM-SHA-256$%d:%s$%s:%s",
		iterations,
		base64.StdEncoding.EncodeToString(salt),
		base64.StdEncoding.EncodeToString(storedKey[:]),
		base64.StdEncoding.EncodeToString(serverKey),
	)
}

func quoteConninfoVal(s string) string {
	if s == "" || strings.ContainsAny(s, " \t\n\r'\\") {
		escaped := strings.ReplaceAll(s, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `'`, `\'`)
		return "'" + escaped + "'"
	}
	return s
}

func escapePGString(s string, stdConforming bool) string {
	if !stdConforming {
		s = strings.ReplaceAll(s, `\`, `\\`)
	}
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

func connInternalEncoding(mrb *oruby.MrbState, self oruby.Value) oruby.Value {
	if enc, err := mrb.Funcall(self, mrb.Intern("internal_encoding")); err == nil {
		return enc
	}
	return mrb.NilValue()
}

func extractParams(mrb *oruby.MrbState, self, paramsVal, typeMap oruby.Value) ([][]byte, []uint32, []int16, error) {
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
	paramEnc := connInternalEncoding(mrb, self)
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
		if useTypeMap {
			enc, err := mrb.Funcall(typeMap, mrb.Intern("__encode_param"), item, mrb.FixnumValue(i), paramEnc)
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
		if item.IsHash() {
			v, _ := mrb.Funcall(item, mrb.Intern("[]"), mrb.Intern("value").Value())
			f, _ := mrb.Funcall(item, mrb.Intern("[]"), mrb.Intern("format").Value())
			t, _ := mrb.Funcall(item, mrb.Intern("[]"), mrb.Intern("type").Value())
			if !f.IsNil() {
				fmts[i] = int16(f.Int())
			}
			if v.IsNil() {
				vals[i] = nil
			} else if v.IsString() {
				var b []byte
				if fmts[i] == 0 {
					sv, serr := encodeToConn(mrb, self, v)
					if serr != nil {
						return nil, nil, nil, serr
					}
					b = []byte(sv)
				} else {
					b = v.Bytes()
				}
				if int64(len(b))+totalBytes > maxTotalParamBytes {
					return nil, nil, nil, errParamTooLarge
				}
				vals[i] = append([]byte(nil), b...)
				totalBytes += int64(len(b))
			} else {
				s := mrb.String(v)
				if fmts[i] == 0 {
					if err := checkNoNullByte(s); err != nil {
						return nil, nil, nil, err
					}
				}
				if int64(len(s))+totalBytes > maxTotalParamBytes {
					return nil, nil, nil, errParamTooLarge
				}
				vals[i] = []byte(s)
				totalBytes += int64(len(s))
			}
			if !t.IsNil() && t.IsInt() {
				oids[i] = uint32(t.Int64())
				hasOID = true
			}
			continue
		}
		switch item.Type() {
		case oruby.MrbTTTrue:
			vals[i] = []byte("true")
		case oruby.MrbTTFalse:
			vals[i] = []byte("false")
		case oruby.MrbTTInteger:
			vals[i] = []byte(strconv.FormatInt(item.Int64(), 10))
		case oruby.MrbTTFloat:
			vals[i] = []byte(strconv.FormatFloat(item.Float64(), 'g', -1, 64))
		case oruby.MrbTTString:
			sv, serr := encodeToConn(mrb, self, item)
			if serr != nil {
				return nil, nil, nil, serr
			}
			b := []byte(sv)
			if int64(len(b))+totalBytes > maxTotalParamBytes {
				return nil, nil, nil, errParamTooLarge
			}
			vals[i] = append([]byte(nil), b...)
			totalBytes += int64(len(b))
		default:
			s := mrb.String(item)
			if err := checkNoNullByte(s); err != nil {
				return nil, nil, nil, err
			}
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

func wrapPGResult(mrb *oruby.MrbState, connVal oruby.Value, c *pgConn, r *pgconn.Result) oruby.Value {
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
	intEnc := connTargetEncoding(mrb, connVal)
	res := &pgResult{
		status:       status,
		fields:       r.FieldDescriptions,
		rows:         r.Rows,
		cmdTag:       r.CommandTag,
		pgErr:        pgErr,
		errMsg:       errMsg,
		typeMap:      c.typeMapResults,
		fieldNameSym: c.fieldNameSym,
		intEnc:       intEnc,
		connVal:      connVal,
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
	if !intEnc.IsNil() {
		mrb.IVSet(val, mrb.Intern("@__int_enc"), intEnc)
	}
	return val
}

func quoteIdentValue(mrb *oruby.MrbState, connVal, v oruby.Value) (oruby.Value, error) {
	if v.IsArray() {
		n := oruby.RArrayLen(v)
		parts := make([]string, n)
		var outEnc oruby.Value = mrb.NilValue()
		if !connVal.IsNil() {
			outEnc = connInternalEncoding(mrb, connVal)
		}
		for i := 0; i < n; i++ {
			elem := mrb.AryRef(v, i)
			if !elem.IsString() {
				return mrb.NilValue(), oruby.ETypeError("wrong argument type %s (expected String)", mrb.ObjClassname(elem))
			}
			var s string
			var err error
			if !connVal.IsNil() {
				s, err = encodeToConn(mrb, connVal, elem)
			} else {
				s = elem.String()
				err = checkNoNullByte(s)
				if outEnc.IsNil() {
					outEnc, _ = mrb.Funcall(elem, mrb.Intern("encoding"))
				}
			}
			if err != nil {
				return mrb.NilValue(), err
			}
			parts[i] = escapePGIdent(s)
		}
		res := mrb.StrNew(strings.Join(parts, ".")).Value()
		if !outEnc.IsNil() {
			_, _ = mrb.Funcall(res, mrb.Intern("force_encoding"), outEnc)
		}
		return res, nil
	}
	if !v.IsString() {
		return mrb.NilValue(), oruby.ETypeError("wrong argument type %s (expected String or Array)", mrb.ObjClassname(v))
	}
	var s string
	var err error
	var outEnc oruby.Value = mrb.NilValue()
	if !connVal.IsNil() {
		s, err = encodeToConn(mrb, connVal, v)
		outEnc = connInternalEncoding(mrb, connVal)
	} else {
		s = v.String()
		err = checkNoNullByte(s)
		outEnc, _ = mrb.Funcall(v, mrb.Intern("encoding"))
	}
	if err != nil {
		return mrb.NilValue(), err
	}
	res := mrb.StrNew(escapePGIdent(s)).Value()
	if !outEnc.IsNil() {
		_, _ = mrb.Funcall(res, mrb.Intern("force_encoding"), outEnc)
	}
	return res, nil
}

func setupConnHooks(c *pgConn, cfg *pgconn.Config) {
	cfg.OnNotification = func(_ *pgconn.PgConn, n *pgconn.Notification) {
		select {
		case c.notifCh <- n:
		default:
		}
	}
	cfg.OnNotice = func(_ *pgconn.PgConn, n *pgconn.Notice) {
		c.noticeMu.Lock()
		c.notices = append(c.notices, n)
		c.noticeMu.Unlock()
		select {
		case c.noticeCh <- struct{}{}:
		default:
		}
	}
	// Collect candidate (host, port) pairs from primary + fallbacks to resolve actual connected host
	type hp struct {
		host string
		port uint16
	}
	candidates := []hp{{host: cfg.Host, port: cfg.Port}}
	for _, fb := range cfg.Fallbacks {
		candidates = append(candidates, hp{host: fb.Host, port: fb.Port})
	}
	origDial := cfg.DialFunc
	if origDial == nil {
		d := &net.Dialer{KeepAlive: 5 * time.Minute}
		origDial = d.DialContext
	}
	cfg.DialFunc = func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := origDial(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		c.activeMu.Lock()
		if network == "unix" {
			// addr is e.g. /path/to/dir/.s.PGSQL.54321
			if idx := strings.LastIndexByte(addr, '/'); idx > 0 {
				c.activeHost = addr[:idx]
			} else {
				c.activeHost = cfg.Host
			}
			c.activeHostAddr = ""
			if dotIdx := strings.LastIndexByte(addr, '.'); dotIdx >= 0 {
				if p, perr := strconv.Atoi(addr[dotIdx+1:]); perr == nil && p > 0 {
					c.activePort = uint16(p)
				}
			}
		} else {
			dialHost, portStr, _ := net.SplitHostPort(addr)
			p64, _ := strconv.ParseUint(portStr, 10, 16)
			port := uint16(p64)
			c.activePort = port
			if ta, ok := conn.RemoteAddr().(*net.TCPAddr); ok {
				c.activeHostAddr = ta.IP.String()
				c.activePort = uint16(ta.Port)
				port = c.activePort
			} else {
				c.activeHostAddr = dialHost
			}
			// Match exact candidate host+port first, then hostname candidate matching port
			matched := ""
			for _, cand := range candidates {
				if cand.port == port && cand.host == dialHost {
					matched = cand.host
					break
				}
			}
			if matched == "" {
				for _, cand := range candidates {
					if cand.port == port && !strings.HasPrefix(cand.host, "/") && net.ParseIP(cand.host) == nil {
						matched = cand.host
						break
					}
				}
			}
			if matched == "" {
				matched = dialHost
			}
			c.activeHost = matched
		}
		c.activeMu.Unlock()
		return conn, nil
	}
}

func sslAttributesMap(c *pgConn) map[string]string {
	if c == nil || c.conn == nil || c.conn.IsClosed() {
		return nil
	}
	nc := c.conn.Conn()
	tc, ok := nc.(*tls.Conn)
	if !ok {
		return nil
	}
	st := tc.ConnectionState()
	proto := "TLSv1.2"
	switch st.Version {
	case tls.VersionTLS13:
		proto = "TLSv1.3"
	case tls.VersionTLS12:
		proto = "TLSv1.2"
	case tls.VersionTLS11:
		proto = "TLSv1.1"
	case tls.VersionTLS10:
		proto = "TLSv1.0"
	}
	cipher := tls.CipherSuiteName(st.CipherSuite)
	bits := "256"
	if strings.Contains(cipher, "128") {
		bits = "128"
	}
	return map[string]string{
		"library":     "Go crypto/tls",
		"protocol":    proto,
		"key_bits":    bits,
		"cipher":      cipher,
		"compression": "off",
	}
}

func defineConnection(mrb *oruby.MrbState, mod, consts oruby.RClass) {
	cls := mrb.DefineClassUnder(mod, "Connection", mrb.ObjectClass())
	rb.DataClass(cls)
	mrb.IncludeModule(cls, consts)

	// Class-level escape & password helpers
	classEscapeFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		v := mrb.GetArgsFirst()
		if !v.IsString() {
			return mrb.ETypeError().Raise("expected String")
		}
		s := v.String()
		if err := checkNoNullByte(s); err != nil {
			return mrb.RaiseError(err)
		}
		out := mrb.StrNew(escapePGString(s, true)).Value()
		if enc, err := mrb.Funcall(v, mrb.Intern("encoding")); err == nil && !enc.IsNil() {
			_, _ = mrb.Funcall(out, mrb.Intern("force_encoding"), enc)
		}
		return out
	}
	cls.DefineClassMethod("escape_string", classEscapeFn, mrb.ArgsReq(1))
	cls.DefineClassMethod("escape", classEscapeFn, mrb.ArgsReq(1))
	cls.DefineClassMethod("quote_ident", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		res, err := quoteIdentValue(mrb, mrb.NilValue(), mrb.GetArgsFirst())
		if err != nil {
			return mrb.RaiseError(err)
		}
		return res
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
	cls.DefineClassMethod("encrypt_password", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		pw := args.Item(0)
		usr := args.Item(1)
		if !pw.IsString() || !usr.IsString() {
			return mrb.ETypeError().Raise("expected String arguments")
		}
		sum := md5.Sum([]byte(pw.String() + usr.String()))
		return mrb.StrNew("md5" + hex.EncodeToString(sum[:]))
	}, mrb.ArgsReq(2))

	cls.DefineMethod("initialize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		kw := mrb.KeywordArgs().Value()
		cfg, svcName, err := parseConnConfig(mrb, args, kw)
		if err != nil {
			if errors.Is(err, oruby.EArgumentError("")) || strings.Contains(err.Error(), "extra positional parameter") || strings.Contains(err.Error(), "null byte") {
				return mrb.RaiseError(err)
			}
			return raiseConnError(mrb, mrb.NilValue(), "PG::ConnectionBad", err.Error())
		}

		notifCh := make(chan *pgconn.Notification, 64)
		noticeCh := make(chan struct{}, 1)
		c := &pgConn{
			cfg:            cfg,
			serviceName:    svcName,
			socketIO:       mrb.NilValue(),
			sockFd:         -1,
			wakeFd:         -1,
			notifCh:        notifCh,
			noticeCh:       noticeCh,
			typeMapResults: mrb.NilValue(),
			typeMapQueries: mrb.NilValue(),
			prepared:       map[string]*pgconn.StatementDescription{},
		}
		setupConnHooks(c, cfg)

		ctx := mrb.GoContext()
		var conn *pgconn.PgConn
		mrb.Blocking(func() {
			conn, err = pgconn.ConnectConfig(ctx, cfg)
		})
		if err != nil {
			return raiseConnError(mrb, mrb.NilValue(), "PG::ConnectionBad", err.Error())
		}
		c.conn = conn
		rb.Set(mrb, self, c)
		_, _ = mrb.Funcall(self, mrb.Intern("set_default_encoding"))
		return self
	}, mrb.ArgsAny())

	cls.DefineClassMethod("connect_start", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		kw := mrb.KeywordArgs().Value()
		blk := mrb.GetArgsBlock()
		cfg, svcName, err := parseConnConfig(mrb, args, kw)
		if err != nil {
			if strings.Contains(err.Error(), "extra positional parameter") || strings.Contains(err.Error(), "null byte") {
				return mrb.RaiseError(err)
			}
			return raiseConnError(mrb, mrb.NilValue(), "PG::ConnectionBad", err.Error())
		}
		notifCh := make(chan *pgconn.Notification, 64)
		noticeCh := make(chan struct{}, 1)
		connectCh := make(chan connectRes, 1)
		ctx, cancel := context.WithCancel(context.Background())
		c := &pgConn{
			cfg:            cfg,
			serviceName:    svcName,
			socketIO:       mrb.NilValue(),
			sockFd:         -1,
			wakeFd:         -1,
			notifCh:        notifCh,
			noticeCh:       noticeCh,
			connectDone:    connectCh,
			connectCancel:  cancel,
			typeMapResults: mrb.NilValue(),
			typeMapQueries: mrb.NilValue(),
			prepared:       map[string]*pgconn.StatementDescription{},
		}
		setupConnHooks(c, cfg)
		go func() {
			conn, cerr := pgconn.ConnectConfig(ctx, cfg)
			connectCh <- connectRes{conn: conn, err: cerr}
			close(connectCh)
			c.signalWake()
		}()
		targetCls := cls
		if self.IsClass() {
			targetCls = mrb.ClassPtr(self)
		}
		connVal := mrb.DataWrapInterface(targetCls, c).Value()
		if !blk.IsNil() {
			out, yerr := mrb.YieldE(blk, connVal)
			if !c.closed {
				_, _ = mrb.Funcall(connVal, mrb.Intern("finish"))
			}
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
		if self.Frozen() {
			return mrb.EFrozenError().Raise("can't modify frozen PG::Connection")
		}
		c, err := getConn(mrb, self)
		if err != nil {
			return raiseConnError(mrb, self, "PG::ConnectionBad", "connection is closed")
		}
		if c.closed && c.connectDone == nil {
			return raiseConnError(mrb, self, "PG::ConnectionBad", "connection is closed")
		}
		if c.connectCancel != nil {
			c.connectCancel()
			c.connectCancel = nil
		}
		c.connectDone = nil
		hadBusy := c.streamCh != nil || c.asyncDone != nil || c.copyMode != 0
		if c.streamCancel != nil {
			c.streamCancel()
			c.streamCancel = nil
		}
		c.streamCh = nil
		c.hasStreamPeek = false
		c.cleanupCopyOnClose()
		c.asyncDone = nil
		c.pendingRaw = nil
		c.closeSocketIO(mrb)
		c.closed = true
		if c.conn != nil {
			if hadBusy {
				// Force-close underlying net.Conn first so any goroutine blocked on a
				// stalled socket (e.g. TCP gate) unblocks immediately instead of deadlocking Close.
				if nc := c.conn.Conn(); nc != nil {
					_ = nc.Close()
				}
				pgc := c.conn
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
					defer cancel()
					_ = pgc.Close(ctx)
				}()
			} else {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				mrb.Blocking(func() {
					_ = c.conn.Close(ctx)
				})
			}
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
		if c.connectDone != nil && !c.closed {
			return mrb.FalseValue()
		}
		return mrb.BoolValue(c.closed || c.conn == nil || c.conn.IsClosed())
	}
	cls.DefineMethod("finished?", finishedFn, mrb.ArgsNone())
	cls.DefineMethod("closed?", finishedFn, mrb.ArgsNone())

	cls.DefineMethod("status", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.closed {
			return mrb.FixnumValue(connectionBad)
		}
		if c.connectDone != nil {
			return mrb.FixnumValue(connectionStarted)
		}
		if c.checkOpen() != nil {
			return mrb.FixnumValue(connectionBad)
		}
		return mrb.FixnumValue(connectionOK)
	}, mrb.ArgsNone())

	cls.DefineMethod("reset", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		if self.Frozen() {
			return mrb.EFrozenError().Raise("can't modify frozen PG::Connection")
		}
		c, err := getConn(mrb, self)
		if err != nil {
			return raiseConnError(mrb, self, "PG::ConnectionBad", err.Error())
		}
		c.closeSocketIO(mrb)
		if c.streamCancel != nil {
			c.streamCancel()
			c.streamCancel = nil
		}
		c.streamCh = nil
		c.hasStreamPeek = false
		c.cleanupCopyOnClose()
		c.asyncDone = nil
		c.pendingRaw = nil
		c.singleRowMode = false
		c.pipelineStatus = pqPipelineOff
		c.pipelineQueue = nil
		c.pipelineAborted = false
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
			return raiseConnError(mrb, self, "PG::ConnectionBad", err.Error())
		}
		c.conn = conn
		c.closed = false
		c.prepared = map[string]*pgconn.StatementDescription{}
		return self
	}, mrb.ArgsNone())

	cls.DefineMethod("error_message", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil {
			return mrb.Call(mrb.StrNew(""), "b")
		}
		v := mrb.StrNew(c.lastErr).Value()
		if c.closed || c.conn == nil || c.conn.IsClosed() {
			return mrb.Call(v, "b")
		}
		return applyResultEncoding(mrb, v, connTargetEncoding(mrb, self))
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

	cls.DefineMethod("socket_io", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.closed || (c.conn == nil && c.connectDone == nil) || (c.conn != nil && c.conn.IsClosed()) {
			return raiseConnError(mrb, self, "PG::ConnectionBad", "connection is closed")
		}
		if !c.socketIO.IsNil() {
			if closedVal, cerr := mrb.Funcall(c.socketIO, mrb.Intern("closed?")); cerr == nil && !closedVal.Bool() {
				return c.socketIO
			}
			c.closeSocketIO(mrb)
		}
		fds, serr := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
		if serr != nil {
			return raiseConnError(mrb, self, "PG::ConnectionBad", serr.Error())
		}
		_ = syscall.SetNonblock(fds[0], true)
		_ = syscall.SetNonblock(fds[1], true)
		ioCls := mrb.ClassGet("IO")
		ioVal, ierr := mrb.Funcall(ioCls, mrb.Intern("for_fd"), mrb.FixnumValue(fds[0]))
		if ierr != nil {
			_ = syscall.Close(fds[0])
			_ = syscall.Close(fds[1])
			return mrb.RaiseError(ierr)
		}
		c.wakeMu.Lock()
		c.sockFd = fds[0]
		c.wakeFd = fds[1]
		c.wakeMu.Unlock()
		c.socketIO = ioVal
		mrb.IVSet(self, mrb.Intern("@__socket_io"), ioVal)
		// If async data is already queued, wake immediately so IO.select won't miss it
		if len(c.pendingRaw) > 0 || c.hasStreamPeek ||
			(c.streamCh != nil && len(c.streamCh) > 0) ||
			(c.asyncDone != nil && len(c.asyncDone) > 0) ||
			(c.connectDone != nil && len(c.connectDone) > 0) {
			c.signalWake()
		}
		return ioVal
	}, mrb.ArgsNone())

	cls.DefineMethod("socket", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		ioVal, err := mrb.Funcall(self, mrb.Intern("socket_io"))
		if err != nil {
			return mrb.RaiseError(err)
		}
		fno, err := mrb.Funcall(ioVal, mrb.Intern("fileno"))
		if err != nil {
			return mrb.RaiseError(err)
		}
		return fno
	}, mrb.ArgsNone())

	cls.DefineMethod("check_socket", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.conn == nil {
			return raiseConnError(mrb, self, "PG::ConnectionBad", "PQsocket can't get socket descriptor")
		}
		if c.closed || c.conn.IsClosed() {
			c.flushNotices(mrb, self)
			return raiseConnError(mrb, self, "PG::ConnectionBad", "server closed the connection unexpectedly")
		}
		c.pollAsyncNonBlocking()
		c.flushNotices(mrb, self)
		if c.streamCh != nil && !c.hasStreamPeek {
			select {
			case chunk, ok := <-c.streamCh:
				if !ok || chunk.allDone {
					c.streamCh = nil
					c.singleRowMode = false
				} else if chunk.isEnd && chunk.err != nil {
					var pgErr *pgconn.PgError
					if (errors.As(chunk.err, &pgErr) && pgErr.Code == "57P01") || c.conn.IsClosed() {
						c.streamCh = nil
						c.closed = true
						c.flushNotices(mrb, self)
						return raiseConnError(mrb, self, "PG::ConnectionBad", "server closed the connection unexpectedly")
					}
					c.streamPeek = chunk
					c.hasStreamPeek = true
				} else {
					c.streamPeek = chunk
					c.hasStreamPeek = true
				}
			default:
			}
		}
		if c.hasStreamPeek && c.streamPeek.isEnd && c.streamPeek.err != nil {
			var pgErr *pgconn.PgError
			if (errors.As(c.streamPeek.err, &pgErr) && pgErr.Code == "57P01") || c.conn.IsClosed() {
				c.hasStreamPeek = false
				c.streamCh = nil
				c.closed = true
				c.flushNotices(mrb, self)
				return raiseConnError(mrb, self, "PG::ConnectionBad", "server closed the connection unexpectedly")
			}
		}
		if c.conn.IsClosed() {
			c.closed = true
			return raiseConnError(mrb, self, "PG::ConnectionBad", "server closed the connection unexpectedly")
		}
		// Check if any pending async result carried a fatal disconnect
		for _, ar := range c.pendingRaw {
			if ar.res != nil && ar.res.Err != nil {
				var pgErr *pgconn.PgError
				if errors.As(ar.res.Err, &pgErr) && pgErr.Code == "57P01" {
					c.closed = true
					return raiseConnError(mrb, self, "PG::ConnectionBad", "server closed the connection unexpectedly")
				}
			}
		}
		if c.asyncDone == nil && c.streamCh == nil && c.copyMode == 0 {
			var cerr error
			mrb.Blocking(func() {
				cerr = c.conn.CheckConn()
			})
			c.flushNotices(mrb, self)
			if cerr != nil {
				c.closed = true
				return raiseConnError(mrb, self, "PG::ConnectionBad", "server closed the connection unexpectedly")
			}
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
		if self.Frozen() {
			return mrb.EFrozenError().Raise("can't modify frozen PG::Connection")
		}
		c, _ := getConn(mrb, self)
		v := mrb.GetArgsFirst()
		if !v.IsSymbol() {
			return mrb.EArgumentError().Raisef("invalid argument %s (expected :string or :symbol)", mrb.Inspect(v))
		}
		switch mrb.SymName(v.Symbol()) {
		case "symbol":
			c.fieldNameSym = true
		case "string":
			c.fieldNameSym = false
		default:
			return mrb.EArgumentError().Raisef("invalid argument :%s (expected :string or :symbol)", mrb.SymName(v.Symbol()))
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
		c.activeMu.Lock()
		h := c.activeHost
		c.activeMu.Unlock()
		if h == "" && c.cfg != nil {
			h = c.cfg.Host
		}
		return mrb.StrNew(h)
	}, mrb.ArgsNone())
	cls.DefineMethod("hostaddr", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := getConn(mrb, self)
		c.activeMu.Lock()
		h := c.activeHost
		ha := c.activeHostAddr
		c.activeMu.Unlock()
		if h == "" && c.cfg != nil {
			h = c.cfg.Host
		}
		if strings.HasPrefix(h, "/") {
			return mrb.StrNew("")
		}
		if ha != "" {
			return mrb.StrNew(ha)
		}
		if h == "localhost" {
			return mrb.StrNew("127.0.0.1")
		}
		return mrb.StrNew(h)
	}, mrb.ArgsNone())
	cls.DefineMethod("port", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := getConn(mrb, self)
		c.activeMu.Lock()
		p := c.activePort
		c.activeMu.Unlock()
		if p == 0 && c.cfg != nil {
			p = c.cfg.Port
		}
		return mrb.FixnumValue(int(p))
	}, mrb.ArgsNone())
	cls.DefineMethod("__connect_timeout", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := getConn(mrb, self)
		if c.cfg != nil && c.cfg.ConnectTimeout > 0 {
			return mrb.StrNew(strconv.Itoa(int(c.cfg.ConnectTimeout / time.Second)))
		}
		return mrb.NilValue()
	}, mrb.ArgsNone())
	cls.DefineMethod("__service_name", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := getConn(mrb, self)
		if c != nil && c.serviceName != "" {
			return mrb.StrNew(c.serviceName)
		}
		return mrb.NilValue()
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
			return raiseConnError(mrb, self, "PG::ConnectionBad", "connection is closed")
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
			return raiseConnError(mrb, self, "PG::ConnectionBad", "connection is closed")
		}
		return mrb.FixnumValue(3)
	}, mrb.ArgsNone())

	cls.DefineMethod("full_protocol_version", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return raiseConnError(mrb, self, "PG::ConnectionBad", "connection is closed")
		}
		return mrb.FixnumValue(30000)
	}, mrb.ArgsNone())

	cls.DefineMethod("transaction_status", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return mrb.FixnumValue(pqtransUnknown)
		}
		if c.asyncDone != nil || c.streamCh != nil || c.hasStreamPeek || len(c.pendingRaw) > 0 || c.copyMode != 0 {
			return mrb.FixnumValue(pqtransActive)
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

	cls.DefineMethod("ssl_in_use?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return raiseConnError(mrb, self, "PG::ConnectionBad", "connection is closed")
		}
		_, isTLS := c.conn.Conn().(*tls.Conn)
		return mrb.BoolValue(isTLS)
	}, mrb.ArgsNone())

	cls.DefineMethod("ssl_attribute_names", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return raiseConnError(mrb, self, "PG::ConnectionBad", "connection is closed")
		}
		attrs := sslAttributesMap(c)
		if attrs == nil {
			return mrb.AryNew()
		}
		ary := mrb.AryNewCapa(len(attrs))
		for _, k := range []string{"library", "protocol", "key_bits", "cipher", "compression"} {
			mrb.AryPush(ary, mrb.StrNew(k))
		}
		return ary
	}, mrb.ArgsNone())

	cls.DefineMethod("ssl_attribute", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return raiseConnError(mrb, self, "PG::ConnectionBad", "connection is closed")
		}
		attrs := sslAttributesMap(c)
		if attrs == nil {
			return mrb.NilValue()
		}
		name := mrb.GetArgsFirst().String()
		if v, ok := attrs[name]; ok {
			return mrb.StrNew(v)
		}
		return mrb.NilValue()
	}, mrb.ArgsReq(1))

	cls.DefineMethod("ssl_attributes", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return raiseConnError(mrb, self, "PG::ConnectionBad", "connection is closed")
		}
		attrs := sslAttributesMap(c)
		h := mrb.HashNew()
		for k, v := range attrs {
			mrb.HashSet(h, mrb.StrNew(k), mrb.StrNew(v))
		}
		return h
	}, mrb.ArgsNone())

	cls.DefineMethod("encrypt_password", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		args := mrb.GetArgs()
		pw := args.Item(0)
		usr := args.Item(1)
		if !pw.IsString() || !usr.IsString() {
			return mrb.ETypeError().Raise("expected String arguments")
		}
		algo := ""
		if args.Len() >= 3 && !args.Item(2).IsNil() {
			if !args.Item(2).IsString() {
				return mrb.ETypeError().Raise("expected String or nil for algorithm")
			}
			algo = args.Item(2).String()
		}
		switch algo {
		case "", "scram-sha-256":
			return mrb.StrNew(scramSHA256Hash(pw.String()))
		case "md5":
			sum := md5.Sum([]byte(pw.String() + usr.String()))
			return mrb.StrNew("md5" + hex.EncodeToString(sum[:]))
		default:
			return raiseConnError(mrb, self, "PG::Error", fmt.Sprintf("unrecognized password encryption algorithm %q", algo))
		}
	}, mrb.ArgsReq(2)|mrb.ArgsOpt(1))

	instEscapeFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := getConn(mrb, self)
		s, err := encodeToConn(mrb, self, mrb.GetArgsFirst())
		if err != nil {
			return mrb.RaiseError(err)
		}
		stdConf := true
		if c != nil && c.conn != nil && !c.conn.IsClosed() && c.conn.ParameterStatus("standard_conforming_strings") == "off" {
			stdConf = false
		}
		out := mrb.StrNew(escapePGString(s, stdConf)).Value()
		return applyResultEncoding(mrb, out, connInternalEncoding(mrb, self))
	}
	cls.DefineMethod("escape_string", instEscapeFn, mrb.ArgsReq(1))
	cls.DefineMethod("escape", instEscapeFn, mrb.ArgsReq(1))
	cls.DefineMethod("escape_literal", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		s, err := encodeToConn(mrb, self, mrb.GetArgsFirst())
		if err != nil {
			return mrb.RaiseError(err)
		}
		out := mrb.StrNew(escapePGLiteral(s)).Value()
		return applyResultEncoding(mrb, out, connInternalEncoding(mrb, self))
	}, mrb.ArgsReq(1))
	cls.DefineMethod("escape_identifier", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		s, err := encodeToConn(mrb, self, mrb.GetArgsFirst())
		if err != nil {
			return mrb.RaiseError(err)
		}
		out := mrb.StrNew(escapePGIdent(s)).Value()
		return applyResultEncoding(mrb, out, connInternalEncoding(mrb, self))
	}, mrb.ArgsReq(1))
	cls.DefineMethod("quote_ident", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		res, err := quoteIdentValue(mrb, self, mrb.GetArgsFirst())
		if err != nil {
			return mrb.RaiseError(err)
		}
		return res
	}, mrb.ArgsReq(1))
	cls.DefineMethod("escape_bytea", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, _ := getConn(mrb, self)
		out := escapeBytea(mrb.GetArgsFirst().Bytes())
		if c != nil && c.conn != nil && !c.conn.IsClosed() && c.conn.ParameterStatus("standard_conforming_strings") == "off" {
			out = `\` + out
		}
		return mrb.StrNew(out)
	}, mrb.ArgsReq(1))
	cls.DefineMethod("unescape_bytea", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		b, err := unescapeBytea(mrb.GetArgsFirst().String())
		if err != nil {
			return raiseConnError(mrb, self, "PG::Error", err.Error())
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
		if self.Frozen() {
			return mrb.EFrozenError().Raise("can't modify frozen PG::Connection")
		}
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
		if self.Frozen() {
			return mrb.EFrozenError().Raise("can't modify frozen PG::Connection")
		}
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

	validateTypeMapArg := func(mrb *oruby.MrbState, tm oruby.Value) error {
		if tm.IsNil() {
			return nil
		}
		tmCls := rb.ClassPath(mrb, "PG::TypeMap")
		if !mrb.ObjIsKindOf(tm, tmCls) {
			return oruby.ETypeError("wrong argument type %s (expected kind of PG::TypeMap)", mrb.ObjClassname(tm))
		}
		return nil
	}

	execFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		if self.Frozen() {
			return mrb.EFrozenError().Raise("can't modify frozen PG::Connection")
		}
		c, err := getConn(mrb, self)
		if err != nil {
			return raiseConnError(mrb, self, "PG::ConnectionBad", "connection is closed")
		}
		if c.connectDone != nil {
			if c.pollConnect(0) != pgresPollingOK {
				return raiseConnError(mrb, self, "PG::UnableToSend", "no connection to the server")
			}
		}
		if c.checkOpen() != nil {
			return raiseConnError(mrb, self, "PG::ConnectionBad", "connection is closed")
		}
		if c.copyMode != 0 {
			c.abortActiveCopy(mrb)
		}
		if c.asyncDone != nil || c.streamCh != nil || c.hasStreamPeek {
			c.waitAsync(mrb, self)
		}
		c.pendingRaw = nil
		args := mrb.GetArgs()
		blk := mrb.GetArgsBlock()
		sql, serr := encodeToConn(mrb, self, args.Item(0))
		if serr != nil {
			return mrb.RaiseError(serr)
		}

		// If params passed to exec/query(sql, params, result_format = 0, type_map = nil) -> delegate to ExecParams
		if args.Len() >= 2 && !args.Item(1).IsNil() {
			resFmt := 0
			if args.Len() >= 3 && !args.Item(2).IsNil() {
				resFmt = args.Item(2).Int()
			}
			tm := c.typeMapQueries
			if args.Len() >= 4 && !args.Item(3).IsNil() {
				if terr := validateTypeMapArg(mrb, args.Item(3)); terr != nil {
					return mrb.RaiseError(terr)
				}
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
			results, err = readAllWithFields(c.conn.Exec(ctx, sql))
		})
		c.flushNotices(mrb, self)
		if err != nil {
			c.lastErr = err.Error()
			return raisePGError(mrb, self, err)
		}
		c.lastErr = ""
		var resVal oruby.Value
		if len(results) == 0 {
			resVal = wrapPGResult(mrb, self, c, &pgconn.Result{})
		} else {
			resVal = wrapPGResult(mrb, self, c, results[len(results)-1])
		}
		return yieldAndClearResult(mrb, resVal, blk)
	}
	cls.DefineMethod("exec", execFn, mrb.ArgsReq(1)|mrb.ArgsOpt(3)|mrb.ArgsBlock())
	cls.DefineMethod("query", execFn, mrb.ArgsReq(1)|mrb.ArgsOpt(3)|mrb.ArgsBlock())

	cls.DefineMethod("exec_params", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		if self.Frozen() {
			return mrb.EFrozenError().Raise("can't modify frozen PG::Connection")
		}
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return raiseConnError(mrb, self, "PG::ConnectionBad", "connection is closed")
		}
		if c.copyMode != 0 {
			c.abortActiveCopy(mrb)
		}
		if c.asyncDone != nil || c.streamCh != nil || c.hasStreamPeek {
			c.waitAsync(mrb, self)
		}
		c.pendingRaw = nil
		args := mrb.GetArgs()
		if args.Len() < 1 || args.Len() > 4 {
			return mrb.EArgumentError().Raisef("wrong number of arguments (given %d, expected 1..4)", args.Len())
		}
		blk := mrb.GetArgsBlock()
		sql, serr := encodeToConn(mrb, self, args.Item(0))
		if serr != nil {
			return mrb.RaiseError(serr)
		}
		params := mrb.NilValue()
		if args.Len() >= 2 {
			params = args.Item(1)
		}
		// Deprecated form in PG 1.x: exec_params(sql) or exec_params(sql, nil, ...) forwards to simple exec
		if params.IsNil() {
			if args.Len() >= 4 && !args.Item(3).IsNil() {
				if terr := validateTypeMapArg(mrb, args.Item(3)); terr != nil {
					return mrb.RaiseError(terr)
				}
			}
			ctx := mrb.GoContext()
			var results []*pgconn.Result
			mrb.Blocking(func() {
				results, err = readAllWithFields(c.conn.Exec(ctx, sql))
			})
			c.flushNotices(mrb, self)
			if err != nil {
				c.lastErr = err.Error()
				return raisePGError(mrb, self, err)
			}
			c.lastErr = ""
			var resVal oruby.Value
			if len(results) == 0 {
				resVal = wrapPGResult(mrb, self, c, &pgconn.Result{})
			} else {
				resVal = wrapPGResult(mrb, self, c, results[len(results)-1])
			}
			return yieldAndClearResult(mrb, resVal, blk)
		}
		resFmt := 0
		if args.Len() >= 3 && !args.Item(2).IsNil() {
			resFmt = args.Item(2).Int()
		}
		tm := c.typeMapQueries
		if args.Len() >= 4 && !args.Item(3).IsNil() {
			if terr := validateTypeMapArg(mrb, args.Item(3)); terr != nil {
				return mrb.RaiseError(terr)
			}
			tm = args.Item(3)
		}
		return execParamsImpl(mrb, self, c, sql, params, resFmt, tm, blk)
	}, mrb.ArgsReq(1)|mrb.ArgsOpt(3)|mrb.ArgsBlock())

	cls.DefineMethod("prepare", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		if self.Frozen() {
			return mrb.EFrozenError().Raise("can't modify frozen PG::Connection")
		}
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return raiseConnError(mrb, self, "PG::ConnectionBad", "connection is closed")
		}
		args := mrb.GetArgs()
		name, nerr := encodeToConn(mrb, self, args.Item(0))
		if nerr != nil {
			return mrb.RaiseError(nerr)
		}
		sql, serr := encodeToConn(mrb, self, args.Item(1))
		if serr != nil {
			return mrb.RaiseError(serr)
		}
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
		resVal := wrapPGResult(mrb, self, c, &pgconn.Result{FieldDescriptions: sd.Fields})
		if r, gerr := getRes(mrb, resVal); gerr == nil {
			r.paramOIDs = sd.ParamOIDs
		}
		return resVal
	}, mrb.ArgsReq(2)|mrb.ArgsOpt(1))

	cls.DefineMethod("describe_prepared", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return raiseConnError(mrb, self, "PG::ConnectionBad", "connection is closed")
		}
		name, nerr := encodeToConn(mrb, self, mrb.GetArgsFirst())
		if nerr != nil {
			return mrb.RaiseError(nerr)
		}
		sd, ok := c.prepared[name]
		if !ok {
			return raiseConnError(mrb, self, "PG::InvalidSqlStatementName", fmt.Sprintf("prepared statement %q does not exist", name))
		}
		resVal := wrapPGResult(mrb, self, c, &pgconn.Result{FieldDescriptions: sd.Fields})
		if r, gerr := getRes(mrb, resVal); gerr == nil {
			r.paramOIDs = sd.ParamOIDs
		}
		return resVal
	}, mrb.ArgsReq(1))

	cls.DefineMethod("describe_portal", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return raiseConnError(mrb, self, "PG::ConnectionBad", "connection is closed")
		}
		name, nerr := encodeToConn(mrb, self, mrb.GetArgsFirst())
		if nerr != nil {
			return mrb.RaiseError(nerr)
		}
		ctx := mrb.GoContext()
		q := "FETCH 0 FROM " + escapePGIdent(name)
		var results []*pgconn.Result
		mrb.Blocking(func() {
			results, err = readAllWithFields(c.conn.Exec(ctx, q))
		})
		c.flushNotices(mrb, self)
		if err != nil {
			c.lastErr = err.Error()
			return raisePGError(mrb, self, err)
		}
		var res *pgconn.Result
		if len(results) > 0 {
			res = results[len(results)-1]
		} else {
			res = &pgconn.Result{}
		}
		return wrapPGResult(mrb, self, c, res)
	}, mrb.ArgsReq(1))

	cls.DefineMethod("exec_prepared", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		if self.Frozen() {
			return mrb.EFrozenError().Raise("can't modify frozen PG::Connection")
		}
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return raiseConnError(mrb, self, "PG::ConnectionBad", "connection is closed")
		}
		args := mrb.GetArgs()
		blk := mrb.GetArgsBlock()
		name, nerr := encodeToConn(mrb, self, args.Item(0))
		if nerr != nil {
			return mrb.RaiseError(nerr)
		}
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
			if terr := validateTypeMapArg(mrb, args.Item(3)); terr != nil {
				return mrb.RaiseError(terr)
			}
			tm = args.Item(3)
		}
		vals, _, fmts, err := extractParams(mrb, self, params, tm)
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
			res = readResultWithFields(c.conn.ExecPrepared(ctx, name, vals, fmts, resFmts))
		})
		c.flushNotices(mrb, self)
		if res.Err != nil {
			c.lastErr = res.Err.Error()
			return raisePGError(mrb, self, res.Err)
		}
		c.lastErr = ""
		resVal := wrapPGResult(mrb, self, c, res)
		return yieldAndClearResult(mrb, resVal, blk)
	}, mrb.ArgsReq(1)|mrb.ArgsOpt(3)|mrb.ArgsBlock())

	// Pipeline mode methods
	cls.DefineMethod("enter_pipeline_mode", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		if self.Frozen() {
			return mrb.EFrozenError().Raise("can't modify frozen PG::Connection")
		}
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return raiseConnError(mrb, self, "PG::ConnectionBad", "connection is closed")
		}
		if c.pipelineStatus != pqPipelineOff {
			return mrb.NilValue()
		}
		if c.asyncDone != nil || c.streamCh != nil || c.hasStreamPeek || len(c.pendingRaw) > 0 {
			return raiseConnError(mrb, self, "PG::Error", "cannot enter pipeline mode while another command is in progress")
		}
		c.pipelineStatus = pqPipelineOn
		c.pipelineAborted = false
		return mrb.NilValue()
	}, mrb.ArgsNone())

	cls.DefineMethod("exit_pipeline_mode", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		if self.Frozen() {
			return mrb.EFrozenError().Raise("can't modify frozen PG::Connection")
		}
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return raiseConnError(mrb, self, "PG::ConnectionBad", "connection is closed")
		}
		if c.pipelineStatus == pqPipelineOff {
			return mrb.NilValue()
		}
		if len(c.pipelineQueue) > 0 || len(c.pendingRaw) > 0 || c.asyncDone != nil || c.streamCh != nil {
			return raiseConnError(mrb, self, "PG::Error", "cannot exit pipeline mode with unretrieved results")
		}
		c.pipelineStatus = pqPipelineOff
		c.pipelineAborted = false
		return mrb.NilValue()
	}, mrb.ArgsNone())

	cls.DefineMethod("pipeline_status", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return mrb.FixnumValue(pqPipelineOff)
		}
		return mrb.FixnumValue(c.pipelineStatus)
	}, mrb.ArgsNone())

	cls.DefineMethod("pipeline_sync", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return raiseConnError(mrb, self, "PG::ConnectionBad", "connection is closed")
		}
		if c.pipelineStatus == pqPipelineOff {
			return raiseConnError(mrb, self, "PG::Error", "not in pipeline mode")
		}
		c.flushPipeline(mrb, self, true)
		return mrb.NilValue()
	}, mrb.ArgsNone())

	cls.DefineMethod("send_flush_request", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return raiseConnError(mrb, self, "PG::ConnectionBad", "connection is closed")
		}
		if c.pipelineStatus == pqPipelineOff {
			if c.asyncDone != nil || c.streamCh != nil || c.hasStreamPeek || len(c.pendingRaw) > 0 {
				return raiseConnError(mrb, self, "PG::Error", "another command is already in progress")
			}
			return mrb.NilValue()
		}
		c.flushPipeline(mrb, self, false)
		return mrb.NilValue()
	}, mrb.ArgsNone())

	cls.DefineMethod("set_single_row_mode", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return raiseConnError(mrb, self, "PG::ConnectionBad", "connection is closed")
		}
		if c.asyncDone == nil && c.streamCh == nil && !c.hasStreamPeek && len(c.pendingRaw) == 0 && len(c.pipelineQueue) == 0 {
			return raiseConnError(mrb, self, "PG::Error", "PQsetSingleRowMode() failed: no query in progress")
		}
		c.singleRowMode = true
		return mrb.NilValue()
	}, mrb.ArgsNone())

	startStreamParams := func(mrb *oruby.MrbState, c *pgConn, sql string, vals [][]byte, oids []uint32, fmts, resFmts []int16) {
		ctx, cancel := context.WithCancel(mrb.GoContext())
		ch := make(chan streamChunk, 64)
		c.streamCh = ch
		c.streamCancel = cancel
		c.hasStreamPeek = false
		go func() {
			rr := c.conn.ExecParams(ctx, sql, vals, oids, fmts, resFmts)
			var fds []pgconn.FieldDescription
			for rr.NextRow() {
				if len(fds) == 0 {
					fds = append([]pgconn.FieldDescription(nil), rr.FieldDescriptions()...)
				}
				ch <- streamChunk{fields: fds, row: copyRowBytes(rr.Values()), isRow: true}
				c.signalWake()
			}
			tag, rerr := rr.Close()
			if len(fds) == 0 {
				fds = append([]pgconn.FieldDescription(nil), rr.FieldDescriptions()...)
			}
			ch <- streamChunk{fields: fds, cmdTag: tag, err: rerr, isEnd: true}
			ch <- streamChunk{allDone: true}
			close(ch)
			c.signalWake()
		}()
	}

	// Truly asynchronous query methods (send_query* run in a Go goroutine so
	// callers can cancel, wait_for_notify, stream single rows, or check is_busy).
	cls.DefineMethod("send_query", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		if self.Frozen() {
			return mrb.EFrozenError().Raise("can't modify frozen PG::Connection")
		}
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return raiseConnError(mrb, self, "PG::ConnectionBad", "connection is closed")
		}
		args := mrb.GetArgs()
		sql, serr := encodeToConn(mrb, self, args.Item(0))
		if serr != nil {
			return mrb.RaiseError(serr)
		}
		if args.Len() >= 2 && !args.Item(1).IsNil() {
			resFmt := 0
			if args.Len() >= 3 && !args.Item(2).IsNil() {
				resFmt = args.Item(2).Int()
			}
			tm := c.typeMapQueries
			if args.Len() >= 4 && !args.Item(3).IsNil() {
				if terr := validateTypeMapArg(mrb, args.Item(3)); terr != nil {
					return mrb.RaiseError(terr)
				}
				tm = args.Item(3)
			}
			vals, oids, fmts, perr := extractParams(mrb, self, args.Item(1), tm)
			if perr != nil {
				return mrb.RaiseError(perr)
			}
			var resFmts []int16
			if resFmt != 0 {
				resFmts = []int16{int16(resFmt)}
			}
			if c.pipelineStatus != pqPipelineOff {
				c.pipelineQueue = append(c.pipelineQueue, pipelineCmd{kind: 1, sql: sql, vals: vals, oids: oids, fmts: fmts, resFmts: resFmts})
				return mrb.NilValue()
			}
			c.pollAsyncNonBlocking()
			if c.asyncDone != nil || c.streamCh != nil || c.hasStreamPeek {
				return raiseConnError(mrb, self, "PG::Error", "another command is already in progress")
			}
			startStreamParams(mrb, c, sql, vals, oids, fmts, resFmts)
			return mrb.NilValue()
		}
		if c.pipelineStatus != pqPipelineOff {
			return raiseConnError(mrb, self, "PG::Error", "synchronous/simple query not allowed in pipeline mode")
		}
		c.pollAsyncNonBlocking()
		if c.asyncDone != nil || c.streamCh != nil || c.hasStreamPeek {
			return raiseConnError(mrb, self, "PG::Error", "another command is already in progress")
		}
		ctx, cancel := context.WithCancel(mrb.GoContext())
		ch := make(chan streamChunk, 64)
		c.streamCh = ch
		c.streamCancel = cancel
		c.hasStreamPeek = false
		go func() {
			mrr := c.conn.Exec(ctx, sql)
			emitted := 0
			for mrr.NextResult() {
				rr := mrr.ResultReader()
				var fds []pgconn.FieldDescription
				for rr.NextRow() {
					if len(fds) == 0 {
						fds = append([]pgconn.FieldDescription(nil), rr.FieldDescriptions()...)
					}
					ch <- streamChunk{fields: fds, row: copyRowBytes(rr.Values()), isRow: true}
					c.signalWake()
				}
				tag, rerr := rr.Close()
				if len(fds) == 0 {
					fds = append([]pgconn.FieldDescription(nil), rr.FieldDescriptions()...)
				}
				ch <- streamChunk{fields: fds, cmdTag: tag, err: rerr, isEnd: true}
				c.signalWake()
				emitted++
			}
			qerr := mrr.Close()
			if emitted == 0 {
				ch <- streamChunk{err: qerr, isEnd: true}
			}
			ch <- streamChunk{allDone: true}
			close(ch)
			c.signalWake()
		}()
		return mrb.NilValue()
	}, mrb.ArgsReq(1)|mrb.ArgsOpt(3))

	cls.DefineMethod("send_query_params", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		if self.Frozen() {
			return mrb.EFrozenError().Raise("can't modify frozen PG::Connection")
		}
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return raiseConnError(mrb, self, "PG::ConnectionBad", "connection is closed")
		}
		args := mrb.GetArgs()
		if args.Len() < 2 || args.Len() > 4 {
			return mrb.EArgumentError().Raisef("wrong number of arguments (given %d, expected 2..4)", args.Len())
		}
		sql, serr := encodeToConn(mrb, self, args.Item(0))
		if serr != nil {
			return mrb.RaiseError(serr)
		}
		params := args.Item(1)
		if !params.IsArray() {
			return mrb.ETypeError().Raise("wrong argument type (expected Array)")
		}
		resFmt := 0
		if args.Len() >= 3 && !args.Item(2).IsNil() {
			resFmt = args.Item(2).Int()
		}
		tm := c.typeMapQueries
		if args.Len() >= 4 && !args.Item(3).IsNil() {
			if terr := validateTypeMapArg(mrb, args.Item(3)); terr != nil {
				return mrb.RaiseError(terr)
			}
			tm = args.Item(3)
		}
		vals, oids, fmts, perr := extractParams(mrb, self, params, tm)
		if perr != nil {
			return mrb.RaiseError(perr)
		}
		var resFmts []int16
		if resFmt != 0 {
			resFmts = []int16{int16(resFmt)}
		}
		if c.pipelineStatus != pqPipelineOff {
			c.pipelineQueue = append(c.pipelineQueue, pipelineCmd{kind: 1, sql: sql, vals: vals, oids: oids, fmts: fmts, resFmts: resFmts})
			return mrb.NilValue()
		}
		c.pollAsyncNonBlocking()
		if c.asyncDone != nil || c.streamCh != nil || c.hasStreamPeek {
			return raiseConnError(mrb, self, "PG::Error", "another command is already in progress")
		}
		startStreamParams(mrb, c, sql, vals, oids, fmts, resFmts)
		return mrb.NilValue()
	}, mrb.ArgsReq(2)|mrb.ArgsOpt(2))

	cls.DefineMethod("send_prepare", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		if self.Frozen() {
			return mrb.EFrozenError().Raise("can't modify frozen PG::Connection")
		}
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return raiseConnError(mrb, self, "PG::ConnectionBad", "connection is closed")
		}
		args := mrb.GetArgs()
		name, nerr := encodeToConn(mrb, self, args.Item(0))
		if nerr != nil {
			return mrb.RaiseError(nerr)
		}
		sql, serr := encodeToConn(mrb, self, args.Item(1))
		if serr != nil {
			return mrb.RaiseError(serr)
		}
		var oids []uint32
		if args.Len() >= 3 && args.Item(2).IsArray() {
			a := args.Item(2)
			oids = make([]uint32, oruby.RArrayLen(a))
			for i := 0; i < len(oids); i++ {
				oids[i] = uint32(mrb.AryRef(a, i).Int64())
			}
		}
		if c.pipelineStatus != pqPipelineOff {
			c.pipelineQueue = append(c.pipelineQueue, pipelineCmd{kind: 2, name: name, sql: sql, oids: oids})
			return mrb.NilValue()
		}
		c.pollAsyncNonBlocking()
		if c.asyncDone != nil || c.streamCh != nil || c.hasStreamPeek {
			return raiseConnError(mrb, self, "PG::Error", "another command is already in progress")
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
			c.signalWake()
		}()
		return mrb.NilValue()
	}, mrb.ArgsReq(2)|mrb.ArgsOpt(1))

	cls.DefineMethod("send_describe_prepared", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return raiseConnError(mrb, self, "PG::ConnectionBad", "connection is closed")
		}
		name, nerr := encodeToConn(mrb, self, mrb.GetArgsFirst())
		if nerr != nil {
			return mrb.RaiseError(nerr)
		}
		c.pollAsyncNonBlocking()
		if c.asyncDone != nil || c.streamCh != nil || c.hasStreamPeek {
			return raiseConnError(mrb, self, "PG::Error", "another command is already in progress")
		}
		sd, ok := c.prepared[name]
		if !ok {
			c.pendingRaw = append(c.pendingRaw, asyncRes{res: &pgconn.Result{Err: &pgconn.PgError{Code: "26000", Message: fmt.Sprintf("prepared statement %q does not exist", name)}}})
			return mrb.NilValue()
		}
		c.pendingRaw = append(c.pendingRaw, asyncRes{
			res:       &pgconn.Result{FieldDescriptions: sd.Fields},
			paramOIDs: sd.ParamOIDs,
		})
		return mrb.NilValue()
	}, mrb.ArgsReq(1))

	cls.DefineMethod("send_describe_portal", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return raiseConnError(mrb, self, "PG::ConnectionBad", "connection is closed")
		}
		name, nerr := encodeToConn(mrb, self, mrb.GetArgsFirst())
		if nerr != nil {
			return mrb.RaiseError(nerr)
		}
		c.pollAsyncNonBlocking()
		if c.asyncDone != nil || c.streamCh != nil || c.hasStreamPeek {
			return raiseConnError(mrb, self, "PG::Error", "another command is already in progress")
		}
		ctx := mrb.GoContext()
		q := "FETCH 0 FROM " + escapePGIdent(name)
		ch := make(chan []asyncRes, 1)
		c.asyncDone = ch
		go func() {
			results, qerr := readAllWithFields(c.conn.Exec(ctx, q))
			if qerr != nil {
				ch <- []asyncRes{{res: &pgconn.Result{Err: qerr}}}
			} else if len(results) > 0 {
				ch <- []asyncRes{{res: results[len(results)-1]}}
			} else {
				ch <- []asyncRes{{res: &pgconn.Result{}}}
			}
			close(ch)
		}()
		return mrb.NilValue()
	}, mrb.ArgsReq(1))

	cls.DefineMethod("send_query_prepared", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		if self.Frozen() {
			return mrb.EFrozenError().Raise("can't modify frozen PG::Connection")
		}
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return raiseConnError(mrb, self, "PG::ConnectionBad", "connection is closed")
		}
		args := mrb.GetArgs()
		name, nerr := encodeToConn(mrb, self, args.Item(0))
		if nerr != nil {
			return mrb.RaiseError(nerr)
		}
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
			if terr := validateTypeMapArg(mrb, args.Item(3)); terr != nil {
				return mrb.RaiseError(terr)
			}
			tm = args.Item(3)
		}
		vals, _, fmts, perr := extractParams(mrb, self, params, tm)
		if perr != nil {
			return mrb.RaiseError(perr)
		}
		var resFmts []int16
		if resFmt != 0 {
			resFmts = []int16{int16(resFmt)}
		}
		if c.pipelineStatus != pqPipelineOff {
			c.pipelineQueue = append(c.pipelineQueue, pipelineCmd{kind: 3, name: name, vals: vals, fmts: fmts, resFmts: resFmts})
			return mrb.NilValue()
		}
		c.pollAsyncNonBlocking()
		if c.asyncDone != nil || c.streamCh != nil || c.hasStreamPeek {
			return raiseConnError(mrb, self, "PG::Error", "another command is already in progress")
		}
		ctx, cancel := context.WithCancel(mrb.GoContext())
		ch := make(chan streamChunk, 64)
		c.streamCh = ch
		c.streamCancel = cancel
		c.hasStreamPeek = false
		go func() {
			rr := c.conn.ExecPrepared(ctx, name, vals, fmts, resFmts)
			var fds []pgconn.FieldDescription
			for rr.NextRow() {
				if len(fds) == 0 {
					fds = append([]pgconn.FieldDescription(nil), rr.FieldDescriptions()...)
				}
				ch <- streamChunk{fields: fds, row: copyRowBytes(rr.Values()), isRow: true}
				c.signalWake()
			}
			tag, rerr := rr.Close()
			if len(fds) == 0 {
				fds = append([]pgconn.FieldDescription(nil), rr.FieldDescriptions()...)
			}
			ch <- streamChunk{fields: fds, cmdTag: tag, err: rerr, isEnd: true}
			ch <- streamChunk{allDone: true}
			close(ch)
			c.signalWake()
		}()
		return mrb.NilValue()
	}, mrb.ArgsReq(1)|mrb.ArgsOpt(3))

	cls.DefineMethod("is_busy", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return raiseConnError(mrb, self, "PG::ConnectionBad", "connection is closed")
		}
		c.pollAsyncNonBlocking()
		c.flushNotices(mrb, self)
		if c.asyncDone != nil {
			return mrb.TrueValue()
		}
		if c.streamCh != nil && !c.hasStreamPeek {
			select {
			case chunk, ok := <-c.streamCh:
				if !ok {
					c.streamCh = nil
					c.singleRowMode = false
				} else {
					c.streamPeek = chunk
					c.hasStreamPeek = true
				}
			default:
				return mrb.TrueValue()
			}
		}
		return mrb.FalseValue()
	}, mrb.ArgsNone())

	cls.DefineMethod("consume_input", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil {
			return raiseConnError(mrb, self, "PG::ConnectionBad", "connection not open")
		}
		if c.connectDone != nil {
			if c.pollConnect(50*time.Millisecond) == pgresPollingFailed {
				if !c.reportedClosedErr {
					c.reportedClosedErr = true
					return raiseConnError(mrb, self, "PG::ConnectionBad", "server closed the connection unexpectedly")
				}
				return raiseConnError(mrb, self, "PG::ConnectionBad", "connection not open")
			}
			return mrb.NilValue()
		}
		if c.checkOpen() != nil {
			if !c.reportedClosedErr {
				c.reportedClosedErr = true
				return raiseConnError(mrb, self, "PG::ConnectionBad", "server closed the connection unexpectedly")
			}
			return raiseConnError(mrb, self, "PG::ConnectionBad", "connection not open")
		}
		c.drainWake()
		c.pollAsyncNonBlocking()
		c.flushNotices(mrb, self)
		return mrb.NilValue()
	}, mrb.ArgsNone())

	cls.DefineMethod("block", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return raiseConnError(mrb, self, "PG::ConnectionBad", "connection is closed")
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
		if c.streamCh != nil && !c.hasStreamPeek {
			ch := c.streamCh
			var timedOut bool
			if hasTimeout {
				deadline := time.Now().Add(dur)
				for c.streamCh != nil && !c.hasStreamPeek {
					rem := time.Until(deadline)
					if rem <= 0 {
						timedOut = true
						break
					}
					timer := time.NewTimer(rem)
					var got, ok bool
					var chunk streamChunk
					mrb.Blocking(func() {
						select {
						case chunk, ok = <-ch:
							got = true
						case <-c.noticeCh:
						case <-timer.C:
							timedOut = true
						}
					})
					timer.Stop()
					c.flushNotices(mrb, self)
					if got {
						if !ok {
							c.streamCh = nil
							c.singleRowMode = false
						} else {
							c.streamPeek = chunk
							c.hasStreamPeek = true
						}
						break
					}
					if timedOut {
						break
					}
				}
			} else {
				for c.streamCh != nil && !c.hasStreamPeek {
					var got, ok bool
					var chunk streamChunk
					mrb.Blocking(func() {
						select {
						case chunk, ok = <-ch:
							got = true
						case <-c.noticeCh:
						}
					})
					c.flushNotices(mrb, self)
					if got {
						if !ok {
							c.streamCh = nil
							c.singleRowMode = false
						} else {
							c.streamPeek = chunk
							c.hasStreamPeek = true
						}
						break
					}
				}
			}
			return mrb.BoolValue(!timedOut)
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

	wrapAsyncRes := func(mrb *oruby.MrbState, self oruby.Value, c *pgConn, ar asyncRes) oruby.Value {
		rv := wrapPGResult(mrb, self, c, ar.res)
		if r, gerr := getRes(mrb, rv); gerr == nil {
			if len(ar.paramOIDs) > 0 {
				r.paramOIDs = ar.paramOIDs
			}
			if ar.forcedStatus != 0 {
				r.status = ar.forcedStatus
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
				r := &pgResult{status: pgresCopyOut, typeMap: mrb.NilValue(), connVal: self}
				return mrb.DataWrapInterface(resCls, r).Value()
			}
		}
		if len(c.pendingRaw) == 0 && (c.streamCh != nil || c.hasStreamPeek) {
			if c.singleRowMode {
				chunk, ok := c.nextStreamChunk(mrb, self)
				if !ok || chunk.allDone {
					c.streamCh = nil
					c.hasStreamPeek = false
					c.singleRowMode = false
					if c.streamCancel != nil {
						c.streamCancel()
						c.streamCancel = nil
					}
					return mrb.NilValue()
				}
				var rv oruby.Value
				if chunk.isRow {
					rv = wrapPGResult(mrb, self, c, &pgconn.Result{
						FieldDescriptions: chunk.fields,
						Rows:              [][][]byte{chunk.row},
					})
					if r, gerr := getRes(mrb, rv); gerr == nil {
						r.status = pgresSingleTuple
						r.singleRow = true
					}
				} else {
					rv = wrapPGResult(mrb, self, c, &pgconn.Result{
						FieldDescriptions: chunk.fields,
						CommandTag:        chunk.cmdTag,
						Err:               chunk.err,
					})
					if r, gerr := getRes(mrb, rv); gerr == nil {
						r.singleRow = true
					}
				}
				blk := mrb.GetArgsBlock()
				return yieldAndClearResult(mrb, rv, blk)
			}
			c.drainStreamToPending(mrb, self, true)
		}
		if len(c.pendingRaw) == 0 && c.asyncDone != nil {
			c.waitAsync(mrb, self)
		}
		if len(c.pendingRaw) == 0 {
			return mrb.NilValue()
		}
		ar := c.pendingRaw[0]
		c.pendingRaw = c.pendingRaw[1:]
		if ar.isSentinel {
			return mrb.NilValue()
		}
		if ar.forcedStatus == pgresPipelineAborted {
			c.pipelineStatus = pqPipelineAborted
		} else if ar.forcedStatus == pgresPipelineSync && c.pipelineStatus == pqPipelineAborted {
			c.pipelineStatus = pqPipelineOn
		}
		rv := wrapAsyncRes(mrb, self, c, ar)
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
				r := &pgResult{status: pgresCopyOut, typeMap: mrb.NilValue(), connVal: self}
				return mrb.DataWrapInterface(resCls, r).Value()
			}
		}
		if c.asyncDone != nil || c.streamCh != nil || c.hasStreamPeek {
			c.waitAsync(mrb, self)
		}
		if len(c.pendingRaw) == 0 {
			return mrb.NilValue()
		}
		var ar asyncRes
		if c.pipelineStatus != pqPipelineOff {
			// Pipeline mode: consume only through the next sentinel (or PGRES_PIPELINE_SYNC)
			found := false
			for len(c.pendingRaw) > 0 {
				item := c.pendingRaw[0]
				c.pendingRaw = c.pendingRaw[1:]
				if item.isSentinel {
					if found {
						break
					}
					continue
				}
				ar = item
				found = true
				if item.forcedStatus == pgresPipelineSync {
					break
				}
			}
			if !found {
				return mrb.NilValue()
			}
		} else {
			ar = c.pendingRaw[len(c.pendingRaw)-1]
			c.pendingRaw = nil
		}
		if ar.forcedStatus == pgresPipelineAborted {
			c.pipelineStatus = pqPipelineAborted
			rv := wrapAsyncRes(mrb, self, c, ar)
			return raiseConnErrorWithResult(mrb, self, rv, "PG::UnableToSend", "pipeline aborted")
		}
		if ar.forcedStatus == pgresPipelineSync && c.pipelineStatus == pqPipelineAborted {
			c.pipelineStatus = pqPipelineOn
		}
		if ar.res != nil && ar.res.Err != nil {
			c.lastErr = ar.res.Err.Error()
			return raisePGError(mrb, self, ar.res.Err)
		}
		rv := wrapAsyncRes(mrb, self, c, ar)
		blk := mrb.GetArgsBlock()
		return yieldAndClearResult(mrb, rv, blk)
	}, mrb.ArgsBlock())

	cls.DefineMethod("discard_results", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil {
			return mrb.FalseValue()
		}
		hadWork := c.asyncDone != nil || c.streamCh != nil || c.hasStreamPeek || len(c.pendingRaw) > 0 || c.copyMode != 0 || len(c.pipelineQueue) > 0
		if !hadWork {
			if c.checkOpen() != nil {
				return mrb.FalseValue()
			}
			return mrb.NilValue()
		}
		if c.asyncDone != nil || c.streamCh != nil || c.hasStreamPeek {
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
		connFailed := false
		for _, ar := range c.pendingRaw {
			if ar.res != nil && ar.res.Err != nil {
				var pgErr *pgconn.PgError
				if (errors.As(ar.res.Err, &pgErr) && pgErr.Code == "57P01") || c.conn == nil || c.conn.IsClosed() {
					connFailed = true
				}
			}
		}
		c.pendingRaw = nil
		c.pipelineQueue = nil
		c.singleRowMode = false
		c.flushNotices(mrb, self)
		if connFailed || c.checkOpen() != nil {
			return mrb.FalseValue()
		}
		return mrb.TrueValue()
	}, mrb.ArgsNone())

	// COPY protocol methods
	putCopyDataFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return raiseConnError(mrb, self, "PG::ConnectionBad", "connection is closed")
		}
		args := mrb.GetArgs()
		data := args.Item(0)
		var coder oruby.Value = mrb.NilValue()
		if args.Len() >= 2 && !args.Item(1).IsNil() {
			coder = args.Item(1)
			if !mrb.RespondTo(coder, mrb.Intern("encode")) {
				return mrb.ETypeError().Raisef("wrong argument type %s (expected kind of PG::Coder)", mrb.ObjClassname(coder))
			}
		} else {
			coder = mrb.IVGet(self, mrb.Intern("@encoder_for_put_copy_data"))
		}
		if c.copyMode != pgresCopyIn || c.copyInW == nil {
			return raiseConnError(mrb, self, "PG::Error", "no COPY in progress")
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
			return raiseConnError(mrb, self, "PG::Error", werr.Error())
		}
		return mrb.TrueValue()
	}
	cls.DefineMethod("put_copy_data", putCopyDataFn, mrb.ArgsReq(1)|mrb.ArgsOpt(1))
	cls.DefineMethod("sync_put_copy_data", putCopyDataFn, mrb.ArgsReq(1)|mrb.ArgsOpt(1))
	cls.DefineMethod("async_put_copy_data", putCopyDataFn, mrb.ArgsReq(1)|mrb.ArgsOpt(1))

	putCopyEndFn := func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return raiseConnError(mrb, self, "PG::ConnectionBad", "connection is closed")
		}
		if c.copyMode != pgresCopyIn || c.copyInW == nil {
			return raiseConnError(mrb, self, "PG::Error", "no COPY in progress")
		}
		args := mrb.GetArgs()
		var errMsg string
		if args.Len() >= 1 && !args.Item(0).IsNil() {
			s, serr := encodeToConn(mrb, self, args.Item(0))
			if serr != nil {
				return mrb.RaiseError(serr)
			}
			errMsg = s
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
			intEnc := connTargetEncoding(mrb, self)
			strVal = applyResultEncoding(mrb, mrb.BytesValue(chunk).Value(), intEnc)
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
			intEnc := connTargetEncoding(mrb, self)
			h := mrb.HashNewCapa(3)
			mrb.HashSet(h, mrb.Intern("relname").Value(), applyResultEncoding(mrb, mrb.StrNew(notif.Channel).Value(), intEnc))
			mrb.HashSet(h, mrb.Intern("be_pid").Value(), mrb.FixnumValue(int(notif.PID)))
			mrb.HashSet(h, mrb.Intern("extra").Value(), applyResultEncoding(mrb, mrb.StrNew(notif.Payload).Value(), intEnc))
			return h
		default:
			return mrb.NilValue()
		}
	}, mrb.ArgsNone())

	cls.DefineMethod("wait_for_notify", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		c, err := getConn(mrb, self)
		if err != nil || c.checkOpen() != nil {
			return raiseConnError(mrb, self, "PG::ConnectionBad", "connection is closed")
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

			// If an async query is running on this connection, wait on notifCh/noticeCh/asyncDone/streamCh
			// without racing c.conn.WaitForNotification.
			for (c.asyncDone != nil || c.streamCh != nil) && notif == nil {
				ch := c.asyncDone
				sch := c.streamCh
				var timedOut bool
				var gotAsync bool
				var gotStream, streamOK bool
				var raw []asyncRes
				var chunk streamChunk
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
					case chunk, streamOK = <-sch:
						gotStream = true
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
				if gotStream {
					if !streamOK || chunk.allDone {
						c.streamCh = nil
						c.singleRowMode = false
					} else {
						c.streamPeek = chunk
						c.hasStreamPeek = true
						break
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

		intEnc := connTargetEncoding(mrb, self)
		chanVal := applyResultEncoding(mrb, mrb.StrNew(notif.Channel).Value(), intEnc)
		if !blk.IsNil() {
			var payloadVal oruby.Value = mrb.NilValue()
			if notif.Payload != "" {
				payloadVal = applyResultEncoding(mrb, mrb.StrNew(notif.Payload).Value(), intEnc)
			}
			if _, yErr := mrb.YieldArgvE(blk, chanVal, mrb.FixnumValue(int(notif.PID)), payloadVal); yErr != nil {
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
		nf := estimateCopyInFields(sql)
		fields := make([]pgconn.FieldDescription, nf)
		if binary {
			for i := range fields {
				fields[i].Format = 1
			}
		}
		r := &pgResult{status: pgresCopyIn, fields: fields, typeMap: mrb.NilValue(), connVal: self}
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
	vals, oids, fmts, err := extractParams(mrb, self, params, typeMap)
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
		res = readResultWithFields(c.conn.ExecParams(ctx, sql, vals, oids, fmts, resFmts))
	})
	c.flushNotices(mrb, self)
	if res.Err != nil {
		c.lastErr = res.Err.Error()
		return raisePGError(mrb, self, res.Err)
	}
	c.lastErr = ""
	resVal := wrapPGResult(mrb, self, c, res)
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
	autoclear    bool
	typeMap      oruby.Value
	fieldNameSym bool
	fieldsCalled bool
	paramOIDs    []uint32
	intEnc       oruby.Value
	singleRow    bool
	connVal      oruby.Value
}

func (r *pgResult) Memsize() int {
	if r.cleared {
		return 48
	}
	return 1024 + len(r.fields)*64 + len(r.rows)*128
}

func internFieldName(mrb *oruby.MrbState, name string, asSym bool) oruby.Value {
	if asSym {
		return mrb.SymbolValue(mrb.Intern(name))
	}
	resCls := rb.ClassPath(mrb, "PG::Result")
	cache := mrb.IVGet(resCls, mrb.Intern("@__fname_cache"))
	if !cache.IsHash() {
		cache = mrb.HashNew().Value()
		mrb.IVSet(resCls, mrb.Intern("@__fname_cache"), cache)
	}
	key := mrb.StrNew(name)
	if existing, err := mrb.Funcall(cache, mrb.Intern("[]"), key); err == nil && !existing.IsNil() {
		return existing
	}
	frozen := mrb.StrNewFrozen(name).Value()
	mrb.HashSet(cache, frozen, frozen)
	return frozen
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
	raw := mrb.BytesValue(cell).Value()
	return applyResultEncoding(mrb, raw, r.intEnc)
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

	cls.DefineMethod("__single_row?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, err := getRes(mrb, self)
		return mrb.BoolValue(err == nil && r.singleRow)
	}, mrb.ArgsNone())

	cls.DefineMethod("__advance_single_row", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, err := getRes(mrb, self)
		if err != nil || r.connVal.IsNil() {
			return raiseConnError(mrb, mrb.NilValue(), "PG::NoResultError", "no result received")
		}
		c, cerr := getConn(mrb, r.connVal)
		if cerr != nil || (c.streamCh == nil && !c.hasStreamPeek) {
			return raiseConnError(mrb, r.connVal, "PG::NoResultError", "no result received")
		}
		chunk, ok := c.nextStreamChunk(mrb, r.connVal)
		if !ok || chunk.allDone {
			c.streamCh = nil
			c.hasStreamPeek = false
			c.singleRowMode = false
			return raiseConnError(mrb, r.connVal, "PG::NoResultError", "no result received")
		}
		if chunk.isRow {
			if len(r.fields) > 0 && len(chunk.fields) != len(r.fields) {
				oldN := len(r.fields)
				newN := len(chunk.fields)
				r.cleared = true
				r.rows = nil
				r.fields = nil
				return raiseConnError(mrb, r.connVal, "PG::InvalidChangeOfResultFields", fmt.Sprintf("number of fields changed from %d to %d ", oldN, newN))
			}
			r.fields = chunk.fields
			r.rows = [][][]byte{chunk.row}
			r.status = pgresSingleTuple
		} else {
			r.rows = nil
			r.cmdTag = chunk.cmdTag
			if chunk.err != nil {
				r.status = pgresFatalError
				r.errMsg = chunk.err.Error()
				var pgErr *pgconn.PgError
				errors.As(chunk.err, &pgErr)
				r.pgErr = pgErr
			} else if len(chunk.fields) > 0 || len(r.fields) > 0 {
				r.status = pgresTuplesOK
			} else {
				r.status = pgresCommandOK
			}
		}
		return self
	}, mrb.ArgsNone())

	cls.DefineMethod("autoclear?", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, err := getRes(mrb, self)
		return mrb.BoolValue(err == nil && r.autoclear)
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
			mrb.AryPush(ary, internFieldName(mrb, f.Name, r.fieldNameSym))
		}
		return ary
	}, mrb.ArgsNone())

	cls.DefineMethod("fname", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := getRes(mrb, self)
		i := mrb.GetArgsFirst().Int()
		if i < 0 || i >= len(r.fields) {
			return mrb.EArgumentError().Raisef("invalid column number %d", i)
		}
		return internFieldName(mrb, r.fields[i].Name, r.fieldNameSym)
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
			return mrb.EArgumentError().Raisef("invalid column number %d", i)
		}
		return mrb.Int64Value(int64(r.fields[i].TableOID))
	}, mrb.ArgsReq(1))

	cls.DefineMethod("ftablecol", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := getRes(mrb, self)
		i := mrb.GetArgsFirst().Int()
		if i < 0 || i >= len(r.fields) {
			return mrb.EArgumentError().Raisef("invalid column number %d", i)
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
			return mrb.EArgumentError().Raisef("invalid column number %d", i)
		}
		return mrb.Int64Value(int64(r.fields[i].DataTypeOID))
	}, mrb.ArgsReq(1))

	cls.DefineMethod("fsize", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := getRes(mrb, self)
		i := mrb.GetArgsFirst().Int()
		if i < 0 || i >= len(r.fields) {
			return mrb.EArgumentError().Raisef("invalid column number %d", i)
		}
		return mrb.FixnumValue(int(r.fields[i].DataTypeSize))
	}, mrb.ArgsReq(1))

	cls.DefineMethod("fmod", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := getRes(mrb, self)
		i := mrb.GetArgsFirst().Int()
		if i < 0 || i >= len(r.fields) {
			return mrb.EArgumentError().Raisef("invalid column number %d", i)
		}
		return mrb.FixnumValue(int(r.fields[i].TypeModifier))
	}, mrb.ArgsReq(1))

	cls.DefineMethod("fformat", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		r, _ := getRes(mrb, self)
		i := mrb.GetArgsFirst().Int()
		if i < 0 || i >= len(r.fields) {
			return mrb.EArgumentError().Raisef("invalid column number %d", i)
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
		if row < 0 || row >= len(r.rows) {
			return mrb.EIndexError().Raisef("Index %d is out of range", row)
		}
		h := mrb.HashNewCapa(len(r.fields))
		for col, f := range r.fields {
			v, err := r.decodeCell(mrb, self, row, col)
			if err != nil {
				return mrb.RaiseError(err)
			}
			key := internFieldName(mrb, f.Name, r.fieldNameSym)
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
		if err := r.checkNotCleared(); err != nil {
			return rb.Raise(mrb, "PG::Error", "%s", err.Error())
		}
		arg := mrb.GetArgsFirst()
		if !arg.IsInt() {
			return mrb.ETypeError().Raise("no implicit conversion into Integer")
		}
		col := arg.Int()
		if col < 0 || col >= len(r.fields) {
			return mrb.EIndexError().Raisef("no column %d in result", col)
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
				return mrb.ETypeError().Raise("fit_to_result must return a kind of PG::TypeMap")
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
		return applyResultEncoding(mrb, mrb.StrNew(r.errMsg).Value(), r.intEnc)
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
				return raisePGError(mrb, r.connVal, r.pgErr)
			}
			return raiseConnErrorWithResult(mrb, r.connVal, self, "PG::Error", r.errMsg)
		}
		return self
	}
	cls.DefineMethod("check", checkFn, mrb.ArgsNone())
	cls.DefineMethod("check_result", checkFn, mrb.ArgsNone())
}
