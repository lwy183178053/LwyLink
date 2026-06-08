package room

import (
	"context"
	"time"
)

const (
	RoomEventConnected        = "connected"
	RoomEventCreated          = "room_created"
	RoomEventMemberJoined     = "member_joined"
	RoomEventMemberLeft       = "member_left"
	RoomEventMemberKicked     = "member_kicked"
	RoomEventOwnerTransferred = "owner_transferred"
	RoomEventDeleted          = "room_deleted"
	RoomEventClosed           = "room_closed"
	RoomEventReopened         = "room_reopened"
	RoomEventPresenceChanged  = "presence_changed"
	RoomEventChatMessage      = "chat_message"
	RoomEventAccountUpdated   = "account_updated"
	RoomEventVoiceState       = "voice_state"
)

type ChatMessage struct {
	ID          string    `json:"id"`
	RoomID      int64     `json:"roomID"`
	UserID      int64     `json:"userID"`
	Username    string    `json:"username"`
	DisplayName string    `json:"displayName"`
	Message     string    `json:"message"`
	CreatedAt   time.Time `json:"createdAt"`
}

type RoomEvent struct {
	Type         string       `json:"type"`
	RoomID       int64        `json:"roomID"`
	ActorUserID  int64        `json:"actorUserID,omitempty"`
	TargetUserID int64        `json:"targetUserID,omitempty"`
	UserIDs      []int64      `json:"userIDs,omitempty"`
	Chat         *ChatMessage `json:"chat,omitempty"`
	Voice        *VoiceState  `json:"voice,omitempty"`
	CreatedAt    time.Time    `json:"createdAt"`
}

type VoiceParticipant struct {
	RoomID      int64     `json:"roomID"`
	UserID      int64     `json:"userID"`
	Username    string    `json:"username"`
	DisplayName string    `json:"displayName"`
	Speaking    bool      `json:"speaking"`
	Muted       bool      `json:"muted"`
	JoinedAt    time.Time `json:"joinedAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type VoiceState struct {
	RoomID           int64              `json:"roomID"`
	MaxMembers       int                `json:"maxMembers"`
	MaxSpeakers      int                `json:"maxSpeakers"`
	ParticipantCount int                `json:"participantCount"`
	SpeakerCount     int                `json:"speakerCount"`
	Participants     []VoiceParticipant `json:"participants"`
	UpdatedAt        time.Time          `json:"updatedAt"`
}

type EventBus interface {
	PublishRoomEvent(ctx context.Context, event RoomEvent) error
	SubscribeUserRoomEvents(ctx context.Context, userID int64) (<-chan RoomEvent, func(), error)
	SubscribeAllRoomEvents(ctx context.Context) (<-chan RoomEvent, func(), error)
}
