# AGENTS.md — backend

基于 Go Gin + GORM + MySQL 的后端服务，采用 **Controller → Service → DAO 三层架构**。Go >=1.26，Air 热重载。

## 目录结构

```
cmd/server/main.go            # 入口（DI 组装 + 优雅关闭）
configs/config.yaml           # 配置文件
internal/
├── conf/                     # 配置加载（YAML + .env overlay）
├── controller/               # Controller 层
│   ├── controller.go         # 接口定义（统一 RegisterRoutes）
│   └── impl/                 # 13 组实现（task, session, message, stream, agent_profile, avatar, diff_snapshot, workspace, announcement, contact_group, skill, admin, agent）
│       └── errors.go         # BizError → HTTP 响应映射
├── service/                  # Service 层（纯业务逻辑，无 Gin 依赖）
│   ├── service.go            # 接口定义 + DTO
│   ├── bizerr.go             # 统一业务错误（Code + Message）
│   └── impl/                 # 11 组实现 + stream_helper + task_route（Agent 路由） + group_chat_window
├── dao/                      # DAO 层（接口可 Mock 替换）
│   ├── dao.go                # 8 组接口（TaskDao, MessageDao, SessionDao, DiffSnapshotDao, AnnouncementDao, ContactGroupDao, SkillDao, AdminDao）
│   ├── gorm/                 # GORM 实现 + cascade.go（级联删除）
│   └── mock/                 # 测试替身
├── stream/                   # SSE 流式中转（RuntimeHub 内存推送 + Redis Stream → MySQL 批量刷写）
├── middleware/                # 中间件（auth, admin_auth, cors, logger, rate_limit）
├── model/                    # 11 个数据模型（task, session, message, diff_snapshot, session_agent, admin_setting, announcement, contact_group/item, skill_hub, agent_skill）
├── generated/                # 契约生成的 Go 类型（勿手改）
└── vo/                       # 统一响应封装
pkg/
├── db/                       # MySQL 单例（sync.Once）
├── redis/                    # Redis 客户端 + StreamKey
├── agentend_client/          # AgentEnd HTTP 客户端
└── qiniu/                    # 七牛云上传
```

## 架构分层

```
Controller（参数绑定 + vo 响应） → Service（纯业务逻辑 + BizError） → DAO（纯数据访问） → MySQL/Redis
```

- **Controller**：仅做参数绑定/校验和 HTTP 响应，通过构造函数内部组装 DAO→Service
- **Service**：接收 DTO，返回业务结果或 `BizError`，可独立单测（注入 mock DAO）
- **DAO**：接口在 `dao.go`，GORM 实现在 `gorm/`，mock 在 `mock/`

## 常用命令

> 根目录 Makefile 执行，排查看 `../logs/backend.log`

```bash
make run-backend       # 启动（Air 热重载）
make stop-backend      # 停止
make restart-backend   # 重启
make status            # 查看状态
make tidy              # go mod tidy
```

## 详细文档

详见 [docs/reference/details.md](docs/reference/details.md)。
