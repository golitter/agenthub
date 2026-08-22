package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"agenthub/backend/internal/app"
	"agenthub/backend/internal/conf"
	"agenthub/backend/internal/dao"
	gormdao "agenthub/backend/internal/dao/gorm"
	skillservice "agenthub/backend/internal/service"
	serviceimpl "agenthub/backend/internal/service/impl"
	"agenthub/backend/internal/stream"
	"agenthub/backend/pkg/agentend_client"
	"agenthub/backend/pkg/artifact_store"
	"agenthub/backend/pkg/db"
	"agenthub/backend/pkg/package_store"
	"agenthub/backend/pkg/redis"
	"agenthub/backend/pkg/skill_upload_session"
	"agenthub/backend/pkg/storage"
)

func main() {
	if err := run(); err != nil {
		slog.Error("backend stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := conf.Load("configs/config.yaml")
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	appCtx, stopApp := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopApp()

	if err := db.Init(&cfg.MySQL); err != nil {
		return fmt.Errorf("init db: %w", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			slog.Warn("close mysql", "error", err)
		}
	}()

	migrationCtx, cancelMigration := context.WithTimeout(appCtx, 2*time.Minute)
	if err := gormdao.RunMigrations(migrationCtx); err != nil {
		cancelMigration()
		return fmt.Errorf("run database migrations: %w", err)
	}
	cancelMigration()
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
		return fmt.Errorf("init redis: %w", err)
	}
	defer func() {
		if err := redis.Close(); err != nil {
			slog.Warn("close redis", "error", err)
		}
	}()
	stream.CleanupStaleMessages(gormdao.NewMessageDao())
	stream.Hub.StartClosedKeysCleanup(appCtx)

	agentClient := agentend_client.New(cfg.AgentEnd.Host, cfg.AgentEnd.Port)
	storageRuntime, err := storage.NewRuntime(&cfg.Storage)
	if err != nil {
		return fmt.Errorf("init storage: %w", err)
	}
	if storageRuntime.MinIO != nil {
		storageCtx, cancelStorage := context.WithTimeout(appCtx, 2*time.Minute)
		if err := ensureAvatarStorage(storageCtx, storageRuntime.MinIO); err != nil {
			cancelStorage()
			return fmt.Errorf("avatar MinIO is not ready: %w", err)
		}
		cancelStorage()
	}

	var artifactStore artifact_store.Store
	if cfg.ArtifactStorage.Enabled {
		requestTimeout, parseErr := time.ParseDuration(cfg.ArtifactStorage.RequestTimeout)
		if parseErr != nil {
			return fmt.Errorf("invalid artifact storage request timeout: %w", parseErr)
		}
		minioStore, createErr := artifact_store.NewMinIOStore(artifact_store.MinIOConfig{
			Endpoint: cfg.ArtifactStorage.Endpoint, Bucket: cfg.ArtifactStorage.Bucket,
			AccessKey: cfg.ArtifactStorage.AccessKey, SecretKey: cfg.ArtifactStorage.SecretKey,
			UseSSL: cfg.ArtifactStorage.UseSSL, CAFile: cfg.ArtifactStorage.CAFile, RequestTimeout: requestTimeout,
		})
		if createErr != nil {
			return fmt.Errorf("init artifact storage: %w", createErr)
		}
		storageCtx, cancelStorage := context.WithTimeout(appCtx, 2*time.Minute)
		if ensureErr := ensureArtifactStorage(storageCtx, minioStore); ensureErr != nil {
			cancelStorage()
			return fmt.Errorf("artifact MinIO bucket is not ready: %w", ensureErr)
		}
		cancelStorage()
		artifactStore = minioStore
		slog.Info("artifact storage enabled", "endpoint", cfg.ArtifactStorage.Endpoint, "bucket", cfg.ArtifactStorage.Bucket)
	}

	var skillPackageStore package_store.PackageStore
	var uploadSessionStore *skill_upload_session.Store
	var operationDao dao.SkillOperationDao = gormdao.NewSkillOperationDao()
	tempRoot := cfg.SkillStorage.TempDir
	if err := skillservice.EnsureSkillTempRoot(tempRoot); err != nil {
		return fmt.Errorf("prepare skill upload temp directory: %w", err)
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
			return fmt.Errorf("init skill package storage: %w", err)
		}
		storageCtx, cancelStorage := context.WithTimeout(appCtx, 2*time.Minute)
		if err := ensureSkillPackageBucket(storageCtx, minioStore); err != nil {
			cancelStorage()
			return fmt.Errorf("ensure skill package bucket: %w", err)
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
			return fmt.Errorf("invalid skill upload session ttl: %w", err)
		}
		lease, err := parseDuration(cfg.SkillStorage.ConfirmLease, 2*time.Minute)
		if err != nil {
			return fmt.Errorf("invalid skill confirmation lease: %w", err)
		}
		retention, err := parseDuration(cfg.SkillStorage.ReceiptRetention, 30*24*time.Hour)
		if err != nil {
			return fmt.Errorf("invalid skill receipt retention: %w", err)
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
		StorageProvider:    storageRuntime.Writer,
		AssetReader:        storageRuntime.AssetReader,
		LocalStorage:       storageRuntime.Local,
		PackageStore:       skillPackageStore,
		UploadSessionStore: uploadSessionStore,
		OperationDao:       operationDao,
		ArtifactStore:      artifactStore,
	})
	operationWorker := serviceimpl.NewSkillOperationWorker(operationDao, gormdao.NewSkillDao(), skillPackageStore, agentClient)
	taskCleanupWorker := serviceimpl.NewTaskCleanupWorker(gormdao.NewTaskCleanupDao(), agentClient)
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
	workerCtx, stopWorker := context.WithCancel(appCtx)
	defer stopWorker()
	var workerWG sync.WaitGroup
	workerWG.Add(1)
	go func() {
		defer workerWG.Done()
		operationWorker.Run(workerCtx)
	}()
	workerWG.Add(1)
	go func() {
		defer workerWG.Done()
		taskCleanupWorker.Run(workerCtx)
	}()
	workerWG.Add(1)
	go func() {
		defer workerWG.Done()
		runSkillReceiptCleanup(workerCtx, gormdao.NewSkillDao(), receiptRetention)
	}()
	workerWG.Add(1)
	go func() {
		defer workerWG.Done()
		runSkillTempCleanup(workerCtx, tempRoot)
	}()
	if artifactStore != nil {
		artifactRetention := 24 * time.Hour
		if parsed, parseErr := time.ParseDuration(cfg.ArtifactStorage.FailedRetention); parseErr == nil && parsed > 0 {
			artifactRetention = parsed
		}
		workerWG.Add(1)
		go func() {
			defer workerWG.Done()
			runArtifactCleanup(workerCtx, gormdao.NewArtifactDao(), artifactStore, artifactRetention)
		}()
	}

	addr := ":" + fmt.Sprint(cfg.Server.Port)
	slog.Info("server starting", "port", cfg.Server.Port)

	srv := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20,
	}

	serverErr := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	var runErr error
	select {
	case <-appCtx.Done():
	case err := <-serverErr:
		runErr = fmt.Errorf("serve http: %w", err)
	}
	slog.Info("shutting down server...")
	// Stop claiming new durable operations before draining HTTP requests. Any
	// in-flight storage/AgentEnd call receives the cancellation and the worker
	// gets the same bounded shutdown window as the HTTP server.
	stopWorker()

	// 给未完成的请求 15 秒处理时间，然后强制关闭
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("server forced to shutdown", "error", err)
	}
	workersDone := make(chan struct{})
	go func() {
		workerWG.Wait()
		close(workersDone)
	}()
	select {
	case <-workersDone:
	case <-ctx.Done():
		slog.Warn("background workers did not stop before server shutdown deadline")
	}

	slog.Info("server exited")
	return runErr
}

// ensureAvatarStorage waits for the bucket and application credentials to be
// ready, but never creates a bucket or changes its policy. Docker's init job
// owns that privileged setup.
func ensureAvatarStorage(ctx context.Context, store *storage.MinIOStorage) error {
	if store == nil {
		return fmt.Errorf("avatar minio storage is not initialized")
	}
	var lastErr error
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			if lastErr != nil {
				return fmt.Errorf("avatar minio startup timeout: %w (last error: %v)", err, lastErr)
			}
			return err
		}
		checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := store.Health(checkCtx)
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt == 0 {
			slog.Warn("avatar MinIO bucket is not ready; retrying", "error", err)
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
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

// ensureArtifactStorage waits for the bucket and least-privilege application
// credentials prepared by the deployment layer. Backend must not create or
// mutate buckets with its runtime account.
func ensureArtifactStorage(ctx context.Context, store *artifact_store.MinIOStore) error {
	if store == nil {
		return fmt.Errorf("artifact minio storage is not initialized")
	}
	var lastErr error
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			if lastErr != nil {
				return fmt.Errorf("artifact MinIO startup timeout: %w (last error: %v)", err, lastErr)
			}
			return err
		}
		checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := store.Health(checkCtx)
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt == 0 {
			slog.Warn("artifact MinIO bucket is not ready; retrying", "error", err)
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

func runArtifactCleanup(ctx context.Context, artifactDao dao.ArtifactDao, store artifact_store.Store, retention time.Duration) {
	if artifactDao == nil || store == nil {
		return
	}
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		artifacts, err := artifactDao.ListStalePendingOrFailed(time.Now().Add(-retention), 200)
		if err != nil {
			slog.Warn("periodic artifact cleanup query failed", "error", err)
		} else {
			for _, artifact := range artifacts {
				if err := store.Delete(ctx, artifact.ObjectKey); err != nil && !errors.Is(err, artifact_store.ErrNotFound) {
					slog.Warn("periodic artifact object cleanup failed", "resource_id", artifact.ResourceID, "error", err)
					if markErr := artifactDao.MarkDeleteFailed(artifact.ResourceID, err.Error()); markErr != nil {
						slog.Warn("record periodic artifact deletion failure failed", "resource_id", artifact.ResourceID, "error", markErr)
					}
					continue
				}
				if err := artifactDao.DeleteRow(artifact.ResourceID); err != nil {
					slog.Warn("periodic artifact metadata cleanup failed", "resource_id", artifact.ResourceID, "error", err)
					if markErr := artifactDao.MarkDeleteFailed(artifact.ResourceID, err.Error()); markErr != nil {
						slog.Warn("record periodic artifact metadata deletion failure failed", "resource_id", artifact.ResourceID, "error", markErr)
					}
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
