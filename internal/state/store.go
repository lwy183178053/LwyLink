package state

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

type RedisStore struct {
	client *redis.Client
}

func NewRedisStore(addr, password string) *RedisStore {
	if addr == "" {
		return &RedisStore{}
	}
	return &RedisStore{
		client: redis.NewClient(&redis.Options{
			Addr:     addr,
			Password: password,
			DB:       0,
		}),
	}
}

func (s *RedisStore) Ping(ctx context.Context) error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Ping(ctx).Err()
}

func (s *RedisStore) Close() error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Close()
}

func (s *RedisStore) requireRedis(feature string) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("redis is required for %s", feature)
	}
	return nil
}
