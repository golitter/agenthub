# AGENTS.md — backend

基于 Go Gin + GORM + MySQL 的后端服务，采用分层架构（handler / model / stream / vo），YAML 配置加载，JWT 认证中间件，Redis Stream 实时消息中转，七牛云存储头像上传。Go >=1.22，热重载使用 Air。

## 目录结构

```
cmd/server/main.go            # 入口
configs/config.yaml           # 配置文件
internal/
├── conf/                     # 配置加载
├── handler/                  # HTTP 处理器（task, session, message, agent, avatar, stream）
├── stream/                   # SSE 流式写入（Redis Stream → MySQL 批量刷写）
├── middleware/                # 中间件（auth, cors, logger）
├── model/                    # 数据模型（task, session, message）
├── generated/                # 契约生成的 Go 类型（勿手改）
├── vo/                       # 统一响应封装
├── controller/impl/          # （预留）
├── dao/                      # （预留）
└── service/impl/             # （预留）
pkg/
├── db/                       # MySQL 单例连接
├── redis/                    # Redis 客户端
├── agentend_client/          # AgentEnd HTTP 客户端
└── qiniu/                    # 七牛云上传
```

## 常用命令

> 通过根目录 Makefile 统一管理，需在项目根目录执行。

```bash
make run-backend            # 启动（Air 热重载）
make stop-backend           # 停止
make restart-backend        # 重启
make status                 # 查看状态
make tidy                   # go mod tidy
```

- Makefile 完整说明：[docs/common/makefile-guide.md](../docs/common/makefile-guide.md)

## 详细文档

- 技术栈详情：[docs/common/tech-stack.md](docs/common/tech-stack.md)
