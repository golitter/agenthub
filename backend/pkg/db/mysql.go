package db

import (
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
	once     sync.Once
)

func Init(cfg *conf.MySQLConfig) error {
	var initErr error
	once.Do(func() {
		dsn := cfg.DSN()
		slog.Info("connecting to mysql")
		db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
		if err != nil {
			initErr = fmt.Errorf("open mysql: %w", err)
			return
		}
		sqlDB, _ := db.DB()
		sqlDB.SetMaxOpenConns(25)
		sqlDB.SetMaxIdleConns(10)
		sqlDB.SetConnMaxLifetime(5 * time.Minute)
		instance = db
		slog.Info("mysql connected", "max_open", 25, "max_idle", 10)
	})
	return initErr
}

func GetDB() *gorm.DB {
	return instance
}
