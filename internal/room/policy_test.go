package room

import (
	"testing"

	"github.com/LwyV-Labs/LwyLink/internal/account"
)

func TestCapacityForUser(t *testing.T) {
	tests := []struct {
		name        string
		role        string
		membership  string
		maxMembers  int
		voiceJoin   int
		voiceSpeaks int
	}{
		{name: "free player", role: "player", membership: account.MembershipFree, maxMembers: 4, voiceJoin: 4, voiceSpeaks: 2},
		{name: "vip player", role: "player", membership: account.MembershipVIP, maxMembers: 16, voiceJoin: 16, voiceSpeaks: 8},
		{name: "admin overrides free membership", role: "admin", membership: account.MembershipFree, maxMembers: 64, voiceJoin: 32, voiceSpeaks: 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CapacityForUser(tt.role, tt.membership)
			if got.MaxMembers != tt.maxMembers || got.VoiceMaxMembers != tt.voiceJoin || got.VoiceMaxSpeakers != tt.voiceSpeaks {
				t.Fatalf("capacity=%+v", got)
			}
		})
	}
}

func TestCapacityCanCreateRoom(t *testing.T) {
	free := CapacityForUser("player", account.MembershipFree)
	for _, size := range []int{2, 4} {
		if !free.CanCreateRoom(size) {
			t.Fatalf("free user should create %d member room", size)
		}
	}
	for _, size := range []int{8, 16, 32, 64} {
		if free.CanCreateRoom(size) {
			t.Fatalf("free user should not create %d member room", size)
		}
	}

	vip := CapacityForUser("player", account.MembershipVIP)
	for _, size := range []int{2, 4, 8, 16} {
		if !vip.CanCreateRoom(size) {
			t.Fatalf("vip user should create %d member room", size)
		}
	}
	for _, size := range []int{32, 64} {
		if vip.CanCreateRoom(size) {
			t.Fatalf("vip user should not create %d member room", size)
		}
	}
	if vip.CanCreateRoom(6) {
		t.Fatalf("vip user should not create unsupported size")
	}
}

func TestCapacityCanOwnRoomForTransfer(t *testing.T) {
	free := CapacityForUser("player", account.MembershipFree)
	if free.CanOwnRoom(16) {
		t.Fatalf("free user should not own transferred 16 member room")
	}

	vip := CapacityForUser("player", account.MembershipVIP)
	if !vip.CanOwnRoom(4) || !vip.CanOwnRoom(16) {
		t.Fatalf("vip user should own lower and vip rooms")
	}
	if vip.CanOwnRoom(32) {
		t.Fatalf("vip user should not own admin room")
	}
}

func TestApplyOwnerCapacityCapsVoiceByRoomSize(t *testing.T) {
	r := Room{MaxMembers: 4}
	applyOwnerCapacity(&r, "player", account.MembershipVIP)
	if r.VoiceMaxMembers != 4 || r.VoiceMaxSpeakers != 4 {
		t.Fatalf("voice caps=%d/%d, want 4/4", r.VoiceMaxMembers, r.VoiceMaxSpeakers)
	}

	r = Room{MaxMembers: 64}
	applyOwnerCapacity(&r, "admin", account.MembershipFree)
	if r.VoiceMaxMembers != 32 || r.VoiceMaxSpeakers != 8 {
		t.Fatalf("voice caps=%d/%d, want 32/8", r.VoiceMaxMembers, r.VoiceMaxSpeakers)
	}
}
