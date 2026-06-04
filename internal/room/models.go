package room

import "time"

const (
	StatusOpen   = "open"
	StatusClosed = "closed"

	MemberRoleOwner  = "owner"
	MemberRoleMember = "member"

	DefaultMaxMembers = 8
	MinMaxMembers     = 2
	MaxMaxMembers     = 64
)

type Room struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	OwnerUserID int64  `json:"ownerUserID"`
	NodeID      int64  `json:"nodeID"`
	// RoomPublicKey is the invite credential players use to join the room.
	// It is also indexed in Redis; deleting/closing the room removes that Redis
	// key so stale invites stop working immediately.
	RoomPublicKey   string    `json:"roomPublicKey"`
	NetworkCIDR     string    `json:"networkCIDR"`
	MaxMembers      int       `json:"maxMembers"`
	MemberCount     int       `json:"memberCount"`
	Status          string    `json:"status"`
	NodeName        string    `json:"nodeName,omitempty"`
	NodeOverlayAddr string    `json:"nodeOverlayAddr,omitempty"`
	NodePublicKey   string    `json:"nodePublicKey,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type Member struct {
	RoomID    int64     `json:"roomID"`
	UserID    int64     `json:"userID"`
	CreatedAt time.Time `json:"createdAt"`
}

type MemberView struct {
	RoomID      int64     `json:"roomID"`
	UserID      int64     `json:"userID"`
	Username    string    `json:"username"`
	DisplayName string    `json:"displayName"`
	Role        string    `json:"role"`
	Online      bool      `json:"online"`
	VirtualIP   string    `json:"virtualIP,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

type Filter struct {
	Query  string
	Status string
	Limit  int
}
