package redis

import (
	"context"
	"fmt"

	"agenthub/backend/internal/conf"

	goredis "github.com/redis/go-redis/v9"
)

var client *goredis.Client

func Init(cfg *conf.RedisConfig) error {
	client = goredis.NewClient(&goredis.Options{
		Addr:     cfg.Addr(),
		Password: cfg.Password,
		DB:       cfg.DB,
	})
	if err := client.Ping(context.Background()).Err(); err != nil {
		return fmt.Errorf("redis ping failed: %w", err)
	}
	return nil
}

func GetClient() *goredis.Client {
	return client
}

func Close() error {
	if client != nil {
		return client.Close()
	}
	return nil
}

// StreamKey returns the Redis Stream key for a given session + message.
func StreamKey(sessionID, messageID string) string {
	return fmt.Sprintf("agent:%s:%s", sessionID, messageID)
}
