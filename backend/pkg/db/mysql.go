package db

import (
	"fmt"
	"log/slog"
	"sync"

	"agenthub/backend/internal/conf"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

var (
	instance *gorm.DB
	once     sync.Once
)

func Init(cfg *conf.MySQLConfig) error {
	var initErr error
	once.Do(func() {
		dsn := cfg.DSN()
		slog.Info("connecting to mysql", "dsn", dsn)
		db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
		if err != nil {
			initErr = fmt.Errorf("open mysql: %w", err)
			return
		}
		instance = db
		slog.Info("mysql connected")
	})
	return initErr
}

func GetDB() *gorm.DB {
	return instance
}
