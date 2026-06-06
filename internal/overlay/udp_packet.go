package overlay

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
)

const (
	UDPTypeRegister byte = 1
	UDPTypeProbe    byte = 2
	UDPTypeProbeAck byte = 3
	UDPTypeIP       byte = 4
	UDPTypePing     byte = 5
	UDPTypePong     byte = 6

	udpPacketVersion byte = 1
	udpHeaderLen     int  = 16
	maxUDPIDLen      int  = 120
)

var udpMagic = [4]byte{'L', 'W', 'Y', 'U'}

type UDPPacket struct {
	Type    byte
	ID      string
	Seq     uint64
	Payload []byte
}

func EncodeUDPJSON(packetType byte, id string, key []byte, seq uint64, value any) ([]byte, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return EncodeUDPPacket(packetType, id, key, seq, payload)
}

func DecodeUDPJSON(raw []byte, key []byte, target any) (UDPPacket, error) {
	packet, err := DecodeUDPPacket(raw, key)
	if err != nil {
		return UDPPacket{}, err
	}
	if err := json.Unmarshal(packet.Payload, target); err != nil {
		return UDPPacket{}, err
	}
	return packet, nil
}

func EncodeUDPPacket(packetType byte, id string, key []byte, seq uint64, payload []byte) ([]byte, error) {
	if len(id) == 0 || len(id) > maxUDPIDLen {
		return nil, fmt.Errorf("bad udp packet id length")
	}
	aead, err := newUDPAEAD(key)
	if err != nil {
		return nil, err
	}
	idBytes := []byte(id)
	header := make([]byte, udpHeaderLen+len(idBytes))
	copy(header[0:4], udpMagic[:])
	header[4] = udpPacketVersion
	header[5] = packetType
	binary.BigEndian.PutUint16(header[6:8], uint16(len(idBytes)))
	binary.BigEndian.PutUint64(header[8:16], seq)
	copy(header[16:], idBytes)
	nonce := udpNonce(seq)
	cipherText := aead.Seal(nil, nonce[:], payload, header)
	return append(header, cipherText...), nil
}

func DecodeUDPPacket(raw []byte, key []byte) (UDPPacket, error) {
	if len(raw) < udpHeaderLen {
		return UDPPacket{}, fmt.Errorf("short udp packet")
	}
	if string(raw[0:4]) != string(udpMagic[:]) {
		return UDPPacket{}, fmt.Errorf("bad udp magic")
	}
	if raw[4] != udpPacketVersion {
		return UDPPacket{}, fmt.Errorf("bad udp version")
	}
	idLen := int(binary.BigEndian.Uint16(raw[6:8]))
	if idLen <= 0 || idLen > maxUDPIDLen || len(raw) < udpHeaderLen+idLen {
		return UDPPacket{}, fmt.Errorf("bad udp id length")
	}
	seq := binary.BigEndian.Uint64(raw[8:16])
	idEnd := udpHeaderLen + idLen
	header := raw[:idEnd]
	cipherText := raw[idEnd:]
	aead, err := newUDPAEAD(key)
	if err != nil {
		return UDPPacket{}, err
	}
	nonce := udpNonce(seq)
	payload, err := aead.Open(nil, nonce[:], cipherText, header)
	if err != nil {
		return UDPPacket{}, err
	}
	return UDPPacket{
		Type:    raw[5],
		ID:      string(raw[16:idEnd]),
		Seq:     seq,
		Payload: payload,
	}, nil
}

func UDPPacketID(raw []byte) (byte, string, error) {
	if len(raw) < udpHeaderLen {
		return 0, "", fmt.Errorf("short udp packet")
	}
	if string(raw[0:4]) != string(udpMagic[:]) || raw[4] != udpPacketVersion {
		return 0, "", fmt.Errorf("bad udp packet")
	}
	idLen := int(binary.BigEndian.Uint16(raw[6:8]))
	if idLen <= 0 || idLen > maxUDPIDLen || len(raw) < udpHeaderLen+idLen {
		return 0, "", fmt.Errorf("bad udp id length")
	}
	return raw[5], string(raw[16 : udpHeaderLen+idLen]), nil
}

func EncodeUDPKey(key []byte) string {
	return base64.RawURLEncoding.EncodeToString(key)
}

func DecodeUDPKey(value string) ([]byte, error) {
	key, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("udp key must be 32 bytes")
	}
	return key, nil
}

func newUDPAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("udp key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func udpNonce(seq uint64) [12]byte {
	var nonce [12]byte
	binary.BigEndian.PutUint64(nonce[4:12], seq)
	return nonce
}
