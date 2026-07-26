package impl

import (
	gormdao "agenthub/backend/internal/dao/gorm"
	"agenthub/backend/internal/service"
	svcimpl "agenthub/backend/internal/service/impl"
	"agenthub/backend/internal/vo"

	"github.com/gin-gonic/gin"
)

type DiffSnapshotController struct {
	service service.DiffSnapshotService
}

func NewDiffSnapshotController() *DiffSnapshotController {
	diffSnapshotDao := gormdao.NewDiffSnapshotDao()
	diffSnapshotService := svcimpl.NewDiffSnapshotService(diffSnapshotDao)

	return &DiffSnapshotController{service: diffSnapshotService}
}

type SaveDiffSnapshotReq struct {
	SessionID string `json:"session_id" binding:"required"`
	Diff      string `json:"diff"`
	Status    string `json:"status" binding:"required"`
}

func (ctrl *DiffSnapshotController) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/diff-snapshots/:snapshotId", ctrl.GetDiffSnapshot)
	rg.PUT("/diff-snapshots/:snapshotId", ctrl.SaveDiffSnapshot)
}

func (ctrl *DiffSnapshotController) GetDiffSnapshot(c *gin.Context) {
	snapshot, err := ctrl.service.GetDiffSnapshot(c.Param("snapshotId"))
	if err != nil {
		handleBizError(c, err)
		return
	}

	vo.OK(c, snapshot)
}

func (ctrl *DiffSnapshotController) SaveDiffSnapshot(c *gin.Context) {
	var req SaveDiffSnapshotReq
	if err := c.ShouldBindJSON(&req); err != nil {
		vo.BadRequest(c, "session_id and status are required")
		return
	}

	snapshot, err := ctrl.service.SaveDiffSnapshot(c.Param("snapshotId"), service.SaveDiffSnapshotInput{
		SessionID: req.SessionID,
		Diff:      req.Diff,
		Status:    req.Status,
	})
	if err != nil {
		handleBizError(c, err)
		return
	}

	vo.OK(c, snapshot)
}
