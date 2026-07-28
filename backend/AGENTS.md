# AGENTS.md — backend

基于 Go Gin + GORM + MySQL 的后端服务，采用 **Controller → Service → DAO 三层架构**：Controller 仅做参数绑定/校验和 HTTP 响应，Service 承载业务逻辑并返回 `BizError`，DAO 接口可 Mock 替换。Go >=1.26，Air 热重载。

## 目录结构

```
cmd/server/main.go            # 入口（配置/基础设施初始化 + 优雅关闭）
configs/config.yaml           # 配置文件
internal/
├── app/                      # 应用组装（DAO → Service → Controller + Gin 路由）
├── conf/                     # 配置加载（YAML + .env overlay）
├── controller/               # Controller 层
│   ├── controller.go         # 接口定义（统一 RegisterRoutes）
│   └── impl/                 # 13 组实现（task, session, message, stream, agent_profile, avatar, diff_snapshot, workspace, announcement, contact_group, skill, admin, agent）
│       └── errors.go         # BizError → HTTP 响应映射
├── service/                  # Service 层（纯业务逻辑，无 Gin 依赖）
│   ├── service.go            # 接口定义 + DTO
│   ├── bizerr.go             # 统一业务错误（Code + Message）
│   ├── skill_validator.go    # 技能 zip 包校验（SKILL.md + 解压白名单 + 大小限制）
│   └── impl/                 # 11 组实现 + stream_helper + task_route（Agent 路由） + group_chat_window
├── dao/                      # DAO 层（接口可 Mock 替换）
│   ├── dao.go                # 8 组接口（TaskDao, MessageDao, SessionDao, DiffSnapshotDao, AnnouncementDao, ContactGroupDao, SkillDao, AdminDao）
│   ├── gorm/                 # GORM 实现 + cascade.go（级联删除）
│   └── mock/                 # 测试替身
├── stream/                   # SSE 流式中转（RuntimeHub 内存推送 + Redis Stream → MySQL 批量刷写）
├── middleware/                # 中间件（auth, admin_auth, body_limit, cors, logger, rate_limit）
├── model/                    # 11 个数据模型（task, session, message, diff_snapshot, session_agent, admin_setting, announcement, contact_group/item, skill_hub, agent_skill）
├── generated/                # 契约生成的 Go 类型（勿手改）
└── vo/                       # 统一响应封装
pkg/
├── db/                       # MySQL 单例（mutex 保护 + Ping 验证）
├── redis/                    # Redis 客户端 + StreamKey
├── agentend_client/          # AgentEnd HTTP 客户端
├── qiniu/                    # 七牛云上传
└── storage/                  # 存储层抽象（七牛云优先，本地磁盘兜底）
```

## 常用命令

> 根目录 Makefile 执行，排查看 `../logs/backend.log`

```bash
make run-backend       # 启动（Air 热重载）
make stop-backend      # 停止
make restart-backend   # 重启
make status            # 查看状态
make tidy              # go mod tidy
```

## 配置文件

| 文件 | 用途 | 入库 |
|------|------|------|
| `configs/config.yaml` | 主配置（MySQL/Redis/JWT/Admin/CORS 等） | ✅ |
| `.env` | 七牛云密钥（`QINIU_ACCESS_KEY` / `QINIU_SECRET_KEY`），通过 godotenv 注入 | ❌ |
| `.env.example` | `.env` 模板，密钥字段已脱敏 | ✅ |

首次运行前：

```bash
cp .env.example .env
# 编辑 .env 填入七牛云密钥；留空会自动回退到本地磁盘存储（uploads/）
```

> Docker 环境下使用 `docker/configs/backend/.env`（结构相同），详见 [../docs/guides/docker-deployment.md](../docs/guides/docker-deployment.md)。

## 详细文档

详见 [docs/reference/details.md](docs/reference/details.md)。
