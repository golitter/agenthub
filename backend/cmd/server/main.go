package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"agenthub/backend/internal/app"
	"agenthub/backend/internal/conf"
	"agenthub/backend/internal/dao"
	gormdao "agenthub/backend/internal/dao/gorm"
	"agenthub/backend/internal/model"
	skillservice "agenthub/backend/internal/service"
	serviceimpl "agenthub/backend/internal/service/impl"
	"agenthub/backend/internal/stream"
	"agenthub/backend/pkg/agentend_client"
	"agenthub/backend/pkg/db"
	"agenthub/backend/pkg/package_store"
	"agenthub/backend/pkg/redis"
	"agenthub/backend/pkg/skill_upload_session"
	"agenthub/backend/pkg/storage"
)

func main() {
	cfg, err := conf.Load("configs/config.yaml")
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	if err := db.Init(&cfg.MySQL); err != nil {
		slog.Error("init db", "error", err)
		os.Exit(1)
	}

	if err := gormdao.CleanupDuplicateJoinRows(); err != nil {
		slog.Error("cleanup duplicate join rows", "error", err)
		os.Exit(1)
	}

	if err := db.GetDB().AutoMigrate(&model.Session{}, &model.Task{}, &model.Message{}, &model.DiffSnapshot{}, &model.SessionAgent{}, &model.AdminSetting{}, &model.Announcement{}, &model.ContactGroup{}, &model.ContactGroupItem{}, &model.SkillHub{}, &model.AgentSkill{}, &model.SkillUploadReceipt{}, &model.SkillOperationJob{}, &model.SkillAuditEvent{}); err != nil {
		slog.Error("auto migrate", "error", err)
		os.Exit(1)
	}
	if err := gormdao.BackfillSkillStorageMetadata(); err != nil {
		slog.Error("backfill skill storage metadata", "error", err)
		os.Exit(1)
	}
	receiptRetention := 30 * 24 * time.Hour
	if raw := cfg.SkillStorage.ReceiptRetention; raw != "" {
		if parsed, parseErr := time.ParseDuration(raw); parseErr == nil && parsed > 0 {
			receiptRetention = parsed
		}
	}
	if removed, cleanupErr := gormdao.NewSkillDao().CleanupSkillUploadReceipts(time.Now().Add(-receiptRetention), 500); cleanupErr != nil {
		slog.Warn("cleanup expired Skill upload receipts failed", "error", cleanupErr)
	} else if removed > 0 {
		slog.Info("cleaned expired Skill upload receipts", "count", removed)
	}

	if err := redis.Init(&cfg.Redis); err != nil {
		slog.Error("init redis", "error", err)
		os.Exit(1)
	}
	defer func() {
		if err := redis.Close(); err != nil {
			slog.Warn("close redis", "error", err)
		}
	}()

	stream.CleanupStaleMessages(gormdao.NewMessageDao())
	stream.Hub.StartClosedKeysCleanup()

	agentClient := agentend_client.New(cfg.AgentEnd.Host, cfg.AgentEnd.Port)
	storageProvider, err := storage.NewProvider(&cfg.Qiniu, &cfg.Storage)
	if err != nil {
		slog.Error("init storage", "error", err)
		os.Exit(1)
	}

	var skillPackageStore package_store.PackageStore
	var uploadSessionStore *skill_upload_session.Store
	var operationDao dao.SkillOperationDao = gormdao.NewSkillOperationDao()
	tempRoot := cfg.SkillStorage.TempDir
	if err := skillservice.EnsureSkillTempRoot(tempRoot); err != nil {
		slog.Error("prepare skill upload temp directory", "error", err)
		os.Exit(1)
	}
	if cfg.SkillStorage.Enabled {
		minioStore, err := package_store.NewMinIOStore(package_store.MinIOConfig{
			Endpoint:  cfg.SkillStorage.Endpoint,
			Bucket:    cfg.SkillStorage.Bucket,
			AccessKey: cfg.SkillStorage.AccessKey,
			SecretKey: cfg.SkillStorage.SecretKey,
			UseSSL:    cfg.SkillStorage.UseSSL,
			CAFile:    cfg.SkillStorage.CAFile,
		})
		if err != nil {
			slog.Error("init skill package storage", "error", err)
			os.Exit(1)
		}
		storageCtx, cancelStorage := context.WithTimeout(context.Background(), 2*time.Minute)
		if err := ensureSkillPackageBucket(storageCtx, minioStore); err != nil {
			cancelStorage()
			slog.Error("ensure skill package bucket", "error", err)
			os.Exit(1)
		}
		cancelStorage()
		skillPackageStore = minioStore
		parseDuration := func(raw string, fallback time.Duration) (time.Duration, error) {
			if raw == "" {
				return fallback, nil
			}
			value, err := time.ParseDuration(raw)
			if err != nil {
				return 0, err
			}
			return value, nil
		}
		ttl, err := parseDuration(cfg.SkillStorage.UploadSessionTTL, 15*time.Minute)
		if err != nil {
			slog.Error("invalid skill upload session ttl", "error", err)
			os.Exit(1)
		}
		lease, err := parseDuration(cfg.SkillStorage.ConfirmLease, 2*time.Minute)
		if err != nil {
			slog.Error("invalid skill confirmation lease", "error", err)
			os.Exit(1)
		}
		retention, err := parseDuration(cfg.SkillStorage.ReceiptRetention, 30*24*time.Hour)
		if err != nil {
			slog.Error("invalid skill receipt retention", "error", err)
			os.Exit(1)
		}
		uploadSessionStore = skill_upload_session.New(redis.GetClient(), skill_upload_session.Options{
			TTL: ttl, Lease: lease, ResultRetention: retention,
		})
		slog.Info("skill package storage enabled", "endpoint", cfg.SkillStorage.Endpoint, "bucket", cfg.SkillStorage.Bucket)
	}
	if _, err := skillservice.CleanupSkillUploadTempDir(tempRoot, 24*time.Hour); err != nil {
		slog.Warn("cleanup stale Skill upload temp files failed", "root", tempRoot, "error", err)
	}
	if tempRoot != os.TempDir() {
		if _, err := skillservice.CleanupSkillUploadTempDir(os.TempDir(), 24*time.Hour); err != nil {
			slog.Warn("cleanup legacy Skill upload temp files failed", "root", os.TempDir(), "error", err)
		}
	}

	r := app.NewRouter(app.Dependencies{
		Config:             cfg,
		AgentClient:        agentClient,
		StorageProvider:    storageProvider,
		PackageStore:       skillPackageStore,
		UploadSessionStore: uploadSessionStore,
		OperationDao:       operationDao,
	})
	operationWorker := serviceimpl.NewSkillOperationWorker(operationDao, gormdao.NewSkillDao(), skillPackageStore, agentClient)
	if cfg.SkillStorage.Enabled {
		operationWorker.SetReadPreference(cfg.SkillStorage.ReadPreference)
		orphanGrace := 48 * time.Hour
		if raw := cfg.SkillStorage.OrphanGracePeriod; raw != "" {
			if parsed, parseErr := time.ParseDuration(raw); parseErr == nil && parsed > 0 {
				orphanGrace = parsed
			}
		}
		operationWorker.SetOrphanCleanupPolicy(orphanGrace)
		incomingTTL := 24 * time.Hour
		if raw := cfg.SkillStorage.IncomingTTL; raw != "" {
			if parsed, parseErr := time.ParseDuration(raw); parseErr == nil && parsed > 0 {
				incomingTTL = parsed
			}
		}
		operationWorker.SetIncomingCleanupPolicy(incomingTTL, 5*time.Minute)
		limits := skillservice.DefaultZipLimits()
		if raw := cfg.SkillStorage.MaxPackageSize; raw != "" {
			if parsed, parseErr := conf.ParseByteSize(raw); parseErr == nil {
				limits.PackageSize = parsed
			}
		}
		if raw := cfg.SkillStorage.MaxUnpackedSize; raw != "" {
			if parsed, parseErr := conf.ParseByteSize(raw); parseErr == nil {
				limits.UnpackedSize = parsed
			}
		}
		if raw := cfg.SkillStorage.MaxFileSize; raw != "" {
			if parsed, parseErr := conf.ParseByteSize(raw); parseErr == nil {
				limits.FileSize = parsed
			}
		}
		if cfg.SkillStorage.MaxCompressionRatio > 0 {
			limits.CompressionRatio = cfg.SkillStorage.MaxCompressionRatio
		}
		if cfg.SkillStorage.MaxFileCount > 0 {
			limits.FileCount = cfg.SkillStorage.MaxFileCount
		}
		operationWorker.SetZipLimits(limits)
	}
	workerCtx, stopWorker := context.WithCancel(context.Background())
	defer stopWorker()
	go operationWorker.Run(workerCtx)
	go runSkillReceiptCleanup(workerCtx, gormdao.NewSkillDao(), receiptRetention)
	go runSkillTempCleanup(workerCtx, tempRoot)

	addr := ":" + fmt.Sprint(cfg.Server.Port)
	slog.Info("server starting", "port", cfg.Server.Port)

	srv := &http.Server{Addr: addr, Handler: r}

	// 在 goroutine 中启动 HTTP 服务
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	// 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("shutting down server...")

	// 给未完成的请求 15 秒处理时间，然后强制关闭
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("server forced to shutdown", "error", err)
	}

	slog.Info("server exited")
}

// ensureSkillPackageBucket waits only when the MinIO feature is enabled.  The
// Compose stack starts the init container independently, so the application
// credentials may not exist on the first attempt; bounded retries cover that
// normal startup race without making MinIO a dependency of the legacy DB path.
func ensureSkillPackageBucket(ctx context.Context, store *package_store.MinIOStore) error {
	if store == nil {
		return fmt.Errorf("minio store is not initialized")
	}
	var lastErr error
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			if lastErr != nil {
				return fmt.Errorf("minio startup timeout: %w (last error: %v)", err, lastErr)
			}
			return err
		}
		callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := store.EnsureBucket(callCtx)
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt == 0 {
			slog.Warn("MinIO bucket is not ready; retrying", "error", err)
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
}

func runSkillReceiptCleanup(ctx context.Context, dao *gormdao.SkillDao, retention time.Duration) {
	if dao == nil {
		return
	}
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		if removed, err := dao.CleanupSkillUploadReceipts(time.Now().Add(-retention), 500); err != nil {
			slog.Warn("periodic Skill upload receipt cleanup failed", "error", err)
		} else if removed > 0 {
			slog.Info("periodic Skill upload receipts cleanup completed", "count", removed)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func runSkillTempCleanup(ctx context.Context, root string) {
	if root == "" {
		return
	}
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		if removed, err := skillservice.CleanupSkillUploadTempDir(root, 24*time.Hour); err != nil {
			slog.Warn("periodic Skill temp cleanup failed", "root", root, "error", err)
		} else if removed > 0 {
			slog.Info("periodic Skill temp cleanup completed", "root", root, "count", removed)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
