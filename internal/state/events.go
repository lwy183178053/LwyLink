package state

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/LwyV-Labs/LwyLink/internal/room"
	"github.com/redis/go-redis/v9"
)

func (s *RedisStore) PublishRoomEvent(ctx context.Context, event room.RoomEvent) error {
	if err := s.requireRedis("room events"); err != nil {
		return err
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

func (s *RedisStore) SubscribeUserRoomEvents(ctx context.Context, userID int64) (<-chan room.RoomEvent, func(), error) {
	if err := s.requireRedis("room events"); err != nil {
		return nil, nil, err
	}
	return s.subscribeRoomEvents(ctx, false, roomEventUserChannel(userID))
}

func (s *RedisStore) SubscribeAllRoomEvents(ctx context.Context) (<-chan room.RoomEvent, func(), error) {
	if err := s.requireRedis("room events"); err != nil {
		return nil, nil, err
	}
	return s.subscribeRoomEvents(ctx, true, "gamenet:room-events:room:*")
}

func (s *RedisStore) subscribeRoomEvents(ctx context.Context, pattern bool, channel string) (<-chan room.RoomEvent, func(), error) {
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
	return out, func() { _ = pubsub.Close() }, nil
}

func roomEventRoomChannel(roomID int64) string {
	return fmt.Sprintf("gamenet:room-events:room:%d", roomID)
}

func roomEventUserChannel(userID int64) string {
	return fmt.Sprintf("gamenet:room-events:user:%d", userID)
}

func uniquePositiveIDs(ids []int64) []int64 {
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
