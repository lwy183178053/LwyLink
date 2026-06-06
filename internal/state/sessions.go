package state

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

func (s *RedisStore) SetCurrentSession(ctx context.Context, userID int64, sessionID string, ttl time.Duration) error {
	if err := s.requireRedis("account sessions"); err != nil {
		return err
	}
	key := redisKey("session:%d", userID)
	return s.client.Set(ctx, key, sessionID, ttl).Err()
}

func (s *RedisStore) CurrentSession(ctx context.Context, userID int64) (string, bool, error) {
	if err := s.requireRedis("account sessions"); err != nil {
		return "", false, err
	}
	key := redisKey("session:%d", userID)
	val, err := s.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return val, true, nil
}

func (s *RedisStore) DeleteCurrentSession(ctx context.Context, userID int64) error {
	if err := s.requireRedis("account sessions"); err != nil {
		return err
	}
	key := redisKey("session:%d", userID)
	return s.client.Del(ctx, key).Err()
}
