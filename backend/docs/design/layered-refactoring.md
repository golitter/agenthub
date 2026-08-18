# 后端分层架构重构记录 — 历史结论与维护边界

## 实现了什么

后端已经完成从旧 handler 结构到 Controller → Service → DAO 三层架构的重构。本文只保留历史结论和当前维护边界；当前实现细节以这些文档为准：

- `backend/docs/design/02-handlers.md`
- `backend/docs/design/05-wiring.md`
- `backend/docs/design/00-backend-deep-dive.md`

## 怎么实现的

### 当前结构

```text
internal/
├── app/          # 统一组装 DAO -> Service -> Controller，并注册路由
├── controller/   # Gin 入站边界，负责绑定参数和返回 HTTP 响应
├── service/      # 业务逻辑，返回 BizError，不依赖 gin.Context
├── dao/          # 数据访问接口、GORM 实现和 mock 替身
├── stream/       # SSE 中转能力，通过 DAO 接口写入持久化状态
└── model/        # GORM 数据模型
```

### 重构结果

| 目标 | 当前状态 |
|------|----------|
| 删除旧 `internal/handler/` | 已完成 |
| Controller 只处理 HTTP 边界 | 已完成 |
| Service 承载业务逻辑和错误语义 | 已完成 |
| DAO 接口可 mock 替换 | 已完成 |
| StreamWriter 不直接散落数据库细节 | 已通过 DAO 注入收敛 |
| 路由和依赖装配集中化 | 已收敛到 `internal/app.NewRouter` |

### 维护规则

1. 新业务模块按 Controller / Service / DAO 三层落位。
2. Controller 不直接依赖 GORM 查询细节。
3. Service 不依赖 `gin.Context` 或 HTTP 响应格式。
4. DAO 不依赖 AgentEnd client、vo 或 Controller。
5. 跨模块装配优先放在 `internal/app/`，避免在 `cmd/server/main.go` 继续膨胀。

### 历史说明

早期阶段计划、接线草案和临时迁移步骤已不再作为当前开发依据。需要理解现在的代码，请阅读：

1. `backend/docs/design/05-wiring.md`
2. `backend/docs/design/02-handlers.md`
3. `backend/internal/app/`
4. `backend/internal/service/service.go`
5. `backend/internal/dao/dao.go`
