package socket

import (
	"encoding/binary"
	"syscall"
)

const (
	unixPathOffset = 2
	unixPathMax    = 108
	unixFullSize   = true
	eaiFamily      = -6
	eaiNoname      = -2
	eaiService     = -8
	niDgram        = 16
	niNumericHost  = 1
	niNumericServ  = 2
)

func putFamily(b []byte, family, _ int) { binary.LittleEndian.PutUint16(b, uint16(family)) }

func getFamily(b []byte) int { return int(binary.LittleEndian.Uint16(b)) }

func osConstants() map[string]int {
	return map[string]int{
		"AF_PACKET": syscall.AF_PACKET, "PF_PACKET": syscall.AF_PACKET,
		"SO_REUSEPORT": 15, "SO_PEERCRED": syscall.SO_PEERCRED, "SO_PASSCRED": syscall.SO_PASSCRED,
		"SO_TIMESTAMP": syscall.SO_TIMESTAMP, "SO_BINDTODEVICE": syscall.SO_BINDTODEVICE,
		"TCP_KEEPIDLE": syscall.TCP_KEEPIDLE, "TCP_CORK": syscall.TCP_CORK, "TCP_QUICKACK": syscall.TCP_QUICKACK,
		"TCP_KEEPINTVL": syscall.TCP_KEEPINTVL, "TCP_KEEPCNT": syscall.TCP_KEEPCNT,
		"MSG_EOR": syscall.MSG_EOR, "MSG_MORE": syscall.MSG_MORE, "MSG_NOSIGNAL": syscall.MSG_NOSIGNAL,
		"IP_PKTINFO":  syscall.IP_PKTINFO,
		"AI_V4MAPPED": 0x8, "AI_ALL": 0x10, "AI_ADDRCONFIG": 0x20, "AI_NUMERICSERV": 0x400,
		"EAI_BADFLAGS": -1, "EAI_NONAME": -2, "EAI_AGAIN": -3, "EAI_FAIL": -4, "EAI_FAMILY": -6,
		"EAI_SOCKTYPE": -7, "EAI_SERVICE": -8, "EAI_MEMORY": -10, "EAI_SYSTEM": -11, "EAI_OVERFLOW": -12,
		"EAI_NODATA": -5, "EAI_ADDRFAMILY": -9,
		"NI_MAXHOST": 1025, "NI_MAXSERV": 32, "NI_NUMERICHOST": 1, "NI_NUMERICSERV": 2, "NI_NOFQDN": 4,
		"NI_NAMEREQD": 8, "NI_DGRAM": 16,
		"IP_DEFAULT_MULTICAST_LOOP": 1, "IP_DEFAULT_MULTICAST_TTL": 1, "IP_MAX_MEMBERSHIPS": 20,
		"IPV6_JOIN_GROUP": syscall.IPV6_JOIN_GROUP, "IPV6_LEAVE_GROUP": syscall.IPV6_LEAVE_GROUP,
	}
}
