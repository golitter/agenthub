package gormdao

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	daoiface "agenthub/backend/internal/dao"
	"agenthub/backend/internal/model"
	"agenthub/backend/pkg/db"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type SkillDao struct{}

const skillMetadataColumns = "id, name, builtin, description, file_count, total_size, object_key, sha256, package_size, storage_type, status, uploaded_by, files_json, contains_executable, contains_binary, created_at, updated_at"

func NewSkillDao() *SkillDao {
	return &SkillDao{}
}

func (dao *SkillDao) CountBuiltinByName(name string) (int64, error) {
	var count int64
	if err := db.GetDB().Model(&model.SkillHub{}).Where("name = ? AND builtin = ?", name, true).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (dao *SkillDao) CountByName(name string) (int64, error) {
	var count int64
	if err := db.GetDB().Model(&model.SkillHub{}).Where("name = ?", name).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (dao *SkillDao) CreateSkill(skill model.SkillHub) error {
	return db.GetDB().Create(&skill).Error
}

func (dao *SkillDao) ListSkills() ([]model.SkillHub, error) {
	var skills []model.SkillHub
	if err := db.GetDB().Select(skillMetadataColumns).Order("builtin DESC, name ASC").Find(&skills).Error; err != nil {
		return nil, err
	}
	return skills, nil
}

func (dao *SkillDao) CountImportsBySkillName(name string) (int64, error) {
	var count int64
	if err := db.GetDB().Model(&model.AgentSkill{}).Where("skill_name = ?", name).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (dao *SkillDao) GetSkillByName(name string) (*model.SkillHub, error) {
	var skill model.SkillHub
	if err := db.GetDB().Select(skillMetadataColumns).Where("name = ?", name).First(&skill).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &skill, nil
}

func (dao *SkillDao) GetSkillByID(id uint) (*model.SkillHub, error) {
	var skill model.SkillHub
	if err := db.GetDB().Select(skillMetadataColumns).Where("id = ?", id).First(&skill).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &skill, nil
}

func (dao *SkillDao) UpdateSkillStatus(name, status string) error {
	return db.GetDB().Model(&model.SkillHub{}).Where("name = ?", name).Update("status", status).Error
}

// UpdateSkillStatusIfNotDeleting is used by background verification/repair.
// Lifecycle transitions own a row once it reaches deleting or migrating; a
// late read failure must not move that row back to storage_error or ready.
func (dao *SkillDao) UpdateSkillStatusIfNotDeleting(name, status string) error {
	return db.GetDB().Model(&model.SkillHub{}).
		Where("name = ? AND (status NOT IN ? OR status IS NULL)", name, []string{model.SkillStatusDeleting, model.SkillStatusMigrating}).
		Update("status", status).Error
}

// UpdateSkillStatusIfNotDeletingWithAudit fences a background status repair
// to the Hub row identity and commits its audit record in the same
// transaction.  Re-running the same repair after the row already has the
// target status is a no-op, so reconciliation remains idempotent without
// emitting an unbounded stream of duplicate audit rows.
func (dao *SkillDao) UpdateSkillStatusIfNotDeletingWithAudit(id uint, status string, event model.SkillAuditEvent) error {
	return db.GetDB().Transaction(func(tx *gorm.DB) error {
		var skill model.SkillHub
		if err := tx.Select("id, name, status").Where("id = ?", id).First(&skill).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if skill.Status == model.SkillStatusDeleting || skill.Status == model.SkillStatusMigrating || skill.Status == status {
			return nil
		}
		result := tx.Model(&model.SkillHub{}).
			Where("id = ? AND (status NOT IN ? OR status IS NULL)", id, []string{model.SkillStatusDeleting, model.SkillStatusMigrating}).
			Update("status", status)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		if event.Action == "" {
			event.Action = "reconcile"
		}
		if event.Outcome == "" {
			event.Outcome = status
		}
		if event.SkillID == nil {
			event.SkillID = &skill.ID
		}
		if event.SkillName == "" {
			event.SkillName = skill.Name
		}
		if event.CreatedAt.IsZero() {
			event.CreatedAt = time.Now()
		}
		return tx.Create(&event).Error
	})
}

// ClaimSkillMigrationJob atomically creates/reuses the durable migration job,
// leases it, and moves the Hub row into migrating.  The row lock also makes a
// concurrent delete/import observe a single coherent state instead of a job
// and status transition split across separate transactions.
func (dao *SkillDao) ClaimSkillMigrationJob(skillID uint, skillName string, now time.Time, lease time.Duration) (*model.SkillOperationJob, error) {
	var claimed *model.SkillOperationJob
	err := db.GetDB().Transaction(func(tx *gorm.DB) error {
		var skill model.SkillHub
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id, name, builtin, object_key, storage_type, status").Where("id = ?", skillID).First(&skill).Error; err != nil {
			return err
		}
		if skill.Builtin || (skillName != "" && skill.Name != skillName) {
			return errors.New("builtin or mismatched Skill cannot be migrated")
		}
		if skill.ObjectKey != "" && skill.StorageType == model.SkillStorageMinIO {
			// Already migrated; the caller can treat this as an idempotent no-op.
			return nil
		}
		if skill.Status != "" && skill.Status != model.SkillStatusReady && skill.Status != model.SkillStatusStorageError && skill.Status != model.SkillStatusMigrating {
			return errors.New("Skill is not in a migratable state")
		}
		job, err := createSkillOperationJob(tx, model.SkillOperationJob{
			Operation: model.SkillOperationMigrate, IdempotencyKey: "migrate:" + fmt.Sprint(skill.ID),
			SkillID: &skill.ID, SkillName: skill.Name,
		})
		if err != nil {
			return err
		}
		if job.Status == model.SkillJobStatusDone {
			// A done job with a non-MinIO row is inconsistent (for example after
			// a manual rollback).  Reopen it under the same lock so the next run
			// repairs the row instead of silently skipping it.
			if err := tx.Model(&model.SkillOperationJob{}).Where("id = ?", job.ID).
				Updates(map[string]interface{}{"status": model.SkillJobStatusPending, "lease_until": nil, "lease_token": "", "next_retry_at": nil}).Error; err != nil {
				return err
			}
			job.Status = model.SkillJobStatusPending
		}
		if job.Status == model.SkillJobStatusRunning && job.LeaseUntil != nil && job.LeaseUntil.After(now) {
			return nil
		}
		if err := tx.Model(&model.SkillHub{}).Where("id = ? AND status IN ?", skill.ID, []string{model.SkillStatusReady, model.SkillStatusStorageError, model.SkillStatusMigrating}).Update("status", model.SkillStatusMigrating).Error; err != nil {
			return err
		}
		return claimLockedJob(tx, job, now, lease, &claimed)
	})
	return claimed, err
}

// PrepareSkillMigrationJob revalidates a claimed migration job immediately
// before reading the legacy BLOB. Deletion cancels migration jobs while
// holding the same Skill row lock; this second fence prevents a worker that
// was already claimed before that transaction from resurrecting the row.
func (dao *SkillDao) PrepareSkillMigrationJob(jobID uint64, leaseToken string, skillID uint) (bool, error) {
	prepared := false
	err := db.GetDB().Transaction(func(tx *gorm.DB) error {
		var job model.SkillOperationJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", jobID).First(&job).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if job.Status != model.SkillJobStatusRunning || (leaseToken != "" && job.LeaseToken != leaseToken) {
			return nil
		}
		var skill model.SkillHub
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id, builtin, status").Where("id = ?", skillID).First(&skill).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if skill.Builtin || skill.Status == model.SkillStatusDeleting {
			return nil
		}
		result := tx.Model(&model.SkillHub{}).
			Where("id = ? AND status IN ?", skillID, []string{model.SkillStatusReady, model.SkillStatusStorageError, model.SkillStatusMigrating}).
			Update("status", model.SkillStatusMigrating)
		if result.Error != nil {
			return result.Error
		}
		prepared = result.RowsAffected > 0
		return nil
	})
	return prepared, err
}

// RestoreSkillMigrationStatus is fenced to migrating rows so a late failure
// cannot turn a concurrently deleting Skill back into ready.
func (dao *SkillDao) RestoreSkillMigrationStatus(id uint) error {
	return db.GetDB().Model(&model.SkillHub{}).
		Where("id = ? AND status = ? AND (object_key = '' OR object_key IS NULL)", id, model.SkillStatusMigrating).
		Updates(map[string]interface{}{
			"storage_type": model.SkillStorageDB,
			"status":       model.SkillStatusReady,
		}).Error
}

func (dao *SkillDao) GetSkillUploadReceipt(uploadID string) (*model.SkillUploadReceipt, error) {
	var receipt model.SkillUploadReceipt
	if err := db.GetDB().Where("upload_id = ?", uploadID).First(&receipt).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &receipt, nil
}

func (dao *SkillDao) CreateSkillUploadReceipt(receipt model.SkillUploadReceipt) error {
	return db.GetDB().Create(&receipt).Error
}

func (dao *SkillDao) CleanupSkillUploadReceipts(before time.Time, batch int) (int64, error) {
	if batch <= 0 {
		batch = 500
	}
	var receipts []model.SkillUploadReceipt
	if err := db.GetDB().Where("created_at < ?", before).Order("created_at ASC").Limit(batch).Find(&receipts).Error; err != nil {
		return 0, err
	}
	if len(receipts) == 0 {
		return 0, nil
	}
	ids := make([]string, 0, len(receipts))
	for _, receipt := range receipts {
		ids = append(ids, receipt.UploadID)
	}
	result := db.GetDB().Where("upload_id IN ?", ids).Delete(&model.SkillUploadReceipt{})
	return result.RowsAffected, result.Error
}

func (dao *SkillDao) ListSkillUploadReceiptsAfter(afterID string, batch int) ([]model.SkillUploadReceipt, error) {
	if batch <= 0 {
		batch = 100
	}
	var receipts []model.SkillUploadReceipt
	if err := db.GetDB().Where("upload_id > ?", afterID).Order("upload_id ASC").Limit(batch).Find(&receipts).Error; err != nil {
		return nil, err
	}
	return receipts, nil
}

// CreateSkillWithReceipt commits the Skill metadata and its upload receipt in
// one transaction.  The receipt is the durable idempotency boundary used when
// Redis loses the short-lived upload session.
func (dao *SkillDao) CreateSkillWithReceipt(skill model.SkillHub, receipt model.SkillUploadReceipt) (*model.SkillHub, error) {
	if err := db.GetDB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&skill).Error; err != nil {
			return err
		}
		receipt.SkillID = skill.ID
		return tx.Create(&receipt).Error
	}); err != nil {
		return nil, err
	}
	return &skill, nil
}

// CreateSkillWithReceiptAndAudit is the authoritative MinIO confirmation
// transaction.  A successful confirmation is not visible unless its Hub
// metadata, receipt idempotency row, and provenance audit event all commit.
func (dao *SkillDao) CreateSkillWithReceiptAndAudit(skill model.SkillHub, receipt model.SkillUploadReceipt, event model.SkillAuditEvent) (*model.SkillHub, error) {
	if err := db.GetDB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&skill).Error; err != nil {
			return err
		}
		receipt.SkillID = skill.ID
		if err := tx.Create(&receipt).Error; err != nil {
			return err
		}
		if event.Action == "" {
			event.Action = "confirm"
		}
		if event.Outcome == "" {
			event.Outcome = "success"
		}
		if event.SkillID == nil {
			event.SkillID = &skill.ID
		}
		if event.SkillName == "" {
			event.SkillName = skill.Name
		}
		if event.CreatedAt.IsZero() {
			event.CreatedAt = time.Now()
		}
		return tx.Create(&event).Error
	}); err != nil {
		return nil, err
	}
	return &skill, nil
}

func (dao *SkillDao) CreateReceiptForExistingSkill(name string, receipt model.SkillUploadReceipt) (*model.SkillHub, error) {
	var skill model.SkillHub
	if err := db.GetDB().Transaction(func(tx *gorm.DB) error {
		// Receipt reuse participates in the same Hub-row lock as deletion.  A
		// duplicate confirmation must not create a receipt for a Skill that is
		// concurrently transitioning to deleting and losing its object.
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select(skillMetadataColumns).Where("name = ?", name).First(&skill).Error; err != nil {
			return err
		}
		if skill.Builtin || (skill.Status != "" && skill.Status != model.SkillStatusReady) || skill.SHA256 == "" || !strings.EqualFold(skill.SHA256, receipt.SHA256) {
			return errors.New("existing Skill object hash does not match upload")
		}
		receipt.SkillID = skill.ID
		return tx.Create(&receipt).Error
	}); err != nil {
		return nil, err
	}
	return &skill, nil
}

// CreateReceiptForExistingSkillAndAudit applies the duplicate-name/hash
// confirmation path under the same Hub row lock as deletion and records its
// audit event in that transaction.
func (dao *SkillDao) CreateReceiptForExistingSkillAndAudit(name string, receipt model.SkillUploadReceipt, event model.SkillAuditEvent) (*model.SkillHub, error) {
	var skill model.SkillHub
	if err := db.GetDB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select(skillMetadataColumns).Where("name = ?", name).First(&skill).Error; err != nil {
			return err
		}
		if skill.Builtin || (skill.Status != "" && skill.Status != model.SkillStatusReady) || skill.SHA256 == "" || !strings.EqualFold(skill.SHA256, receipt.SHA256) {
			return errors.New("existing Skill object hash does not match upload")
		}
		receipt.SkillID = skill.ID
		if err := tx.Create(&receipt).Error; err != nil {
			return err
		}
		if event.Action == "" {
			event.Action = "confirm"
		}
		if event.Outcome == "" {
			event.Outcome = "success"
		}
		if event.SkillID == nil {
			event.SkillID = &skill.ID
		}
		if event.SkillName == "" {
			event.SkillName = skill.Name
		}
		if event.CreatedAt.IsZero() {
			event.CreatedAt = time.Now()
		}
		return tx.Create(&event).Error
	}); err != nil {
		return nil, err
	}
	return &skill, nil
}

// CreateSkillAndAudit is used by the legacy DB-BLOB confirmation bridge.  It
// keeps that compatibility path subject to the same provenance requirement
// without forcing the MinIO-only receipt schema onto old requests.
func (dao *SkillDao) CreateSkillAndAudit(skill model.SkillHub, event model.SkillAuditEvent) error {
	return db.GetDB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&skill).Error; err != nil {
			return err
		}
		if event.Action == "" {
			event.Action = "confirm"
		}
		if event.Outcome == "" {
			event.Outcome = "success"
		}
		if event.SkillID == nil {
			event.SkillID = &skill.ID
		}
		if event.SkillName == "" {
			event.SkillName = skill.Name
		}
		if event.CreatedAt.IsZero() {
			event.CreatedAt = time.Now()
		}
		return tx.Create(&event).Error
	})
}

func (dao *SkillDao) GetSkillContent(name string) ([]byte, error) {
	var skill model.SkillHub
	if err := db.GetDB().Select("content").Where("name = ?", name).First(&skill).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return skill.Content, nil
}

// GetSkillContentLimited performs the same bounded preflight for name-based
// reads used by the import path.  It prevents a corrupt legacy BLOB from
// being materialized before the configured package limit is enforced.
func (dao *SkillDao) GetSkillContentLimited(name string, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 || maxBytes == 1<<63-1 {
		return nil, fmt.Errorf("invalid Skill content limit")
	}
	// Read at most maxBytes+1 bytes in the same statement that fetches the
	// payload.  A separate length preflight followed by SELECT content has a
	// TOCTOU window: a concurrent update could grow the BLOB between the two
	// queries and defeat the memory bound.  The prefix is read through the
	// model's []byte Content field: scanning a raw LEFT() expression into a
	// bare []byte variable makes database/sql try to assign the whole BLOB to a
	// single uint8 element ("converting []uint8 to uint8"), so alias the
	// projection back onto the content column GORM already knows how to scan.
	var skill model.SkillHub
	err := db.GetDB().Model(&model.SkillHub{}).
		Where("name = ?", name).
		Select("LEFT(content, ?) AS content", maxBytes+1).
		Take(&skill).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if int64(len(skill.Content)) > maxBytes {
		return nil, fmt.Errorf("Skill content exceeds configured package limit: %d > %d", len(skill.Content), maxBytes)
	}
	return skill.Content, nil
}

func (dao *SkillDao) GetSkillContentByID(id uint) ([]byte, error) {
	var skill model.SkillHub
	if err := db.GetDB().Select("content").Where("id = ?", id).First(&skill).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return skill.Content, nil
}

// GetSkillContentByIDLimited reads at most maxBytes+1 bytes in the same
// statement that fetches the payload. Historical migration must remain
// bounded even when a legacy row contains a corrupt or unexpectedly huge
// payload; a separate length preflight followed by SELECT content would have
// a TOCTOU window in which a concurrent update could grow the BLOB.
func (dao *SkillDao) GetSkillContentByIDLimited(id uint, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 || maxBytes == 1<<63-1 {
		return nil, fmt.Errorf("invalid Skill content limit")
	}
	// See GetSkillContentLimited: the LEFT() prefix must be scanned through the
	// model's []byte Content field (aliased back to the content column) because
	// scanning a raw expression into a bare []byte makes database/sql target a
	// single uint8 element.
	var skill model.SkillHub
	err := db.GetDB().Model(&model.SkillHub{}).
		Where("id = ?", id).
		Select("LEFT(content, ?) AS content", maxBytes+1).
		Take(&skill).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if int64(len(skill.Content)) > maxBytes {
		return nil, fmt.Errorf("Skill content exceeds configured package limit: %d > %d", len(skill.Content), maxBytes)
	}
	return skill.Content, nil
}

func (dao *SkillDao) ListExternalSkillMetadataAfter(afterID uint, batchSize int, name string) ([]model.SkillHub, error) {
	query := db.GetDB().Select(skillMetadataColumns).
		Where("builtin = ? AND id > ? AND (object_key = '' OR object_key IS NULL) AND content IS NOT NULL", false, afterID).
		Where("status <> ? OR status IS NULL", model.SkillStatusDeleting).
		Order("id ASC").Limit(batchSize)
	if name != "" {
		query = query.Where("name = ?", name)
	}
	var skills []model.SkillHub
	if err := query.Find(&skills).Error; err != nil {
		return nil, err
	}
	return skills, nil
}

func (dao *SkillDao) ListMinIOSkillMetadataAfter(afterID uint, batchSize int) ([]model.SkillHub, error) {
	var skills []model.SkillHub
	if err := db.GetDB().Select(skillMetadataColumns).Where("builtin = ? AND storage_type = ? AND object_key <> '' AND id > ?", false, model.SkillStorageMinIO, afterID).Order("id ASC").Limit(batchSize).Find(&skills).Error; err != nil {
		return nil, err
	}
	return skills, nil
}

// ListSkillStorageMetadataAfter is the reconciliation projection. It keeps
// rows that either declare MinIO storage or still carry an object key, even
// when the other half of that pair is missing. Such malformed metadata must
// reach the verifier so it can be surfaced as storage_error instead of being
// silently filtered out by the stricter migration projection above.
func (dao *SkillDao) ListSkillStorageMetadataAfter(afterID uint, batchSize int) ([]model.SkillHub, error) {
	var skills []model.SkillHub
	if err := db.GetDB().Select(skillMetadataColumns).
		Where("builtin = ? AND id > ? AND (storage_type = ? OR (object_key IS NOT NULL AND object_key <> ''))", false, afterID, model.SkillStorageMinIO).
		Order("id ASC").Limit(batchSize).Find(&skills).Error; err != nil {
		return nil, err
	}
	return skills, nil
}

// ListMinIOSkillMetadataAfterObjectKey supports bounded object-to-database
// reconciliation without building an in-memory set of the whole Hub.
func (dao *SkillDao) ListMinIOSkillMetadataAfterObjectKey(afterKey string, batchSize int) ([]model.SkillHub, error) {
	if batchSize <= 0 {
		batchSize = 100
	}
	var skills []model.SkillHub
	query := db.GetDB().Select(skillMetadataColumns).
		Where("builtin = ? AND storage_type = ? AND object_key <> '' AND object_key > ?", false, model.SkillStorageMinIO, afterKey).
		Order("object_key ASC").Limit(batchSize)
	if err := query.Find(&skills).Error; err != nil {
		return nil, err
	}
	return skills, nil
}

func (dao *SkillDao) GetSkillByObjectKey(objectKey string) (*model.SkillHub, error) {
	var skill model.SkillHub
	if err := db.GetDB().Select(skillMetadataColumns).Where("object_key = ?", objectKey).First(&skill).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &skill, nil
}

func (dao *SkillDao) UpdateSkillStorageMetadata(id uint, objectKey, sha string, packageSize int64, storageType, status string) error {
	return db.GetDB().Model(&model.SkillHub{}).Where("id = ?", id).Updates(map[string]interface{}{
		"object_key": objectKey, "sha256": sha, "package_size": packageSize,
		"storage_type": storageType, "status": status,
	}).Error
}

func (dao *SkillDao) UpdateSkillManifest(id uint, files []string, containsExecutable, containsBinary bool) error {
	data, err := json.Marshal(files)
	if err != nil {
		return err
	}
	return db.GetDB().Model(&model.SkillHub{}).Where("id = ?", id).Updates(map[string]interface{}{
		"files_json": string(data), "contains_executable": containsExecutable, "contains_binary": containsBinary,
	}).Error
}

// UpdateMigratedSkillMetadata commits the canonical shadow BLOB and all MinIO
// metadata together.  A migration never leaves SHA256 pointing at the
// canonical object while Content still contains a pre-normalized ZIP.
func (dao *SkillDao) UpdateMigratedSkillMetadata(id uint, content []byte, objectKey, sha string, packageSize int64, files []string, containsExecutable, containsBinary bool) error {
	data, err := json.Marshal(files)
	if err != nil {
		return err
	}
	return db.GetDB().Transaction(func(tx *gorm.DB) error {
		var skill model.SkillHub
		if err := tx.Select("name").Where("id = ?", id).First(&skill).Error; err != nil {
			return err
		}
		result := tx.Model(&model.SkillHub{}).Where("id = ? AND status = ?", id, model.SkillStatusMigrating).Updates(map[string]interface{}{
			"content": content, "object_key": objectKey, "sha256": sha, "package_size": packageSize,
			"storage_type": model.SkillStorageMinIO, "status": model.SkillStatusReady,
			"files_json": string(data), "contains_executable": containsExecutable, "contains_binary": containsBinary,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("Skill migration target is no longer in migrating state")
		}
		return tx.Create(&model.SkillAuditEvent{
			Action: "migrate", Outcome: "success", SkillID: &id, SkillName: skill.Name,
			ObjectKey: objectKey, SHA256: sha, CreatedAt: time.Now(),
		}).Error
	})
}

func (dao *SkillDao) CreateSkillAuditEvent(event model.SkillAuditEvent) error {
	return db.GetDB().Create(&event).Error
}

func (dao *SkillDao) UpdateSkillContentAndMetadata(id uint, content []byte, sha string, packageSize int64) error {
	return db.GetDB().Model(&model.SkillHub{}).Where("id = ?", id).Updates(map[string]interface{}{
		"content": content, "sha256": sha, "package_size": packageSize,
	}).Error
}

// RestoreSkillContentFromMinIO fences the reverse-migration write against
// lifecycle transitions.  A reverse command may run while the worker is
// deleting or verifying a row; only a stable MinIO row may receive the
// restored shadow BLOB.  Returning an error on a lost race is intentional so
// the operator can inspect the row instead of silently writing stale content.
func (dao *SkillDao) RestoreSkillContentFromMinIO(id uint, content []byte, sha string, packageSize int64) error {
	return dao.RestoreSkillContentFromMinIOWithAudit(id, content, sha, packageSize, model.SkillAuditEvent{
		Action: "reverse_to_db", Outcome: "success", SkillID: &id, SHA256: sha,
	})
}

// RestoreSkillContentFromMinIOWithAudit makes the reverse-migration write and
// its audit record one transaction. A successful BLOB backfill therefore
// cannot be left without an operator-visible repair record.
func (dao *SkillDao) RestoreSkillContentFromMinIOWithAudit(id uint, content []byte, sha string, packageSize int64, event model.SkillAuditEvent) error {
	return db.GetDB().Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.SkillHub{}).
			Where("id = ? AND builtin = ? AND storage_type = ? AND object_key <> '' AND status IN ?", id, false, model.SkillStorageMinIO, []string{model.SkillStatusReady, model.SkillStatusStorageError}).
			Updates(map[string]interface{}{"content": content, "sha256": sha, "package_size": packageSize})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("Skill reverse target is no longer a stable MinIO row")
		}
		if event.Action == "" {
			event.Action = "reverse_to_db"
		}
		if event.Outcome == "" {
			event.Outcome = "success"
		}
		if event.SkillID == nil {
			event.SkillID = &id
		}
		if event.SHA256 == "" {
			event.SHA256 = sha
		}
		if event.CreatedAt.IsZero() {
			event.CreatedAt = time.Now()
		}
		return tx.Create(&event).Error
	})
}

// ClearMigratedSkillContent removes only a verified MinIO-backed shadow BLOB.
// The predicates make the destructive step safe to retry and prevent it from
// clearing a row that has changed storage or lifecycle state concurrently.
func (dao *SkillDao) ClearMigratedSkillContent(id uint, sha string) (bool, error) {
	result := db.GetDB().Model(&model.SkillHub{}).
		Where("id = ? AND builtin = ? AND storage_type = ? AND object_key <> '' AND status = ? AND sha256 = ? AND content IS NOT NULL",
			id, false, model.SkillStorageMinIO, model.SkillStatusReady, sha).
		Updates(map[string]interface{}{"content": nil})
	return result.RowsAffected > 0, result.Error
}

// ClearMigratedSkillContentWithAudit makes the destructive update and its
// audit record one transaction.  A successful clear therefore cannot be
// left without an audit event merely because the second insert failed.
func (dao *SkillDao) ClearMigratedSkillContentWithAudit(id uint, sha string, event model.SkillAuditEvent) (bool, error) {
	cleared := false
	err := db.GetDB().Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.SkillHub{}).
			Where("id = ? AND builtin = ? AND storage_type = ? AND object_key <> '' AND status = ? AND sha256 = ? AND content IS NOT NULL",
				id, false, model.SkillStorageMinIO, model.SkillStatusReady, sha).
			Updates(map[string]interface{}{"content": nil})
		if result.Error != nil {
			return result.Error
		}
		cleared = result.RowsAffected > 0
		if !cleared {
			return nil
		}
		if event.CreatedAt.IsZero() {
			event.CreatedAt = time.Now()
		}
		return tx.Create(&event).Error
	})
	if err != nil {
		// The transaction rolled the Content update back, so callers must not
		// report a successful clear or advance an operator cursor past it.
		cleared = false
	}
	return cleared, err
}

func (dao *SkillDao) DeleteSkillCascade(name string) error {
	return db.GetDB().Transaction(func(tx *gorm.DB) error {
		var skill model.SkillHub
		if err := tx.Select("id").Where("name = ?", name).First(&skill).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if skill.ID != 0 {
			if err := tx.Where("skill_id = ?", skill.ID).Delete(&model.SkillUploadReceipt{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("skill_name = ?", name).Delete(&model.AgentSkill{}).Error; err != nil {
			return err
		}
		if err := tx.Where("name = ?", name).Delete(&model.SkillHub{}).Error; err != nil {
			return err
		}
		return nil
	})
}

// DeleteSkillCascadeWithOperation closes the final Hub-delete crash window.
// The MinIO object is removed before this call, but that operation is
// idempotent; keeping the row and claimed outbox task in one transaction means
// a database failure leaves a retryable lifecycle record instead of a missing
// task or a half-deleted Skill.
func (dao *SkillDao) DeleteSkillCascadeWithOperation(name string, jobID uint64, leaseToken string, event model.SkillAuditEvent) error {
	return db.GetDB().Transaction(func(tx *gorm.DB) error {
		var job model.SkillOperationJob
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND operation = ? AND status = ?", jobID, model.SkillOperationDeleteObject, model.SkillJobStatusRunning)
		if leaseToken != "" {
			query = query.Where("lease_token = ?", leaseToken)
		}
		if err := query.First(&job).Error; err != nil {
			return err
		}
		if job.SkillName != "" && job.SkillName != name {
			return errors.New("Skill delete operation identity mismatch")
		}

		var skill model.SkillHub
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id, name, builtin, object_key").Where("name = ?", name).First(&skill).Error; err != nil {
			return err
		}
		if skill.Builtin {
			return errors.New("cannot delete builtin Skill")
		}
		if job.SkillID != nil && *job.SkillID != skill.ID {
			return errors.New("Skill delete operation Skill ID mismatch")
		}
		if job.ObjectKey != "" && job.ObjectKey != skill.ObjectKey {
			return errors.New("Skill delete operation object key mismatch")
		}
		if err := tx.Where("skill_id = ?", skill.ID).Delete(&model.SkillUploadReceipt{}).Error; err != nil {
			return err
		}
		if err := tx.Where("skill_name = ?", name).Delete(&model.AgentSkill{}).Error; err != nil {
			return err
		}
		if err := tx.Where("id = ?", skill.ID).Delete(&model.SkillHub{}).Error; err != nil {
			return err
		}
		if event.Action == "" {
			event.Action = "delete"
		}
		if event.Outcome == "" {
			event.Outcome = "success"
		}
		if event.SkillID == nil {
			event.SkillID = &skill.ID
		}
		if event.SkillName == "" {
			event.SkillName = name
		}
		if event.CreatedAt.IsZero() {
			event.CreatedAt = time.Now()
		}
		if err := tx.Create(&event).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", job.ID).Delete(&model.SkillOperationJob{}).Error
	})
}

func (dao *SkillDao) HasAgentSkill(sessionID, skillName string) (bool, error) {
	var count int64
	if err := db.GetDB().Model(&model.AgentSkill{}).Where("session_id = ? AND skill_name = ?", sessionID, skillName).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (dao *SkillDao) GetAgentSkill(sessionID, skillName string) (*model.AgentSkill, error) {
	var skill model.AgentSkill
	if err := db.GetDB().Where("session_id = ? AND skill_name = ?", sessionID, skillName).First(&skill).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &skill, nil
}

// GetAgentSkillByID is used by operation workers to fence retries against a
// later AgentSkill row that happens to reuse the same session/name pair.
func (dao *SkillDao) GetAgentSkillByID(id uint) (*model.AgentSkill, error) {
	var skill model.AgentSkill
	if err := db.GetDB().Where("id = ?", id).First(&skill).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &skill, nil
}

func (dao *SkillDao) CreateAgentSkill(skill model.AgentSkill) error {
	if err := db.GetDB().Create(&skill).Error; err != nil {
		if isDuplicateKeyError(err) {
			return errors.Join(daoiface.ErrDuplicate, err)
		}
		return err
	}
	return nil
}

// CreateAgentSkillAndOperation closes the reservation/outbox crash window for
// production GORM stores.  The request can still execute the returned job
// synchronously, while a worker can reclaim it after a process crash.
func (dao *SkillDao) CreateAgentSkillAndOperation(skill model.AgentSkill, job model.SkillOperationJob) (*model.SkillOperationJob, error) {
	var created *model.SkillOperationJob
	err := db.GetDB().Transaction(func(tx *gorm.DB) error {
		if skillID := skillIDFromJob(job); skillID != 0 {
			var hub model.SkillHub
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id, status, builtin").Where("id = ?", skillID).First(&hub).Error; err != nil {
				return err
			}
			if hub.Builtin || (hub.Status != "" && hub.Status != model.SkillStatusReady) {
				return errors.New("skill is not ready for import")
			}
		}
		if err := tx.Create(&skill).Error; err != nil {
			if isDuplicateKeyError(err) {
				return errors.Join(daoiface.ErrDuplicate, err)
			}
			return err
		}
		job.AgentSkillID = &skill.ID
		var err error
		created, err = createSkillOperationJob(tx, job)
		return err
	})
	return created, err
}

func skillIDFromJob(job model.SkillOperationJob) uint {
	if job.SkillID == nil {
		return 0
	}
	return *job.SkillID
}

// UpdateAgentSkillStatusAndOperation atomically reserves a removal and writes
// its outbox job. Installing rows are intentionally excluded so remove cannot
// race an in-flight import.
func (dao *SkillDao) UpdateAgentSkillStatusAndOperation(sessionID, skillName, status string, job model.SkillOperationJob) (*model.SkillOperationJob, error) {
	var created *model.SkillOperationJob
	err := db.GetDB().Transaction(func(tx *gorm.DB) error {
		var agentSkill model.AgentSkill
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("session_id = ? AND skill_name = ? AND status IN ?", sessionID, skillName,
				[]string{model.AgentSkillStatusReady, model.AgentSkillStatusSyncError}).
			First(&agentSkill).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("agent skill is not in a removable state")
			}
			return err
		}
		if err := tx.Model(&model.AgentSkill{}).Where("id = ?", agentSkill.ID).Update("status", status).Error; err != nil {
			return err
		}
		job.AgentSkillID = &agentSkill.ID
		if agentSkill.ID == 0 {
			return errors.New("agent skill is not in a removable state")
		}
		// A sync_error AgentSkill may still have an install retry waiting in the
		// outbox. Once removal owns the row, fence those retries before the
		// remove job commits; the worker also rechecks the row around AgentEnd
		// calls for an already-running request.
		if err := tx.Model(&model.SkillOperationJob{}).
			Where("session_id = ? AND skill_name = ? AND operation = ? AND status IN ?", sessionID, skillName, model.SkillOperationInstall,
				[]string{model.SkillJobStatusPending, model.SkillJobStatusRunning}).
			Updates(map[string]interface{}{
				"status": model.SkillJobStatusDone, "lease_until": nil, "lease_token": "",
				"next_retry_at": nil, "last_error": "canceled by Skill removal",
			}).Error; err != nil {
			return err
		}
		var err error
		created, err = createSkillOperationJob(tx, job)
		return err
	})
	return created, err
}

// UpdateSkillStatusAndOperation atomically transitions a Hub row to deleting
// and creates the durable object-delete job.
func (dao *SkillDao) UpdateSkillStatusAndOperation(name, status string, job model.SkillOperationJob) (*model.SkillOperationJob, error) {
	var created *model.SkillOperationJob
	err := db.GetDB().Transaction(func(tx *gorm.DB) error {
		// Lock the Hub row before checking references.  Import reservations use
		// the same row lock in CreateAgentSkillAndOperation; without this lock a
		// concurrent import could be inserted after the reference count and
		// before the deleting transition, leaving a live Session pointing at a
		// Skill whose object is already being removed.
		var hub model.SkillHub
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id, builtin, status").Where("name = ?", name).First(&hub).Error; err != nil {
			return err
		}
		if hub.Builtin || (hub.Status != "" && hub.Status != model.SkillStatusReady && hub.Status != model.SkillStatusStorageError) {
			return errors.New("skill is not in a deletable state")
		}
		var imports int64
		if err := tx.Model(&model.AgentSkill{}).Where("skill_name = ?", name).Count(&imports).Error; err != nil {
			return err
		}
		if imports > 0 {
			return errors.New("skill has active session references")
		}
		// A storage_error row may have a pending object verification or migration
		// repair.  Cancel those jobs under the same Hub row lock before creating
		// the delete task; otherwise a late repair worker could resurrect the row
		// to ready after deletion has begun.  The worker also fences its final
		// status update against the deleting state.
		if err := tx.Model(&model.SkillOperationJob{}).
			Where("skill_id = ? AND operation IN ? AND status IN ?", hub.ID,
				[]string{model.SkillOperationVerifyObject, model.SkillOperationMigrate},
				[]string{model.SkillJobStatusPending, model.SkillJobStatusRunning}).
			Updates(map[string]interface{}{
				"status": model.SkillJobStatusDone, "lease_until": nil, "lease_token": "",
				"next_retry_at": nil, "last_error": "canceled by Skill deletion",
			}).Error; err != nil {
			return err
		}
		result := tx.Model(&model.SkillHub{}).
			Where("id = ? AND status IN ?", hub.ID, []string{model.SkillStatusReady, model.SkillStatusStorageError}).
			Update("status", status)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("skill is not in a deletable state")
		}
		var err error
		created, err = createSkillOperationJob(tx, job)
		return err
	})
	return created, err
}

func (dao *SkillDao) UpdateAgentSkillStatus(sessionID, skillName, status string) error {
	return db.GetDB().Model(&model.AgentSkill{}).
		Where("session_id = ? AND skill_name = ?", sessionID, skillName).
		Update("status", status).Error
}

// CompleteAgentSkillInstall is the status fence at the end of an AgentEnd
// install. Removal changes the row to removing in a transaction; a late
// installer must not turn that row back into ready.
func (dao *SkillDao) CompleteAgentSkillInstall(sessionID, skillName string) (bool, error) {
	result := db.GetDB().Model(&model.AgentSkill{}).
		Where("session_id = ? AND skill_name = ? AND status IN ?", sessionID, skillName,
			[]string{model.AgentSkillStatusInstalling, model.AgentSkillStatusSyncError}).
		Update("status", model.AgentSkillStatusReady)
	return result.RowsAffected > 0, result.Error
}

func (dao *SkillDao) CompleteAgentSkillInstallByID(id uint) (bool, error) {
	result := db.GetDB().Model(&model.AgentSkill{}).
		Where("id = ? AND status IN ?", id,
			[]string{model.AgentSkillStatusInstalling, model.AgentSkillStatusSyncError}).
		Update("status", model.AgentSkillStatusReady)
	return result.RowsAffected > 0, result.Error
}

// MarkAgentSkillInstallError is fenced to the install-owned states. A remove
// transaction can move the row to removing while an AgentEnd request is
// still returning; a late install error must not resurrect that reservation
// as sync_error.
func (dao *SkillDao) MarkAgentSkillInstallError(sessionID, skillName string) (bool, error) {
	result := db.GetDB().Model(&model.AgentSkill{}).
		Where("session_id = ? AND skill_name = ? AND status IN ?", sessionID, skillName,
			[]string{model.AgentSkillStatusInstalling, model.AgentSkillStatusSyncError}).
		Update("status", model.AgentSkillStatusSyncError)
	return result.RowsAffected > 0, result.Error
}

func (dao *SkillDao) MarkAgentSkillInstallErrorByID(id uint) (bool, error) {
	result := db.GetDB().Model(&model.AgentSkill{}).
		Where("id = ? AND status IN ?", id,
			[]string{model.AgentSkillStatusInstalling, model.AgentSkillStatusSyncError}).
		Update("status", model.AgentSkillStatusSyncError)
	return result.RowsAffected > 0, result.Error
}

func (dao *SkillDao) UpdateAgentSkillStatusByID(id uint, status string) (bool, error) {
	result := db.GetDB().Model(&model.AgentSkill{}).Where("id = ?", id).Update("status", status)
	return result.RowsAffected > 0, result.Error
}

func (dao *SkillDao) DeleteAgentSkill(sessionID, skillName string) error {
	return db.GetDB().Where("session_id = ? AND skill_name = ?", sessionID, skillName).Delete(&model.AgentSkill{}).Error
}

func (dao *SkillDao) DeleteAgentSkillByID(id uint) error {
	return db.GetDB().Where("id = ?", id).Delete(&model.AgentSkill{}).Error
}

// CompleteAgentSkillRemovalByID atomically removes a completed AgentEnd
// reservation, its claimed remove task, and the success audit event.  The
// operation/job identity is fenced so a late response cannot delete a newer
// reservation for the same session/name pair.
func (dao *SkillDao) CompleteAgentSkillRemovalByID(agentSkillID uint, jobID uint64, leaseToken string, event model.SkillAuditEvent) error {
	return db.GetDB().Transaction(func(tx *gorm.DB) error {
		var job model.SkillOperationJob
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND operation = ? AND status = ?", jobID, model.SkillOperationRemove, model.SkillJobStatusRunning)
		if leaseToken != "" {
			query = query.Where("lease_token = ?", leaseToken)
		}
		if err := query.First(&job).Error; err != nil {
			return err
		}
		if job.AgentSkillID == nil || *job.AgentSkillID != agentSkillID {
			return errors.New("Skill remove operation identity mismatch")
		}
		result := tx.Where("id = ? AND status IN ?", agentSkillID,
			[]string{model.AgentSkillStatusRemoving, model.AgentSkillStatusSyncError}).
			Delete(&model.AgentSkill{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("AgentSkill is not in a removable state")
		}
		if event.Action == "" {
			event.Action = "remove"
		}
		if event.Outcome == "" {
			event.Outcome = "success"
		}
		if event.CreatedAt.IsZero() {
			event.CreatedAt = time.Now()
		}
		if event.SkillName == "" {
			event.SkillName = job.SkillName
		}
		if err := tx.Create(&event).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", job.ID).Delete(&model.SkillOperationJob{}).Error
	})
}

// RollbackAgentSkillInstall removes a reservation and its claimed install
// outbox row in one transaction after AgentEnd has explicitly rejected the
// request. The lease/row identity fence prevents a late HTTP response from
// deleting a newer reservation that reused the same session/name pair.
func (dao *SkillDao) RollbackAgentSkillInstall(agentSkillID uint, jobID uint64, leaseToken string) error {
	return db.GetDB().Transaction(func(tx *gorm.DB) error {
		var job model.SkillOperationJob
		query := tx.Where("id = ? AND status = ?", jobID, model.SkillJobStatusRunning)
		if leaseToken != "" {
			query = query.Where("lease_token = ?", leaseToken)
		}
		if err := query.First(&job).Error; err != nil {
			return err
		}
		if job.Operation != model.SkillOperationInstall || job.AgentSkillID == nil || *job.AgentSkillID != agentSkillID {
			return errors.New("Skill install rollback identity mismatch")
		}
		if err := tx.Where("id = ?", agentSkillID).Delete(&model.AgentSkill{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", jobID).Delete(&model.SkillOperationJob{}).Error
	})
}

// RollbackAgentSkillRemove restores a relation after AgentEnd explicitly
// rejected a removal and removes the claimed outbox row in one transaction.
// The lease/identity fence prevents a stale response from restoring a newer
// reservation that reused the same session/name pair.
func (dao *SkillDao) RollbackAgentSkillRemove(agentSkillID uint, jobID uint64, leaseToken string) error {
	return db.GetDB().Transaction(func(tx *gorm.DB) error {
		var job model.SkillOperationJob
		query := tx.Where("id = ? AND status = ?", jobID, model.SkillJobStatusRunning)
		if leaseToken != "" {
			query = query.Where("lease_token = ?", leaseToken)
		}
		if err := query.First(&job).Error; err != nil {
			return err
		}
		if job.Operation != model.SkillOperationRemove || job.AgentSkillID == nil || *job.AgentSkillID != agentSkillID {
			return errors.New("Skill remove rollback identity mismatch")
		}
		if err := tx.Model(&model.AgentSkill{}).Where("id = ? AND status IN ?", agentSkillID, []string{model.AgentSkillStatusRemoving, model.AgentSkillStatusSyncError}).Update("status", model.AgentSkillStatusReady).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", jobID).Delete(&model.SkillOperationJob{}).Error
	})
}

func (dao *SkillDao) UpsertSkillHub(name, description string, builtin bool) error {
	// Do not change an existing row between builtin and external.  Besides
	// violating the ownership boundary, doing so would leave an external
	// object's key/blob attached to a row that is no longer supposed to have
	// package storage.  AgentEnd reports are retried, so surfacing the conflict
	// is safer than silently orphaning the package or making it undeletable.
	return db.GetDB().Transaction(func(tx *gorm.DB) error {
		var existing model.SkillHub
		err := tx.Select("id, builtin").Where("name = ?", name).First(&existing).Error
		if err == nil {
			if existing.Builtin != builtin {
				return fmt.Errorf("skill %q already exists with a different builtin flag", name)
			}
			return tx.Model(&existing).Update("description", description).Error
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return tx.Create(&model.SkillHub{
			Name:        name,
			Builtin:     builtin,
			Description: description,
		}).Error
	})
}

func (dao *SkillDao) EnsureAgentSkill(sessionID, skillName, agentType string) error {
	var existing model.AgentSkill
	err := db.GetDB().Where("session_id = ? AND skill_name = ?", sessionID, skillName).First(&existing).Error
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if err := db.GetDB().Create(&model.AgentSkill{
		SessionID:  sessionID,
		SkillName:  skillName,
		AgentType:  agentType,
		Status:     model.AgentSkillStatusReady,
		ImportedAt: time.Now(),
	}).Error; err != nil {
		if isDuplicateKeyError(err) {
			return nil
		}
		return err
	}
	return nil
}

func (dao *SkillDao) ListBuiltinSkills() ([]model.SkillHub, error) {
	var skills []model.SkillHub
	if err := db.GetDB().Select(skillMetadataColumns).Where("builtin = ?", true).Find(&skills).Error; err != nil {
		return nil, err
	}
	return skills, nil
}

func (dao *SkillDao) ListExternalSkillsBySession(sessionID string) ([]model.SkillHub, error) {
	var skills []model.SkillHub
	if err := db.GetDB().
		Table("skill_hubs").
		Select("skill_hubs."+strings.ReplaceAll(skillMetadataColumns, ", ", ", skill_hubs.")).
		Joins("JOIN agent_skill ON agent_skill.skill_name = skill_hubs.name").
		Where("agent_skill.session_id = ? AND skill_hubs.builtin = ?", sessionID, false).
		Find(&skills).Error; err != nil {
		return nil, err
	}
	return skills, nil
}
