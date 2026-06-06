package state

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	overlayOnlineTTL = 15 * time.Second
	roomAppOnlineTTL = 8 * time.Second
)

func (s *RedisStore) SetOnline(ctx context.Context, roomID, userID int64, ip string) {
	if s == nil || s.client == nil {
		return
	}
	_ = s.client.Set(ctx, overlayOnlineKey(roomID, userID), ip, overlayOnlineTTL).Err()
}

func (s *RedisStore) SetRoomAppOnline(ctx context.Context, roomID, userID int64) {
	if s == nil || s.client == nil {
		return
	}
	_ = s.client.Set(ctx, roomAppOnlineKey(roomID, userID), "1", roomAppOnlineTTL).Err()
}

func (s *RedisStore) SetOffline(ctx context.Context, roomID, userID int64) {
	if s == nil || s.client == nil {
		return
	}
	_ = s.client.Del(ctx, overlayOnlineKey(roomID, userID)).Err()
}

func (s *RedisStore) RoomHasOnline(ctx context.Context, roomID int64) (bool, error) {
	if err := s.requireRedis("online room cleanup"); err != nil {
		return false, err
	}
	for _, pattern := range []string{overlayOnlinePattern(roomID), roomAppOnlinePattern(roomID)} {
		has, err := s.hasKey(ctx, pattern)
		if err != nil || has {
			return has, err
		}
	}
	return false, nil
}

func (s *RedisStore) RoomOnlineUsers(ctx context.Context, roomID int64) (map[int64]string, error) {
	if err := s.requireRedis("room online users"); err != nil {
		return nil, err
	}
	out := make(map[int64]string)
	if err := s.scanOnlineUsers(ctx, overlayOnlinePrefix(roomID), true, out); err != nil {
		return nil, err
	}
	if err := s.scanOnlineUsers(ctx, roomAppOnlinePrefix(roomID), false, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *RedisStore) DeleteRoomOnline(ctx context.Context, roomID int64) {
	if s == nil || s.client == nil {
		return
	}
	s.deleteByPattern(ctx, overlayOnlinePattern(roomID))
	s.deleteByPattern(ctx, roomAppOnlinePattern(roomID))
}

func (s *RedisStore) DeleteRoomUserOnline(ctx context.Context, roomID, userID int64) {
	if s == nil || s.client == nil {
		return
	}
	for _, key := range []string{overlayOnlineKey(roomID, userID), roomAppOnlineKey(roomID, userID)} {
		_ = s.client.Del(ctx, key).Err()
	}
}

func (s *RedisStore) hasKey(ctx context.Context, pattern string) (bool, error) {
	iter := s.client.Scan(ctx, 0, pattern, 10).Iterator()
	if iter.Next(ctx) {
		return true, nil
	}
	return false, iter.Err()
}

func (s *RedisStore) scanOnlineUsers(ctx context.Context, prefix string, readIP bool, out map[int64]string) error {
	iter := s.client.Scan(ctx, 0, prefix+"*", 100).Iterator()
	for iter.Next(ctx) {
		rawUserID := strings.TrimPrefix(iter.Val(), prefix)
		if rawUserID == "" {
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
		ip, err := s.client.Get(ctx, iter.Val()).Result()
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

func (s *RedisStore) deleteByPattern(ctx context.Context, pattern string) {
	iter := s.client.Scan(ctx, 0, pattern, 100).Iterator()
	keys := make([]string, 0, 100)
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
		if len(keys) == cap(keys) {
			_ = s.client.Del(ctx, keys...).Err()
			keys = keys[:0]
		}
	}
	if len(keys) > 0 {
		_ = s.client.Del(ctx, keys...).Err()
	}
}

func overlayOnlineKey(roomID, userID int64) string {
	return overlayOnlinePrefix(roomID) + strconv.FormatInt(userID, 10)
}

func roomAppOnlineKey(roomID, userID int64) string {
	return roomAppOnlinePrefix(roomID) + strconv.FormatInt(userID, 10)
}

func overlayOnlinePrefix(roomID int64) string {
	return redisKey("online:%d:", roomID)
}

func roomAppOnlinePrefix(roomID int64) string {
	return redisKey("roomapp:%d:", roomID)
}

func overlayOnlinePattern(roomID int64) string {
	return overlayOnlinePrefix(roomID) + "*"
}

func roomAppOnlinePattern(roomID int64) string {
	return roomAppOnlinePrefix(roomID) + "*"
}
