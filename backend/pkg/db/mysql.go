package db

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"agenthub/backend/internal/conf"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

var (
	instance *gorm.DB
	mu       sync.Mutex
)

func Init(cfg *conf.MySQLConfig) error {
	mu.Lock()
	defer mu.Unlock()

	if instance != nil {
		return nil
	}

	dsn := cfg.DSN()
	slog.Info("connecting to mysql")
	gormDB, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		return fmt.Errorf("open mysql: %w", err)
	}
	sqlDB, err := gormDB.DB()
	if err != nil {
		return fmt.Errorf("get mysql sql db: %w", err)
	}
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return fmt.Errorf("ping mysql: %w", err)
	}

	instance = gormDB
	slog.Info("mysql connected", "max_open", 25, "max_idle", 10)
	return nil
}

func GetDB() *gorm.DB {
	return instance
}

func Close() error {
	mu.Lock()
	defer mu.Unlock()
	if instance == nil {
		return nil
	}
	sqlDB, err := instance.DB()
	if err != nil {
		return err
	}
	err = sqlDB.Close()
	instance = nil
	return err
}
