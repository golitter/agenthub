# Admin API — 管理面板后端

## 实现了什么

管理面板 REST API，提供密码认证、系统资源监控、会话清理、工作区管理、Agent 概览、服务健康检查、统计数据查询和头像管理 8 个模块。使用 JWT Bearer Token 保护，路由挂载在 `/api/admin` 下。公开接口（auth/health/avatar GET）和受保护接口分离。采用 Controller → Service → DAO 三层架构。

## 怎么实现的

### 三层架构

```
AdminController
    │ 参数绑定 → AdminService 调用 → vo 响应
    ▼
AdminService
    │ 业务逻辑（认证/资源聚合/统计）
    ▼
AdminDao + SessionDao + agentend_client
    │ 数据访问
    ▼
MySQL + AgentEnd API
```

### Controller 层 (`internal/controller/impl/admin_controller.go`)

`AdminController` 通过构造函数注入 `Config`、`AdminService`：

```go
type AdminController struct {
    service service.AdminService
    cfg     *conf.Config
}

func NewAdminController(cfg *conf.Config, _ storage.Provider, agentClient *agentend_client.Client) *AdminController {
    adminDao := gormdao.NewAdminDao()
    sessionDao := gormdao.NewSessionDao()
    adminService := svcimpl.NewAdminService(cfg, adminDao, sessionDao, agentClient)
    return &AdminController{service: adminService, cfg: cfg}
}
```

`RegisterRoutes` 自注册路由，公开接口和受保护接口分离，auth 路由带 IP 限流（5 次/分钟）：

```go
func (ctrl *AdminController) RegisterRoutes(rg *gin.RouterGroup) {
    admin := rg.Group("/admin")
    {
        authLimiter := middleware.NewIPRateLimiter(5, time.Minute)
        admin.POST("/auth", authLimiter.Middleware(), ctrl.Auth)
        admin.GET("/health", ctrl.HealthCheck)
        admin.GET("/avatar", ctrl.GetAvatar)

        protected := admin.Group("")
        protected.Use(middleware.AdminAuth(ctrl.cfg.JWT.Secret))
        {
            protected.GET("/resources", ctrl.GetResources)
            protected.DELETE("/sessions", ctrl.DeleteSessions)
            protected.GET("/workspaces", ctrl.GetWorkspaces)
            protected.DELETE("/workspaces/:id", ctrl.DeleteWorkspace)
            protected.GET("/agents", ctrl.GetAgents)
            protected.GET("/services", ctrl.GetServices)
            protected.GET("/statistics", ctrl.GetStatistics)
            protected.PUT("/avatar", ctrl.UpdateAvatar)
        }
    }
}
```

### Service 接口 (`internal/service/service.go`)

```go
type AdminService interface {
    Auth(password string) (*AuthResponse, error)
    GetAvatar() (string, error)
    UpdateAvatar(url string) error
    GetAgents() ([]AgentInfo, error)
    GetServices() []ServiceInfo
    GetResources() (*ResourceSummary, error)
    DeleteSessions(sessionIDs []string) (int, error)
    GetStatistics() (*StatisticsResponse, error)
    GetWorkspaces() (*WorkspaceSummary, error)
    DeleteWorkspace(id string) error
}
```

### 认证 (`AdminService.Auth` + `internal/middleware/admin_auth.go`)

**Auth** — 密码验证，成功后返回 JWT（1 小时有效）：

```go
func (s *adminService) Auth(password string) (*service.AuthResponse, error) {
    if !middleware.VerifyAdminPassword(password, s.cfg.Admin.Password) {
        return nil, service.ErrUnauthorized("密码错误")
    }
    token, err := middleware.GenerateAdminToken(s.cfg.JWT.Secret)
    if err != nil {
        return nil, service.ErrInternal("failed to generate token")
    }
    return &service.AuthResponse{Token: token, ExpiresIn: 3600}, nil
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

### DAO 接口 (`internal/dao/dao.go`)

```go
type AdminDao interface {
    GetAdminSetting(key string) (*model.AdminSetting, error)
    ReplaceAdminSetting(key, value string) error
    DeleteSessions(sessionIDs []string) (int, error)
    CountSessionsByDate(date string) (int64, error)
    CountSessionsBetween(start, end time.Time) (int64, error)
    CountMessages() (int64, error)
    CountMessagesByAgent() (map[string]int64, error)
}
```

### 各功能模块

**系统资源 (`GetResources`)** — 聚合两路数据源：
- **磁盘/内存**：代理到 AgentEnd 的 `/v1/resources` 接口
- **Redis**：直接调用 `redis.GetClient().Info("memory")` 解析 `used_memory` / `maxmemory`

```go
type ResourceInfo struct {
    Used  float64 `json:"used"`
    Total float64 `json:"total"`
    Unit  string  `json:"unit"`
}
```

**服务健康 (`GetServices`)** — 检测三个服务的 HTTP 可达性（3s 超时）：

| 服务 | 检测 URL |
|------|---------|
| Frontend | `http://localhost:5173` |
| Backend | `http://localhost:8080/ping` |
| AgentEnd | `http://localhost:{port}/health` |

**会话清理 (`DeleteSessions`)** — 批量删除指定 session_id 列表的会话记录，通过 `AdminDao.DeleteSessions` 执行。

**工作区管理 (`GetWorkspaces` / `DeleteWorkspace`)** — 查询 MySQL sessions 表（`status = "running"`）构造工作区列表。`DeleteWorkspace` 将对应 Session 状态更新为 `cleaned`。

**Agent 概览 (`GetAgents`)** — 代理到 AgentEnd 的 `/v1/agents/configs` 接口读取各 Agent CLI 的系统级配置文件内容（`~/.claude/settings.json`、`~/.opencode/opencode.jsonc` 等），返回 Agent 信息列表，敏感字段自动脱敏。

**统计数据 (`GetStatistics`)** — 聚合 MySQL 统计（Task/Session/Message 计数）和 AgentEnd 代理数据。通过 `AdminDao` 的 `CountSessionsByDate` / `CountMessages` / `CountMessagesByAgent` 等方法获取。

**头像管理 (`GetAvatar` / `UpdateAvatar`)** — 管理管理员头像，通过 `AdminDao.GetAdminSetting` / `ReplaceAdminSetting` 存储头像 URL。

### Admin 配置 (`internal/conf/conf.go`)

```go
type AdminConfig struct {
    Password string `yaml:"password"`
}
```

Config 结构体包含 `Admin AdminConfig` 字段。
