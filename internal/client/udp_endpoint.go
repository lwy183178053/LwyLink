package client

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/LwyV-Labs/LwyLink/internal/overlay"
)

const (
	defaultOverlaySTUNAddr = "stun.lwylink.xyz:3478"
	stunBindingRequest     = 0x0001
	stunBindingSuccess     = 0x0101
	stunMagicCookie        = 0x2112A442
	stunAttrXORMappedAddr  = 0x0020
	stunAttrMappedAddr     = 0x0001
	stunProbeTimeout       = 1200 * time.Millisecond
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

func (m *udpManager) refreshSTUNReflexive() {
	ctx, cancel := context.WithTimeout(m.ctx, stunProbeTimeout)
	defer cancel()
	endpoint, err := discoverSTUNReflexiveEndpoint(ctx, m.conn, m.cfg.STUNAddr)
	if err != nil || endpoint == "" {
		return
	}
	m.mu.Lock()
	m.reflexive = overlay.NormalizeUDPReflexiveEndpoints([]string{endpoint})
	m.mu.Unlock()
}

func (m *udpManager) reflexiveEndpoints() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.reflexive...)
}

const (
	udpCandidateTypeLAN      = "lan"
	udpCandidateTypeSTUN     = "srflx-stun"
	udpCandidateTypeObserved = "observed"
)

func resolvePeerCandidateEndpoints(candidate overlay.UDPPeerCandidate) []udpCandidateEndpoint {
	out := make([]udpCandidateEndpoint, 0, len(candidate.LANEndpoints)+len(candidate.ReflexiveEndpoints)+1)
	seen := make(map[string]struct{}, len(candidate.LANEndpoints)+len(candidate.ReflexiveEndpoints)+1)
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
	for _, value := range overlay.NormalizeUDPReflexiveEndpoints(candidate.ReflexiveEndpoints) {
		addr, err := net.ResolveUDPAddr("udp", value)
		if err != nil {
			continue
		}
		if _, ok := seen[addr.String()]; ok {
			continue
		}
		seen[addr.String()] = struct{}{}
		out = append(out, udpCandidateEndpoint{Addr: addr, Type: udpCandidateTypeSTUN})
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

func discoverSTUNReflexiveEndpoint(ctx context.Context, conn *net.UDPConn, server string) (string, error) {
	if conn == nil {
		return "", fmt.Errorf("missing udp socket")
	}
	server = normalizeSTUNServer(server)
	remote, err := net.ResolveUDPAddr("udp", server)
	if err != nil {
		return "", err
	}
	var txID [12]byte
	if _, err := rand.Read(txID[:]); err != nil {
		return "", err
	}
	request := make([]byte, 20)
	binary.BigEndian.PutUint16(request[0:2], stunBindingRequest)
	binary.BigEndian.PutUint32(request[4:8], stunMagicCookie)
	copy(request[8:20], txID[:])
	if _, err := conn.WriteToUDP(request, remote); err != nil {
		return "", err
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(stunProbeTimeout)
	}
	if err := conn.SetReadDeadline(deadline); err != nil {
		return "", err
	}
	buf := make([]byte, 1500)
	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			return "", err
		}
		endpoint, ok := parseSTUNBindingResponse(buf[:n], txID)
		if ok {
			return endpoint, nil
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
	}
}

func parseSTUNBindingResponse(raw []byte, txID [12]byte) (string, bool) {
	if len(raw) < 20 {
		return "", false
	}
	if binary.BigEndian.Uint16(raw[0:2]) != stunBindingSuccess {
		return "", false
	}
	msgLen := int(binary.BigEndian.Uint16(raw[2:4]))
	if binary.BigEndian.Uint32(raw[4:8]) != stunMagicCookie || len(raw) < 20+msgLen || !equalBytes(raw[8:20], txID[:]) {
		return "", false
	}
	attrs := raw[20 : 20+msgLen]
	for len(attrs) >= 4 {
		typ := binary.BigEndian.Uint16(attrs[0:2])
		size := int(binary.BigEndian.Uint16(attrs[2:4]))
		if len(attrs) < 4+size {
			return "", false
		}
		value := attrs[4 : 4+size]
		if endpoint, ok := parseSTUNAddressAttribute(typ, value); ok {
			return endpoint, true
		}
		padded := (size + 3) &^ 3
		if len(attrs) < 4+padded {
			return "", false
		}
		attrs = attrs[4+padded:]
	}
	return "", false
}

func parseSTUNAddressAttribute(typ uint16, value []byte) (string, bool) {
	if typ != stunAttrXORMappedAddr && typ != stunAttrMappedAddr {
		return "", false
	}
	if len(value) < 8 || value[1] != 0x01 {
		return "", false
	}
	port := binary.BigEndian.Uint16(value[2:4])
	ip4 := []byte{value[4], value[5], value[6], value[7]}
	if typ == stunAttrXORMappedAddr {
		port ^= uint16(stunMagicCookie >> 16)
		cookie := make([]byte, 4)
		binary.BigEndian.PutUint32(cookie, stunMagicCookie)
		for i := 0; i < 4; i++ {
			ip4[i] ^= cookie[i]
		}
	}
	addr := &net.UDPAddr{IP: net.IPv4(ip4[0], ip4[1], ip4[2], ip4[3]), Port: int(port)}
	if !overlay.IsReflexiveUDPAddr(addr) {
		return "", false
	}
	return addr.String(), true
}

func normalizeSTUNServer(server string) string {
	if server == "" {
		return defaultOverlaySTUNAddr
	}
	server = strings.TrimSpace(server)
	server = strings.TrimPrefix(server, "stun:")
	if _, _, err := net.SplitHostPort(server); err == nil {
		return server
	}
	return net.JoinHostPort(server, "19302")
}

func equalBytes(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
