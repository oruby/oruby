// Package rack provides a Go net/http Rack 2/3 adapter for ORuby
// (ORuby::Rack::Server, Rack::Handler::ORuby, Rackup::Handler::ORuby).
package rack

import (
	"context"
	_ "embed"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/internal/rb"
)

//go:embed rack.rb
var rackRb []byte

func init() {
	oruby.LazyGem("oruby/rack", initRack)
	oruby.GemAlias("rack/handler/oruby", "oruby/rack")
	oruby.GemAlias("rackup/handler/oruby", "oruby/rack")
}

func initRack(mrb *oruby.MrbState) any {
	mrb.Require("stringio")

	orubyMod := mrb.DefineModule("ORuby")
	rackMod := mrb.DefineModuleUnder(orubyMod, "Rack")
	srvCls := mrb.DefineClassUnder(rackMod, "Server", mrb.ObjectClass())
	rb.DataClass(srvCls)

	srvCls.DefineMethod("initialize", srvInit, mrb.ArgsReq(1)|mrb.ArgsOpt(1))
	srvCls.DefineMethod("host", srvHost, mrb.ArgsNone())
	srvCls.DefineMethod("port", srvPort, mrb.ArgsNone())
	srvCls.DefineMethod("addr", srvAddr, mrb.ArgsNone())
	srvCls.DefineMethod("running?", srvRunning, mrb.ArgsNone())
	srvCls.DefineMethod("start", srvStartAsync, mrb.ArgsNone())
	srvCls.DefineMethod("run", srvRunBlocking, mrb.ArgsNone())
	srvCls.DefineMethod("stop", srvStop, mrb.ArgsNone())

	rb.LoadRuby(mrb, "<internal:oruby/rack>", rackRb)
	return nil
}

type rackResponse struct {
	status  int
	headers http.Header
	body    [][]byte
	err     error
}

type rackRequest struct {
	r      *http.Request
	body   []byte
	respCh chan rackResponse
}

type rackServer struct {
	mrb      *oruby.MrbState
	app      oruby.Value
	host     string
	port     int
	listener net.Listener
	server   *http.Server
	mu       sync.Mutex
	running  bool
	blocking bool
	reqCh    chan rackRequest
	stopCh   chan struct{}
	stopOnce sync.Once
}

func getSrv(mrb *oruby.MrbState, self oruby.Value) *rackServer {
	s, _ := rb.Get[*rackServer](mrb, self)
	return s
}

func optString(mrb *oruby.MrbState, opts oruby.Value, key, def string) string {
	if opts.IsNil() || !opts.IsHash() {
		return def
	}
	for _, k := range []oruby.Value{mrb.Intern(key).Value(), mrb.StrNew(key)} {
		if v, err := mrb.Funcall(opts, mrb.Intern("[]"), k); err == nil && !v.IsNil() {
			return v.String()
		}
	}
	return def
}

func optInt(mrb *oruby.MrbState, opts oruby.Value, key string, def int) int {
	if opts.IsNil() || !opts.IsHash() {
		return def
	}
	for _, k := range []oruby.Value{mrb.Intern(key).Value(), mrb.StrNew(key)} {
		if v, err := mrb.Funcall(opts, mrb.Intern("[]"), k); err == nil && !v.IsNil() {
			if v.IsInt() {
				return v.Int()
			}
			if iv, err := mrb.Funcall(v, mrb.Intern("to_i")); err == nil && iv.IsInt() {
				return iv.Int()
			}
		}
	}
	return def
}

func srvInit(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	app := args.Item(0)
	opts := mrb.NilValue()
	if args.Len() >= 2 && args.Item(1).IsHash() {
		opts = args.Item(1)
	}
	if kw := mrb.KeywordArgs().Value(); !kw.IsNil() && kw.IsHash() {
		if opts.IsNil() {
			opts = kw
		} else if merged, err := mrb.Funcall(opts, mrb.Intern("merge"), kw); err == nil {
			opts = merged
		}
	}

	host := optString(mrb, opts, "Host", optString(mrb, opts, "host", "127.0.0.1"))
	port := optInt(mrb, opts, "Port", optInt(mrb, opts, "port", 9292))

	s := &rackServer{
		mrb:    mrb,
		app:    app,
		host:   host,
		port:   port,
		reqCh:  make(chan rackRequest, 64),
		stopCh: make(chan struct{}),
	}
	rb.Set(mrb, self, s)
	mrb.IVSet(self, mrb.Intern("@app"), app)
	return self
}

func srvHost(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	return mrb.StrNew(getSrv(mrb, self).host)
}

func srvPort(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s := getSrv(mrb, self)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		if ta, ok := s.listener.Addr().(*net.TCPAddr); ok {
			return mrb.FixnumValue(ta.Port)
		}
	}
	return mrb.FixnumValue(s.port)
}

func srvAddr(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s := getSrv(mrb, self)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return mrb.StrNew(s.listener.Addr().String())
	}
	return mrb.StrNew(net.JoinHostPort(s.host, fmt.Sprintf("%d", s.port)))
}

func srvRunning(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s := getSrv(mrb, self)
	s.mu.Lock()
	defer s.mu.Unlock()
	return mrb.BoolValue(s.running)
}

func (s *rackServer) listen(blocking bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return nil
	}
	addr := net.JoinHostPort(s.host, fmt.Sprintf("%d", s.port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.listener = ln
	s.blocking = blocking
	s.running = true
	s.server = &http.Server{
		Handler:           http.HandlerFunc(s.serveHTTP),
		ReadHeaderTimeout: 30 * time.Second,
	}
	return nil
}

func (s *rackServer) serveHTTP(w http.ResponseWriter, r *http.Request) {
	var body []byte
	if r.Body != nil {
		var err error
		body, err = io.ReadAll(r.Body)
		_ = r.Body.Close()
		if err != nil {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
	}

	var resp rackResponse
	if s.blocking {
		req := rackRequest{r: r, body: body, respCh: make(chan rackResponse, 1)}
		select {
		case s.reqCh <- req:
		case <-s.stopCh:
			http.Error(w, "Server shutting down", http.StatusServiceUnavailable)
			return
		case <-r.Context().Done():
			return
		}
		select {
		case resp = <-req.respCh:
		case <-s.stopCh:
			http.Error(w, "Server shutting down", http.StatusServiceUnavailable)
			return
		case <-r.Context().Done():
			return
		}
	} else {
		s.mrb.Lock()
		resp = s.dispatchRuby(r, body)
		s.mrb.Unlock()
	}

	if resp.err != nil {
		http.Error(w, "Internal Server Error: "+resp.err.Error(), http.StatusInternalServerError)
		return
	}
	for k, vals := range resp.headers {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	status := resp.status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	for _, chunk := range resp.body {
		if len(chunk) > 0 {
			_, _ = w.Write(chunk)
		}
	}
}

func (s *rackServer) dispatchRuby(r *http.Request, body []byte) rackResponse {
	mrb := s.mrb
	ai := mrb.GCArenaSave()
	defer mrb.GCArenaRestore(ai)

	env := buildRackEnv(mrb, r, body)
	res, err := mrb.Funcall(s.app, mrb.Intern("call"), env)
	if err != nil {
		return rackResponse{err: err}
	}
	if !res.IsArray() || oruby.RArrayLen(res) < 3 {
		return rackResponse{err: fmt.Errorf("rack app must return [status, headers, body]")}
	}

	status := mrb.AryRef(res, 0).Int()
	headersVal := mrb.AryRef(res, 1)
	bodyVal := mrb.AryRef(res, 2)

	hdrs := make(http.Header)
	if headersVal.IsHash() {
		keys := mrb.HashKeys(headersVal)
		for i := 0; i < oruby.RArrayLen(keys); i++ {
			k := mrb.AryRef(keys, i)
			v, err := mrb.Funcall(headersVal, mrb.Intern("[]"), k)
			if err != nil || v.IsNil() {
				continue
			}
			ks := k.String()
			if v.IsArray() {
				// Rack 3 multi-value array headers
				for j := 0; j < oruby.RArrayLen(v); j++ {
					hdrs.Add(ks, mrb.AryRef(v, j).String())
				}
			} else {
				// Rack 2 newline-separated multi-value headers
				vs := v.String()
				if strings.Contains(vs, "\n") {
					for _, part := range strings.Split(vs, "\n") {
						hdrs.Add(ks, part)
					}
				} else {
					hdrs.Add(ks, vs)
				}
			}
		}
	}

	var chunks [][]byte
	if bodyVal.IsArray() {
		n := oruby.RArrayLen(bodyVal)
		chunks = make([][]byte, 0, n)
		for i := 0; i < n; i++ {
			b := mrb.AryRef(bodyVal, i).Bytes()
			cp := make([]byte, len(b))
			copy(cp, b)
			chunks = append(chunks, cp)
		}
	} else {
		// Collect via Ruby helper (handles #each and Rack 3 callable body + #close)
		helper := rb.ClassPath(mrb, "ORuby::Rack")
		collected, err := mrb.Funcall(helper, mrb.Intern("__collect_body"), bodyVal)
		if err != nil {
			return rackResponse{err: err}
		}
		if collected.IsArray() {
			n := oruby.RArrayLen(collected)
			chunks = make([][]byte, 0, n)
			for i := 0; i < n; i++ {
				b := mrb.AryRef(collected, i).Bytes()
				cp := make([]byte, len(b))
				copy(cp, b)
				chunks = append(chunks, cp)
			}
		}
	}

	if bodyVal.IsArray() && mrb.RespondTo(bodyVal, mrb.Intern("close")) {
		_, _ = mrb.Funcall(bodyVal, mrb.Intern("close"))
	}

	return rackResponse{status: status, headers: hdrs, body: chunks}
}

func buildRackEnv(mrb *oruby.MrbState, r *http.Request, body []byte) oruby.Value {
	env := mrb.HashNew()
	setStr := func(k, v string) {
		mrb.HashSet(env, mrb.StrNew(k), mrb.StrNew(v))
	}

	setStr("REQUEST_METHOD", r.Method)
	setStr("SCRIPT_NAME", "")
	setStr("PATH_INFO", r.URL.Path)
	setStr("QUERY_STRING", r.URL.RawQuery)
	setStr("REQUEST_URI", r.RequestURI)
	setStr("SERVER_PROTOCOL", r.Proto)

	serverName, serverPort := r.Host, ""
	if h, p, err := net.SplitHostPort(r.Host); err == nil {
		serverName, serverPort = h, p
	} else if r.TLS != nil {
		serverPort = "443"
	} else {
		serverPort = "80"
	}
	setStr("SERVER_NAME", serverName)
	setStr("SERVER_PORT", serverPort)
	if r.Host != "" {
		setStr("HTTP_HOST", r.Host)
	}

	remoteAddr := r.RemoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		remoteAddr = h
	}
	setStr("REMOTE_ADDR", remoteAddr)

	for k, vals := range r.Header {
		upper := strings.ToUpper(strings.ReplaceAll(k, "-", "_"))
		sep := ", "
		if upper == "COOKIE" {
			sep = "; "
		}
		joined := strings.Join(vals, sep)
		switch upper {
		case "CONTENT_TYPE":
			setStr("CONTENT_TYPE", joined)
		case "CONTENT_LENGTH":
			setStr("CONTENT_LENGTH", joined)
		default:
			setStr("HTTP_"+upper, joined)
		}
	}
	if r.ContentLength >= 0 {
		if _, ok := r.Header["Content-Length"]; !ok {
			setStr("CONTENT_LENGTH", fmt.Sprintf("%d", r.ContentLength))
		}
	}

	ver := mrb.AryNewCapa(2)
	mrb.AryPush(ver, mrb.FixnumValue(1))
	mrb.AryPush(ver, mrb.FixnumValue(3))
	mrb.HashSet(env, mrb.StrNew("rack.version"), ver)

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	setStr("rack.url_scheme", scheme)

	// rack.input as binary StringIO
	sioCls := mrb.ClassGet("StringIO")
	bodyVal := mrb.Call(mrb.BytesValue(body), "b")
	inputVal, err := mrb.Funcall(sioCls, mrb.Intern("new"), bodyVal)
	if err == nil {
		mrb.HashSet(env, mrb.StrNew("rack.input"), inputVal)
	}
	mrb.HashSet(env, mrb.StrNew("rack.errors"), mrb.GVGet(mrb.Intern("$stderr")))
	mrb.HashSet(env, mrb.StrNew("rack.multithread"), mrb.FalseValue())
	mrb.HashSet(env, mrb.StrNew("rack.multiprocess"), mrb.FalseValue())
	mrb.HashSet(env, mrb.StrNew("rack.run_once"), mrb.FalseValue())
	mrb.HashSet(env, mrb.StrNew("rack.hijack?"), mrb.FalseValue())

	return env.Value()
}

func srvStartAsync(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s := getSrv(mrb, self)
	if err := s.listen(false); err != nil {
		return mrb.RaiseError(err)
	}
	go func() {
		_ = s.server.Serve(s.listener)
	}()
	return self
}

func srvRunBlocking(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	s := getSrv(mrb, self)
	if err := s.listen(true); err != nil {
		return mrb.RaiseError(err)
	}

	serveDone := make(chan error, 1)
	go func() {
		serveDone <- s.server.Serve(s.listener)
	}()

	ctx := mrb.GoContext()
	sigWake := mrb.SignalWake()

	for {
		select {
		case req := <-s.reqCh:
			resp := s.dispatchRuby(req.r, req.body)
			req.respCh <- resp
		case <-s.stopCh:
			s.shutdown()
			return self
		case <-ctx.Done():
			s.shutdown()
			if err := mrb.ContextErr(ctx); err != nil {
				return mrb.RaiseError(err)
			}
			return self
		case <-sigWake:
			if err := mrb.CheckSignals(); err != nil {
				s.shutdown()
				return mrb.RaiseError(err)
			}
		case err := <-serveDone:
			if err != nil && err != http.ErrServerClosed {
				return mrb.RaiseError(err)
			}
			return self
		}
	}
}

func (s *rackServer) shutdown() {
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.mu.Lock()
	srv := s.server
	s.running = false
	s.mu.Unlock()
	if srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
}

func srvStop(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	getSrv(mrb, self).shutdown()
	return self
}
