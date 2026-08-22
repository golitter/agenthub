# AGENTS.md — backend

基于 Go Gin + GORM + MySQL 的后端服务，采用 **Controller → Service → DAO 三层架构**：Controller 仅做参数绑定/校验和 HTTP 响应，Service 承载业务逻辑并返回 `BizError`，DAO 接口可 Mock 替换。Go >=1.26，Air 热重载。

## 目录结构

```
cmd/                          # server（主服务入口 + 优雅关闭）+ skill-migrate / skill-reconcile（Skill MinIO 迁移/对账工具）
configs/config.yaml           # 配置文件
internal/
├── app/                      # 应用组装（DAO → Service → Controller + Gin 路由）
├── conf/                     # 配置加载（YAML + .env overlay）
├── controller/               # Controller 层
│   ├── controller.go         # 接口定义（统一 RegisterRoutes）
│   └── impl/                 # 15 组实现（task, session, message, stream, agent_profile, avatar, asset, diff_snapshot, workspace, announcement, contact_group, skill, admin, agent, artifact）
│       └── errors.go         # BizError → HTTP 响应映射
├── service/                  # Service 层（纯业务逻辑，无 Gin 依赖）
│   ├── service.go            # 接口定义 + DTO
│   ├── bizerr.go             # 统一业务错误（Code + Message）
│   ├── skill_validator.go    # 技能 zip 包校验（SKILL.md + 解压白名单 + 大小限制）
│   └── impl/                 # 11 组实现 + stream_helper + task_route（Agent 路由） + group_chat_window + skill_operation_worker + task_cleanup_worker
├── dao/                      # DAO 层（接口可 Mock 替换）
│   ├── dao.go + *_dao.go        # 11 组接口（含 Skill/Task cleanup outbox 与 Artifact）
│   └── gorm/ + mock/         # GORM 实现 + cascade.go（级联删除）；mock 测试替身
├── stream/                   # SSE 流式中转（RuntimeHub 内存推送 + Redis Stream → MySQL 批量刷写）
├── middleware/                # 中间件（auth, admin_auth, body_limit, cors, logger, rate_limit, service_auth）
├── model/                    # 16 个数据模型（含 Skill/Task cleanup outbox 与 Artifact，详见 01-models.md）
├── generated/                # 契约生成的 Go 类型（勿手改）
└── vo/                       # 统一响应封装
pkg/
├── db/ + redis/              # MySQL 单例（mutex + Ping）；Redis 客户端 + StreamKey
├── agentend_client/          # AgentEnd HTTP 客户端
├── package_store/ + skill_upload_session/  # Skill MinIO 对象存储 + 断点上传会话
├── artifact_store/           # Artifact 私有 MinIO 对象存储（capability token 直传）
└── storage/ + qiniu/         # 头像存储抽象（storage：MinIO 默认 + 本地可选）；qiniu/ 已废弃清空
```

## 常用命令

> 根目录 Makefile 执行，排查看 `../logs/backend.log`

```bash
make run-backend       # 启动（Air 热重载）
make stop-backend      # 停止
make restart-backend   # 重启
make status            # 查看状态
make backend tidy      # go mod tidy
```

## 配置文件

| 文件 | 用途 | 入库 |
|------|------|------|
| `configs/config.yaml` | 主配置（MySQL/Redis/JWT/Admin/CORS，含 `artifact_storage` 段与 `agentend.service_auth_enabled`） | ✅ |
| `configs/config.example.yaml` | 主配置模板（敏感值留空） | ✅ |
| `.env` | 头像/Skill/Artifact MinIO 凭据 + AgentEnd 服务令牌 + MySQL/Redis/CORS 等本机环境覆盖，通过 godotenv 注入 | ❌ |
| `.env.example` | `.env` 模板（MinIO 凭据 + 本机服务地址覆盖，密钥字段脱敏） | ✅ |

首次运行前：

```bash
cp .env.example .env && cp configs/config.example.yaml configs/config.yaml  # config.yaml 不存在时；编辑 .env 填 Asset MinIO 凭据，无 MinIO 时须显式 AVATAR_STORAGE_WRITE_PROVIDER=local（不会自动回退）
```

> Docker 环境下使用 `docker/configs/backend/.env`（结构相同），详见 [../docs/guides/docker-deployment.md](../docs/guides/docker-deployment.md)。

## 详细文档

详见 [docs/reference/details.md](docs/reference/details.md)。
