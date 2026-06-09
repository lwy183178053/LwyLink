package overlay

import (
	"net"
	"sort"
)

const MaxUDPLANEndpoints = 8
const MaxUDPReflexiveEndpoints = 4

func NormalizeUDPLANEndpoints(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if len(out) >= MaxUDPLANEndpoints {
			break
		}
		addr, err := net.ResolveUDPAddr("udp", value)
		if err != nil || !IsLANUDPAddr(addr) {
			continue
		}
		normalized := addr.String()
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	sort.Strings(out)
	return out
}

func NormalizeUDPReflexiveEndpoints(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if len(out) >= MaxUDPReflexiveEndpoints {
			break
		}
		addr, err := net.ResolveUDPAddr("udp", value)
		if err != nil || !IsReflexiveUDPAddr(addr) {
			continue
		}
		normalized := addr.String()
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	sort.Strings(out)
	return out
}

func IsLANUDPAddr(addr *net.UDPAddr) bool {
	if addr == nil || addr.Port <= 0 {
		return false
	}
	return IsLANIPv4(addr.IP.To4())
}

func IsReflexiveUDPAddr(addr *net.UDPAddr) bool {
	if addr == nil || addr.Port <= 0 {
		return false
	}
	ip := addr.IP
	if ip == nil || ip.IsUnspecified() || ip.IsLoopback() || ip.IsMulticast() {
		return false
	}
	if ip4 := ip.To4(); ip4 != nil {
		return !IsLANIPv4(ip4)
	}
	return true
}

func IsLANIPv4(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsUnspecified() {
		return false
	}
	return ip[0] == 10 ||
		(ip[0] == 172 && ip[1] >= 16 && ip[1] <= 31) ||
		(ip[0] == 192 && ip[1] == 168) ||
		(ip[0] == 169 && ip[1] == 254)
}
