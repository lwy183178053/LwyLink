package netutil

import "net"

func IPToUint32(ip net.IP) uint32 {
	v := ip.To4()
	return uint32(v[0])<<24 | uint32(v[1])<<16 | uint32(v[2])<<8 | uint32(v[3])
}

func Uint32ToIP(v uint32) net.IP {
	return net.IPv4(byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

func PrefixToMask(prefix int) string {
	mask := net.CIDRMask(prefix, 32)
	return net.IPv4(mask[0], mask[1], mask[2], mask[3]).String()
}

func SubnetBroadcast(ip, mask net.IP) net.IP {
	ip4 := ip.To4()
	mask4 := mask.To4()
	if ip4 == nil || mask4 == nil {
		return nil
	}
	return net.IPv4(
		ip4[0]|^mask4[0],
		ip4[1]|^mask4[1],
		ip4[2]|^mask4[2],
		ip4[3]|^mask4[3],
	)
}
