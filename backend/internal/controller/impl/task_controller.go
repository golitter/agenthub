package impl

import (
	gormdao "agenthub/backend/internal/dao/gorm"
	"agenthub/backend/internal/service"
	svcimpl "agenthub/backend/internal/service/impl"
	"agenthub/backend/internal/vo"
	"agenthub/backend/pkg/agentend_client"

	"github.com/gin-gonic/gin"
)

type TaskController struct {
	service     service.TaskService
	agentClient *agentend_client.Client
}

func NewTaskController(agentClient *agentend_client.Client) *TaskController {
	taskDao := gormdao.NewTaskDao()
	sessionDao := gormdao.NewSessionDao()
	messageDao := gormdao.NewMessageDao()
	diffDao := gormdao.NewDiffSnapshotDao()
	taskService := svcimpl.NewTaskService(taskDao, sessionDao, messageDao, diffDao, agentClient)
	return &TaskController{service: taskService, agentClient: agentClient}
}

type ValidateRepoPathReq struct {
	RepoPath string `json:"repo_path" binding:"required"`
}

func (ctrl *TaskController) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/tasks", ctrl.CreateTask)
	rg.GET("/tasks", ctrl.ListTasks)
	rg.GET("/tasks/:taskId", ctrl.GetTask)
	rg.DELETE("/tasks/:taskId", ctrl.DeleteTask)
	rg.DELETE("/tasks/:taskId/leave", ctrl.LeaveTask)
	rg.PATCH("/tasks/:taskId", ctrl.PatchTask)
	rg.POST("/tasks/:taskId/run", ctrl.RunTask)
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
	tasks, err := ctrl.service.ListTasks()
	if err != nil {
		handleBizError(c, err)
		return
	}
	vo.OK(c, tasks)
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
	var req service.PatchTaskInput
	if err := c.ShouldBindJSON(&req); err != nil {
		vo.BadRequest(c, "invalid request body")
		return
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

	result, err := ctrl.agentClient.ValidateRepoPath(req.RepoPath)
	if err != nil {
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

	result, err := ctrl.agentClient.InitGitRepo(req.RepoPath)
	if err != nil {
		vo.ServiceUnavailable(c, "agent service unavailable")
		return
	}
	vo.OK(c, result)
}
