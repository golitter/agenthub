package impl

import (
	"agenthub/backend/internal/service"
	"agenthub/backend/internal/vo"

	"github.com/gin-gonic/gin"
)

type ContactGroupController struct {
	service service.ContactGroupService
}

func NewContactGroupController(contactGroupService service.ContactGroupService) *ContactGroupController {
	return &ContactGroupController{service: contactGroupService}
}

type CreateGroupReq struct {
	Name string `json:"name" binding:"required"`
}

type UpdateGroupReq struct {
	Name string `json:"name" binding:"required"`
}

type AddItemReq struct {
	TaskID string `json:"task_id" binding:"required"`
}

func (ctrl *ContactGroupController) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/contact-groups", ctrl.ListGroups)
	rg.POST("/contact-groups", ctrl.CreateGroup)
	rg.PUT("/contact-groups/:groupId", ctrl.UpdateGroup)
	rg.DELETE("/contact-groups/:groupId", ctrl.DeleteGroup)
	rg.POST("/contact-groups/:groupId/items", ctrl.AddItem)
	rg.DELETE("/contact-groups/:groupId/items/:taskID", ctrl.RemoveItem)
}

func (ctrl *ContactGroupController) ListGroups(c *gin.Context) {
	result, err := ctrl.service.ListGroups()
	if err != nil {
		handleBizError(c, err)
		return
	}
	vo.OK(c, result)
}

func (ctrl *ContactGroupController) CreateGroup(c *gin.Context) {
	var req CreateGroupReq
	if err := c.ShouldBindJSON(&req); err != nil {
		vo.BadRequest(c, "name is required")
		return
	}

	group, err := ctrl.service.CreateGroup(req.Name)
	if err != nil {
		handleBizError(c, err)
		return
	}
	vo.Created(c, group)
}

func (ctrl *ContactGroupController) UpdateGroup(c *gin.Context) {
	var req UpdateGroupReq
	if err := c.ShouldBindJSON(&req); err != nil {
		vo.BadRequest(c, "name is required")
		return
	}
	if err := ctrl.service.UpdateGroup(c.Param("groupId"), req.Name); err != nil {
		handleBizError(c, err)
		return
	}
	vo.OK(c, nil)
}

func (ctrl *ContactGroupController) DeleteGroup(c *gin.Context) {
	if err := ctrl.service.DeleteGroup(c.Param("groupId")); err != nil {
		handleBizError(c, err)
		return
	}
	vo.OK(c, nil)
}

func (ctrl *ContactGroupController) AddItem(c *gin.Context) {
	var req AddItemReq
	if err := c.ShouldBindJSON(&req); err != nil {
		vo.BadRequest(c, "task_id is required")
		return
	}

	item, err := ctrl.service.AddItem(c.Param("groupId"), req.TaskID)
	if err != nil {
		handleBizError(c, err)
		return
	}
	vo.Created(c, item)
}

func (ctrl *ContactGroupController) RemoveItem(c *gin.Context) {
	if err := ctrl.service.RemoveItem(c.Param("groupId"), c.Param("taskID")); err != nil {
		handleBizError(c, err)
		return
	}
	vo.OK(c, nil)
}
