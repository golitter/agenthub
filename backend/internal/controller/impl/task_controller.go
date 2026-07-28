package impl

import (
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"agenthub/backend/internal/middleware"
	"agenthub/backend/internal/service"
	"agenthub/backend/internal/vo"
	"agenthub/backend/pkg/agentend_client"

	"github.com/gin-gonic/gin"
)

type TaskController struct {
	service     service.TaskService
	agentClient *agentend_client.Client
}

const maxControllerRepoPathLen = 512

func NewTaskController(taskService service.TaskService, agentClient *agentend_client.Client) *TaskController {
	return &TaskController{service: taskService, agentClient: agentClient}
}

type ValidateRepoPathReq struct {
	RepoPath string `json:"repo_path" binding:"required"`
}

func (ctrl *TaskController) RegisterRoutes(rg *gin.RouterGroup) {
	runLimiter := middleware.NewIPRateLimiter(30, time.Minute)

	rg.POST("/tasks", ctrl.CreateTask)
	rg.GET("/tasks", ctrl.ListTasks)
	rg.GET("/tasks/:taskId", ctrl.GetTask)
	rg.DELETE("/tasks/:taskId", ctrl.DeleteTask)
	rg.DELETE("/tasks/:taskId/leave", ctrl.LeaveTask)
	rg.PATCH("/tasks/:taskId", ctrl.PatchTask)
	rg.POST("/tasks/:taskId/run", runLimiter.Middleware(), ctrl.RunTask)
	rg.POST("/tasks/:taskId/review", ctrl.ReviewTask)
	rg.POST("/validate-repo-path", ctrl.ValidateRepoPath)
	rg.POST("/init-git-repo", ctrl.InitGitRepo)
}

func (ctrl *TaskController) CreateTask(c *gin.Context) {
	var req service.CreateTaskInput
	if err := c.ShouldBindJSON(&req); err != nil {
		vo.BadRequest(c, "title is required")
		return
	}

	task, err := ctrl.service.CreateTask(req)
	if err != nil {
		handleBizError(c, err)
		return
	}
	vo.Created(c, task)
}

func (ctrl *TaskController) ListTasks(c *gin.Context) {
	limit := 0
	if rawLimit := c.Query("limit"); rawLimit != "" {
		parsedLimit, err := strconv.Atoi(rawLimit)
		if err != nil {
			vo.BadRequest(c, "limit must be an integer")
			return
		}
		limit = parsedLimit
	}

	result, err := ctrl.service.ListTasks(service.TaskListOptions{
		Limit:  limit,
		Before: c.Query("before"),
	})
	if err != nil {
		handleBizError(c, err)
		return
	}
	if result.HasMore {
		c.Header("X-Has-More", "true")
		c.Header("X-Next-Cursor", result.Next)
	} else {
		c.Header("X-Has-More", "false")
	}
	vo.OK(c, result.Items)
}

func (ctrl *TaskController) GetTask(c *gin.Context) {
	result, err := ctrl.service.GetTask(c.Param("taskId"))
	if err != nil {
		handleBizError(c, err)
		return
	}
	vo.OK(c, result)
}

func (ctrl *TaskController) DeleteTask(c *gin.Context) {
	if err := ctrl.service.DeleteTask(c.Param("taskId")); err != nil {
		handleBizError(c, err)
		return
	}
	vo.OK(c, nil)
}

func (ctrl *TaskController) LeaveTask(c *gin.Context) {
	if err := ctrl.service.LeaveTask(c.Param("taskId")); err != nil {
		handleBizError(c, err)
		return
	}
	vo.OK(c, nil)
}

func (ctrl *TaskController) PatchTask(c *gin.Context) {
	var raw map[string]json.RawMessage
	if err := c.ShouldBindJSON(&raw); err != nil {
		vo.BadRequest(c, "invalid request body")
		return
	}

	pinnedAtRaw, ok := raw["pinned_at"]
	if !ok {
		vo.BadRequest(c, "no fields to update")
		return
	}

	req := service.PatchTaskInput{PinnedAtSet: true}
	if string(pinnedAtRaw) != "null" {
		var pinnedAt string
		if err := json.Unmarshal(pinnedAtRaw, &pinnedAt); err != nil {
			vo.BadRequest(c, "pinned_at must be an RFC3339 string, empty string, or null")
			return
		}
		req.PinnedAt = &pinnedAt
	} else {
		empty := ""
		req.PinnedAt = &empty
	}

	if err := ctrl.service.PatchTask(c.Param("taskId"), req); err != nil {
		handleBizError(c, err)
		return
	}
	vo.OK(c, gin.H{"task_id": c.Param("taskId")})
}

func (ctrl *TaskController) RunTask(c *gin.Context) {
	var req service.RunTaskInput
	if err := c.ShouldBindJSON(&req); err != nil {
		vo.BadRequest(c, "message and session_id are required")
		return
	}

	result, err := ctrl.service.RunTask(c.Param("taskId"), req)
	if err != nil {
		handleBizError(c, err)
		return
	}
	vo.Accepted(c, result)
}

func (ctrl *TaskController) ReviewTask(c *gin.Context) {
	var req service.ReviewTaskInput
	if err := c.ShouldBindJSON(&req); err != nil {
		vo.BadRequest(c, "session_id and action are required")
		return
	}

	result, err := ctrl.service.ReviewTask(c.Param("taskId"), req)
	if err != nil {
		handleBizError(c, err)
		return
	}
	vo.OK(c, result)
}

func (ctrl *TaskController) ValidateRepoPath(c *gin.Context) {
	var req ValidateRepoPathReq
	if err := c.ShouldBindJSON(&req); err != nil {
		vo.BadRequest(c, "repo_path is required")
		return
	}
	repoPath, ok := normalizeControllerRepoPath(c, req.RepoPath)
	if !ok {
		return
	}

	result, err := ctrl.agentClient.ValidateRepoPath(repoPath)
	if err != nil {
		slog.Warn("validate repo path failed", "error", err)
		vo.ServiceUnavailable(c, "agent service unavailable")
		return
	}
	vo.OK(c, result)
}

type InitGitRepoReq struct {
	RepoPath string `json:"repo_path" binding:"required"`
}

func (ctrl *TaskController) InitGitRepo(c *gin.Context) {
	var req InitGitRepoReq
	if err := c.ShouldBindJSON(&req); err != nil {
		vo.BadRequest(c, "repo_path is required")
		return
	}
	repoPath, ok := normalizeControllerRepoPath(c, req.RepoPath)
	if !ok {
		return
	}

	result, err := ctrl.agentClient.InitGitRepo(repoPath)
	if err != nil {
		slog.Warn("init git repo failed", "error", err)
		vo.ServiceUnavailable(c, "agent service unavailable")
		return
	}
	vo.OK(c, result)
}

func normalizeControllerRepoPath(c *gin.Context, repoPath string) (string, bool) {
	repoPath, message := normalizeControllerRepoPathValue(repoPath)
	if message == "" {
		return repoPath, true
	}
	vo.BadRequest(c, message)
	return "", false
}

func normalizeControllerRepoPathValue(repoPath string) (string, string) {
	repoPath = strings.TrimSpace(repoPath)
	if repoPath == "" {
		return "", "repo_path is required"
	}
	if len(repoPath) > maxControllerRepoPathLen {
		return "", "repo_path is too long"
	}
	if strings.ContainsRune(repoPath, 0) {
		return "", "repo_path contains invalid character"
	}
	return repoPath, ""
}
