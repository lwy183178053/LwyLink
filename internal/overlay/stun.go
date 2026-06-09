package overlay

import (
	"encoding/binary"
	"net"
)

const (
	stunBindingRequest    uint16 = 0x0001
	stunBindingSuccess    uint16 = 0x0101
	stunMagicCookie       uint32 = 0x2112A442
	stunAttrXORMappedAddr uint16 = 0x0020
)

func isSTUNBindingRequest(raw []byte) bool {
	return len(raw) >= 20 &&
		binary.BigEndian.Uint16(raw[0:2]) == stunBindingRequest &&
		binary.BigEndian.Uint32(raw[4:8]) == stunMagicCookie
}

func buildSTUNBindingResponse(raw []byte, remote *net.UDPAddr) ([]byte, bool) {
	if !isSTUNBindingRequest(raw) || remote == nil {
		return nil, false
	}
	ip4 := remote.IP.To4()
	if ip4 == nil || remote.Port <= 0 || remote.Port > 65535 {
		return nil, false
	}
	out := make([]byte, 32)
	binary.BigEndian.PutUint16(out[0:2], stunBindingSuccess)
	binary.BigEndian.PutUint16(out[2:4], 12)
	binary.BigEndian.PutUint32(out[4:8], stunMagicCookie)
	copy(out[8:20], raw[8:20])
	binary.BigEndian.PutUint16(out[20:22], stunAttrXORMappedAddr)
	binary.BigEndian.PutUint16(out[22:24], 8)
	out[25] = 0x01
	binary.BigEndian.PutUint16(out[26:28], uint16(remote.Port)^uint16(stunMagicCookie>>16))
	var cookie [4]byte
	binary.BigEndian.PutUint32(cookie[:], stunMagicCookie)
	for i := 0; i < 4; i++ {
		out[28+i] = ip4[i] ^ cookie[i]
	}
	return out, true
}
