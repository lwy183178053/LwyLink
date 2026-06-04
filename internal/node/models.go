package node

import "time"

const (
	StatusUnknown  = "unknown"
	StatusOnline   = "online"
	StatusPaused   = "paused"
	StatusDisabled = "disabled"
)

type Node struct {
	ID                int64      `json:"id"`
	Name              string     `json:"name"`
	PublicOverlayAddr string     `json:"publicOverlayAddr"`
	HealthAddr        string     `json:"healthAddr"`
	OverlayPublicKey  string     `json:"overlayPublicKey"`
	MaxRooms          int        `json:"maxRooms"`
	CurrentRooms      int        `json:"currentRooms"`
	Enabled           bool       `json:"enabled"`
	Status            string     `json:"status"`
	LastSeenAt        *time.Time `json:"lastSeenAt,omitempty"`
	LastError         string     `json:"lastError,omitempty"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}
