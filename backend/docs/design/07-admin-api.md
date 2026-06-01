# Admin API — 管理面板后端

## 实现了什么

管理面板 REST API，提供密码认证、系统资源监控、会话清理、工作区管理、Agent 概览、服务健康检查、统计数据查询和头像管理 8 个模块。使用 JWT Bearer Token 保护，路由挂载在 `/api/admin` 下。公开接口（auth/health/avatar GET）和受保护接口分离。

## 怎么实现的

### 路由注册 (`internal/handler/admin.go`)

`AdminHandler` 通过 `RegisterRoutes` 方法自注册路由，公开接口（auth/health）和受保护接口分离：

```go
func (h *AdminHandler) RegisterRoutes(rg *gin.RouterGroup) {
    admin := rg.Group("/admin")
    {
        admin.POST("/auth", h.Auth)
        admin.GET("/health", h.HealthCheck)
        admin.GET("/avatar", h.GetAvatar)

        protected := admin.Group("")
        protected.Use(middleware.AdminAuth(h.cfg.JWT.Secret))
        {
            protected.GET("/resources", h.GetResources)
            protected.DELETE("/sessions", h.DeleteSessions)
            protected.GET("/workspaces", h.GetWorkspaces)
            protected.DELETE("/workspaces/:id", h.DeleteWorkspace)
            protected.GET("/agents", h.GetAgents)
            protected.GET("/services", h.GetServices)
            protected.GET("/statistics", h.GetStatistics)
            protected.PUT("/avatar", h.UpdateAvatar)
        }
    }
}
```

构造函数注入 `Config`（密码验证）、`Uploader`（头像上传）和 `agentend_client.Client`（代理请求）。

### 认证 (`internal/handler/admin.go` + `internal/middleware/admin_auth.go`)

**Auth** — 密码验证，成功后返回 JWT（1 小时有效）：

```go
func (h *AdminHandler) Auth(c *gin.Context) {
    if !middleware.VerifyAdminPassword(req.Password, h.cfg.Admin.Password) {
        vo.Unauthorized(c, "密码错误")
        return
    }
    token, err := middleware.GenerateAdminToken(h.cfg.JWT.Secret)
    if err != nil {
        vo.InternalError(c, "failed to generate token")
        return
    }
    vo.OK(c, gin.H{"token": token, "expires_in": 3600})
}
```

**AdminAuth 中间件** — 校验 Bearer Token，验证 `admin: true` claim：

```go
func AdminAuth(jwtSecret string) gin.HandlerFunc {
    return func(c *gin.Context) {
        token, err := jwt.Parse(parts[1], ...)
        claims, _ := token.Claims.(jwt.MapClaims)
        if isAdmin, _ := claims["admin"].(bool); !isAdmin {
            c.AbortWithStatusJSON(401, ...)
        }
        c.Set("isAdmin", true)
        c.Next()
    }
}
```

密码存储在 `configs/config.yaml` 的 `admin.password` 字段。

### 系统资源 (`internal/handler/admin_resource.go`)

`GetResources` 聚合两路数据源：
- **磁盘/内存**：代理到 AgentEnd 的 `/v1/admin/resources` 接口
- **Redis**：直接调用 `redis.GetClient().Info("memory")` 解析 `used_memory` / `maxmemory`（默认 512MB）

```go
type ResourceInfo struct {
    Used  float64 `json:"used"`
    Total float64 `json:"total"`
    Unit  string  `json:"unit"`
}
```

### 服务健康 (`internal/handler/admin_health.go`)

`GetServices` 检测三个服务的 HTTP 可达性（3s 超时）：

| 服务 | 检测 URL |
|------|---------|
| Frontend | `http://localhost:5173` |
| Backend | `http://localhost:8080/ping` |
| AgentEnd | `http://localhost:{port}/health` |

返回 `ServiceInfo` 列表，包含名称、状态（Running/Down）、运行时间、端口、最后检查时间。

### 会话清理 (`internal/handler/admin_session.go`)

`DeleteSessions` 批量删除指定 session_id 列表的会话记录。

### 工作区管理 (`internal/handler/admin_workspace.go`)

`GetWorkspaces` 代理到 AgentEnd 获取工作区列表。`DeleteWorkspace` 代理删除指定工作区。

### Agent 概览 (`internal/handler/admin_agent.go`)

`GetAgents` 代理到 AgentEnd 获取 Agent 列表。

### 统计数据 (`internal/handler/admin_stats.go`)

`GetStatistics` 聚合 MySQL 统计（Task/Session/Message 计数）和 AgentEnd 代理数据。

### 头像管理 (`internal/handler/admin_avatar.go`)

`GetAvatar` / `UpdateAvatar` 管理管理员头像，使用七牛云存储。

### Admin 配置 (`internal/conf/conf.go`)

```go
type AdminConfig struct {
    Password string `yaml:"password"`
}
```

Config 结构体新增 `Admin AdminConfig` 字段。
