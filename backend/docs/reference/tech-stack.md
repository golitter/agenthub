# Backend 技术栈

## 语言与运行时

| 工具 | 版本 | 用途 |
|------|------|------|
| Go | 1.26.2 | 编译型后端语言 |
| Air | — | 热重载 |

## 核心框架

| 库 | 版本 | 用途 |
|----|------|------|
| Gin | v1.12.0 | HTTP 框架（路由、中间件、请求处理） |
| GORM | v1.31.1 | ORM 框架（模型映射、CRUD、迁移） |

## 数据库

| 库 | 版本 | 用途 |
|----|------|------|
| gorm.io/driver/mysql | v1.6.0 | MySQL 驱动（GORM Dialector） |
| go-sql-driver/mysql | v1.8.1 | MySQL 底层驱动 |

MySQL 8.0，通过 `pkg/db` 包以 mutex 保护的单例模式初始化连接；启动时会配置连接池并执行 `PingContext` 验证可达性，失败时不缓存半初始化实例。

## 缓存与消息

| 库 | 版本 | 用途 |
|----|------|------|
| redis/go-redis | v9.18.0 | Redis 客户端，用于 Stream 实时消息中转 |

Redis 通过 `pkg/redis` 包初始化，StreamKey 工具 + 流清理功能。

## 配置管理

| 库 | 版本 | 用途 |
|----|------|------|
| gopkg.in/yaml.v3 | v3.0.1 | YAML 配置文件解析 |
| joho/godotenv | v1.5.1 | .env 环境变量加载 |

配置文件位于 `configs/config.yaml`，包含 MySQL、JWT、AgentEnd、Server、Auth、Redis、七牛云、Storage、SkillStorage、Admin、CORS 配置段。支持环境变量覆盖（如 `JWT_SECRET`、`ADMIN_PASSWORD`、`API_AUTH_ENABLED`、七牛云 access_key、`SERVER_PORT`、`SKILL_STORAGE_*`、`MINIO_*`），生产模式会拒绝默认 JWT secret 和默认 Admin 密码，启用 Skill 存储时还强制 `use_ssl=true`，并默认开启普通 API Auth。

## 认证

| 库 | 版本 | 用途 |
|----|------|------|
| golang-jwt/jwt/v5 | v5.3.1 | JWT Token 生成与校验 |

中间件位于 `internal/middleware/auth.go`，提供 `GenerateToken`、`AuthWithSkips` 和 Bearer Token 校验。普通 API Auth 由 `auth.enabled` / `API_AUTH_ENABLED` 控制；`GET .../stream` SSE 可通过 `access_token` query 携带同一 JWT。

## 跨域

| 库 | 版本 | 用途 |
|----|------|------|
| gin-contrib/cors | v1.7.7 | CORS 中间件 |

CORS origins 从 `configs/config.yaml` 的 `cors.allow_origins` 加载；本地默认允许 `http://localhost:5173`，Docker / 生产环境通过配置或环境变量覆盖。

## 云存储

| 库 | 版本 | 用途 |
|----|------|------|
| qiniu/go-sdk/v7 | v7.26.12 | 七牛云 SDK（头像上传） |
| minio/minio-go/v7 | v7.0.100 | MinIO SDK（Skill 包对象存储，feature-gated） |

头像上传器位于 `pkg/qiniu`，支持字节/Reader 上传，生成公开/私有 URL。Skill 包私有对象存储位于 `pkg/package_store`，`MinIOStore` 为生产实现，`MemoryStore` 为单测替身。

## 存储层

存储层通过 `pkg/storage/` 包提供统一抽象，支持七牛云优先、本地磁盘兜底策略。`Provider` 接口由 `storage.NewProvider(qiniuCfg, storageCfg)` 工厂方法根据配置自动选择实现，启动入口创建后传入 `internal/app.NewRouter`。

Skill 包对象存储是独立的一套私有存储（`pkg/package_store`），由 `config.yaml` 的 `skill_storage` 段控制（feature-gated）。启用时配合 `pkg/skill_upload_session`（Redis 上传会话）与 `SkillOperationJob` outbox 做补偿；未启用时走 DB blob 兼容路径。

## 工具库

| 库 | 版本 | 用途 |
|----|------|------|
| google/uuid | v1.6.0 | UUID 生成（task_id、message_id） |
| golang.org/x/crypto | v0.52.0 | bcrypt（Admin 密码加密校验，`internal/middleware/admin_auth.go`） |

## 项目结构

```
backend/
├── cmd/
│   ├── server/
│   │   └── main.go          # 入口（配置/基础设施初始化 + 优雅关闭）
│   ├── skill-migrate/       # Skill 对象存储迁移独立工具
│   └── skill-reconcile/     # Skill 状态对账独立工具
├── configs/
│   └── config.yaml          # 配置文件
├── internal/
│   ├── app/                 # 应用组装（DAO → Service → Controller + Gin 路由）
│   ├── conf/                # 配置加载
│   ├── controller/          # Controller 层
│   │   ├── controller.go    # 接口定义（共享 RegisterRoutes 形状）
│   │   └── impl/            # 13 组 Controller 实现
│   ├── service/             # Service 层
│   │   ├── service.go       # 接口定义 + DTO
│   │   ├── bizerr.go        # 统一业务错误
│   │   ├── context.go       # Skill 上传 owner/admin 上下文工具
│   │   ├── skill_validator.go
│   │   ├── skill_scanner.go # 可选 Skill 内容扫描器
│   │   └── impl/            # 11 组 Service 实现 + 4 辅助模块（stream_helper / task_route / group_chat_window / skill_operation_worker）
│   ├── dao/                 # DAO 层
│   │   ├── dao.go           # 接口定义（8 组）
│   │   ├── skill_operation_dao.go # SkillOperationJob outbox 接口（第 9 组）
│   │   ├── gorm/            # GORM 实现
│   │   └── mock/            # Mock 实现
│   ├── stream/              # SSE 流式写入（RuntimeHub + StreamWriter）
│   ├── middleware/           # 中间件（auth, admin_auth, body_limit, cors, logger, rate_limit）
│   ├── model/               # 数据模型（11 核心模型 + 3 Skill 存储迁移模型）
│   ├── generated/           # 契约生成的 Go 类型（勿手改）
│   └── vo/                  # 统一响应封装
├── pkg/
│   ├── db/                  # MySQL 单例连接（mutex + Ping）
│   ├── redis/               # Redis 客户端 + StreamKey 工具
│   ├── agentend_client/     # AgentEnd HTTP 客户端
│   ├── qiniu/               # 七牛云上传
│   ├── storage/             # 存储层抽象（七牛云优先，本地磁盘兜底）
│   ├── package_store/       # Skill 包对象存储（MinIO 实现 + 内存 mock）
│   └── skill_upload_session/ # Redis 上传会话（TTL + 确认租约 + 收据保留）
├── go.mod
└── go.sum
```

## API 响应格式

所有接口统一使用 `{code, data, msg}` 格式：

| 场景 | HTTP 状态码 | code | 示例 |
|------|------------|------|------|
| 成功 | 200 | 0 | `{"code":0,"data":{"message":"pong"}}` |
| 创建 | 201 | 0 | `{"code":0,"data":{...}}` |
| 已接受 | 202 | 0 | `{"code":0,"data":{"message_id":"...","session_id":"...","route_mode":"direct"}}` |
| 请求错误 | 400 | 400 | `{"code":400,"msg":"invalid"}` |
| 未授权 | 401 | 401 | `{"code":401,"msg":"missing authorization header"}` |
| 禁止 | 403 | 403 | `{"code":403,"msg":"forbidden"}` |
| 未找到 | 404 | 404 | `{"code":404,"msg":"not found"}` |
| 冲突 | 409 | 409 | `{"code":409,"msg":"conflict"}` |
| 已消失 | 410 | 410 | `{"code":410,"msg":"gone"}` |
| 服务不可用 | 503 | 503 | `{"code":503,"msg":"service unavailable"}` |
| 内部错误 | 500 | 500 | `{"code":500,"msg":"internal error"}` |

## 关键设计决策

- **三层架构**：Controller → Service → DAO，职责清晰。Controller 仅做参数绑定/响应；Service 封装纯业务逻辑（无 Gin 依赖）；DAO 封装纯数据访问（接口可 Mock 替换）
- **BizError 统一错误**：Service 层通过 `BizError{Code, Message}` 表达业务错误，Controller 层 `handleBizError` 自动映射为 HTTP 状态码
- **自注册路由**：Controller 暴露 `RegisterRoutes(rg *gin.RouterGroup)`，路由注册内聚到 Controller；`internal/app.NewRouter` 负责统一装配和挂载
- **配置方案**：gopkg.in/yaml.v3 直接解析，不引入 Viper，保持轻量；支持环境变量覆盖敏感字段
- **数据库连接**：mutex 保护的单例，`db.Init(cfg)` 初始化并 Ping，`db.GetDB()` 全局获取，启动时 AutoMigrate
- **存储层抽象**：`pkg/storage/` 提供统一 `Provider` 接口，七牛云优先、本地磁盘兜底，`storage.NewProvider(&cfg.Qiniu, &cfg.Storage)` 工厂方法按配置自动选择实现，Controller 通过构造函数注入 `storage.Provider`
- **SSE 流式**：StreamWriter 通过双层通道（内存 RuntimeHub + Redis Stream）推送事件，Hub 用于低延迟实时推送，Redis 用于断线重连和数据恢复，30min 超时保护
- **优雅关闭**：SIGINT/SIGTERM 信号处理，15 秒优雅关闭等待
- **IP 限流**：Admin auth 路由使用 `IPRateLimiter`（5 次/分钟）防止暴力破解
