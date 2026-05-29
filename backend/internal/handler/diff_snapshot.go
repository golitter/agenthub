package handler

import (
	"agenthub/backend/internal/model"
	"agenthub/backend/internal/vo"
	"agenthub/backend/pkg/db"

	"github.com/gin-gonic/gin"
)

type DiffSnapshotHandler struct{}

func NewDiffSnapshotHandler() *DiffSnapshotHandler {
	return &DiffSnapshotHandler{}
}

func (h *DiffSnapshotHandler) GetDiffSnapshot(c *gin.Context) {
	snapshotID := c.Param("snapshotId")

	var snap model.DiffSnapshot
	if err := db.GetDB().Where("snapshot_id = ?", snapshotID).First(&snap).Error; err != nil {
		vo.NotFound(c, "snapshot not found")
		return
	}

	vo.OK(c, snap)
}

type SaveDiffSnapshotReq struct {
	SessionID string `json:"session_id" binding:"required"`
	Diff      string `json:"diff"`
	Status    string `json:"status" binding:"required"`
}

func (h *DiffSnapshotHandler) SaveDiffSnapshot(c *gin.Context) {
	snapshotID := c.Param("snapshotId")

	var req SaveDiffSnapshotReq
	if err := c.ShouldBindJSON(&req); err != nil {
		vo.BadRequest(c, err.Error())
		return
	}

	d := db.GetDB()

	// Check if snapshot already exists and is in terminal state
	var existing model.DiffSnapshot
	if err := d.Where("snapshot_id = ?", snapshotID).First(&existing).Error; err == nil {
		if existing.Status == "committed" || existing.Status == "reverted" || existing.Status == "cancelled" {
			vo.Conflict(c, "snapshot is in terminal state")
			return
		}
	}

	// Auto-cancel same-session pending snapshots when creating a new pending one
	if req.Status == "pending" {
		d.Model(&model.DiffSnapshot{}).
			Where("session_id = ? AND snapshot_id != ? AND status = ?", req.SessionID, snapshotID, "pending").
			Update("status", "cancelled")
	}

	// Upsert
	snap := model.DiffSnapshot{
		SnapshotID:  snapshotID,
		SessionID:   req.SessionID,
		DiffContent: req.Diff,
		Status:      req.Status,
	}

	result := d.Where("snapshot_id = ?", snapshotID).
		Assign(model.DiffSnapshot{
			DiffContent: req.Diff,
			Status:      req.Status,
			SessionID:   req.SessionID,
		}).
		FirstOrCreate(&snap)

	if result.Error != nil {
		vo.InternalError(c, result.Error.Error())
		return
	}

	vo.OK(c, snap)
}
