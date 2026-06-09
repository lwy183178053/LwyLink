package room

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/LwyV-Labs/LwyLink/internal/netutil"
	"github.com/LwyV-Labs/LwyLink/internal/node"
	"github.com/LwyV-Labs/LwyLink/internal/tcpx"
)

const MaxChatMessageRunes = 500

func NormalizeChatMessage(message string) (string, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return "", fmt.Errorf("聊天内容不能为空")
	}
	if len([]rune(message)) > MaxChatMessageRunes {
		return "", fmt.Errorf("聊天内容不能超过 %d 个字符", MaxChatMessageRunes)
	}
	return message, nil
}

func ChatRelayTrafficBytes(message string, recipients int) int64 {
	if recipients < 1 {
		recipients = 1
	}
	return int64(len([]byte(message))) * int64(recipients)
}

type PublicKeyStore interface {
	PutRoomPublicKey(ctx context.Context, publicKey string, roomID int64) error
	RoomIDByPublicKey(ctx context.Context, publicKey string) (int64, bool, error)
	DeleteRoomPublicKey(ctx context.Context, publicKey string) error
}

type PresenceStore interface {
	SetRoomAppOnline(ctx context.Context, roomID, userID int64)
	RoomHasOnline(ctx context.Context, roomID int64) (bool, error)
	RoomOnlineUsers(ctx context.Context, roomID int64) (map[int64]string, error)
	DeleteRoomUserOnline(ctx context.Context, roomID, userID int64)
	DeleteRoomOnline(ctx context.Context, roomID int64)
}

type RuntimeStore interface {
	PublicKeyStore
	PresenceStore
	EventBus
}

type VoiceStats struct {
	Rooms        int `json:"rooms"`
	Participants int `json:"participants"`
	Speakers     int `json:"speakers"`
}

type NodeScheduler interface {
	SelectForRoom(ctx context.Context) (node.Node, error)
}

type Service struct {
	repo             *Repository
	runtime          RuntimeStore
	nodes            NodeScheduler
	roomNetworkBase  string
	roomSubnetPrefix int
	voiceMu          sync.Mutex
	voiceRooms       map[int64]map[int64]VoiceParticipant
}

func NewService(repo *Repository, runtime RuntimeStore, nodes NodeScheduler, roomNetworkBase string, roomSubnetPrefix int) *Service {
	return &Service{
		repo:             repo,
		runtime:          runtime,
		nodes:            nodes,
		roomNetworkBase:  roomNetworkBase,
		roomSubnetPrefix: roomSubnetPrefix,
		voiceRooms:       make(map[int64]map[int64]VoiceParticipant),
	}
}

func (s *Service) CreateRoom(ctx context.Context, ownerUserID int64, name string, maxMembers int) (Room, error) {
	name = strings.TrimSpace(name)
	if len(name) < 2 || len(name) > 80 {
		return Room{}, fmt.Errorf("room name length must be 2-80")
	}
	maxMembers, err := normalizeMaxMembers(maxMembers)
	if err != nil {
		return Room{}, err
	}
	if err := s.requireRoomCapacity(ctx, ownerUserID, maxMembers); err != nil {
		return Room{}, err
	}
	hasRoom, err := s.repo.UserHasMembership(ctx, ownerUserID)
	if err != nil {
		return Room{}, err
	}
	if hasRoom {
		return Room{}, fmt.Errorf("你已经在一个房间中，请先退出或删除当前房间")
	}
	var selectedNode node.Node
	if s.nodes != nil {
		n, err := s.nodes.SelectForRoom(ctx)
		if err != nil {
			return Room{}, err
		}
		selectedNode = n
	}
	for i := 0; i < 20; i++ {
		joinCode := randomJoinCode()
		_, roomPublicKey, err := tcpx.GenerateStaticKeyBase64()
		if err != nil {
			return Room{}, err
		}
		cidr, err := s.randomRoomCIDR(ctx)
		if err != nil {
			return Room{}, err
		}
		room, err := s.repo.Create(ctx, name, ownerUserID, selectedNode.ID, joinCode, roomPublicKey, cidr, maxMembers)
		if err == nil {
			if err := s.activateRoomKey(ctx, room); err != nil {
				_ = s.repo.Delete(ctx, room.ID)
				return Room{}, err
			}
			s.touchRoomAppOnline(ctx, room.ID, ownerUserID)
			s.publishRoomEvent(ctx, RoomEvent{
				Type:        RoomEventCreated,
				RoomID:      room.ID,
				ActorUserID: ownerUserID,
				UserIDs:     []int64{ownerUserID},
			})
			return room, nil
		}
	}
	return Room{}, fmt.Errorf("failed to create room after retries")
}

func (s *Service) JoinRoom(ctx context.Context, userID int64, roomPublicKey string) (Room, error) {
	roomPublicKey = strings.TrimSpace(roomPublicKey)
	if roomPublicKey == "" {
		return Room{}, fmt.Errorf("room public key is empty")
	}
	roomID, ok, err := s.roomIDByActiveKey(ctx, roomPublicKey)
	if err != nil {
		return Room{}, err
	}
	if !ok {
		return Room{}, fmt.Errorf("room public key is invalid or expired")
	}
	room, err := s.repo.FindByID(ctx, roomID)
	if err != nil {
		return Room{}, err
	}
	if room.Status != StatusOpen {
		return Room{}, fmt.Errorf("room is not open")
	}
	if room.RoomPublicKey != roomPublicKey {
		return Room{}, fmt.Errorf("room public key mismatch")
	}
	current, hasCurrent, err := s.repo.FindForUser(ctx, userID)
	if err != nil {
		return Room{}, err
	}
	if hasCurrent {
		if current.ID == room.ID {
			return current, nil
		}
		return Room{}, fmt.Errorf("你已经在一个房间中，请先退出当前房间")
	}
	maxMembers := room.MaxMembers
	if maxMembers <= 0 {
		maxMembers = DefaultMaxMembers
	}
	ok, err = s.repo.AddMemberWithinLimit(ctx, room.ID, userID, maxMembers)
	if err != nil {
		return Room{}, err
	}
	if !ok {
		return Room{}, fmt.Errorf("room is full or you already joined another room")
	}
	joined, err := s.repo.FindByID(ctx, room.ID)
	if err != nil {
		return Room{}, err
	}
	s.touchRoomAppOnline(ctx, joined.ID, userID)
	s.publishRoomEventToMembers(ctx, RoomEvent{
		Type:         RoomEventMemberJoined,
		RoomID:       joined.ID,
		ActorUserID:  userID,
		TargetUserID: userID,
	})
	return joined, nil
}

func (s *Service) EnsureMemberByRoomKey(ctx context.Context, nodeID, roomID, userID int64, roomPublicKey string) (Room, error) {
	roomPublicKey = strings.TrimSpace(roomPublicKey)
	if roomPublicKey == "" {
		return Room{}, fmt.Errorf("room public key is empty")
	}
	activeRoomID, ok, err := s.roomIDByActiveKey(ctx, roomPublicKey)
	if err != nil {
		return Room{}, err
	}
	if !ok || activeRoomID != roomID {
		return Room{}, fmt.Errorf("room public key is invalid or expired")
	}
	room, err := s.EnsureMember(ctx, roomID, userID)
	if err != nil {
		return Room{}, err
	}
	if nodeID > 0 && room.NodeID > 0 && room.NodeID != nodeID {
		return Room{}, fmt.Errorf("room belongs to another node")
	}
	if room.RoomPublicKey != roomPublicKey {
		return Room{}, fmt.Errorf("room public key mismatch")
	}
	return room, nil
}

func (s *Service) EnsureMember(ctx context.Context, roomID, userID int64) (Room, error) {
	room, err := s.repo.FindByID(ctx, roomID)
	if err != nil {
		return Room{}, err
	}
	if room.Status != StatusOpen {
		return Room{}, fmt.Errorf("room is not open")
	}
	member, err := s.repo.IsMember(ctx, roomID, userID)
	if err != nil {
		return Room{}, err
	}
	if !member {
		return Room{}, fmt.Errorf("user is not a room member")
	}
	return room, nil
}

func (s *Service) ListForUser(ctx context.Context, userID int64) ([]Room, error) {
	rooms, err := s.repo.ListForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if s.runtime != nil {
		for _, item := range rooms {
			s.touchRoomAppOnline(ctx, item.ID, userID)
		}
	}
	return rooms, nil
}

func (s *Service) ListMembers(ctx context.Context, requesterUserID, roomID int64) ([]MemberView, error) {
	if _, err := s.EnsureMember(ctx, roomID, requesterUserID); err != nil {
		return nil, err
	}
	s.touchRoomAppOnline(ctx, roomID, requesterUserID)
	members, err := s.repo.ListMembers(ctx, roomID)
	if err != nil || s.runtime == nil {
		return members, err
	}
	online, err := s.runtime.RoomOnlineUsers(ctx, roomID)
	if err != nil {
		return nil, err
	}
	for i := range members {
		ip, ok := online[members[i].UserID]
		members[i].Online = ok
		members[i].VirtualIP = ip
	}
	return members, nil
}

func (s *Service) SendChat(ctx context.Context, userID, roomID int64, message string) (ChatMessage, error) {
	return s.SendChatWithTrafficCharge(ctx, userID, roomID, message, nil)
}

func (s *Service) SendChatWithTrafficCharge(ctx context.Context, userID, roomID int64, message string, charge func(bytes int64) error) (ChatMessage, error) {
	message, err := NormalizeChatMessage(message)
	if err != nil {
		return ChatMessage{}, err
	}
	if _, err := s.EnsureMember(ctx, roomID, userID); err != nil {
		return ChatMessage{}, err
	}
	members, err := s.repo.ListMembers(ctx, roomID)
	if err != nil {
		return ChatMessage{}, err
	}
	sender, ok := memberByUserID(members, userID)
	if !ok {
		return ChatMessage{}, fmt.Errorf("user is not a room member")
	}
	if charge != nil {
		if err := charge(ChatRelayTrafficBytes(message, len(members))); err != nil {
			return ChatMessage{}, err
		}
	}
	chat := ChatMessage{
		ID:          randomChatID(),
		RoomID:      roomID,
		UserID:      userID,
		Username:    sender.Username,
		DisplayName: sender.DisplayName,
		Message:     message,
		CreatedAt:   time.Now(),
	}
	s.touchRoomAppOnline(ctx, roomID, userID)
	s.publishRoomEventToMembers(ctx, RoomEvent{
		Type:        RoomEventChatMessage,
		RoomID:      roomID,
		ActorUserID: userID,
		Chat:        &chat,
		CreatedAt:   chat.CreatedAt,
	})
	return chat, nil
}

func (s *Service) VoiceState(ctx context.Context, requesterUserID, roomID int64) (VoiceState, error) {
	room, err := s.EnsureMember(ctx, roomID, requesterUserID)
	if err != nil {
		return VoiceState{}, err
	}
	s.touchRoomAppOnline(ctx, roomID, requesterUserID)
	return s.voiceStateForRoom(room), nil
}

func (s *Service) JoinVoice(ctx context.Context, userID, roomID int64) (VoiceState, error) {
	room, err := s.EnsureMember(ctx, roomID, userID)
	if err != nil {
		return VoiceState{}, err
	}
	participant, err := s.voiceParticipantForUser(ctx, roomID, userID)
	if err != nil {
		return VoiceState{}, err
	}

	now := time.Now()
	s.voiceMu.Lock()
	participants := s.voiceRooms[roomID]
	if participants == nil {
		participants = make(map[int64]VoiceParticipant)
		s.voiceRooms[roomID] = participants
	}
	if _, exists := participants[userID]; !exists && len(participants) >= room.VoiceMaxMembers {
		s.voiceMu.Unlock()
		return VoiceState{}, fmt.Errorf("语音房人数已满，最多 %d 人加入语音", room.VoiceMaxMembers)
	}
	if existing, exists := participants[userID]; exists {
		participant.Speaking = existing.Speaking
		participant.Muted = existing.Muted
		participant.JoinedAt = existing.JoinedAt
	} else {
		participant.JoinedAt = now
	}
	participant.UpdatedAt = now
	participants[userID] = participant
	state := s.voiceStateLocked(room)
	s.voiceMu.Unlock()

	s.publishVoiceState(ctx, state, userID)
	return state, nil
}

func (s *Service) LeaveVoice(ctx context.Context, userID, roomID int64) (VoiceState, error) {
	room, err := s.EnsureMember(ctx, roomID, userID)
	if err != nil {
		return VoiceState{}, err
	}
	state, changed := s.removeVoiceParticipant(room, userID)
	if changed {
		s.publishVoiceState(ctx, state, userID)
	}
	return state, nil
}

func (s *Service) SetVoiceSpeaking(ctx context.Context, userID, roomID int64, speaking bool) (VoiceState, error) {
	room, err := s.EnsureMember(ctx, roomID, userID)
	if err != nil {
		return VoiceState{}, err
	}
	s.voiceMu.Lock()
	participants := s.voiceRooms[roomID]
	participant, exists := participants[userID]
	if !exists {
		s.voiceMu.Unlock()
		return VoiceState{}, fmt.Errorf("请先加入语音房")
	}
	if speaking && !participant.Speaking && s.voiceSpeakerCountLocked(roomID) >= room.VoiceMaxSpeakers {
		s.voiceMu.Unlock()
		return VoiceState{}, fmt.Errorf("当前房间最多 %d 人同时开麦", room.VoiceMaxSpeakers)
	}
	participant.Speaking = speaking
	participant.Muted = !speaking
	participant.UpdatedAt = time.Now()
	participants[userID] = participant
	state := s.voiceStateLocked(room)
	s.voiceMu.Unlock()

	s.publishVoiceState(ctx, state, userID)
	return state, nil
}

func (s *Service) SendVoiceSignal(ctx context.Context, fromUserID, roomID, toUserID int64, signalType string, payload []byte) (VoiceSignal, error) {
	if fromUserID <= 0 || toUserID <= 0 || fromUserID == toUserID {
		return VoiceSignal{}, fmt.Errorf("invalid voice signal target")
	}
	signalType = strings.TrimSpace(strings.ToLower(signalType))
	if !validVoiceSignalType(signalType) {
		return VoiceSignal{}, fmt.Errorf("invalid voice signal type")
	}
	if len(payload) == 0 {
		payload = []byte("{}")
	}
	if !json.Valid(payload) {
		return VoiceSignal{}, fmt.Errorf("voice signal payload must be valid JSON")
	}
	if _, err := s.EnsureMember(ctx, roomID, fromUserID); err != nil {
		return VoiceSignal{}, err
	}
	if _, err := s.EnsureMember(ctx, roomID, toUserID); err != nil {
		return VoiceSignal{}, fmt.Errorf("target user is not a room member")
	}
	if !s.voiceParticipantExists(roomID, fromUserID) {
		return VoiceSignal{}, fmt.Errorf("请先加入语音房")
	}
	if !s.voiceParticipantExists(roomID, toUserID) {
		return VoiceSignal{}, fmt.Errorf("target user has not joined voice")
	}
	signal := VoiceSignal{
		RoomID:     roomID,
		FromUserID: fromUserID,
		ToUserID:   toUserID,
		Type:       signalType,
		Payload:    append([]byte(nil), payload...),
	}
	s.publishRoomEvent(ctx, RoomEvent{
		Type:         RoomEventVoiceSignal,
		RoomID:       roomID,
		ActorUserID:  fromUserID,
		TargetUserID: toUserID,
		UserIDs:      []int64{toUserID},
		VoiceSignal:  &signal,
	})
	return signal, nil
}

func (s *Service) Search(ctx context.Context, filter Filter) ([]Room, error) {
	return s.repo.List(ctx, filter)
}

func (s *Service) Stats(ctx context.Context) (Stats, error) {
	return s.repo.Stats(ctx)
}

func (s *Service) VoiceStats() VoiceStats {
	s.voiceMu.Lock()
	defer s.voiceMu.Unlock()
	stats := VoiceStats{}
	for _, participants := range s.voiceRooms {
		if len(participants) == 0 {
			continue
		}
		stats.Rooms++
		stats.Participants += len(participants)
		for _, participant := range participants {
			if participant.Speaking {
				stats.Speakers++
			}
		}
	}
	return stats
}

func (s *Service) SyncOpenRoomKeys(ctx context.Context) error {
	rooms, err := s.repo.ListOpen(ctx)
	if err != nil {
		return err
	}
	for _, room := range rooms {
		if err := s.activateRoomKey(ctx, room); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) StartEmptyRoomCleaner(ctx context.Context, interval, grace time.Duration) {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	if grace <= 0 {
		grace = 20 * time.Second
	}
	runOnce := func() {
		deleted, err := s.CleanupEmptyRooms(ctx, grace)
		if err != nil {
			log.Printf("empty room cleanup failed: %v", err)
			return
		}
		if deleted > 0 {
			log.Printf("empty room cleanup deleted %d room(s)", deleted)
		}
	}
	go func() {
		runOnce()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runOnce()
			}
		}
	}()
}

func (s *Service) CleanupEmptyRooms(ctx context.Context, grace time.Duration) (int, error) {
	if s.runtime == nil {
		return 0, nil
	}
	if grace <= 0 {
		grace = 20 * time.Second
	}
	rooms, err := s.repo.ListOpenOlderThan(ctx, grace)
	if err != nil {
		return 0, err
	}
	deleted := 0
	for _, room := range rooms {
		hasOnline, err := s.runtime.RoomHasOnline(ctx, room.ID)
		if err != nil {
			log.Printf("skip empty room cleanup room=%d: %v", room.ID, err)
			continue
		}
		if hasOnline {
			continue
		}
		if err := s.deleteRoom(ctx, room, 0); err != nil {
			log.Printf("skip empty room cleanup room=%d: %v", room.ID, err)
			continue
		}
		deleted++
	}
	return deleted, nil
}

func (s *Service) CloseRoom(ctx context.Context, roomID int64) error {
	room, err := s.repo.FindByID(ctx, roomID)
	if err != nil {
		return err
	}
	if err := s.deactivateRoomKey(ctx, room); err != nil {
		return err
	}
	if err := s.repo.SetStatus(ctx, roomID, StatusClosed); err != nil {
		return err
	}
	s.publishRoomEventToMembers(ctx, RoomEvent{Type: RoomEventClosed, RoomID: roomID})
	return nil
}

func (s *Service) ReopenRoom(ctx context.Context, roomID int64) error {
	room, err := s.repo.FindByID(ctx, roomID)
	if err != nil {
		return err
	}
	if err := s.repo.SetStatus(ctx, roomID, StatusOpen); err != nil {
		return err
	}
	if err := s.activateRoomKey(ctx, room); err != nil {
		return err
	}
	s.publishRoomEventToMembers(ctx, RoomEvent{Type: RoomEventReopened, RoomID: roomID})
	return nil
}

func (s *Service) DeleteRoom(ctx context.Context, requesterUserID, roomID int64) error {
	room, err := s.repo.FindByID(ctx, roomID)
	if err != nil {
		return err
	}
	if room.OwnerUserID != requesterUserID {
		return fmt.Errorf("only room owner can delete the room")
	}
	return s.deleteRoom(ctx, room, requesterUserID)
}

func (s *Service) LeaveRoom(ctx context.Context, requesterUserID, roomID int64) error {
	room, err := s.EnsureMember(ctx, roomID, requesterUserID)
	if err != nil {
		return err
	}
	if room.OwnerUserID == requesterUserID {
		return fmt.Errorf("房主不能直接退出，请先转让房主或删除房间")
	}
	if err := s.repo.RemoveMember(ctx, roomID, requesterUserID); err != nil {
		return err
	}
	voiceState, voiceChanged := s.removeVoiceParticipant(room, requesterUserID)
	s.deleteRoomUserOnline(ctx, roomID, requesterUserID)
	s.publishRoomEventToMembers(ctx, RoomEvent{
		Type:         RoomEventMemberLeft,
		RoomID:       roomID,
		ActorUserID:  requesterUserID,
		TargetUserID: requesterUserID,
		UserIDs:      []int64{requesterUserID},
	})
	if voiceChanged {
		s.publishVoiceState(ctx, voiceState, requesterUserID)
	}
	return nil
}

func (s *Service) KickMember(ctx context.Context, requesterUserID, roomID, targetUserID int64) error {
	room, err := s.repo.FindByID(ctx, roomID)
	if err != nil {
		return err
	}
	if room.OwnerUserID != requesterUserID {
		return fmt.Errorf("only room owner can kick members")
	}
	if targetUserID == requesterUserID || targetUserID == room.OwnerUserID {
		return fmt.Errorf("房主不能踢出自己")
	}
	member, err := s.repo.IsMember(ctx, roomID, targetUserID)
	if err != nil {
		return err
	}
	if !member {
		return fmt.Errorf("target user is not a room member")
	}
	s.touchRoomAppOnline(ctx, roomID, requesterUserID)
	if err := s.repo.RemoveMember(ctx, roomID, targetUserID); err != nil {
		return err
	}
	voiceState, voiceChanged := s.removeVoiceParticipant(room, targetUserID)
	s.deleteRoomUserOnline(ctx, roomID, targetUserID)
	s.publishRoomEventToMembers(ctx, RoomEvent{
		Type:         RoomEventMemberKicked,
		RoomID:       roomID,
		ActorUserID:  requesterUserID,
		TargetUserID: targetUserID,
		UserIDs:      []int64{targetUserID},
	})
	if voiceChanged {
		s.publishVoiceState(ctx, voiceState, targetUserID)
	}
	return nil
}

func (s *Service) TransferOwner(ctx context.Context, requesterUserID, roomID, targetUserID int64) (Room, error) {
	room, err := s.repo.FindByID(ctx, roomID)
	if err != nil {
		return Room{}, err
	}
	if room.OwnerUserID != requesterUserID {
		return Room{}, fmt.Errorf("only room owner can transfer ownership")
	}
	if targetUserID == requesterUserID {
		return room, nil
	}
	member, err := s.repo.IsMember(ctx, roomID, targetUserID)
	if err != nil {
		return Room{}, err
	}
	if !member {
		return Room{}, fmt.Errorf("target user is not a room member")
	}
	capacity, err := s.repo.UserCapacity(ctx, targetUserID)
	if err != nil {
		return Room{}, err
	}
	if !capacity.CanOwnRoom(room.MaxMembers) {
		return Room{}, fmt.Errorf("目标用户身份最多只能承接 %d 人房间，当前房间为 %d 人", capacity.MaxMembers, room.MaxMembers)
	}
	s.touchRoomAppOnline(ctx, roomID, requesterUserID)
	s.touchRoomAppOnline(ctx, roomID, targetUserID)
	if err := s.repo.SetOwner(ctx, roomID, targetUserID); err != nil {
		return Room{}, err
	}
	updated, err := s.repo.FindByID(ctx, roomID)
	if err != nil {
		return Room{}, err
	}
	s.publishRoomEventToMembers(ctx, RoomEvent{
		Type:         RoomEventOwnerTransferred,
		RoomID:       roomID,
		ActorUserID:  requesterUserID,
		TargetUserID: targetUserID,
	})
	s.publishVoiceState(ctx, s.voiceStateForRoom(updated), targetUserID)
	return updated, nil
}

func (s *Service) DeleteRoomsOwnedBy(ctx context.Context, ownerUserID int64) error {
	rooms, err := s.repo.ListOwnedByOwner(ctx, ownerUserID)
	if err != nil {
		return err
	}
	for _, room := range rooms {
		if err := s.deleteRoom(ctx, room, ownerUserID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) deleteRoom(ctx context.Context, room Room, actorUserID int64) error {
	userIDs := s.roomUserIDs(ctx, room.ID)
	if err := s.deactivateRoomKey(ctx, room); err != nil {
		return err
	}
	if err := s.repo.Delete(ctx, room.ID); err != nil {
		return err
	}
	s.clearVoiceRoom(room.ID)
	if s.runtime != nil {
		s.runtime.DeleteRoomOnline(ctx, room.ID)
	}
	s.publishRoomEvent(ctx, RoomEvent{
		Type:        RoomEventDeleted,
		RoomID:      room.ID,
		ActorUserID: actorUserID,
		UserIDs:     userIDs,
	})
	return nil
}

func (s *Service) activateRoomKey(ctx context.Context, room Room) error {
	if s.runtime == nil {
		return fmt.Errorf("room public key store is not configured")
	}
	return s.runtime.PutRoomPublicKey(ctx, room.RoomPublicKey, room.ID)
}

func (s *Service) deactivateRoomKey(ctx context.Context, room Room) error {
	if s.runtime == nil {
		return fmt.Errorf("room public key store is not configured")
	}
	return s.runtime.DeleteRoomPublicKey(ctx, room.RoomPublicKey)
}

func (s *Service) roomIDByActiveKey(ctx context.Context, publicKey string) (int64, bool, error) {
	if s.runtime == nil {
		return 0, false, fmt.Errorf("room public key store is not configured")
	}
	return s.runtime.RoomIDByPublicKey(ctx, publicKey)
}

func (s *Service) touchRoomAppOnline(ctx context.Context, roomID, userID int64) {
	if s.runtime == nil || roomID <= 0 || userID <= 0 {
		return
	}
	s.runtime.SetRoomAppOnline(ctx, roomID, userID)
}

func (s *Service) deleteRoomUserOnline(ctx context.Context, roomID, userID int64) {
	if s.runtime == nil || roomID <= 0 || userID <= 0 {
		return
	}
	s.runtime.DeleteRoomUserOnline(ctx, roomID, userID)
}

func (s *Service) TouchForUser(ctx context.Context, userID int64) error {
	rooms, err := s.repo.ListForUser(ctx, userID)
	if err != nil {
		return err
	}
	for _, item := range rooms {
		s.touchRoomAppOnline(ctx, item.ID, userID)
	}
	return nil
}

func (s *Service) NotifyPresenceChanged(ctx context.Context, roomID, userID int64) {
	s.publishRoomEventToMembers(ctx, RoomEvent{
		Type:         RoomEventPresenceChanged,
		RoomID:       roomID,
		ActorUserID:  userID,
		TargetUserID: userID,
	})
}

func (s *Service) SubscribeUserEvents(ctx context.Context, userID int64) (<-chan RoomEvent, func(), error) {
	if s.runtime == nil {
		return nil, nil, fmt.Errorf("room event bus is not configured")
	}
	return s.runtime.SubscribeUserRoomEvents(ctx, userID)
}

func (s *Service) SubscribeAllEvents(ctx context.Context) (<-chan RoomEvent, func(), error) {
	if s.runtime == nil {
		return nil, nil, fmt.Errorf("room event bus is not configured")
	}
	return s.runtime.SubscribeAllRoomEvents(ctx)
}

func (s *Service) NotifyUserUpdated(ctx context.Context, userID int64) {
	s.publishRoomEvent(ctx, RoomEvent{
		Type:         RoomEventAccountUpdated,
		TargetUserID: userID,
		UserIDs:      []int64{userID},
	})
}

func (s *Service) publishRoomEventToMembers(ctx context.Context, event RoomEvent) {
	event.UserIDs = append(event.UserIDs, s.roomUserIDs(ctx, event.RoomID)...)
	s.publishRoomEvent(ctx, event)
}

func (s *Service) publishRoomEvent(ctx context.Context, event RoomEvent) {
	event.UserIDs = uniquePositiveUserIDs(event.UserIDs)
	if s.runtime == nil || event.Type == "" || (event.RoomID <= 0 && len(event.UserIDs) == 0) {
		return
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}
	if err := s.runtime.PublishRoomEvent(ctx, event); err != nil {
		log.Printf("publish room event failed type=%s room=%d: %v", event.Type, event.RoomID, err)
	}
}

func (s *Service) roomUserIDs(ctx context.Context, roomID int64) []int64 {
	members, err := s.repo.ListMembers(ctx, roomID)
	if err != nil {
		return nil
	}
	ids := make([]int64, 0, len(members))
	for _, member := range members {
		ids = append(ids, member.UserID)
	}
	return ids
}

func memberByUserID(members []MemberView, userID int64) (MemberView, bool) {
	for _, member := range members {
		if member.UserID == userID {
			return member, true
		}
	}
	return MemberView{}, false
}

func (s *Service) voiceParticipantForUser(ctx context.Context, roomID, userID int64) (VoiceParticipant, error) {
	members, err := s.repo.ListMembers(ctx, roomID)
	if err != nil {
		return VoiceParticipant{}, err
	}
	member, ok := memberByUserID(members, userID)
	if !ok {
		return VoiceParticipant{}, fmt.Errorf("user is not a room member")
	}
	return VoiceParticipant{
		RoomID:      roomID,
		UserID:      userID,
		Username:    member.Username,
		DisplayName: member.DisplayName,
		Muted:       true,
	}, nil
}

func (s *Service) voiceStateForRoom(room Room) VoiceState {
	s.voiceMu.Lock()
	defer s.voiceMu.Unlock()
	return s.voiceStateLocked(room)
}

func (s *Service) voiceStateLocked(room Room) VoiceState {
	participantsByUser := s.voiceRooms[room.ID]
	participants := make([]VoiceParticipant, 0, len(participantsByUser))
	for _, participant := range participantsByUser {
		participants = append(participants, participant)
	}
	sort.Slice(participants, func(i, j int) bool {
		if participants[i].JoinedAt.Equal(participants[j].JoinedAt) {
			return participants[i].UserID < participants[j].UserID
		}
		return participants[i].JoinedAt.Before(participants[j].JoinedAt)
	})
	state := VoiceState{
		RoomID:           room.ID,
		MaxMembers:       room.VoiceMaxMembers,
		MaxSpeakers:      room.VoiceMaxSpeakers,
		ParticipantCount: len(participants),
		Participants:     participants,
		UpdatedAt:        time.Now(),
	}
	for _, participant := range participants {
		if participant.Speaking {
			state.SpeakerCount++
		}
	}
	return state
}

func (s *Service) voiceSpeakerCountLocked(roomID int64) int {
	count := 0
	for _, participant := range s.voiceRooms[roomID] {
		if participant.Speaking {
			count++
		}
	}
	return count
}

func (s *Service) voiceParticipantExists(roomID, userID int64) bool {
	s.voiceMu.Lock()
	defer s.voiceMu.Unlock()
	participants := s.voiceRooms[roomID]
	_, ok := participants[userID]
	return ok
}

func (s *Service) removeVoiceParticipant(room Room, userID int64) (VoiceState, bool) {
	s.voiceMu.Lock()
	defer s.voiceMu.Unlock()
	participants := s.voiceRooms[room.ID]
	if _, exists := participants[userID]; !exists {
		return s.voiceStateLocked(room), false
	}
	delete(participants, userID)
	if len(participants) == 0 {
		delete(s.voiceRooms, room.ID)
	}
	return s.voiceStateLocked(room), true
}

func (s *Service) clearVoiceRoom(roomID int64) {
	s.voiceMu.Lock()
	defer s.voiceMu.Unlock()
	delete(s.voiceRooms, roomID)
}

func (s *Service) publishVoiceState(ctx context.Context, state VoiceState, actorUserID int64) {
	s.publishRoomEventToMembers(ctx, RoomEvent{
		Type:        RoomEventVoiceState,
		RoomID:      state.RoomID,
		ActorUserID: actorUserID,
		Voice:       &state,
		CreatedAt:   state.UpdatedAt,
	})
}

func uniquePositiveUserIDs(ids []int64) []int64 {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func validVoiceSignalType(signalType string) bool {
	switch signalType {
	case "offer", "answer", "ice", "hangup":
		return true
	default:
		return false
	}
}

func randomChatID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func (s *Service) randomRoomCIDR(ctx context.Context) (string, error) {
	_, baseNet, err := net.ParseCIDR(s.roomNetworkBase)
	if err != nil {
		return "", err
	}
	basePrefix, bits := baseNet.Mask.Size()
	if bits != 32 {
		return "", fmt.Errorf("room network base must be IPv4")
	}
	if s.roomSubnetPrefix <= basePrefix || s.roomSubnetPrefix > 30 {
		return "", fmt.Errorf("room subnet prefix must be between %d and 30", basePrefix+1)
	}
	subnetCount := int64(1) << uint(s.roomSubnetPrefix-basePrefix)
	blockSize := uint32(1) << uint(32-s.roomSubnetPrefix)
	baseU := netutil.IPToUint32(baseNet.IP)

	for i := 0; i < 64; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(subnetCount))
		if err != nil {
			return "", err
		}
		network := netutil.Uint32ToIP(baseU + uint32(n.Int64())*blockSize)
		cidr := fmt.Sprintf("%s/%d", network.String(), s.roomSubnetPrefix)
		exists, err := s.repo.NetworkExists(ctx, cidr)
		if err != nil {
			return "", err
		}
		if !exists {
			return cidr, nil
		}
	}
	return "", fmt.Errorf("no available room network")
}

func randomJoinCode() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "ROOMFAIL"
	}
	code := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:])
	if len(code) > 8 {
		code = code[:8]
	}
	return code
}

func (s *Service) requireRoomCapacity(ctx context.Context, ownerUserID int64, maxMembers int) error {
	capacity, err := s.repo.UserCapacity(ctx, ownerUserID)
	if err != nil {
		return err
	}
	if !capacity.CanCreateRoom(maxMembers) {
		return capacity.createRoomError()
	}
	return nil
}

func normalizeMaxMembers(v int) (int, error) {
	if v <= 0 {
		return DefaultMaxMembers, nil
	}
	if slices.Contains(allRoomMemberOptions, v) {
		return v, nil
	}
	return 0, fmt.Errorf("房间人数只能选择 %s", allowedMaxMembersText())
}
