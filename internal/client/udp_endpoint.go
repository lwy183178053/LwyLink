package client

import (
	"fmt"
	"net"

	"github.com/LwyV-Labs/LwyLink/internal/overlay"
)

func (m *udpManager) localLANEndpoints() []string {
	local, ok := m.conn.LocalAddr().(*net.UDPAddr)
	if !ok || local.Port <= 0 {
		return nil
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	out := make([]string, 0, 4)
	seen := make(map[string]struct{})
	for _, iface := range ifaces {
		if len(out) >= overlay.MaxUDPLANEndpoints {
			break
		}
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if len(out) >= overlay.MaxUDPLANEndpoints {
				break
			}
			ip := addrIP(addr).To4()
			if !overlay.IsLANIPv4(ip) {
				continue
			}
			if ip.String() == m.cfg.VirtualIP {
				continue
			}
			endpoint := net.JoinHostPort(ip.String(), fmt.Sprintf("%d", local.Port))
			if _, ok := seen[endpoint]; ok {
				continue
			}
			seen[endpoint] = struct{}{}
			out = append(out, endpoint)
		}
	}
	return overlay.NormalizeUDPLANEndpoints(out)
}

const (
	udpCandidateTypeLAN      = "lan"
	udpCandidateTypeObserved = "observed"
)

func resolvePeerCandidateEndpoints(candidate overlay.UDPPeerCandidate) []udpCandidateEndpoint {
	out := make([]udpCandidateEndpoint, 0, len(candidate.LANEndpoints)+1)
	seen := make(map[string]struct{}, len(candidate.LANEndpoints)+1)
	for _, value := range overlay.NormalizeUDPLANEndpoints(candidate.LANEndpoints) {
		addr, err := net.ResolveUDPAddr("udp", value)
		if err != nil {
			continue
		}
		if addr.IP.String() == candidate.VirtualIP {
			continue
		}
		if _, ok := seen[addr.String()]; ok {
			continue
		}
		seen[addr.String()] = struct{}{}
		out = append(out, udpCandidateEndpoint{Addr: addr, Type: udpCandidateTypeLAN})
	}
	if candidate.Endpoint != "" {
		addr, err := net.ResolveUDPAddr("udp", candidate.Endpoint)
		if err == nil {
			if _, ok := seen[addr.String()]; !ok {
				out = append(out, udpCandidateEndpoint{Addr: addr, Type: udpCandidateTypeObserved})
			}
		}
	}
	return out
}

func cloneUDPAddr(addr *net.UDPAddr) *net.UDPAddr {
	if addr == nil {
		return nil
	}
	cp := *addr
	return &cp
}

func cloneUDPCandidateEndpoints(endpoints []udpCandidateEndpoint) []udpCandidateEndpoint {
	if len(endpoints) == 0 {
		return nil
	}
	out := make([]udpCandidateEndpoint, 0, len(endpoints))
	for _, endpoint := range endpoints {
		if endpoint.Addr != nil {
			out = append(out, udpCandidateEndpoint{Addr: cloneUDPAddr(endpoint.Addr), Type: endpoint.Type})
		}
	}
	return out
}

func cloneUDPAddrs(addrs []*net.UDPAddr) []*net.UDPAddr {
	if len(addrs) == 0 {
		return nil
	}
	out := make([]*net.UDPAddr, 0, len(addrs))
	for _, addr := range addrs {
		if addr != nil {
			out = append(out, cloneUDPAddr(addr))
		}
	}
	return out
}

func udpCandidateSlicesEqual(left, right []udpCandidateEndpoint) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if udpAddrString(left[i].Addr) != udpAddrString(right[i].Addr) || left[i].Type != right[i].Type {
			return false
		}
	}
	return true
}

func udpCandidateTypeForAddr(endpoints []udpCandidateEndpoint, addr *net.UDPAddr) string {
	if addr == nil {
		return ""
	}
	value := addr.String()
	for _, endpoint := range endpoints {
		if udpAddrString(endpoint.Addr) == value {
			return endpoint.Type
		}
	}
	return ""
}

func udpAddrString(addr *net.UDPAddr) string {
	if addr == nil {
		return ""
	}
	return addr.String()
}

func addrIP(addr net.Addr) net.IP {
	switch v := addr.(type) {
	case *net.IPNet:
		return v.IP
	case *net.IPAddr:
		return v.IP
	default:
		return nil
	}
}
