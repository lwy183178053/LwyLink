package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/LwyV-Labs/LwyV-GameNet/internal/room"
	"github.com/redis/go-redis/v9"
)

const (
	overlayOnlineTTL = 15 * time.Second
	roomAppOnlineTTL = 8 * time.Second
)

type OnlineStore struct {
	client *redis.Client
}

// NewOnlineStore returns a no-op store when addr is empty. Room public key
// indexing still requires Redis in normal server mode because invite validity
// is intentionally tied to this shared runtime store.
func NewOnlineStore(addr, password string) *OnlineStore {
	if addr == "" {
		return &OnlineStore{}
	}
	return &OnlineStore{
		client: redis.NewClient(&redis.Options{
			Addr:     addr,
			Password: password,
			DB:       0,
		}),
	}
}

func (s *OnlineStore) Ping(ctx context.Context) error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Ping(ctx).Err()
}

// SetOnline stores short-lived presence for admin/diagnostic views. It is not
// the routing source of truth; overlay forwarding uses in-memory sessions.
func (s *OnlineStore) SetOnline(ctx context.Context, roomID, userID int64, ip string) {
	if s == nil || s.client == nil {
		return
	}
	key := fmt.Sprintf("gamenet:online:%d:%d", roomID, userID)
	_ = s.client.Set(ctx, key, ip, overlayOnlineTTL).Err()
}

// SetRoomAppOnline records that the player's client is actively viewing or
// managing the room, even if the Wintun overlay has not been started yet.
func (s *OnlineStore) SetRoomAppOnline(ctx context.Context, roomID, userID int64) {
	if s == nil || s.client == nil {
		return
	}
	key := fmt.Sprintf("gamenet:roomapp:%d:%d", roomID, userID)
	_ = s.client.Set(ctx, key, "1", roomAppOnlineTTL).Err()
}

func (s *OnlineStore) SetOffline(ctx context.Context, roomID, userID int64) {
	if s == nil || s.client == nil {
		return
	}
	key := fmt.Sprintf("gamenet:online:%d:%d", roomID, userID)
	_ = s.client.Del(ctx, key).Err()
}

func (s *OnlineStore) RoomHasOnline(ctx context.Context, roomID int64) (bool, error) {
	if s == nil || s.client == nil {
		return false, fmt.Errorf("redis is required for online room cleanup")
	}
	for _, pattern := range []string{
		fmt.Sprintf("gamenet:online:%d:*", roomID),
		fmt.Sprintf("gamenet:roomapp:%d:*", roomID),
	} {
		has, err := s.hasKey(ctx, pattern)
		if err != nil || has {
			return has, err
		}
	}
	return false, nil
}

func (s *OnlineStore) RoomOnlineUsers(ctx context.Context, roomID int64) (map[int64]string, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("redis is required for room online users")
	}
	out := make(map[int64]string)
	if err := s.scanOnlineUsers(ctx, fmt.Sprintf("gamenet:online:%d:", roomID), true, out); err != nil {
		return nil, err
	}
	if err := s.scanOnlineUsers(ctx, fmt.Sprintf("gamenet:roomapp:%d:", roomID), false, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *OnlineStore) hasKey(ctx context.Context, pattern string) (bool, error) {
	iter := s.client.Scan(ctx, 0, pattern, 10).Iterator()
	if iter.Next(ctx) {
		return true, nil
	}
	return false, iter.Err()
}

func (s *OnlineStore) scanOnlineUsers(ctx context.Context, prefix string, readIP bool, out map[int64]string) error {
	iter := s.client.Scan(ctx, 0, prefix+"*", 100).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()
		rawUserID := strings.TrimPrefix(key, prefix)
		if rawUserID == key || rawUserID == "" {
			continue
		}
		userID, err := strconv.ParseInt(rawUserID, 10, 64)
		if err != nil {
			continue
		}
		if !readIP {
			if _, exists := out[userID]; !exists {
				out[userID] = ""
			}
			continue
		}
		ip, err := s.client.Get(ctx, key).Result()
		if err == redis.Nil {
			continue
		}
		if err != nil {
			return err
		}
		out[userID] = ip
	}
	return iter.Err()
}

func (s *OnlineStore) DeleteRoomOnline(ctx context.Context, roomID int64) {
	if s == nil || s.client == nil {
		return
	}
	for _, pattern := range []string{
		fmt.Sprintf("gamenet:online:%d:*", roomID),
		fmt.Sprintf("gamenet:roomapp:%d:*", roomID),
	} {
		s.deleteByPattern(ctx, pattern)
	}
}

func (s *OnlineStore) DeleteRoomUserOnline(ctx context.Context, roomID, userID int64) {
	if s == nil || s.client == nil {
		return
	}
	for _, key := range []string{
		fmt.Sprintf("gamenet:online:%d:%d", roomID, userID),
		fmt.Sprintf("gamenet:roomapp:%d:%d", roomID, userID),
	} {
		_ = s.client.Del(ctx, key).Err()
	}
}

func (s *OnlineStore) PublishRoomEvent(ctx context.Context, event room.RoomEvent) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("redis is required for room events")
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if event.RoomID > 0 {
		if err := s.client.Publish(ctx, roomEventRoomChannel(event.RoomID), data).Err(); err != nil {
			return err
		}
	}
	for _, userID := range uniquePositiveIDs(event.UserIDs) {
		if err := s.client.Publish(ctx, roomEventUserChannel(userID), data).Err(); err != nil {
			return err
		}
	}
	return nil
}

func (s *OnlineStore) SubscribeUserRoomEvents(ctx context.Context, userID int64) (<-chan room.RoomEvent, func(), error) {
	if s == nil || s.client == nil {
		return nil, nil, fmt.Errorf("redis is required for room events")
	}
	return s.subscribeRoomEvents(ctx, false, roomEventUserChannel(userID))
}

func (s *OnlineStore) SubscribeAllRoomEvents(ctx context.Context) (<-chan room.RoomEvent, func(), error) {
	if s == nil || s.client == nil {
		return nil, nil, fmt.Errorf("redis is required for room events")
	}
	return s.subscribeRoomEvents(ctx, true, "gamenet:room-events:room:*")
}

func (s *OnlineStore) subscribeRoomEvents(ctx context.Context, pattern bool, channel string) (<-chan room.RoomEvent, func(), error) {
	var pubsub *redis.PubSub
	if pattern {
		pubsub = s.client.PSubscribe(ctx, channel)
	} else {
		pubsub = s.client.Subscribe(ctx, channel)
	}
	if err := pubsub.Ping(ctx); err != nil {
		_ = pubsub.Close()
		return nil, nil, err
	}
	out := make(chan room.RoomEvent, 64)
	cleanup := func() {
		_ = pubsub.Close()
	}
	go func() {
		defer close(out)
		for msg := range pubsub.Channel() {
			var event room.RoomEvent
			if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
				continue
			}
			select {
			case out <- event:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, cleanup, nil
}

func (s *OnlineStore) deleteByPattern(ctx context.Context, pattern string) {
	iter := s.client.Scan(ctx, 0, pattern, 100).Iterator()
	var keys []string
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
		if len(keys) >= 100 {
			_ = s.client.Del(ctx, keys...).Err()
			keys = keys[:0]
		}
	}
	if len(keys) > 0 {
		_ = s.client.Del(ctx, keys...).Err()
	}
}

func roomEventRoomChannel(roomID int64) string {
	return fmt.Sprintf("gamenet:room-events:room:%d", roomID)
}

func roomEventUserChannel(userID int64) string {
	return fmt.Sprintf("gamenet:room-events:user:%d", userID)
}

func uniquePositiveIDs(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	var out []int64
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

// PutRoomPublicKey publishes the current valid invite key for a room. Deleting
// this Redis key is what makes old room public keys stop working immediately.
func (s *OnlineStore) PutRoomPublicKey(ctx context.Context, publicKey string, roomID int64) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("redis is required for room public key index")
	}
	key := fmt.Sprintf("gamenet:roomkey:%s", publicKey)
	return s.client.Set(ctx, key, strconv.FormatInt(roomID, 10), 0).Err()
}

// RoomIDByPublicKey resolves only active invite keys. Missing keys are not an
// error; they mean the room is closed, deleted, or the key is expired/invalid.
func (s *OnlineStore) RoomIDByPublicKey(ctx context.Context, publicKey string) (int64, bool, error) {
	if s == nil || s.client == nil {
		return 0, false, fmt.Errorf("redis is required for room public key index")
	}
	key := fmt.Sprintf("gamenet:roomkey:%s", publicKey)
	val, err := s.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	roomID, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return 0, false, err
	}
	return roomID, true, nil
}

func (s *OnlineStore) DeleteRoomPublicKey(ctx context.Context, publicKey string) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("redis is required for room public key index")
	}
	key := fmt.Sprintf("gamenet:roomkey:%s", publicKey)
	return s.client.Del(ctx, key).Err()
}

// SetCurrentSession records the only login session that is allowed for a user.
// A new login overwrites this value, so older tokens fail on the next API or
// overlay session check.
func (s *OnlineStore) SetCurrentSession(ctx context.Context, userID int64, sessionID string, ttl time.Duration) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("redis is required for account sessions")
	}
	key := fmt.Sprintf("gamenet:session:%d", userID)
	return s.client.Set(ctx, key, sessionID, ttl).Err()
}

func (s *OnlineStore) CurrentSession(ctx context.Context, userID int64) (string, bool, error) {
	if s == nil || s.client == nil {
		return "", false, fmt.Errorf("redis is required for account sessions")
	}
	key := fmt.Sprintf("gamenet:session:%d", userID)
	val, err := s.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return val, true, nil
}

func (s *OnlineStore) DeleteCurrentSession(ctx context.Context, userID int64) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("redis is required for account sessions")
	}
	key := fmt.Sprintf("gamenet:session:%d", userID)
	return s.client.Del(ctx, key).Err()
}

func (s *OnlineStore) Close() error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Close()
}
