package http

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oruby/oruby"
)

func init() {
	oruby.LazyGem("http", func(mrb *oruby.MrbState) interface{} {
		httpMod := mrb.DefineModule("HTTP")

		// Module methods
		httpMod.DefineClassMethod("get", mrbHTTPGet, mrb.ArgsReq(1))
		httpMod.DefineClassMethod("post", mrbHTTPPost, mrb.ArgsArg(2, 1))

		// HTTP::Response class
		respClass := httpMod.DefineClassUnder("Response", mrb.ObjectClass())
		oruby.MrbSetInstanceTT(respClass, oruby.MrbTTCData)
		respClass.DefineMethod("initialize", mrbRespInitialize, mrb.ArgsArg(1, 2))
		respClass.DefineMethod("status", mrbRespStatus, mrb.ArgsNone())
		respClass.DefineMethod("headers", mrbRespHeaders, mrb.ArgsNone())
		respClass.DefineMethod("body", mrbRespBody, mrb.ArgsNone())
		respClass.DefineMethod("to_s", mrbRespBody, mrb.ArgsNone())

		// HTTP::Request class
		reqClass := httpMod.DefineClassUnder("Request", mrb.ObjectClass())
		oruby.MrbSetInstanceTT(reqClass, oruby.MrbTTCData)
		reqClass.DefineMethod("method", mrbReqMethod, mrb.ArgsNone())
		reqClass.DefineMethod("path", mrbReqPath, mrb.ArgsNone())
		reqClass.DefineMethod("query", mrbReqQuery, mrb.ArgsNone())
		reqClass.DefineMethod("headers", mrbReqHeaders, mrb.ArgsNone())
		reqClass.DefineMethod("header", mrbReqHeader, mrb.ArgsReq(1))
		reqClass.DefineMethod("body", mrbReqBody, mrb.ArgsNone())

		// HTTP::Server class
		serverClass := httpMod.DefineClassUnder("Server", mrb.ObjectClass())
		oruby.MrbSetInstanceTT(serverClass, oruby.MrbTTCData)
		serverClass.DefineMethod("initialize", mrbServerInitialize, mrb.ArgsOpt(1))
		serverClass.DefineMethod("handle", mrbServerHandle, mrb.ArgsReq(1))
		serverClass.DefineMethod("mount", mrbServerMount, mrb.ArgsReq(2))
		serverClass.DefineMethod("start", mrbServerStart, mrb.ArgsNone())
		serverClass.DefineMethod("start_async", mrbServerStartAsync, mrb.ArgsNone())
		serverClass.DefineMethod("stop", mrbServerStop, mrb.ArgsNone())
		serverClass.DefineMethod("port", mrbServerPort, mrb.ArgsNone())
		serverClass.DefineMethod("addr", mrbServerAddr, mrb.ArgsNone())

		return nil
	})
}

// RequestData holds data for a Ruby HTTP::Request
type RequestData struct {
	Method  string
	Path    string
	Query   string
	Headers map[string]string
	Body    string
}

// ResponseData holds data for a Ruby HTTP::Response
type ResponseData struct {
	Status  int
	Headers map[string]string
	Body    string
}

// Server wraps Go net/http.Server for ORuby
type Server struct {
	mrb      *oruby.MrbState
	addr     string
	mux      *http.ServeMux
	server   *http.Server
	listener net.Listener
	mu       sync.Mutex
	running  bool
	handlers []oruby.Value
}

func mrbServerInitialize(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	addr := ":8080"
	if args.Len() > 0 {
		arg := args.Item(0)
		if arg.Type() == oruby.MrbTTInteger {
			addr = fmt.Sprintf(":%d", arg.Int())
		} else if arg.Type() == oruby.MrbTTString {
			addr = arg.String()
			if !strings.Contains(addr, ":") {
				addr = ":" + addr
			}
		}
	}

	srv := &Server{
		mrb:  mrb,
		addr: addr,
		mux:  http.NewServeMux(),
	}

	mrb.DataSetInterface(self, srv)
	return self
}

func mrbServerHandle(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	srv, ok := mrb.DataGetInterface(self).(*Server)
	if !ok || srv == nil {
		return mrb.Raise(mrb.ERuntimeError(), "HTTP::Server is uninitialized")
	}

	pattern := mrb.GetArgsFirst().String()
	block := mrb.GetArgsBlock()
	if block.IsNil() {
		return mrb.Raise(mrb.EArgumentError(), "block required for handle")
	}

	// Keep block alive in GC
	srv.handlers = append(srv.handlers, block.Value())

	srv.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()

		headers := make(map[string]string, len(r.Header))
		for k, v := range r.Header {
			if len(v) > 0 {
				headers[k] = strings.Join(v, ", ")
			}
		}

		reqData := &RequestData{
			Method:  r.Method,
			Path:    r.URL.Path,
			Query:   r.URL.RawQuery,
			Headers: headers,
			Body:    string(bodyBytes),
		}

		srv.mu.Lock()
		defer srv.mu.Unlock()

		reqKlass := srv.mrb.ClassGetUnder(srv.mrb.ModuleGet("HTTP"), "Request")
		reqVal := srv.mrb.DataWrapInterface(reqKlass, reqData).Value()

		resVal := srv.mrb.YieldArgv(block, reqVal)
		writeHTTPResponse(srv.mrb, w, resVal)
	})

	return self
}

func mrbServerMount(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	srv, ok := mrb.DataGetInterface(self).(*Server)
	if !ok || srv == nil {
		return mrb.Raise(mrb.ERuntimeError(), "HTTP::Server is uninitialized")
	}

	args := mrb.GetArgs()
	pattern := args.Item(0).String()
	dirPath := args.Item(1).String()

	prefix := strings.TrimSuffix(pattern, "/")
	fs := http.FileServer(http.Dir(dirPath))
	if prefix == "" {
		srv.mux.Handle(pattern, fs)
	} else {
		srv.mux.Handle(pattern, http.StripPrefix(prefix, fs))
	}

	return self
}

func (s *Server) ensureListener() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.listener != nil {
		return nil
	}

	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.listener = ln
	s.server = &http.Server{Handler: s.mux}
	s.running = true
	return nil
}

func mrbServerStart(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	srv, ok := mrb.DataGetInterface(self).(*Server)
	if !ok || srv == nil {
		return mrb.Raise(mrb.ERuntimeError(), "HTTP::Server is uninitialized")
	}

	if err := srv.ensureListener(); err != nil {
		return mrb.Raise(mrb.ERuntimeError(), err.Error())
	}

	err := srv.server.Serve(srv.listener)
	if err != nil && err != http.ErrServerClosed {
		return mrb.Raise(mrb.ERuntimeError(), err.Error())
	}
	return self
}

func mrbServerStartAsync(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	srv, ok := mrb.DataGetInterface(self).(*Server)
	if !ok || srv == nil {
		return mrb.Raise(mrb.ERuntimeError(), "HTTP::Server is uninitialized")
	}

	if err := srv.ensureListener(); err != nil {
		return mrb.Raise(mrb.ERuntimeError(), err.Error())
	}

	go func() {
		_ = srv.server.Serve(srv.listener)
	}()

	return self
}

func mrbServerStop(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	srv, ok := mrb.DataGetInterface(self).(*Server)
	if !ok || srv == nil {
		return mrb.Raise(mrb.ERuntimeError(), "HTTP::Server is uninitialized")
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()

	if srv.server != nil && srv.running {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.server.Shutdown(ctx)
		srv.running = false
	}
	return self
}

func mrbServerPort(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	srv, ok := mrb.DataGetInterface(self).(*Server)
	if !ok || srv == nil {
		return mrb.Raise(mrb.ERuntimeError(), "HTTP::Server is uninitialized")
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()

	if srv.listener != nil {
		if tcpAddr, ok := srv.listener.Addr().(*net.TCPAddr); ok {
			return mrb.FixnumValue(tcpAddr.Port)
		}
	}
	return mrb.NilValue()
}

func mrbServerAddr(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	srv, ok := mrb.DataGetInterface(self).(*Server)
	if !ok || srv == nil {
		return mrb.Raise(mrb.ERuntimeError(), "HTTP::Server is uninitialized")
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()

	if srv.listener != nil {
		return mrb.StrNew(srv.listener.Addr().String())
	}
	return mrb.StrNew(srv.addr)
}

func writeHTTPResponse(mrb *oruby.MrbState, w http.ResponseWriter, res oruby.Value) {
	if exc := mrb.Exc(); exc != nil {
		w.WriteHeader(http.StatusInternalServerError)
		msg, _ := mrb.Funcall(exc.Value(), oruby.Sym_inspect)
		_, _ = w.Write([]byte("Internal Server Error: " + mrb.String(msg)))
		mrb.ExcClear()
		return
	}

	if res.IsNil() {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if res.Type() == oruby.MrbTTException {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("Internal Server Error: " + mrb.String(res)))
		return
	}

	// 1. String: text/plain 200 OK
	if res.Type() == oruby.MrbTTString {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(res.String()))
		return
	}

	// 2. ResponseData struct
	if data, ok := mrb.DataGetInterface(res).(*ResponseData); ok && data != nil {
		for k, v := range data.Headers {
			w.Header().Set(k, v)
		}
		status := data.Status
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(data.Body))
		return
	}

	// 3. Rack style: [status, headers, body]
	if res.Type() == oruby.MrbTTArray && oruby.RArrayLen(res) >= 3 {
		statusVal := mrb.AryRef(res, 0)
		headersVal := mrb.AryRef(res, 1)
		bodyVal := mrb.AryRef(res, 2)

		status := http.StatusOK
		if statusVal.Type() == oruby.MrbTTInteger {
			status = statusVal.Int()
		}

		if headersVal.Type() == oruby.MrbTTHash {
			keys := mrb.HashKeys(headersVal)
			kcnt := oruby.RArrayLen(keys)
			for i := 0; i < kcnt; i++ {
				k := mrb.AryRef(keys, i)
				v := mrb.HashGet(headersVal, k)
				w.Header().Set(mrb.String(k), mrb.String(v))
			}
		}

		w.WriteHeader(status)

		if bodyVal.Type() == oruby.MrbTTArray {
			bcnt := oruby.RArrayLen(bodyVal)
			for i := 0; i < bcnt; i++ {
				chunk := mrb.AryRef(bodyVal, i)
				_, _ = w.Write([]byte(mrb.String(chunk)))
			}
		} else {
			_, _ = w.Write([]byte(mrb.String(bodyVal)))
		}
		return
	}

	// Fallback: string representation
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(mrb.String(res)))
}

// HTTP::Request methods
func mrbReqMethod(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	req := mrb.DataGetInterface(self).(*RequestData)
	return mrb.StrNew(req.Method)
}

func mrbReqPath(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	req := mrb.DataGetInterface(self).(*RequestData)
	return mrb.StrNew(req.Path)
}

func mrbReqQuery(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	req := mrb.DataGetInterface(self).(*RequestData)
	return mrb.StrNew(req.Query)
}

func mrbReqHeaders(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	req := mrb.DataGetInterface(self).(*RequestData)
	h := mrb.HashNewCapa(len(req.Headers))
	for k, v := range req.Headers {
		mrb.HashSet(h, mrb.StrNew(k), mrb.StrNew(v))
	}
	return h.Value()
}

func mrbReqHeader(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	req := mrb.DataGetInterface(self).(*RequestData)
	key := mrb.GetArgsFirst().String()
	for k, v := range req.Headers {
		if strings.EqualFold(k, key) {
			return mrb.StrNew(v)
		}
	}
	return mrb.NilValue()
}

func mrbReqBody(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	req := mrb.DataGetInterface(self).(*RequestData)
	return mrb.StrNew(req.Body)
}

// HTTP::Response methods
func mrbRespInitialize(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	status := http.StatusOK
	headers := make(map[string]string)
	body := ""

	if args.Len() > 0 {
		status = args.Item(0).Int()
	}
	if args.Len() > 1 && args.Item(1).Type() == oruby.MrbTTHash {
		hVal := args.Item(1)
		keys := mrb.HashKeys(hVal)
		kcnt := oruby.RArrayLen(keys)
		for i := 0; i < kcnt; i++ {
			k := mrb.AryRef(keys, i)
			v := mrb.HashGet(hVal, k)
			headers[k.String()] = v.String()
		}
	}
	if args.Len() > 2 {
		body = args.Item(2).String()
	}

	data := &ResponseData{
		Status:  status,
		Headers: headers,
		Body:    body,
	}
	mrb.DataSetInterface(self, data)
	return self
}

func mrbRespStatus(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	data := mrb.DataGetInterface(self).(*ResponseData)
	return mrb.FixnumValue(data.Status)
}

func mrbRespHeaders(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	data := mrb.DataGetInterface(self).(*ResponseData)
	h := mrb.HashNewCapa(len(data.Headers))
	for k, v := range data.Headers {
		mrb.HashSet(h, mrb.StrNew(k), mrb.StrNew(v))
	}
	return h.Value()
}

func mrbRespBody(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	data := mrb.DataGetInterface(self).(*ResponseData)
	return mrb.StrNew(data.Body)
}

// HTTP client helpers: HTTP.get and HTTP.post
func mrbHTTPGet(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	url := mrb.GetArgsFirst().String()

	resp, err := http.Get(url)
	if err != nil {
		return mrb.Raise(mrb.ERuntimeError(), err.Error())
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	headers := make(map[string]string, len(resp.Header))
	for k, v := range resp.Header {
		if len(v) > 0 {
			headers[k] = strings.Join(v, ", ")
		}
	}

	data := &ResponseData{
		Status:  resp.StatusCode,
		Headers: headers,
		Body:    string(bodyBytes),
	}

	respKlass := mrb.ClassGetUnder(mrb.ModuleGet("HTTP"), "Response")
	return mrb.DataWrapInterface(respKlass, data).Value()
}

func mrbHTTPPost(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
	args := mrb.GetArgs()
	url := args.Item(0).String()
	body := args.Item(1).String()
	contentType := "text/plain"
	if args.Len() > 2 {
		contentType = args.Item(2).String()
	}

	resp, err := http.Post(url, contentType, strings.NewReader(body))
	if err != nil {
		return mrb.Raise(mrb.ERuntimeError(), err.Error())
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	headers := make(map[string]string, len(resp.Header))
	for k, v := range resp.Header {
		if len(v) > 0 {
			headers[k] = strings.Join(v, ", ")
		}
	}

	data := &ResponseData{
		Status:  resp.StatusCode,
		Headers: headers,
		Body:    string(bodyBytes),
	}

	respKlass := mrb.ClassGetUnder(mrb.ModuleGet("HTTP"), "Response")
	return mrb.DataWrapInterface(respKlass, data).Value()
}

// Helper to convert port to string
func itoa(i int) string {
	return strconv.Itoa(i)
}
