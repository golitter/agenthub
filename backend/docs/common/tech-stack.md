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

MySQL 8.0，通过 `pkg/db` 包以 sync.Once 单例模式初始化连接。

## 缓存与消息

| 库 | 版本 | 用途 |
|----|------|------|
| redis/go-redis | — | Redis 客户端，用于 Stream 实时消息中转 |

Redis 通过 `pkg/redis` 包初始化，StreamKey 工具 + 流清理功能。

## 配置管理

| 库 | 版本 | 用途 |
|----|------|------|
| gopkg.in/yaml.v3 | v3.0.1 | YAML 配置文件解析 |

配置文件位于 `configs/config.yaml`，包含 MySQL、JWT、AgentEnd、Redis、七牛云配置段。支持环境变量覆盖（如七牛云 access_key）。

## 认证

| 库 | 版本 | 用途 |
|----|------|------|
| golang-jwt/jwt/v5 | v5.3.1 | JWT Token 生成与校验 |

中间件位于 `internal/middleware/auth.go`，提供 `GenerateToken` 和 Bearer Token 校验。

## 跨域

| 库 | 版本 | 用途 |
|----|------|------|
| gin-contrib/cors | v1.7.7 | CORS 中间件 |

允许 `http://localhost:5173`（前端开发服务器）跨域访问。

## 云存储

| 库 | 版本 | 用途 |
|----|------|------|
| qiniu/go-sdk/v7 | v7.26.12 | 七牛云 SDK（头像上传） |

上传器位于 `pkg/qiniu`，支持字节/Reader 上传，生成公开/私有 URL。

## 工具库

| 库 | 版本 | 用途 |
|----|------|------|
| google/uuid | v1.6.0 | UUID 生成（task_id、message_id） |

## 项目结构

```
backend/
├── cmd/
│   └── server/
│       └── main.go          # 入口
├── configs/
│   └── config.yaml          # 配置文件
├── internal/
│   ├── conf/                # 配置加载
│   ├── handler/             # HTTP 处理器（task, session, message, agent, avatar, stream）
│   ├── stream/              # SSE 流式写入（Redis Stream → MySQL 批量刷写）
│   ├── middleware/           # 中间件（auth, cors, logger）
│   ├── model/               # 数据模型（task, session, message）
│   ├── generated/           # 契约生成的 Go 类型（勿手改）
│   ├── vo/                  # 统一响应封装
│   ├── controller/impl/     # （预留）
│   ├── dao/                 # （预留）
│   └── service/impl/        # （预留）
├── pkg/
│   ├── db/                  # MySQL 单例连接（sync.Once + AutoMigrate）
│   ├── redis/               # Redis 客户端 + StreamKey 工具
│   ├── agentend_client/     # AgentEnd HTTP 客户端
│   └── qiniu/               # 七牛云上传
├── Makefile                 # build / run / fmt / tidy
├── go.mod
└── go.sum
```

## API 响应格式

所有接口统一使用 `{code, data, msg}` 格式：

| 场景 | HTTP 状态码 | code | 示例 |
|------|------------|------|------|
| 成功 | 200 | 0 | `{"code":0,"data":{"message":"pong"}}` |
| 创建 | 201 | 0 | `{"code":0,"data":{...}}` |
| 请求错误 | 400 | 400 | `{"code":400,"msg":"invalid"}` |
| 未找到 | 404 | 404 | `{"code":404,"msg":"not found"}` |
| 未授权 | 401 | 401 | `{"code":401,"msg":"missing authorization header"}` |
| 服务不可用 | 503 | 503 | `{"code":503,"msg":"service unavailable"}` |
| 内部错误 | 500 | 500 | `{"code":500,"msg":"internal error"}` |

## 关键设计决策

- **分层架构**：handler / stream / model / vo 四层，interface 与 impl 分离，每层可独立测试
- **配置方案**：gopkg.in/yaml.v3 直接解析，不引入 Viper，保持轻量；支持环境变量覆盖敏感字段
- **数据库连接**：sync.Once 单例，`db.Init(cfg)` 初始化，`db.GetDB()` 全局获取，启动时 AutoMigrate
- **SSE 流式**：StreamWriter 发布到 Redis Stream → Handler.ServeStream 消费并分块推送，30min 超时保护
- **JWT Auth**：中间件预置但 ping 接口不挂，后续业务接口按需启用
- **请求日志**：使用标准库 slog，按状态码分级（>=500 ERROR, >=400 WARN, 其余 INFO）
