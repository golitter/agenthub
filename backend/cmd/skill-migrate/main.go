// Command skill-migrate moves Skill ZIP BLOBs to MinIO in bounded, resumable
// batches.  It intentionally performs no schema migration and never loads a
// batch of BLOBs into memory at once.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"agenthub/backend/internal/conf"
	gormdao "agenthub/backend/internal/dao/gorm"
	"agenthub/backend/internal/model"
	"agenthub/backend/internal/service"
	"agenthub/backend/pkg/db"
	"agenthub/backend/pkg/package_store"
)

type flags struct {
	config     string
	batch      int
	dryRun     bool
	resume     bool
	name       string
	verify     bool
	reverse    bool
	clear      bool
	clearAck   string
	cursorFile string
}

func main() {
	var f flags
	flag.StringVar(&f.config, "config", "configs/config.yaml", "backend config path")
	flag.IntVar(&f.batch, "batch-size", 10, "number of metadata rows per batch")
	flag.BoolVar(&f.dryRun, "dry-run", false, "validate and report without writing MinIO or MySQL")
	flag.BoolVar(&f.resume, "resume", false, "resume from the cursor file")
	flag.StringVar(&f.name, "skill-name", "", "only migrate one Skill name")
	flag.BoolVar(&f.verify, "verify-only", false, "verify MinIO objects and database hashes")
	flag.BoolVar(&f.reverse, "reverse-to-db", false, "read MinIO objects back into the BLOB column")
	flag.BoolVar(&f.clear, "clear-content", false, "clear verified migrated shadow BLOBs (requires explicit acknowledgement)")
	flag.StringVar(&f.clearAck, "confirm-clear-content", "", "required acknowledgement: CLEAR-SKILL-BLOBS")
	flag.StringVar(&f.cursorFile, "cursor-file", ".skill-migrate.cursor", "cursor file used by --resume")
	flag.Parse()
	if f.batch <= 0 {
		fatal("batch-size must be positive")
	}
	if (f.verify && f.reverse) || (f.clear && (f.verify || f.reverse)) {
		fatal("--verify-only, --reverse-to-db and --clear-content are mutually exclusive")
	}
	if f.clear && !f.dryRun && f.clearAck != "CLEAR-SKILL-BLOBS" {
		fatal("--clear-content requires --confirm-clear-content=CLEAR-SKILL-BLOBS")
	}
	cfg, err := conf.Load(f.config)
	if err != nil {
		fatal("load config: %v", err)
	}
	// A regular migration, verification, reverse-fill, or shadow-BLOB cleanup
	// all require a reachable MinIO bucket.  Refuse a disabled feature gate
	// early instead of constructing a client with incomplete credentials and
	// failing later after database setup has already started.
	if (!f.dryRun || f.verify || f.reverse || f.clear) && !cfg.SkillStorage.Enabled {
		fatal("skill_storage.enabled must be true for this MinIO operation")
	}
	limits, err := migrationZipLimits(*cfg)
	if err != nil {
		fatal("load Skill ZIP limits: %v", err)
	}
	tempRoot := strings.TrimSpace(cfg.SkillStorage.TempDir)
	if tempRoot == "" {
		tempRoot = "./data/skill-tmp"
	}
	if !f.verify && !f.reverse {
		if err := service.EnsureSkillTempRoot(tempRoot); err != nil {
			fatal("prepare Skill migration temp directory: %v", err)
		}
		if _, cleanupErr := service.CleanupSkillUploadTempDir(tempRoot, 24*time.Hour); cleanupErr != nil {
			fatal("cleanup stale Skill migration temp data: %v", cleanupErr)
		}
	}
	if err := db.Init(&cfg.MySQL); err != nil {
		fatal("init db: %v", err)
	}
	defer db.Close()
	var store *package_store.MinIOStore
	// Verification and reverse-migration are read-only, but they still need a
	// live Store in dry-run mode.  Only the regular migration dry-run can avoid
	// constructing MinIO because it validates the legacy BLOB locally.
	if !f.dryRun || f.verify || f.reverse || f.clear {
		store, err = package_store.NewMinIOStore(package_store.MinIOConfig{
			Endpoint: cfg.SkillStorage.Endpoint, Bucket: cfg.SkillStorage.Bucket,
			AccessKey: cfg.SkillStorage.AccessKey, SecretKey: cfg.SkillStorage.SecretKey, UseSSL: cfg.SkillStorage.UseSSL,
			CAFile: cfg.SkillStorage.CAFile,
		})
		if err != nil {
			fatal("init MinIO store: %v", err)
		}
	}
	dao := gormdao.NewSkillDao()
	operationJobs := gormdao.NewSkillOperationDao()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	afterID := uint(0)
	if f.resume {
		afterID = readCursor(f.cursorFile)
	}
	if f.verify {
		if err := verifyMinIO(ctx, dao, store, f, afterID, limits); err != nil {
			fatal("verify: %v", err)
		}
		return
	}
	if f.reverse {
		if err := reverseToDB(ctx, dao, store, f, afterID, limits); err != nil {
			fatal("reverse-to-db: %v", err)
		}
		return
	}
	if f.clear {
		if !f.dryRun && !cfg.SkillStorage.Enabled {
			fatal("refusing to clear Content while skill storage is disabled")
		}
		if !f.dryRun && cfg.SkillStorage.ShadowWriteBlob {
			fatal("refusing to clear Content while skill_storage.shadow_write_blob is enabled")
		}
		if !f.dryRun && cfg.SkillStorage.ReadPreference != "minio" {
			fatal("refusing to clear Content unless skill_storage.read_preference=minio")
		}
		if err := clearMigratedContent(ctx, dao, store, f, afterID, limits); err != nil {
			fatal("clear-content: %v", err)
		}
		return
	}
	if !f.dryRun {
		if err := processPendingMigrationJobs(ctx, dao, operationJobs, store, f.batch, limits, tempRoot); err != nil {
			fatal("resume pending migration jobs: %v", err)
		}
	}
	for {
		skills, err := dao.ListExternalSkillMetadataAfter(afterID, f.batch, f.name)
		if err != nil {
			fatal("list skills after %d: %v", afterID, err)
		}
		if len(skills) == 0 {
			break
		}
		for _, skill := range skills {
			afterID = skill.ID
			if skill.ObjectKey != "" || skill.StorageType == model.SkillStorageMinIO {
				continue
			}
			if err := migrateOneWithJob(ctx, dao, operationJobs, store, skill, f.dryRun, limits, tempRoot); err != nil {
				slog.Error("migrate skill failed", "skill", skill.Name, "id", skill.ID, "error", err)
				continue
			}
			slog.Info("migrated skill", "skill", skill.Name, "id", skill.ID, "dry_run", f.dryRun)
		}
		if f.resume && !f.dryRun {
			writeCursor(f.cursorFile, afterID)
		}
		if len(skills) < f.batch {
			break
		}
	}
}

func migrateOneWithJob(ctx context.Context, dao *gormdao.SkillDao, jobs *gormdao.SkillOperationDao, store *package_store.MinIOStore, skill model.SkillHub, dryRun bool, limits service.ZipLimits, tempRoot string) error {
	if dryRun {
		return migrateOne(ctx, dao, store, skill, true, limits, tempRoot)
	}
	claimed, err := dao.ClaimSkillMigrationJob(skill.ID, skill.Name, time.Now(), 5*time.Minute)
	if err != nil {
		return err
	}
	if claimed == nil {
		// Another worker owns the lease, or the row was already migrated.  Both
		// cases are safe no-ops for this cursor pass.
		return nil
	}
	if err := migrateOne(ctx, dao, store, skill, false, limits, tempRoot); err != nil {
		_ = jobs.RetrySkillOperationJob(claimed.ID, claimed.LeaseToken, err.Error(), time.Now().Add(time.Minute))
		return err
	}
	// Capture the deterministic formal object key on the durable task after
	// the metadata transaction succeeds.  If this update fails, the job remains
	// retryable and the migration itself is still safely idempotent.
	if updated, getErr := dao.GetSkillByID(skill.ID); getErr == nil && updated != nil && updated.ObjectKey != "" {
		if updateErr := jobs.UpdateSkillOperationObjectKey(claimed.ID, claimed.LeaseToken, updated.ObjectKey); updateErr != nil {
			_ = jobs.RetrySkillOperationJob(claimed.ID, claimed.LeaseToken, updateErr.Error(), time.Now().Add(time.Minute))
			return updateErr
		}
	}
	return jobs.CompleteSkillOperationJob(claimed.ID, claimed.LeaseToken)
}

func processPendingMigrationJobs(ctx context.Context, dao *gormdao.SkillDao, jobs *gormdao.SkillOperationDao, store *package_store.MinIOStore, batch int, limits service.ZipLimits, tempRoot string) error {
	for i := 0; i < batch; i++ {
		job, err := jobs.ClaimDueSkillMigrationJob(time.Now(), 5*time.Minute)
		if err != nil || job == nil {
			return err
		}
		if job.SkillID == nil {
			err := errors.New("migration job has no Skill ID")
			_ = jobs.RetrySkillOperationJob(job.ID, job.LeaseToken, err.Error(), time.Now().Add(time.Minute))
			continue
		}
		skill, err := dao.GetSkillByID(*job.SkillID)
		if err != nil {
			_ = jobs.RetrySkillOperationJob(job.ID, job.LeaseToken, err.Error(), time.Now().Add(time.Minute))
			continue
		}
		if skill == nil {
			// Deletion is terminal for a migration target.  Retrying a job whose
			// Hub row no longer exists would leave an immortal outbox entry.
			if completeErr := jobs.CompleteSkillOperationJob(job.ID, job.LeaseToken); completeErr != nil {
				return completeErr
			}
			continue
		}
		if skill.Status == model.SkillStatusDeleting {
			// Deletion is the terminal owner of the Hub row.  The delete
			// transaction cancels this job; do not let a previously claimed
			// migration resurrect the row or rewrite its metadata.
			_ = jobs.CompleteSkillOperationJob(job.ID, job.LeaseToken)
			continue
		}
		if skill.ObjectKey != "" && skill.StorageType == model.SkillStorageMinIO {
			// The metadata transaction may have committed immediately before a
			// process crash.  The durable job is then still pending/running, but
			// there is nothing left to migrate; close it idempotently instead of
			// trying to read a BLOB that may already have been cleared.
			if completeErr := jobs.CompleteSkillOperationJob(job.ID, job.LeaseToken); completeErr != nil {
				return completeErr
			}
			continue
		}
		prepared, prepareErr := dao.PrepareSkillMigrationJob(job.ID, job.LeaseToken, *job.SkillID)
		if prepareErr != nil {
			_ = jobs.RetrySkillOperationJob(job.ID, job.LeaseToken, prepareErr.Error(), time.Now().Add(time.Minute))
			continue
		}
		if !prepared {
			// The lease was fenced or deletion won the row lock.  Leave the
			// durable job to its owner; a canceled job is already terminal.
			continue
		}
		err = migrateOne(ctx, dao, store, *skill, false, limits, tempRoot)
		if err != nil {
			_ = jobs.RetrySkillOperationJob(job.ID, job.LeaseToken, err.Error(), time.Now().Add(time.Minute))
			continue
		}
		if updated, getErr := dao.GetSkillByID(*job.SkillID); getErr == nil && updated != nil && updated.ObjectKey != "" {
			if updateErr := jobs.UpdateSkillOperationObjectKey(job.ID, job.LeaseToken, updated.ObjectKey); updateErr != nil {
				_ = jobs.RetrySkillOperationJob(job.ID, job.LeaseToken, updateErr.Error(), time.Now().Add(time.Minute))
				continue
			}
		}
		if err := jobs.CompleteSkillOperationJob(job.ID, job.LeaseToken); err != nil {
			return err
		}
	}
	return nil
}

func migrateOne(ctx context.Context, dao *gormdao.SkillDao, store *package_store.MinIOStore, skill model.SkillHub, dryRun bool, limits service.ZipLimits, tempRoot string) (err error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	normalizedName, nameErr := service.NormalizeSkillName(skill.Name)
	if nameErr != nil || normalizedName != skill.Name {
		return fmt.Errorf("Skill name is not a canonical safe object-key name: %q", skill.Name)
	}
	if !dryRun {
		defer func() {
			if err != nil {
				if restoreErr := dao.RestoreSkillMigrationStatus(skill.ID); restoreErr != nil {
					slog.Error("restore Skill status after migration failure", "skill", skill.Name, "error", restoreErr)
				}
			}
		}()
	}
	content, err := dao.GetSkillContentByIDLimited(skill.ID, limits.PackageSize)
	if err != nil {
		return err
	}
	if len(content) == 0 {
		return errors.New("skill has empty Content")
	}
	metadata, tmpDir, validateErr := service.ValidateZipReaderAtContextWithLimits(
		ctx, bytes.NewReader(content), int64(len(content)), tempRoot, limits,
	)
	if tmpDir != "" {
		defer os.RemoveAll(tmpDir)
	}
	if validateErr != nil || metadata == nil || !metadata.Valid {
		if metadata != nil && len(metadata.Errors) > 0 {
			return errors.New(strings.Join(metadata.Errors, "; "))
		}
		return errors.New("invalid Skill ZIP")
	}
	if metadata.Name != skill.Name {
		return fmt.Errorf("SKILL.md name %q does not match row name %q", metadata.Name, skill.Name)
	}
	canonical, err := service.PackValidatedSkillDirInRootContextWithLimit(ctx, skill.Name, tmpDir, tempRoot, limits.PackageSize)
	if err != nil {
		return err
	}
	if int64(len(canonical)) > limits.PackageSize {
		return fmt.Errorf("canonical Skill package exceeds configured limit")
	}
	if err := service.ValidateCanonicalSkillPackageContext(ctx, bytes.NewReader(canonical), int64(len(canonical)), tempRoot, skill.Name, limits); err != nil {
		return err
	}
	hash := sha256.Sum256(canonical)
	sha := hex.EncodeToString(hash[:])
	key := "skills/" + skill.Name + "/" + sha + ".zip"
	if dryRun {
		slog.Info("would upload Skill object", "key", key, "size", len(canonical), "sha256", sha)
		return nil
	}
	requestCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := store.Put(requestCtx, key, bytes.NewReader(canonical), int64(len(canonical)), sha); err != nil {
		return err
	}
	verifiedInfo, err := store.Stat(requestCtx, key)
	if err != nil {
		return err
	}
	if verifiedInfo.Size != int64(len(canonical)) {
		return fmt.Errorf("migrated object size mismatch")
	}
	verified, err := readObject(requestCtx, store, key, verifiedInfo.Size, limits.PackageSize)
	if err != nil {
		return err
	}
	if !strings.EqualFold(hashBytes(verified), sha) {
		return fmt.Errorf("migrated object SHA-256 mismatch")
	}
	return dao.UpdateMigratedSkillMetadata(skill.ID, canonical, key, sha, int64(len(canonical)), metadata.Files, metadata.ContainsExecutable, metadata.ContainsBinary)
}

func verifyMinIO(ctx context.Context, dao *gormdao.SkillDao, store *package_store.MinIOStore, f flags, afterID uint, limits service.ZipLimits) error {
	for {
		skills, err := dao.ListMinIOSkillMetadataAfter(afterID, f.batch)
		if err != nil {
			return err
		}
		if len(skills) == 0 {
			return nil
		}
		for _, skill := range skills {
			afterID = skill.ID
			if f.name != "" && skill.Name != f.name {
				continue
			}
			if skill.Status == model.SkillStatusDeleting {
				// Deletion owns the object lifecycle; a concurrent delete must not
				// make verification or rollback treat its disappearing object as a
				// storage fault.
				continue
			}
			callCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			info, err := store.Stat(callCtx, skill.ObjectKey)
			if err != nil {
				cancel()
				return fmt.Errorf("%s: %w", skill.Name, err)
			}
			if skill.PackageSize > 0 && info.Size != skill.PackageSize {
				cancel()
				return fmt.Errorf("%s: object size mismatch", skill.Name)
			}
			data, err := readObject(callCtx, store, skill.ObjectKey, info.Size, limits.PackageSize)
			cancel()
			if err != nil {
				return err
			}
			actual := hashBytes(data)
			if skill.SHA256 == "" || !strings.EqualFold(skill.SHA256, actual) {
				return fmt.Errorf("%s: object SHA-256 mismatch", skill.Name)
			}
		}
		if f.resume && !f.dryRun {
			writeCursor(f.cursorFile, afterID)
		}
		if len(skills) < f.batch {
			return nil
		}
	}
}

func reverseToDB(ctx context.Context, dao *gormdao.SkillDao, store *package_store.MinIOStore, f flags, afterID uint, limits service.ZipLimits) error {
	for {
		skills, err := dao.ListMinIOSkillMetadataAfter(afterID, f.batch)
		if err != nil {
			return err
		}
		if len(skills) == 0 {
			return nil
		}
		for _, skill := range skills {
			afterID = skill.ID
			if f.name != "" && f.name != skill.Name {
				continue
			}
			if skill.Status == model.SkillStatusDeleting || skill.Status == model.SkillStatusMigrating {
				continue
			}
			callCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			info, err := store.Stat(callCtx, skill.ObjectKey)
			if err != nil {
				cancel()
				return err
			}
			data, err := readObject(callCtx, store, skill.ObjectKey, info.Size, limits.PackageSize)
			cancel()
			if err != nil {
				return err
			}
			actual := hashBytes(data)
			if skill.SHA256 == "" || !strings.EqualFold(skill.SHA256, actual) {
				return fmt.Errorf("%s: refusing reverse write with hash mismatch", skill.Name)
			}
			if !f.dryRun {
				existing, existingErr := dao.GetSkillContentByIDLimited(skill.ID, limits.PackageSize)
				if existingErr != nil {
					return existingErr
				}
				if len(existing) > 0 && !strings.EqualFold(hashBytes(existing), actual) {
					return fmt.Errorf("%s: refusing to overwrite a different existing BLOB", skill.Name)
				}
				if err := dao.RestoreSkillContentFromMinIOWithAudit(skill.ID, data, actual, int64(len(data)), model.SkillAuditEvent{
					Action: "reverse_to_db", Outcome: "success", SkillID: &skill.ID, SkillName: skill.Name,
					ObjectKey: skill.ObjectKey, SHA256: actual,
				}); err != nil {
					return err
				}
			}
		}
		if f.resume && !f.dryRun {
			writeCursor(f.cursorFile, afterID)
		}
		if len(skills) < f.batch {
			return nil
		}
	}
}

// clearMigratedContent is the final, explicitly acknowledged migration gate.
// Every row is checked against the authoritative MinIO bytes immediately
// before clearing its legacy BLOB, and the DAO update is conditional on the
// row still being a ready MinIO Skill.  A cursor makes the operation resumable
// without loading a batch of BLOBs into memory.
func clearMigratedContent(ctx context.Context, dao *gormdao.SkillDao, store *package_store.MinIOStore, f flags, afterID uint, limits service.ZipLimits) error {
	if store == nil {
		return errors.New("MinIO store is required for Content cleanup")
	}
	for {
		skills, err := dao.ListMinIOSkillMetadataAfter(afterID, f.batch)
		if err != nil {
			return err
		}
		if len(skills) == 0 {
			return nil
		}
		for _, skill := range skills {
			afterID = skill.ID
			if f.name != "" && f.name != skill.Name {
				continue
			}
			if skill.Status != model.SkillStatusReady || skill.ObjectKey == "" || skill.SHA256 == "" {
				continue
			}
			content, err := dao.GetSkillContentByIDLimited(skill.ID, limits.PackageSize)
			if err != nil {
				return err
			}
			if len(content) == 0 {
				continue
			}
			if skill.PackageSize > 0 && int64(len(content)) != skill.PackageSize {
				return fmt.Errorf("%s: refusing to clear shadow BLOB with size mismatch", skill.Name)
			}
			if !strings.EqualFold(hashBytes(content), skill.SHA256) {
				return fmt.Errorf("%s: refusing to clear shadow BLOB with hash mismatch", skill.Name)
			}
			callCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			info, err := store.Stat(callCtx, skill.ObjectKey)
			if err == nil {
				if skill.PackageSize > 0 && info.Size != skill.PackageSize {
					err = fmt.Errorf("%s: MinIO object size mismatch", skill.Name)
				} else {
					data, readErr := readObject(callCtx, store, skill.ObjectKey, info.Size, limits.PackageSize)
					if readErr != nil {
						err = readErr
					} else if !strings.EqualFold(hashBytes(data), skill.SHA256) {
						err = fmt.Errorf("%s: MinIO object SHA-256 mismatch", skill.Name)
					}
				}
			}
			cancel()
			if err != nil {
				return err
			}
			if f.dryRun {
				slog.Info("would clear migrated Skill BLOB", "skill", skill.Name, "id", skill.ID, "object_key", skill.ObjectKey)
				continue
			}
			cleared, err := dao.ClearMigratedSkillContentWithAudit(skill.ID, skill.SHA256, model.SkillAuditEvent{
				Action: "clear_content", Outcome: "success", SkillID: &skill.ID,
				SkillName: skill.Name, ObjectKey: skill.ObjectKey, SHA256: skill.SHA256,
			})
			if err != nil {
				return err
			}
			if cleared {
				slog.Info("cleared migrated Skill BLOB", "skill", skill.Name, "id", skill.ID, "object_key", skill.ObjectKey, "sha256", skill.SHA256)
			}
		}
		if f.resume && !f.dryRun {
			writeCursor(f.cursorFile, afterID)
		}
		if len(skills) < f.batch {
			return nil
		}
	}
}

func readObject(ctx context.Context, store *package_store.MinIOStore, key string, size, packageLimit int64) ([]byte, error) {
	if packageLimit <= 0 {
		packageLimit = service.MaxPackageSize
	}
	if packageLimit == 1<<63-1 || size < 0 || size > packageLimit {
		return nil, errors.New("object exceeds configured package limit")
	}
	rc, err := store.Open(ctx, key)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, packageLimit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != size || int64(len(data)) > packageLimit {
		return nil, errors.New("object size mismatch")
	}
	return data, nil
}

func migrationZipLimits(cfg conf.Config) (service.ZipLimits, error) {
	limits := service.DefaultZipLimits()
	parse := func(name, raw string, target *int64) error {
		if strings.TrimSpace(raw) == "" {
			return nil
		}
		value, err := conf.ParseByteSize(raw)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		*target = value
		return nil
	}
	if err := parse("max_upload_size", cfg.SkillStorage.MaxUploadSize, &limits.UploadSize); err != nil {
		return service.ZipLimits{}, err
	}
	if err := parse("max_package_size", cfg.SkillStorage.MaxPackageSize, &limits.PackageSize); err != nil {
		return service.ZipLimits{}, err
	}
	if err := parse("max_file_size", cfg.SkillStorage.MaxFileSize, &limits.FileSize); err != nil {
		return service.ZipLimits{}, err
	}
	if err := parse("max_unpacked_size", cfg.SkillStorage.MaxUnpackedSize, &limits.UnpackedSize); err != nil {
		return service.ZipLimits{}, err
	}
	if cfg.SkillStorage.MaxCompressionRatio > 0 {
		limits.CompressionRatio = cfg.SkillStorage.MaxCompressionRatio
	}
	if cfg.SkillStorage.MaxFileCount > 0 {
		limits.FileCount = cfg.SkillStorage.MaxFileCount
	}
	return service.NormalizeZipLimits(limits), nil
}

func hashBytes(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func readCursor(path string) uint {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	value, _ := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	return uint(value)
}

func writeCursor(path string, id uint) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".skill-migrate-cursor-*")
	if err == nil {
		_ = tmp.Chmod(0o600)
		_, err = tmp.WriteString(strconv.FormatUint(uint64(id), 10))
		if syncErr := tmp.Sync(); err == nil {
			err = syncErr
		}
		if closeErr := tmp.Close(); err == nil {
			err = closeErr
		}
		if err == nil {
			err = os.Rename(tmp.Name(), path)
		} else {
			_ = os.Remove(tmp.Name())
		}
	}
	if err != nil {
		slog.Warn("write migration cursor failed", "path", path, "error", err)
	}
}

func fatal(format string, args ...interface{}) {
	slog.Error(fmt.Sprintf(format, args...))
	os.Exit(1)
}
