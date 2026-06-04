package tcpx

import "time"

const (
	DefaultMagic        uint32 = 0x54435058 // "TCPX"
	DefaultVersion      uint16 = 1
	DefaultMaxPayload          = 4 << 20 // 4 MiB
	DefaultReplayWindow        = 64
)

type BaseConfig struct {
	Magic        uint32
	Version      uint16
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	MaxPayload   uint32

	// Heartbeats keep NAT mappings warm and let the client detect broken links
	// quickly enough to reconnect during a game session.
	HeartbeatBase   time.Duration
	HeartbeatJitter time.Duration

	// Key rotation limits how much data is protected by one AES-GCM key. The old
	// receive key is kept briefly so in-flight packets survive the rotation.
	KeyRotateInterval time.Duration
	OldKeyGrace       time.Duration
	// ReplayWindow rejects duplicated or very old encrypted sequence numbers.
	ReplayWindow uint64
}

func (c *BaseConfig) setDefaults() {
	if c.Magic == 0 {
		c.Magic = DefaultMagic
	}
	if c.Version == 0 {
		c.Version = DefaultVersion
	}
	if c.MaxPayload == 0 {
		c.MaxPayload = DefaultMaxPayload
	}
	if c.ReadTimeout == 0 {
		c.ReadTimeout = 45 * time.Second
	}
	if c.WriteTimeout == 0 {
		c.WriteTimeout = 10 * time.Second
	}
	if c.HeartbeatBase == 0 {
		c.HeartbeatBase = 15 * time.Second
	}
	if c.HeartbeatJitter == 0 {
		c.HeartbeatJitter = 5 * time.Second
	}
	if c.KeyRotateInterval == 0 {
		c.KeyRotateInterval = 10 * time.Minute
	}
	if c.OldKeyGrace == 0 {
		c.OldKeyGrace = 2 * time.Minute
	}
	if c.ReplayWindow == 0 {
		c.ReplayWindow = DefaultReplayWindow
	}
}
