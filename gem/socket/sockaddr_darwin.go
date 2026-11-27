package socket

import "syscall"

const (
	unixPathOffset = 2
	unixPathMax    = 104
	unixFullSize   = true
	eaiFamily      = 5
	eaiNoname      = 8
	eaiService     = 9
	niDgram        = 0x10
	niNumericHost  = 0x2
	niNumericServ  = 0x8
)

func putFamily(b []byte, family, length int) {
	if length > 255 {
		length = 255
	}
	b[0] = byte(length)
	b[1] = byte(family)
}

func getFamily(b []byte) int { return int(b[1]) }

func osConstants() map[string]int {
	return map[string]int{
		"AF_LINK": syscall.AF_LINK, "PF_LINK": syscall.AF_LINK,
		"SO_REUSEPORT": syscall.SO_REUSEPORT, "SO_NOSIGPIPE": syscall.SO_NOSIGPIPE,
		"TCP_KEEPALIVE": syscall.TCP_KEEPALIVE, "TCP_NOPUSH": syscall.TCP_NOPUSH,
		"IP_RECVDSTADDR": syscall.IP_RECVDSTADDR, "IP_RECVIF": syscall.IP_RECVIF,
		"MSG_EOR": syscall.MSG_EOR, "MSG_EOF": syscall.MSG_EOF, "MSG_HAVEMORE": 0x2000, "MSG_RCVMORE": 0x4000,
		"IP_PKTINFO": 26, "IP_RECVTTL": 24, "IP_RECVTOS": 27, "IPV6_PKTINFO": 46, "IPV6_NEXTHOP": 48,
		"IPV6_RECVPKTINFO": 61, "IPV6_HOPLIMIT": 47, "SCM_CREDS": 3, "SCM_TIMESTAMP": 2,
		"LOCAL_PEERCRED": 0x001, "SO_USELOOPBACK": syscall.SO_USELOOPBACK, "SO_TIMESTAMP": syscall.SO_TIMESTAMP,
		"AI_ALL": 0x100, "AI_V4MAPPED_CFG": 0x200, "AI_ADDRCONFIG": 0x400, "AI_V4MAPPED": 0x800, "AI_DEFAULT": 0x600,
		"AI_MASK": 0x1407, "AI_NUMERICSERV": 0x1000,
		"EAI_ADDRFAMILY": 1, "EAI_AGAIN": 2, "EAI_BADFLAGS": 3, "EAI_FAIL": 4, "EAI_FAMILY": 5, "EAI_MEMORY": 6,
		"EAI_NODATA": 7, "EAI_NONAME": 8, "EAI_SERVICE": 9, "EAI_SOCKTYPE": 10, "EAI_SYSTEM": 11, "EAI_BADHINTS": 12,
		"EAI_PROTOCOL": 13, "EAI_OVERFLOW": 14, "EAI_MAX": 15,
		"NI_MAXHOST": 1025, "NI_MAXSERV": 32, "NI_NOFQDN": 0x1, "NI_NUMERICHOST": 0x2, "NI_NAMEREQD": 0x4,
		"NI_NUMERICSERV": 0x8, "NI_DGRAM": 0x10,
		"IP_DEFAULT_MULTICAST_LOOP": 1, "IP_DEFAULT_MULTICAST_TTL": 1, "IP_MAX_MEMBERSHIPS": 4095,
		"IPV6_JOIN_GROUP": syscall.IPV6_JOIN_GROUP, "IPV6_LEAVE_GROUP": syscall.IPV6_LEAVE_GROUP,
	}
}
