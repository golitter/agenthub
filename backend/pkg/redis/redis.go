package redis

import (
	"context"
	"fmt"
	"time"

	"agenthub/backend/internal/conf"

	goredis "github.com/redis/go-redis/v9"
)

var client *goredis.Client

func Init(cfg *conf.RedisConfig) error {
	client = goredis.NewClient(&goredis.Options{
		Addr:         cfg.Addr(),
		Password:     cfg.Password,
		DB:           cfg.DB,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     10,
	})
	if err := client.Ping(context.Background()).Err(); err != nil {
		_ = client.Close()
		client = nil
		return fmt.Errorf("redis ping failed: %w", err)
	}
	return nil
}

func GetClient() *goredis.Client {
	return client
}

func Close() error {
	if client != nil {
		err := client.Close()
		client = nil
		return err
	}
	return nil
}

// StreamKey 根据会话 + 消息返回对应的 Redis Stream key。
func StreamKey(sessionID, messageID string) string {
	return fmt.Sprintf("agent:%s:%s", sessionID, messageID)
}
