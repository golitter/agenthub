package controller

import "github.com/gin-gonic/gin"

type AgentController interface {
	RegisterRoutes(rg *gin.RouterGroup)
}

type SessionController interface {
	RegisterRoutes(rg *gin.RouterGroup)
}

type DiffSnapshotController interface {
	RegisterRoutes(rg *gin.RouterGroup)
}

type AnnouncementController interface {
	RegisterRoutes(rg *gin.RouterGroup)
}

type ContactGroupController interface {
	RegisterRoutes(rg *gin.RouterGroup)
}

type MessageController interface {
	RegisterRoutes(rg *gin.RouterGroup)
}

type AvatarController interface {
	RegisterRoutes(rg *gin.RouterGroup)
}

type AssetController interface {
	RegisterRoutes(rg *gin.RouterGroup)
}

type SkillController interface {
	RegisterRoutes(rg *gin.RouterGroup)
}

type AgentProfileController interface {
	RegisterRoutes(rg *gin.RouterGroup)
}

type WorkspaceController interface {
	RegisterRoutes(rg *gin.RouterGroup)
}

type TaskController interface {
	RegisterRoutes(rg *gin.RouterGroup)
}
