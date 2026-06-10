package room

import "time"

const (
	StatusOpen   = "open"
	StatusClosed = "closed"

	MemberRoleOwner  = "owner"
	MemberRoleMember = "member"

	FreeMaxMembers    = 4
	DefaultMaxMembers = FreeMaxMembers
)

type Room struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	OwnerUserID int64  `json:"ownerUserID"`
	NodeID      int64  `json:"nodeID"`
	// RoomPublicKey is the invite credential players use to join the room.
	// It is also indexed in Redis; deleting/closing the room removes that Redis
	// key so stale invites stop working immediately.
	RoomPublicKey    string    `json:"roomPublicKey"`
	NetworkCIDR      string    `json:"networkCIDR"`
	MaxMembers       int       `json:"maxMembers"`
	VoiceMaxMembers  int       `json:"voiceMaxMembers"`
	VoiceMaxSpeakers int       `json:"voiceMaxSpeakers"`
	MemberCount      int       `json:"memberCount"`
	Status           string    `json:"status"`
	NodeName         string    `json:"nodeName,omitempty"`
	NodeOverlayAddr  string    `json:"nodeOverlayAddr,omitempty"`
	NodeUDPAddr      string    `json:"nodeUDPAddr,omitempty"`
	NodePublicKey    string    `json:"nodePublicKey,omitempty"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
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
	Offset int
}

type TransportDiagnostic struct {
	VirtualIP           string   `json:"virtualIP"`
	CandidateCount      int      `json:"candidateCount"`
	CandidateTypes      []string `json:"candidateTypes"`
	SelectedType        string   `json:"selectedType"`
	SelectedPath        string   `json:"selectedPath"`
	LastProbeAt         string   `json:"lastProbeAt"`
	LastSuccessAt       string   `json:"lastSuccessAt"`
	LastFailureAt       string   `json:"lastFailureAt"`
	LastRTTMS           int64    `json:"lastRTTMS"`
	FailedProbes        int      `json:"failedProbes"`
	ConsecutiveFailures int      `json:"consecutiveFailures"`
	FailureReason       string   `json:"failureReason"`
	DisabledReason      string   `json:"disabledReason"`
	LastObservedPeerAt  string   `json:"lastObservedPeerAt"`
}

type NetworkDiagnosticReport struct {
	RoomID               int64                 `json:"roomID"`
	UserID               int64                 `json:"userID"`
	Status               string                `json:"status"`
	Message              string                `json:"message"`
	IP                   string                `json:"ip"`
	TransportMode        string                `json:"transportMode"`
	DirectPeerIPs        []string              `json:"directPeerIPs"`
	TransportDiagnostics []TransportDiagnostic `json:"transportDiagnostics"`
	UpdatedAt            time.Time             `json:"updatedAt"`
}
