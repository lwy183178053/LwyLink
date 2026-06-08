package overlay

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
)

// FrameType is the lightweight overlay protocol carried inside a tcpx encrypted
// message. tcpx already provides length framing and encryption; this byte only
// identifies LwyLink semantics.
type FrameType byte

const (
	FrameAuth       FrameType = 1
	FrameDHCP       FrameType = 2
	FrameIP         FrameType = 3
	FrameError      FrameType = 4
	FramePing       FrameType = 5
	FrameUDPControl FrameType = 6
	FrameVoice      FrameType = 7
)

// AuthRequest is the first overlay frame a client must send after tcpx is ready.
// RequestID is echoed in the DHCP response so clients can reject stale replies
// from an older reconnect attempt.
type AuthRequest struct {
	Token         string `json:"token"`
	RoomID        int64  `json:"roomID"`
	RoomPublicKey string `json:"roomPublicKey"`
	RequestID     string `json:"requestID"`
}

// DHCPResponse is the virtual DHCP lease assigned by the server after token,
// room membership, and room public key checks all pass.
type DHCPResponse struct {
	RequestID    string   `json:"requestID"`
	IP           string   `json:"ip"`
	SubnetMask   string   `json:"subnetMask"`
	Gateway      string   `json:"gateway"`
	DNS          []string `json:"dns"`
	MTU          int      `json:"mtu"`
	LeaseSeconds int64    `json:"leaseSeconds"`
}

const (
	UDPControlConfig = "config"
	UDPControlPeer   = "peer"
)

type UDPControl struct {
	Type  string            `json:"type"`
	Token string            `json:"token,omitempty"`
	Key   string            `json:"key,omitempty"`
	Peer  *UDPPeerCandidate `json:"peer,omitempty"`
}

type UDPPeerCandidate struct {
	VirtualIP    string   `json:"virtualIP"`
	Endpoint     string   `json:"endpoint"`
	LANEndpoints []string `json:"lanEndpoints,omitempty"`
	SessionID    string   `json:"sessionID"`
	Key          string   `json:"key"`
}

type UDPRegisterPayload struct {
	RoomID       int64    `json:"roomID"`
	UserID       int64    `json:"userID"`
	VirtualIP    string   `json:"virtualIP"`
	LANEndpoints []string `json:"lanEndpoints,omitempty"`
}

type VoicePayload struct {
	RoomID      int64  `json:"roomID"`
	FromUserID  int64  `json:"fromUserID"`
	ToUserID    int64  `json:"toUserID"`
	Seq         uint64 `json:"seq"`
	TimestampMS int64  `json:"timestampMS"`
	Codec       string `json:"codec"`
	SampleRate  int    `json:"sampleRate"`
	Payload     []byte `json:"payload"`
}

// PackFrame wraps an overlay payload for transport through tcpx.SecureConn.
// It does not include a length header because tcpx already frames encrypted
// messages.
func PackFrame(typ FrameType, payload []byte) []byte {
	out := make([]byte, 1+len(payload))
	out[0] = byte(typ)
	copy(out[1:], payload)
	return out
}

// UnpackFrame validates the overlay type byte and returns the remaining payload.
func UnpackFrame(raw []byte) (FrameType, []byte, error) {
	if len(raw) < 1 {
		return 0, nil, fmt.Errorf("empty overlay frame")
	}
	typ := FrameType(raw[0])
	switch typ {
	case FrameAuth, FrameDHCP, FrameIP, FrameError, FramePing, FrameUDPControl, FrameVoice:
		return typ, raw[1:], nil
	default:
		return 0, nil, fmt.Errorf("unknown overlay frame: %d", raw[0])
	}
}

// PackJSON is a convenience for control frames such as auth, DHCP, and errors.
func PackJSON(typ FrameType, v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return PackFrame(typ, b), nil
}

const (
	ipPayloadVersion   byte = 1
	ipPayloadHeaderLen      = 16
)

var ipPayloadMagic = [4]byte{'L', 'W', 'Y', 'I'}

func PackIPPayload(packetID uint64, pkt []byte) []byte {
	out := make([]byte, ipPayloadHeaderLen+len(pkt))
	copy(out[0:4], ipPayloadMagic[:])
	out[4] = ipPayloadVersion
	binary.BigEndian.PutUint64(out[8:16], packetID)
	copy(out[ipPayloadHeaderLen:], pkt)
	return out
}

func UnpackIPPayload(payload []byte) (packetID uint64, pkt []byte, err error) {
	if len(payload) < ipPayloadHeaderLen {
		return 0, nil, fmt.Errorf("short ip payload")
	}
	if string(payload[0:4]) != string(ipPayloadMagic[:]) {
		return 0, nil, fmt.Errorf("bad ip payload")
	}
	if payload[4] != ipPayloadVersion {
		return 0, nil, fmt.Errorf("bad ip payload version")
	}
	return binary.BigEndian.Uint64(payload[8:16]), payload[ipPayloadHeaderLen:], nil
}

func IPv4SrcDst(pkt []byte) (src, dst string, err error) {
	if len(pkt) < 20 {
		return "", "", fmt.Errorf("short ipv4 packet")
	}
	if pkt[0]>>4 != 4 {
		return "", "", fmt.Errorf("not ipv4")
	}
	ihl := int(pkt[0]&0x0f) * 4
	if ihl < 20 || len(pkt) < ihl {
		return "", "", fmt.Errorf("bad ipv4 header length")
	}
	srcIP := net.IPv4(pkt[12], pkt[13], pkt[14], pkt[15])
	dstIP := net.IPv4(pkt[16], pkt[17], pkt[18], pkt[19])
	return srcIP.String(), dstIP.String(), nil
}

func isLimitedBroadcast(ip string) bool {
	parsed := net.ParseIP(ip).To4()
	if parsed == nil {
		return false
	}
	return parsed.Equal(net.IPv4bcast)
}

func isIPv4Multicast(ip string) bool {
	parsed := net.ParseIP(ip).To4()
	if parsed == nil {
		return false
	}
	return parsed[0] >= 224 && parsed[0] <= 239
}

func isSubnetBroadcast(dst, localIP, subnetMask string) bool {
	dst4 := net.ParseIP(dst).To4()
	local4 := net.ParseIP(localIP).To4()
	mask4 := net.ParseIP(subnetMask).To4()
	if dst4 == nil || local4 == nil || mask4 == nil {
		return false
	}
	dstNum := ipv4ToUint32(dst4)
	localNum := ipv4ToUint32(local4)
	maskNum := ipv4ToUint32(mask4)
	broadcast := (localNum & maskNum) | ^maskNum
	return dstNum == broadcast
}

func isRoomFanoutDestination(dst, localIP, subnetMask string) bool {
	return isLimitedBroadcast(dst) || isIPv4Multicast(dst) || isSubnetBroadcast(dst, localIP, subnetMask)
}

func ipv4ToUint32(ip net.IP) uint32 {
	v := ip.To4()
	return uint32(v[0])<<24 | uint32(v[1])<<16 | uint32(v[2])<<8 | uint32(v[3])
}
