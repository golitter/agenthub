package impl

import (
	"strings"

	"agenthub/backend/internal/dao"
	"agenthub/backend/internal/model"
	"agenthub/backend/internal/service"
)

const (
	maxDiffSnapshotIDLen = 36
	maxDiffContentLen    = 2 * 1024 * 1024
)

type DiffSnapshotService struct {
	dao dao.DiffSnapshotDao
}

func NewDiffSnapshotService(diffSnapshotDao dao.DiffSnapshotDao) *DiffSnapshotService {
	return &DiffSnapshotService{dao: diffSnapshotDao}
}

func (svc *DiffSnapshotService) GetDiffSnapshot(snapshotID string) (*model.DiffSnapshot, error) {
	snapshotID = strings.TrimSpace(snapshotID)
	if snapshotID == "" {
		return nil, service.ErrBadRequest("snapshot_id is required")
	}
	if len([]rune(snapshotID)) > maxDiffSnapshotIDLen {
		return nil, service.ErrBadRequest("snapshot_id is too long")
	}

	snapshot, err := svc.dao.GetBySnapshotID(snapshotID)
	if err != nil {
		return nil, err
	}
	if snapshot == nil {
		return nil, service.ErrNotFound("snapshot not found")
	}

	return snapshot, nil
}

func (svc *DiffSnapshotService) SaveDiffSnapshot(snapshotID string, input service.SaveDiffSnapshotInput) (*model.DiffSnapshot, error) {
	snapshotID, input, err := normalizeDiffSnapshotInput(snapshotID, input)
	if err != nil {
		return nil, err
	}

	existing, err := svc.dao.GetBySnapshotID(snapshotID)
	if err != nil {
		return nil, err
	}
	if existing != nil && isTerminalDiffSnapshotStatus(existing.Status) {
		return nil, service.ErrConflict("snapshot is in terminal state")
	}

	if input.Status == "pending" {
		if err := svc.dao.CancelPendingBySession(input.SessionID, snapshotID); err != nil {
			return nil, err
		}
	}

	return svc.dao.Upsert(model.DiffSnapshot{
		SnapshotID:  snapshotID,
		SessionID:   input.SessionID,
		DiffContent: input.Diff,
		Status:      input.Status,
	})
}

func normalizeDiffSnapshotInput(snapshotID string, input service.SaveDiffSnapshotInput) (string, service.SaveDiffSnapshotInput, error) {
	snapshotID = strings.TrimSpace(snapshotID)
	input.SessionID = strings.TrimSpace(input.SessionID)
	input.Status = strings.TrimSpace(input.Status)

	if snapshotID == "" {
		return snapshotID, input, service.ErrBadRequest("snapshot_id is required")
	}
	if len([]rune(snapshotID)) > maxDiffSnapshotIDLen {
		return snapshotID, input, service.ErrBadRequest("snapshot_id is too long")
	}
	if input.SessionID == "" {
		return snapshotID, input, service.ErrBadRequest("session_id is required")
	}
	if len([]rune(input.SessionID)) > maxSessionIDLen {
		return snapshotID, input, service.ErrBadRequest("session_id is too long")
	}
	if !isAllowedDiffSnapshotStatus(input.Status) {
		return snapshotID, input, service.ErrBadRequest("invalid snapshot status")
	}
	if len(input.Diff) > maxDiffContentLen {
		return snapshotID, input, service.ErrBadRequest("diff is too long")
	}
	return snapshotID, input, nil
}

func isAllowedDiffSnapshotStatus(status string) bool {
	switch status {
	case "pending", "committed", "reverted", "cancelled":
		return true
	default:
		return false
	}
}

func isTerminalDiffSnapshotStatus(status string) bool {
	switch status {
	case "committed", "reverted", "cancelled":
		return true
	default:
		return false
	}
}
