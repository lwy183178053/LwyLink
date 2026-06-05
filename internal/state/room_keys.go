package state

import (
	"context"
	"fmt"
	"strconv"

	"github.com/redis/go-redis/v9"
)

func (s *RedisStore) PutRoomPublicKey(ctx context.Context, publicKey string, roomID int64) error {
	if err := s.requireRedis("room public key index"); err != nil {
		return err
	}
	key := fmt.Sprintf("gamenet:roomkey:%s", publicKey)
	return s.client.Set(ctx, key, strconv.FormatInt(roomID, 10), 0).Err()
}

func (s *RedisStore) RoomIDByPublicKey(ctx context.Context, publicKey string) (int64, bool, error) {
	if err := s.requireRedis("room public key index"); err != nil {
		return 0, false, err
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

func (s *RedisStore) DeleteRoomPublicKey(ctx context.Context, publicKey string) error {
	if err := s.requireRedis("room public key index"); err != nil {
		return err
	}
	key := fmt.Sprintf("gamenet:roomkey:%s", publicKey)
	return s.client.Del(ctx, key).Err()
}
