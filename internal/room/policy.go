package room

import (
	"fmt"
	"slices"
	"strings"

	"github.com/LwyV-Labs/LwyLink/internal/account"
)

const (
	adminRole = "admin"
)

var allRoomMemberOptions = []int{2, 4, 8, 16, 32, 64}

type Capacity struct {
	Tier             string
	Label            string
	MaxMembers       int
	VoiceMaxMembers  int
	VoiceMaxSpeakers int
}

func CapacityForUser(role, membership string) Capacity {
	if strings.EqualFold(strings.TrimSpace(role), adminRole) {
		return Capacity{
			Tier:             "admin",
			Label:            "管理员",
			MaxMembers:       64,
			VoiceMaxMembers:  32,
			VoiceMaxSpeakers: 8,
		}
	}
	if strings.EqualFold(strings.TrimSpace(membership), account.MembershipVIP) {
		return Capacity{
			Tier:             account.MembershipVIP,
			Label:            "会员",
			MaxMembers:       16,
			VoiceMaxMembers:  16,
			VoiceMaxSpeakers: 8,
		}
	}
	return Capacity{
		Tier:             account.MembershipFree,
		Label:            "普通用户",
		MaxMembers:       4,
		VoiceMaxMembers:  4,
		VoiceMaxSpeakers: 2,
	}
}

func (c Capacity) CanCreateRoom(maxMembers int) bool {
	return slices.Contains(allRoomMemberOptions, maxMembers) && maxMembers <= c.MaxMembers
}

func (c Capacity) CanOwnRoom(maxMembers int) bool {
	if maxMembers <= 0 {
		maxMembers = DefaultMaxMembers
	}
	return maxMembers <= c.MaxMembers
}

func (c Capacity) allowedOptionsText() string {
	parts := make([]string, 0, len(allRoomMemberOptions))
	for _, option := range allRoomMemberOptions {
		if option <= c.MaxMembers {
			parts = append(parts, fmt.Sprintf("%d", option))
		}
	}
	return strings.Join(parts, "、")
}

func (c Capacity) AllowedOptionsText() string {
	return c.allowedOptionsText()
}

func (c Capacity) createRoomError() error {
	return fmt.Errorf("%s可创建房间人数为 %s", c.Label, c.allowedOptionsText())
}

func allowedMaxMembersText() string {
	parts := make([]string, 0, len(allRoomMemberOptions))
	for _, option := range allRoomMemberOptions {
		parts = append(parts, fmt.Sprintf("%d", option))
	}
	return strings.Join(parts, "、")
}

func applyOwnerCapacity(r *Room, ownerRole, ownerMembership string) {
	capacity := CapacityForUser(ownerRole, ownerMembership)
	roomMembers := r.MaxMembers
	if roomMembers <= 0 {
		roomMembers = DefaultMaxMembers
	}
	r.VoiceMaxMembers = min(roomMembers, capacity.VoiceMaxMembers)
	r.VoiceMaxSpeakers = min(r.VoiceMaxMembers, capacity.VoiceMaxSpeakers)
}
