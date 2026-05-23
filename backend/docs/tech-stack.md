# Backend 技术栈

## 语言与运行时

| 工具 | 版本 | 用途 |
|------|------|------|
| Go | 1.26.2 | 编译型后端语言 |
| Module | agenthub/backend | Go 模块名 |

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

## 配置管理

| 库 | 版本 | 用途 |
|----|------|------|
| gopkg.in/yaml.v3 | v3.0.1 | YAML 配置文件解析 |

配置文件位于 `configs/config.yaml`，包含 MySQL 连接信息和 JWT 配置。

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

## 项目结构

```
backend/
├── cmd/
│   └── server/
│       └── main.go          # 入口：加载配置 → 连 DB → 中间件 → 路由 → 启动
├── configs/
│   └── config.yaml          # MySQL + JWT 配置
├── docs/
│   └── api/                 # API 文档（预留）
├── internal/
│   ├── conf/
│   │   └── conf.go          # YAML 配置加载（Config / MySQLConfig / JWTConfig）
│   ├── controller/
│   │   └── impl/            # 控制器实现（预留）
│   ├── dao/
│   │   ├── gorm/            # GORM 数据访问实现（预留）
│   │   └── mock/            # DAO Mock（预留）
│   ├── middleware/
│   │   ├── auth.go          # JWT Auth 中间件
│   │   ├── cors.go          # CORS 中间件
│   │   └── logger.go        # 请求日志（slog，按状态码分级）
│   ├── model/               # 数据模型（预留）
│   ├── service/
│   │   └── impl/            # 业务逻辑实现（预留）
│   └── vo/
│       └── response.go      # 统一响应（OK / Created / BadRequest / NotFound / InternalError）
├── pkg/
│   └── db/
│       └── mysql.go         # MySQL 单例连接（sync.Once）
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
| 内部错误 | 500 | 500 | `{"code":500,"msg":"internal error"}` |

## 关键设计决策

- **分层架构**：采用 gormlab 模式（controller / service / dao / model），interface 与 impl 分离，每层可独立测试
- **配置方案**：gopkg.in/yaml.v3 直接解析，不引入 Viper，保持轻量
- **数据库连接**：sync.Once 单例，`db.Init(cfg)` 初始化，`db.GetDB()` 全局获取
- **JWT Auth**：中间件预置但 ping 接口不挂，后续业务接口按需启用
- **请求日志**：使用标准库 slog，按状态码分级（>=500 ERROR, >=400 WARN, 其余 INFO）
