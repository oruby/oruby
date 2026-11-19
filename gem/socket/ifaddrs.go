//go:build darwin || linux || freebsd

package socket

import (
	"fmt"
	"net"
	"syscall"

	"github.com/oruby/oruby"
)

func posixIfFlags(f net.Flags) int64 {
	var flags int64
	if f&net.FlagUp != 0 {
		flags |= syscall.IFF_UP
	}
	if f&net.FlagBroadcast != 0 {
		flags |= syscall.IFF_BROADCAST
	}
	if f&net.FlagLoopback != 0 {
		flags |= syscall.IFF_LOOPBACK
	}
	if f&net.FlagPointToPoint != 0 {
		flags |= syscall.IFF_POINTOPOINT
	}
	if f&net.FlagMulticast != 0 {
		flags |= syscall.IFF_MULTICAST
	}
	if f&net.FlagRunning != 0 {
		flags |= syscall.IFF_RUNNING
	}
	return flags
}

func makeIPAddrinfo(mrb *oruby.MrbState, addrinfoCls oruby.RClass, ip net.IP, zoneID uint32) oruby.Value {
	if ip == nil {
		return mrb.NilValue()
	}
	var sab []byte
	var fam int
	if ip4 := ip.To4(); ip4 != nil && len(ip) == net.IPv4len {
		fam = syscall.AF_INET
		sa := &syscall.SockaddrInet4{}
		copy(sa.Addr[:], ip4)
		sab = sockaddrToBytes(sa)
	} else if ip16 := ip.To16(); ip16 != nil {
		fam = syscall.AF_INET6
		sa := &syscall.SockaddrInet6{ZoneId: zoneID}
		copy(sa.Addr[:], ip16)
		sab = sockaddrToBytes(sa)
	} else {
		return mrb.NilValue()
	}
	ai, err := mrb.ObjNew(addrinfoCls, binStr(mrb, sab), fam, 0, 0)
	if err != nil {
		mrb.SetExc(nil)
		return mrb.NilValue()
	}
	return ai.Value()
}

func makeMaskAddrinfo(mrb *oruby.MrbState, addrinfoCls oruby.RClass, mask net.IPMask) oruby.Value {
	if len(mask) == net.IPv4len {
		sa := &syscall.SockaddrInet4{}
		copy(sa.Addr[:], mask)
		ai, err := mrb.ObjNew(addrinfoCls, binStr(mrb, sockaddrToBytes(sa)), syscall.AF_INET, 0, 0)
		if err != nil {
			mrb.SetExc(nil)
			return mrb.NilValue()
		}
		return ai.Value()
	}
	if len(mask) == net.IPv6len {
		sa := &syscall.SockaddrInet6{}
		copy(sa.Addr[:], mask)
		ai, err := mrb.ObjNew(addrinfoCls, binStr(mrb, sockaddrToBytes(sa)), syscall.AF_INET6, 0, 0)
		if err != nil {
			mrb.SetExc(nil)
			return mrb.NilValue()
		}
		return ai.Value()
	}
	return mrb.NilValue()
}

func initIfaddrs(mrb *oruby.MrbState, sock oruby.RClass) {
	ifaddrCls := mrb.DefineClassUnder(sock, "Ifaddr", mrb.ObjectClass())
	ifaddrCls.DefineMethod("name", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.GetIV(self, "@name")
	}, mrb.ArgsNone())
	ifaddrCls.DefineMethod("ifindex", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.GetIV(self, "@ifindex")
	}, mrb.ArgsNone())
	ifaddrCls.DefineMethod("flags", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.GetIV(self, "@flags")
	}, mrb.ArgsNone())
	ifaddrCls.DefineMethod("addr", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.GetIV(self, "@addr")
	}, mrb.ArgsNone())
	ifaddrCls.DefineMethod("netmask", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.GetIV(self, "@netmask")
	}, mrb.ArgsNone())
	ifaddrCls.DefineMethod("broadaddr", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.GetIV(self, "@broadaddr")
	}, mrb.ArgsNone())
	ifaddrCls.DefineMethod("dstaddr", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		return mrb.GetIV(self, "@dstaddr")
	}, mrb.ArgsNone())
	ifaddrCls.DefineMethod("inspect", func(mrb *oruby.MrbState, self oruby.Value) oruby.MrbValue {
		name := mrb.GetIV(self, "@name").String()
		return mrb.StrNew(fmt.Sprintf("#<Socket::Ifaddr %s>", name))
	}, mrb.ArgsNone())

	sock.DefineClassMethod("getifaddrs", func(mrb *oruby.MrbState, _ oruby.Value) oruby.MrbValue {
		ifaces, err := net.Interfaces()
		if err != nil {
			return mrb.SysFail(err)
		}
		addrinfoCls := mrb.ClassGet("Addrinfo")
		out := mrb.AryNew()
		for _, ifi := range ifaces {
			flags := posixIfFlags(ifi.Flags)
			addrs, err := ifi.Addrs()
			if err != nil || len(addrs) == 0 {
				obj := mrb.ObjAlloc(oruby.MrbTTObject, ifaddrCls)
				mrb.SetIV(obj, "@name", mrb.StrNew(ifi.Name))
				mrb.SetIV(obj, "@ifindex", ifi.Index)
				mrb.SetIV(obj, "@flags", flags)
				mrb.SetIV(obj, "@addr", mrb.NilValue())
				mrb.SetIV(obj, "@netmask", mrb.NilValue())
				mrb.SetIV(obj, "@broadaddr", mrb.NilValue())
				mrb.SetIV(obj, "@dstaddr", mrb.NilValue())
				out.Push(obj)
				continue
			}
			for _, a := range addrs {
				ipnet, ok := a.(*net.IPNet)
				if !ok {
					continue
				}
				ip := ipnet.IP
				var addrVal, maskVal, bcastVal oruby.Value
				bcastVal = mrb.NilValue()
				if ip4 := ip.To4(); ip4 != nil {
					addrVal = makeIPAddrinfo(mrb, addrinfoCls, ip4, 0)
					mask := ipnet.Mask
					if len(mask) == net.IPv6len {
						mask = mask[12:16]
					}
					maskVal = makeMaskAddrinfo(mrb, addrinfoCls, mask)
					if ifi.Flags&net.FlagBroadcast != 0 && len(mask) == net.IPv4len {
						bcast := make(net.IP, net.IPv4len)
						for i := 0; i < net.IPv4len; i++ {
							bcast[i] = ip4[i] | ^mask[i]
						}
						bcastVal = makeIPAddrinfo(mrb, addrinfoCls, bcast, 0)
					}
				} else {
					var zone uint32
					if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
						zone = uint32(ifi.Index)
					}
					addrVal = makeIPAddrinfo(mrb, addrinfoCls, ip.To16(), zone)
					maskVal = makeMaskAddrinfo(mrb, addrinfoCls, ipnet.Mask)
				}
				obj := mrb.ObjAlloc(oruby.MrbTTObject, ifaddrCls)
				mrb.SetIV(obj, "@name", mrb.StrNew(ifi.Name))
				mrb.SetIV(obj, "@ifindex", ifi.Index)
				mrb.SetIV(obj, "@flags", flags)
				mrb.SetIV(obj, "@addr", addrVal)
				mrb.SetIV(obj, "@netmask", maskVal)
				mrb.SetIV(obj, "@broadaddr", bcastVal)
				mrb.SetIV(obj, "@dstaddr", mrb.NilValue())
				out.Push(obj)
			}
		}
		return out
	}, mrb.ArgsNone())
}
