package gormdao

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	artifactdao "agenthub/backend/internal/dao"
	"agenthub/backend/internal/generated"
	"agenthub/backend/internal/model"
	"agenthub/backend/pkg/db"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ArtifactDao struct{}

const artifactDeleteRetryAfter = 15 * time.Minute
const artifactLastErrorMaxBytes = 4096

func NewArtifactDao() *ArtifactDao { return &ArtifactDao{} }

func (dao *ArtifactDao) CreatePending(artifact *model.Artifact, maxObjects int64) error {
	if artifact == nil {
		return errors.New("artifact is required")
	}
	// Serialize the insert with TaskService's message-cascade delete. Without
	// this row lock, an upload that validated the message just before deletion
	// could create a ready artifact after the task's final cleanup sweep.
	return db.GetDB().Transaction(func(tx *gorm.DB) error {
		var message model.Message
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("message_id").
			Where("message_id = ? AND task_id = ? AND session_id = ? AND role = ?", artifact.MessageID, artifact.TaskID, artifact.SessionID, string(generated.MessageRoleAgent)).
			First(&message).Error; err != nil {
			return err
		}
		if maxObjects > 0 {
			var count int64
			if err := tx.Model(&model.Artifact{}).
				Where("message_id = ? AND status <> ?", artifact.MessageID, model.ArtifactStatusDeleted).
				Count(&count).Error; err != nil {
				return err
			}
			if count >= maxObjects {
				return artifactdao.ErrArtifactQuota
			}
		}
		return tx.Create(artifact).Error
	})
}

func (dao *ArtifactDao) MarkReady(resourceID string, size int64, sha256 string) error {
	// The task-delete transaction locks the Agent message before removing its
	// task. Take that same lock before finalizing a pending upload. This makes a
	// deletion that wins the race fail closed instead of allowing a ready row to
	// be committed for a message that has already been deleted.
	return db.GetDB().Transaction(func(tx *gorm.DB) error {
		var artifact model.Artifact
		if err := tx.Where("resource_id = ? AND status = ?", resourceID, model.ArtifactStatusPending).First(&artifact).Error; err != nil {
			return err
		}
		var message model.Message
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("message_id").
			Where("message_id = ? AND task_id = ? AND session_id = ? AND role = ?", artifact.MessageID, artifact.TaskID, artifact.SessionID, string(generated.MessageRoleAgent)).
			First(&message).Error; err != nil {
			return err
		}
		result := tx.Model(&model.Artifact{}).Where("id = ? AND status = ?", artifact.ID, model.ArtifactStatusPending).Updates(map[string]interface{}{
			"size": size, "sha256": sha256, "status": model.ArtifactStatusReady, "last_error": "",
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

func (dao *ArtifactDao) MarkFailed(resourceID, message string) error {
	result := db.GetDB().Model(&model.Artifact{}).Where("resource_id = ? AND status = ?", resourceID, model.ArtifactStatusPending).Updates(map[string]interface{}{
		"status": model.ArtifactStatusFailed, "last_error": message,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (dao *ArtifactDao) FindReadyByResourceID(resourceID string) (*model.Artifact, error) {
	return dao.find(resourceID, model.ArtifactStatusReady)
}

func (dao *ArtifactDao) FindByResourceID(resourceID string) (*model.Artifact, error) {
	return dao.find(resourceID, "")
}

func (dao *ArtifactDao) FindByMessageAndIdempotency(messageID, key string) (*model.Artifact, error) {
	if strings.TrimSpace(messageID) == "" || strings.TrimSpace(key) == "" {
		return nil, nil
	}
	var artifact model.Artifact
	if err := db.GetDB().Where("message_id = ? AND idempotency_key = ?", messageID, key).First(&artifact).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &artifact, nil
}

func (dao *ArtifactDao) find(resourceID, status string) (*model.Artifact, error) {
	var artifact model.Artifact
	query := db.GetDB().Where("resource_id = ?", resourceID)
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if err := query.First(&artifact).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &artifact, nil
}

func (dao *ArtifactDao) CountByMessageID(messageID string) (int64, error) {
	var count int64
	err := db.GetDB().Model(&model.Artifact{}).Where("message_id = ? AND status NOT IN ?", messageID, []string{model.ArtifactStatusDeleted}).Count(&count).Error
	return count, err
}

func (dao *ArtifactDao) ListObjectKeysByTaskID(taskID string) ([]model.Artifact, error) {
	var artifacts []model.Artifact
	err := db.GetDB().Where("task_id = ?", taskID).Find(&artifacts).Error
	return artifacts, err
}

func (dao *ArtifactDao) MarkDeletingByTaskID(taskID string) ([]model.Artifact, error) {
	var artifacts []model.Artifact
	err := db.GetDB().Transaction(func(tx *gorm.DB) error {
		// Do not remove an object while its upload is still in flight. Pending
		// rows are finalized (or become failed) by the uploader and then picked
		// up by the normal stale-row cleanup path.
		deletableStatuses := []string{model.ArtifactStatusReady, model.ArtifactStatusFailed, model.ArtifactStatusDeleting}
		if err := tx.Where("task_id = ? AND status IN ?", taskID, deletableStatuses).Find(&artifacts).Error; err != nil {
			return err
		}
		return tx.Model(&model.Artifact{}).Where("task_id = ? AND status IN ?", taskID, deletableStatuses).Update("status", model.ArtifactStatusDeleting).Error
	})
	return artifacts, err
}

func (dao *ArtifactDao) MarkDeleteFailed(resourceID, message string) error {
	message = strings.TrimSpace(message)
	if len(message) > artifactLastErrorMaxBytes {
		messageBytes := []byte(message)[:artifactLastErrorMaxBytes]
		for !utf8.Valid(messageBytes) {
			messageBytes = messageBytes[:len(messageBytes)-1]
		}
		message = string(messageBytes)
	}
	result := db.GetDB().Model(&model.Artifact{}).
		Where("resource_id = ? AND status IN ?", resourceID, []string{model.ArtifactStatusPending, model.ArtifactStatusFailed, model.ArtifactStatusDeleting}).
		Updates(map[string]interface{}{"last_error": message, "updated_at": time.Now()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (dao *ArtifactDao) DeleteRow(resourceID string) error {
	return db.GetDB().Where("resource_id = ?", resourceID).Delete(&model.Artifact{}).Error
}

func (dao *ArtifactDao) ListStalePendingOrFailed(before time.Time, limit int) ([]model.Artifact, error) {
	if limit <= 0 {
		limit = 100
	}
	var artifacts []model.Artifact
	// Pending/failed rows use the retention window, while deleting rows need a
	// much shorter retry interval: task deletion can commit after a transient
	// MinIO outage and should not wait a full retention period for its next try.
	deleteBefore := time.Now().Add(-artifactDeleteRetryAfter)
	err := db.GetDB().Transaction(func(tx *gorm.DB) error {
		// Lock the exact candidate rows while selecting them. Without a locking
		// read, an upload could move a row from pending to ready after this
		// SELECT but before the claim UPDATE, and the worker would then delete a
		// freshly-ready object using its stale in-memory copy.
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"(status IN ? AND updated_at < ?) OR (status = ? AND updated_at < ?)",
			[]string{model.ArtifactStatusPending, model.ArtifactStatusFailed}, before,
			model.ArtifactStatusDeleting, deleteBefore,
		).Order("updated_at ASC").Limit(limit).Find(&artifacts).Error; err != nil {
			return err
		}
		if len(artifacts) == 0 {
			return nil
		}
		resourceIDs := make([]string, 0, len(artifacts))
		for _, artifact := range artifacts {
			resourceIDs = append(resourceIDs, artifact.ResourceID)
		}
		// Claim the rows before touching MinIO. An upload that is still in
		// flight then observes deleting and cannot finalize a row after this
		// worker removes the object/metadata pair.
		return tx.Model(&model.Artifact{}).
			Where("resource_id IN ? AND status IN ?", resourceIDs, []string{model.ArtifactStatusPending, model.ArtifactStatusFailed, model.ArtifactStatusDeleting}).
			Update("status", model.ArtifactStatusDeleting).Error
	})
	return artifacts, err
}
