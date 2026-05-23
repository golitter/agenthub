# AGENTS.md — backend

基于 Go Gin + GORM + MySQL 的后端服务，采用 gormlab 分层架构（controller / service / dao / model），YAML 配置加载，JWT 认证中间件。Go >=1.22。

## 目录结构

```
cmd/server/main.go            # 入口：加载配置 → 连 DB → 中间件 → 路由 → 启动
configs/config.yaml           # MySQL + JWT 配置
internal/
├── conf/conf.go              # YAML 配置加载
├── controller/impl/          # 控制器实现（预留）
├── dao/gorm/                 # GORM 数据访问（预留）
├── dao/mock/                 # DAO Mock（预留）
├── middleware/auth.go        # JWT Auth 中间件
├── middleware/cors.go        # CORS 中间件
├── middleware/logger.go      # 请求日志（slog）
├── model/                    # 数据模型（预留）
├── service/impl/             # 业务逻辑（预留）
└── vo/response.go            # 统一响应
pkg/db/mysql.go               # MySQL 单例连接（sync.Once）
```

## 常用命令

> 通过根目录 Makefile 统一管理，需在项目根目录执行。

```bash
make run-backend            # 启动（热重载）
make stop-backend           # 停止
make restart-backend        # 重启
make status                 # 查看状态
```

如需手动启动：`cd backend && go run cmd/server/main.go`

- Makefile 完整说明：[docs/common/makefile-guide.md](../docs/common/makefile-guide.md)

## 详细文档

- 技术栈详情：[docs/tech-stack.md](docs/tech-stack.md)
