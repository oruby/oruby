//go:build darwin || linux || freebsd

// Package socket implements Ruby's socket library on top of Go syscall and
// net packages. Sockets are IO objects over plain file descriptors; this file
// provides the system call primitives, the Ruby classes are in socket.rb.
package socket

import (
	_ "embed"
	"encoding/binary"
	"errors"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/oruby/oruby"
	"github.com/oruby/oruby/gem/rb"
	"golang.org/x/sys/unix"
)

//go:embed socket.rb
var socketRb []byte

func init() {
	oruby.LazyGem("socket", func(mrb *oruby.MrbState) interface{} {
		mrb.Resolve("io")
		bs := mrb.DefineClass("BasicSocket", mrb.ClassGet("IO"))
		sock := mrb.DefineClass("Socket", bs)
		consts := mrb.DefineModuleUnder(sock, "Constants")
		for name, v := range constants() {
			consts.Const(name, v)
			sock.Const(name, v)
		}
		def := func(name string, n int, f oruby.MrbFuncT) {
			sock.DefineClassMethod("__"+name, f, mrb.ArgsReq(uint32(n)))
		}
		def("socket", 3, sysSocket)
		def("bind", 2, sysBind)
		def("connect", 3, sysConnect)
		def("listen", 2, sysListen)
		def("accept", 2, sysAccept)
		def("getsockname", 1, sysGetsockname)
		def("getpeername", 1, sysGetpeername)
		def("setsockopt", 4, sysSetsockopt)
		def("getsockopt", 3, sysGetsockopt)
		def("shutdown", 2, sysShutdown)
		def("send", 4, sysSend)
		def("recv", 4, sysRecv)
		def("socketpair", 3, sysSocketpair)
		def("getaddrinfo", 6, sysGetaddrinfo)
		def("getnameinfo", 2, sysGetnameinfo)
		def("pack_in", 2, packIn)
		def("unpack_in", 1, unpackIn)
		def("pack_un", 1, packUn)
		def("unpack_un", 1, unpackUn)
		def("sa_family", 1, saFamily)
		def("send_io", 2, sysSendIO)
		def("recv_io", 1, sysRecvIO)
		def("gethostname", 0, func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
			h, err := os.Hostname()
			if err != nil {
				return mrb.RaiseError(err)
			}
			return mrb.StrNew(h)
		})
		def("getservbyname", 2, sysGetservbyname)
		def("getservbyport", 2, sysGetservbyport)
		def("ip_address_list", 0, ipAddressList)
		def("set_nonblock", 2, func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
			a := mrb.GetArgs().Slice()
			if err := syscall.SetNonblock(int(a[0].Int64()), a[1].Bool()); err != nil {
				return mrb.RaiseError(err)
			}
			return mrb.NilValue()
		})
		rb.LoadRuby(mrb, "socket.rb", socketRb)
		initIfaddrs(mrb, sock)
		bs.DefineMethod("timeout", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
			return mrb.GetIV(self, "@__timeout")
		}, mrb.ArgsNone())
		bs.DefineMethod("timeout=", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
			v := mrb.GetArgsFirst()
			if !v.IsNil() && !v.IsInt() && !v.IsFloat() {
				if mrb.RespondTo(v, oruby.Sym_to_f) {
					fv, err := mrb.Funcall(v, oruby.Sym_to_f)
					if err != nil {
						return mrb.RaiseError(err)
					}
					v = fv.Value()
				} else {
					return rb.TypeError(mrb, errors.New("can't convert timeout into Float"))
				}
			}
			mrb.SetIV(self, "@__timeout", v)
			return v
		}, mrb.ArgsReq(1))
		return nil
	})
}

func fdArg(v oruby.Value) int { return int(v.Int64()) }

// waitFd blocks until fd is ready for reading (write=false) or writing.
// Sockets are non-blocking (as in Ruby 3+), blocking calls wait here.
func waitFd(mrb *oruby.MrbState, fd int, write bool) error {
	ev := int16(unix.POLLIN)
	if write {
		ev = unix.POLLOUT
	}
	ctx := mrb.GoContext()
	fds := []unix.PollFd{{Fd: int32(fd), Events: ev}}
	for {
		var n int
		var err error
		mrb.Blocking(func() {
			n, err = unix.Poll(fds, 100)
		})
		if err != nil && !errors.Is(err, syscall.EINTR) {
			return err
		}
		if err == nil && n > 0 {
			if fds[0].Revents&unix.POLLNVAL != 0 {
				return syscall.EBADF
			}
			return nil
		}
		if err := mrb.CheckSignals(); err != nil {
			return err
		}
		if ctx.Err() != nil {
			if err := mrb.ContextErr(ctx); err != nil {
				return err
			}
			return oruby.ERuntimeError("interrupted")
		}
	}
}

func isAgain(err error) bool {
	return errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK)
}

// resErr raises Socket::ResolutionError with error_code
func resErr(mrb *oruby.MrbState, msg string, code int) oruby.MrbValue {
	cls := rb.ClassPath(mrb, "Socket::ResolutionError")
	if cls.IsNil() {
		return mrb.Raise(rb.ClassPath(mrb, "SocketError"), msg)
	}
	exc, err := mrb.Funcall(cls.Value(), oruby.Sym_new, mrb.StrNew(msg), code)
	if err != nil {
		return mrb.Exc().Value()
	}
	return mrb.RaiseObj(exc)
}

// sysErr raises Errno for syscall errors, IO::EAGAINWaitReadable etc. for EAGAIN.
// op is "r" or "w" for non-blocking calls, "" otherwise.
func sysErr(mrb *oruby.MrbState, err error, op string) oruby.MrbValue {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return mrb.RaiseError(err)
	}
	msg := errno.Error()
	if msg != "" {
		msg = strings.ToUpper(msg[:1]) + msg[1:]
	}
	name := errnoName(errno)
	if (errno == syscall.EAGAIN || errno == syscall.EINPROGRESS) && op != "" {
		wait := "IO::EAGAINWaitReadable"
		detail := "read would block"
		if errno == syscall.EINPROGRESS {
			wait, detail = "IO::EINPROGRESSWaitWritable", "connect(2) would block"
		} else if op == "w" {
			wait, detail = "IO::EAGAINWaitWritable", "write would block"
		}
		if cls := rb.ClassPath(mrb, wait); !cls.IsNil() {
			return mrb.Raise(cls, msg+" - "+detail)
		}
	}
	if name != "" {
		if cls := rb.ClassPath(mrb, "Errno::"+name); !cls.IsNil() {
			return mrb.Raise(cls, msg)
		}
	}
	return mrb.RaiseError(err)
}

// -- sockaddr -----------------------------------------------------------------

func sockaddrToBytes(sa syscall.Sockaddr) []byte {
	switch a := sa.(type) {
	case *syscall.SockaddrInet4:
		b := make([]byte, 16)
		putFamily(b, syscall.AF_INET, 16)
		binary.BigEndian.PutUint16(b[2:], uint16(a.Port))
		copy(b[4:8], a.Addr[:])
		return b
	case *syscall.SockaddrInet6:
		b := make([]byte, 28)
		putFamily(b, syscall.AF_INET6, 28)
		binary.BigEndian.PutUint16(b[2:], uint16(a.Port))
		copy(b[8:24], a.Addr[:])
		binary.LittleEndian.PutUint32(b[24:], a.ZoneId)
		return b
	case *syscall.SockaddrUnix:
		b := make([]byte, unixPathOffset+len(a.Name)+1)
		putFamily(b, syscall.AF_UNIX, len(b))
		copy(b[unixPathOffset:], a.Name)
		if unixFullSize {
			full := make([]byte, unixPathOffset+unixPathMax)
			copy(full, b)
			putFamily(full, syscall.AF_UNIX, len(b))
			return full
		}
		return b
	}
	return nil
}

func bytesToSockaddr(b []byte) (syscall.Sockaddr, error) {
	if len(b) < 2 {
		return nil, errors.New("too short sockaddr")
	}
	switch getFamily(b) {
	case syscall.AF_INET:
		if len(b) < 8 {
			return nil, errors.New("too short sockaddr")
		}
		sa := &syscall.SockaddrInet4{Port: int(binary.BigEndian.Uint16(b[2:]))}
		copy(sa.Addr[:], b[4:8])
		return sa, nil
	case syscall.AF_INET6:
		if len(b) < 24 {
			return nil, errors.New("too short sockaddr")
		}
		sa := &syscall.SockaddrInet6{Port: int(binary.BigEndian.Uint16(b[2:]))}
		copy(sa.Addr[:], b[8:24])
		if len(b) >= 28 {
			sa.ZoneId = binary.LittleEndian.Uint32(b[24:])
		}
		return sa, nil
	case syscall.AF_UNIX:
		name := string(b[unixPathOffset:])
		if i := strings.IndexByte(name, 0); i >= 0 {
			name = name[:i]
		}
		return &syscall.SockaddrUnix{Name: name}, nil
	}
	return nil, syscall.EAFNOSUPPORT
}

func saArg(mrb *oruby.MrbState, v oruby.Value) (syscall.Sockaddr, oruby.MrbValue) {
	b, err := rb.Bytes(mrb, v)
	if err != nil {
		return nil, rb.TypeError(mrb, err)
	}
	sa, err := bytesToSockaddr(b)
	if err != nil {
		return nil, rb.Raise(mrb, "ArgumentError", "%v", err)
	}
	return sa, nil
}

func binStr(mrb *oruby.MrbState, b []byte) oruby.Value {
	v := mrb.BytesValue(b)
	return mrb.Call(v, "force_encoding", mrb.StrNew("ASCII-8BIT"))
}

func saFamily(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	b, err := rb.Bytes(mrb, mrb.GetArgsFirst())
	if err != nil || len(b) < 2 {
		return mrb.NilValue()
	}
	return mrb.FixnumValue(getFamily(b))
}

func packIn(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	port, err := portNumber(mrb, a[0])
	if err != nil {
		return mrb.Raise(rb.ClassPath(mrb, "SocketError"), err.Error())
	}
	host := "127.0.0.1"
	switch {
	case a[1].IsNil():
	case a[1].Type() == oruby.MrbTTInteger:
		n := uint32(a[1].Int64())
		host = net.IPv4(byte(n>>24), byte(n>>16), byte(n>>8), byte(n)).String()
	default:
		host, err = rb.Str(mrb, a[1])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
	}
	ip, err := resolveOne(host)
	if err != nil {
		return mrb.Raise(rb.ClassPath(mrb, "SocketError"), "getaddrinfo: "+err.Error())
	}
	if ip4 := ip.To4(); ip4 != nil && !strings.Contains(host, ":") {
		sa := &syscall.SockaddrInet4{Port: port}
		copy(sa.Addr[:], ip4)
		return binStr(mrb, sockaddrToBytes(sa))
	}
	sa := &syscall.SockaddrInet6{Port: port}
	copy(sa.Addr[:], ip.To16())
	return binStr(mrb, sockaddrToBytes(sa))
}

func formatIPv6(addr [16]byte) string {
	v4mapped := true
	for i := 0; i < 10; i++ {
		if addr[i] != 0 {
			v4mapped = false
			break
		}
	}
	if v4mapped && addr[10] == 0xff && addr[11] == 0xff {
		return "::ffff:" + net.IP(addr[12:16]).String()
	}
	return net.IP(addr[:]).String()
}

func unpackIn(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	sa, exc := saArg(mrb, mrb.GetArgsFirst())
	if exc != nil {
		return exc
	}
	switch a := sa.(type) {
	case *syscall.SockaddrInet4:
		return mrb.Value([]interface{}{a.Port, net.IP(a.Addr[:]).String()})
	case *syscall.SockaddrInet6:
		return mrb.Value([]interface{}{a.Port, formatIPv6(a.Addr)})
	}
	return rb.Raise(mrb, "ArgumentError", "not an AF_INET/AF_INET6 sockaddr")
}

func packUn(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	path, err := rb.Str(mrb, mrb.GetArgsFirst())
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	if strings.ContainsRune(path, 0) {
		return rb.Raise(mrb, "ArgumentError", "string contains null byte")
	}
	if len(path) >= unixPathMax {
		return rb.Raise(mrb, "ArgumentError", "too long unix socket path (%d bytes given but %d bytes max)", len(path), unixPathMax)
	}
	b := make([]byte, unixPathOffset+unixPathMax)
	putFamily(b, syscall.AF_UNIX, len(b))
	copy(b[unixPathOffset:], path)
	return binStr(mrb, b)
}

func unpackUn(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	b, err := rb.Bytes(mrb, mrb.GetArgsFirst())
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	if len(b) < 2 || getFamily(b) != syscall.AF_UNIX {
		return rb.Raise(mrb, "ArgumentError", "not an AF_UNIX sockaddr")
	}
	name := string(b[unixPathOffset:])
	if i := strings.IndexByte(name, 0); i >= 0 {
		name = name[:i]
	}
	return mrb.StrNew(name)
}

// -- syscalls -----------------------------------------------------------------

func sysSocket(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	fd, err := syscall.Socket(int(a[0].Int64()), int(a[1].Int64()), int(a[2].Int64()))
	if err != nil {
		return sysErr(mrb, err, "")
	}
	syscall.CloseOnExec(fd)
	syscall.SetNonblock(fd, true)
	return mrb.FixnumValue(fd)
}

func sysBind(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	sa, exc := saArg(mrb, a[1])
	if exc != nil {
		return exc
	}
	if err := syscall.Bind(fdArg(a[0]), sa); err != nil {
		return sysErr(mrb, err, "")
	}
	return mrb.FixnumValue(0)
}

func sysConnect(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	sa, exc := saArg(mrb, a[1])
	if exc != nil {
		return exc
	}
	nonblock := a[2].Bool()
	fd := fdArg(a[0])
	err := syscall.Connect(fd, sa)
	for errors.Is(err, syscall.EINTR) {
		err = syscall.Connect(fd, sa)
	}
	if err == nil {
		return mrb.FixnumValue(0)
	}
	if nonblock {
		return sysErr(mrb, err, "w")
	}
	if errors.Is(err, syscall.EINPROGRESS) || errors.Is(err, syscall.EALREADY) {
		if err := waitFd(mrb, fd, true); err != nil {
			return sysErr(mrb, err, "")
		}
		n, gerr := syscall.GetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_ERROR)
		if gerr != nil {
			return sysErr(mrb, gerr, "")
		}
		if n != 0 {
			return sysErr(mrb, syscall.Errno(n), "")
		}
		return mrb.FixnumValue(0)
	}
	return sysErr(mrb, err, "")
}

func sysListen(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	if err := syscall.Listen(fdArg(a[0]), int(a[1].Int64())); err != nil {
		return sysErr(mrb, err, "")
	}
	return mrb.FixnumValue(0)
}

func sysAccept(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	fd := fdArg(a[0])
	for {
		nfd, sa, err := syscall.Accept(fd)
		if err != nil {
			if errors.Is(err, syscall.EINTR) {
				continue
			}
			if isAgain(err) && !a[1].Bool() {
				if werr := waitFd(mrb, fd, false); werr != nil {
					return sysErr(mrb, werr, "")
				}
				continue
			}
			if a[1].Bool() {
				return sysErr(mrb, err, "r")
			}
			return sysErr(mrb, err, "")
		}
		syscall.CloseOnExec(nfd)
		syscall.SetNonblock(nfd, true)
		var sab []byte
		if sa != nil {
			sab = sockaddrToBytes(sa)
		} else {
			sab = sockaddrToBytes(&syscall.SockaddrUnix{})
		}
		return mrb.Value([]interface{}{nfd, binStr(mrb, sab)})
	}
}

func sysGetsockname(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	sa, err := syscall.Getsockname(fdArg(mrb.GetArgsFirst()))
	if err != nil {
		return sysErr(mrb, err, "")
	}
	return binStr(mrb, sockaddrToBytes(sa))
}

func sysGetpeername(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	sa, err := syscall.Getpeername(fdArg(mrb.GetArgsFirst()))
	if err != nil {
		return sysErr(mrb, err, "")
	}
	return binStr(mrb, sockaddrToBytes(sa))
}

func sysSetsockopt(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	data, err := rb.Bytes(mrb, a[3])
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	var p unsafe.Pointer
	if len(data) > 0 {
		p = unsafe.Pointer(&data[0])
	}
	_, _, e := syscall.Syscall6(syscall.SYS_SETSOCKOPT, uintptr(fdArg(a[0])), uintptr(a[1].Int64()), uintptr(a[2].Int64()),
		uintptr(p), uintptr(len(data)), 0)
	if e != 0 {
		return sysErr(mrb, e, "")
	}
	return mrb.FixnumValue(0)
}

func sysGetsockopt(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	buf := make([]byte, 256)
	l := uint32(len(buf))
	_, _, e := syscall.Syscall6(syscall.SYS_GETSOCKOPT, uintptr(fdArg(a[0])), uintptr(a[1].Int64()), uintptr(a[2].Int64()),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&l)), 0)
	if e != 0 {
		return sysErr(mrb, e, "")
	}
	return binStr(mrb, buf[:l])
}

func sysShutdown(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	if err := syscall.Shutdown(fdArg(a[0]), int(a[1].Int64())); err != nil {
		return sysErr(mrb, err, "")
	}
	return mrb.FixnumValue(0)
}

// send(fd, data, flags, dest_sockaddr_or_nil)
func sysSend(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	data, err := rb.Bytes(mrb, a[1])
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	flags := int(a[2].Int64())
	var to syscall.Sockaddr
	if !a[3].IsNil() {
		sa, exc := saArg(mrb, a[3])
		if exc != nil {
			return exc
		}
		to = sa
	}
	for {
		var n int
		if to == nil {
			n, err = syscall.SendmsgN(fdArg(a[0]), data, nil, nil, flags)
		} else {
			err = syscall.Sendto(fdArg(a[0]), data, flags, to)
			n = len(data)
		}
		if err != nil {
			if errors.Is(err, syscall.EINTR) {
				continue
			}
			if isAgain(err) && flags&syscall.MSG_DONTWAIT == 0 {
				if werr := waitFd(mrb, fdArg(a[0]), true); werr != nil {
					return sysErr(mrb, werr, "")
				}
				continue
			}
			return sysErr(mrb, err, "w")
		}
		return mrb.FixnumValue(n)
	}
}

// recv(fd, maxlen, flags, nonblock) -> [data, sockaddr or nil]
func sysRecv(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	fd := fdArg(a[0])
	maxlen := int(a[1].Int64())
	if maxlen < 0 {
		return rb.Raise(mrb, "ArgumentError", "negative buffer size (or size too big)")
	}
	buf := make([]byte, maxlen)
	for {
		n, from, err := syscall.Recvfrom(fd, buf, int(a[2].Int64()))
		if err != nil {
			if errors.Is(err, syscall.EINTR) {
				continue
			}
			if isAgain(err) && !a[3].Bool() {
				if werr := waitFd(mrb, fd, false); werr != nil {
					return sysErr(mrb, werr, "")
				}
				continue
			}
			if a[3].Bool() {
				return sysErr(mrb, err, "r")
			}
			return sysErr(mrb, err, "")
		}
		sa := oruby.Nil
		if from != nil {
			sa = binStr(mrb, sockaddrToBytes(from))
		}
		return mrb.Value([]interface{}{binStr(mrb, buf[:n]), sa, n})
	}
}

func sysSocketpair(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	fds, err := syscall.Socketpair(int(a[0].Int64()), int(a[1].Int64()), int(a[2].Int64()))
	if err != nil {
		return sysErr(mrb, err, "")
	}
	syscall.CloseOnExec(fds[0])
	syscall.CloseOnExec(fds[1])
	syscall.SetNonblock(fds[0], true)
	syscall.SetNonblock(fds[1], true)
	return mrb.Value([]int{fds[0], fds[1]})
}

// -- resolution ---------------------------------------------------------------

func portNumber(mrb *oruby.MrbState, v oruby.Value) (int, error) {
	switch v.Type() {
	case oruby.MrbTTInteger:
		return int(v.Int64()), nil
	case oruby.MrbTTFalse:
		if v.IsNil() {
			return 0, nil
		}
	}
	s, err := rb.Str(mrb, v)
	if err != nil {
		return 0, err
	}
	if s == "" {
		return 0, nil
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n, nil
	}
	return net.LookupPort("tcp", s)
}

func resolveOne(host string) (net.IP, error) {
	switch host {
	case "", "0.0.0.0", "<any>":
		return net.IPv4zero, nil
	case "<broadcast>":
		return net.IPv4bcast, nil
	}
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
		return ip, nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, err
	}
	for _, ip := range ips {
		if ip.To4() != nil {
			return ip, nil
		}
	}
	return ips[0], nil
}

const (
	aiPassive     = 0x1
	aiCanonname   = 0x2
	aiNumericHost = 0x4
)

// getaddrinfo(host, service, family, socktype, protocol, flags)
// -> [[family, socktype, protocol, sockaddr, canonname], ...]
func sysGetaddrinfo(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	host := ""
	hostNil := a[0].IsNil()
	if !hostNil {
		h, err := rb.Str(mrb, a[0])
		if err != nil {
			return rb.TypeError(mrb, err)
		}
		host = h
	}
	port, err := portNumber(mrb, a[1])
	if err != nil {
		return resErr(mrb, "getaddrinfo: Servname not supported for ai_socktype", eaiService)
	}
	family := int(a[2].Int64())
	if family != 0 && family != syscall.AF_INET && family != syscall.AF_INET6 {
		return resErr(mrb, "getaddrinfo: ai_family not supported", eaiFamily)
	}
	socktype := int(a[3].Int64())
	protocol := int(a[4].Int64())
	flags := int(a[5].Int64())

	var ips []net.IP
	canon := ""
	switch {
	case hostNil || host == "":
		if flags&aiPassive != 0 {
			ips = []net.IP{net.IPv6zero, net.IPv4zero}
		} else {
			ips = []net.IP{net.IPv6loopback, net.IPv4(127, 0, 0, 1)}
		}
	case host == "<any>":
		ips = []net.IP{net.IPv4zero}
	case host == "<broadcast>":
		ips = []net.IP{net.IPv4bcast}
	default:
		if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
			ips = []net.IP{ip}
			canon = host
		} else if flags&aiNumericHost != 0 {
			return resErr(mrb, "getaddrinfo: nodename nor servname provided, or not known", eaiNoname)
		} else {
			ctx := mrb.GoContext()
			var addrs []net.IPAddr
			var err error
			var cn string
			mrb.Blocking(func() {
				addrs, err = net.DefaultResolver.LookupIPAddr(ctx, host)
				if err == nil && len(addrs) > 0 {
					cn, _ = net.LookupCNAME(host)
				}
			})
			if err != nil || len(addrs) == 0 {
				return resErr(mrb, "getaddrinfo: nodename nor servname provided, or not known", eaiNoname)
			}
			for _, ad := range addrs {
				ips = append(ips, ad.IP)
			}
			canon = host
			if cn != "" {
				canon = strings.TrimSuffix(cn, ".")
			}
		}
	}
	if host == "localhost" {
		sort.SliceStable(ips, func(i, j int) bool { return ips[i].To4() == nil && ips[j].To4() != nil })
	}
	type st struct{ typ, proto int }
	var types []st
	switch {
	case socktype == 0 && protocol == 0:
		types = []st{{syscall.SOCK_STREAM, syscall.IPPROTO_TCP}, {syscall.SOCK_DGRAM, syscall.IPPROTO_UDP}, {syscall.SOCK_RAW, 0}}
	case socktype == syscall.SOCK_STREAM:
		types = []st{{socktype, syscall.IPPROTO_TCP}}
	case socktype == syscall.SOCK_DGRAM:
		types = []st{{socktype, syscall.IPPROTO_UDP}}
	default:
		types = []st{{socktype, protocol}}
	}
	if socktype == 0 && protocol != 0 {
		if protocol == syscall.IPPROTO_TCP {
			types = []st{{syscall.SOCK_STREAM, protocol}}
		} else if protocol == syscall.IPPROTO_UDP {
			types = []st{{syscall.SOCK_DGRAM, protocol}}
		}
	}
	if protocol != 0 && socktype != 0 {
		types = []st{{socktype, protocol}}
	}
	v6literal := strings.Contains(host, ":")
	res := mrb.AryNew()
	for _, ip := range ips {
		var sab []byte
		fam := syscall.AF_INET6
		if ip4 := ip.To4(); ip4 != nil && !v6literal && !(len(ip) == net.IPv6len && ip.Equal(net.IPv6zero)) {
			fam = syscall.AF_INET
			sa := &syscall.SockaddrInet4{Port: port}
			copy(sa.Addr[:], ip4)
			sab = sockaddrToBytes(sa)
		} else {
			sa := &syscall.SockaddrInet6{Port: port}
			copy(sa.Addr[:], ip.To16())
			sab = sockaddrToBytes(sa)
		}
		if family != 0 && family != fam {
			continue
		}
		for _, t := range types {
			entry := mrb.AryNew()
			entry.Push(mrb.FixnumValue(fam))
			entry.Push(mrb.FixnumValue(t.typ))
			entry.Push(mrb.FixnumValue(t.proto))
			entry.Push(binStr(mrb, sab))
			if flags&aiCanonname != 0 && canon != "" {
				entry.Push(mrb.StrNew(canon))
			} else {
				entry.Push(mrb.NilValue())
			}
			res.Push(entry)
		}
	}
	if res.Len() == 0 {
		return resErr(mrb, "getaddrinfo: Address family for hostname not supported", eaiFamily)
	}
	return res
}

func sysGetnameinfo(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	b, err := rb.Bytes(mrb, a[0])
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	sa, err := bytesToSockaddr(b)
	if err != nil {
		return resErr(mrb, "getnameinfo: "+err.Error(), eaiFamily)
	}
	flags := int(a[1].Int64())
	var host string
	var port int
	switch s := sa.(type) {
	case *syscall.SockaddrInet4:
		host, port = net.IP(s.Addr[:]).String(), s.Port
	case *syscall.SockaddrInet6:
		host, port = formatIPv6(s.Addr), s.Port
	default:
		return resErr(mrb, "getnameinfo: ai_family not supported", eaiFamily)
	}
	if flags&niNumericHost == 0 {
		var names []string
		var err error
		mrb.Blocking(func() {
			names, err = net.LookupAddr(host)
		})
		if err == nil && len(names) > 0 {
			host = strings.TrimSuffix(names[0], ".")
		}
	}
	serv := strconv.Itoa(port)
	if flags&niNumericServ == 0 {
		proto := "tcp"
		if flags&niDgram != 0 {
			proto = "udp"
		}
		if name := serviceName(port, proto); name != "" {
			serv = name
		}
	}
	return mrb.Value([]string{host, serv})
}

var (
	servicesOnce sync.Once
	services     map[string]string // "port/proto" -> name
)

// serviceName looks up the service name of port in /etc/services
func serviceName(port int, proto string) string {
	servicesOnce.Do(func() {
		services = map[string]string{}
		data, err := os.ReadFile("/etc/services")
		if err != nil {
			return
		}
		for _, line := range strings.Split(string(data), "\n") {
			if i := strings.IndexByte(line, '#'); i >= 0 {
				line = line[:i]
			}
			f := strings.Fields(line)
			if len(f) < 2 {
				continue
			}
			if _, ok := services[f[1]]; !ok {
				services[f[1]] = f[0]
			}
		}
	})
	return services[strconv.Itoa(port)+"/"+proto]
}

func sysGetservbyport(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	proto := "tcp"
	if !a[1].IsNil() {
		proto = a[1].String()
	}
	if name := serviceName(int(a[0].Int64()), proto); name != "" {
		return mrb.StrNew(name)
	}
	return mrb.NilValue()
}

func sysGetservbyname(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	name, err := rb.Str(mrb, a[0])
	if err != nil {
		return rb.TypeError(mrb, err)
	}
	proto := "tcp"
	if !a[1].IsNil() {
		proto = a[1].String()
	}
	if n, err := strconv.Atoi(name); err == nil {
		return mrb.FixnumValue(n)
	}
	p, err := net.LookupPort(proto, name)
	if err != nil {
		return mrb.Raise(rb.ClassPath(mrb, "SocketError"), "no such service "+name+"/"+proto)
	}
	return mrb.FixnumValue(p)
}

func ipAddressList(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	res := mrb.AryNew()
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return res
	}
	for _, ad := range addrs {
		ipn, ok := ad.(*net.IPNet)
		if !ok {
			continue
		}
		res.Push(mrb.StrNew(ipn.IP.String()))
	}
	return res
}

func errnoName(e syscall.Errno) string {
	for name, v := range errnoNames {
		if v == e {
			return name
		}
	}
	return ""
}

var errnoNames = map[string]syscall.Errno{
	"EADDRINUSE": syscall.EADDRINUSE, "EADDRNOTAVAIL": syscall.EADDRNOTAVAIL, "EAFNOSUPPORT": syscall.EAFNOSUPPORT,
	"EAGAIN": syscall.EAGAIN, "EALREADY": syscall.EALREADY, "EBADF": syscall.EBADF, "ECONNABORTED": syscall.ECONNABORTED,
	"ECONNREFUSED": syscall.ECONNREFUSED, "ECONNRESET": syscall.ECONNRESET, "EDESTADDRREQ": syscall.EDESTADDRREQ,
	"EHOSTUNREACH": syscall.EHOSTUNREACH, "EINPROGRESS": syscall.EINPROGRESS, "EINTR": syscall.EINTR,
	"EINVAL": syscall.EINVAL, "EISCONN": syscall.EISCONN, "EMSGSIZE": syscall.EMSGSIZE, "ENETDOWN": syscall.ENETDOWN,
	"ENETUNREACH": syscall.ENETUNREACH, "ENOBUFS": syscall.ENOBUFS, "ENOENT": syscall.ENOENT,
	"ENOPROTOOPT": syscall.ENOPROTOOPT, "ENOTCONN": syscall.ENOTCONN, "ENOTSOCK": syscall.ENOTSOCK,
	"EOPNOTSUPP": syscall.EOPNOTSUPP, "EPIPE": syscall.EPIPE, "EPROTONOSUPPORT": syscall.EPROTONOSUPPORT,
	"EPROTOTYPE": syscall.EPROTOTYPE, "ETIMEDOUT": syscall.ETIMEDOUT, "EACCES": syscall.EACCES,
	"EPERM": syscall.EPERM, "ENAMETOOLONG": syscall.ENAMETOOLONG, "EEXIST": syscall.EEXIST,
	"ENOTDIR": syscall.ENOTDIR, "ESOCKTNOSUPPORT": syscall.ESOCKTNOSUPPORT,
}

func sysSendIO(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	a := mrb.GetArgs().Slice()
	oob := syscall.UnixRights(fdArg(a[1]))
	if err := syscall.Sendmsg(fdArg(a[0]), []byte{0}, oob, nil, 0); err != nil {
		return sysErr(mrb, err, "")
	}
	return mrb.NilValue()
}

func sysRecvIO(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
	buf := make([]byte, 1)
	oob := make([]byte, syscall.CmsgSpace(4))
	fd := fdArg(mrb.GetArgsFirst())
	_, oobn, _, _, err := syscall.Recvmsg(fd, buf, oob, 0)
	for err != nil && (isAgain(err) || errors.Is(err, syscall.EINTR)) {
		if werr := waitFd(mrb, fd, false); werr != nil {
			return sysErr(mrb, werr, "")
		}
		_, oobn, _, _, err = syscall.Recvmsg(fd, buf, oob, 0)
	}
	if err != nil {
		return sysErr(mrb, err, "")
	}
	msgs, err := syscall.ParseSocketControlMessage(oob[:oobn])
	if err != nil || len(msgs) == 0 {
		return mrb.Raise(rb.ClassPath(mrb, "SocketError"), "file descriptor was not passed")
	}
	fds, err := syscall.ParseUnixRights(&msgs[0])
	if err != nil || len(fds) == 0 {
		return mrb.Raise(rb.ClassPath(mrb, "SocketError"), "file descriptor was not passed")
	}
	syscall.CloseOnExec(fds[0])
	return mrb.FixnumValue(fds[0])
}
