# 遗留清单 — 本期未实现项

> 本期开发周期已于 **2026-06-09** 结束。以下为基于 2026-06-03 代码审计的未实现项，作为后续迭代的输入。
> 已实现的核心能力见 [README.md](README.md) 交付状态；本文档不阻塞交付，仅作记录。
>
> **状态更新（2026-07-23 文档校对）**：下列条目为 2026-06-09 时的快照，后续已有变动 ——
> - 「离开群聊」（条目 12）已在 `frontend/src/components/chat/SidebarActions.tsx` 接入 `leaveTask` API，当前已实现；
> - 「Durable Resume」实际基于文件持久化（`logs/session_mappings.json` + `shared/.agent/memory/conversation_memory.json`），而非 LangGraph MemorySaver，详见 [agentend/docs/design/07-session-mapping.md](../../../agentend/docs/design/07-session-mapping.md)；
> - `backend/docs/api/` 目录并未创建，REST API 端点文档现集中于 [backend/docs/design/02-handlers.md](../../../backend/docs/design/02-handlers.md) 与 [backend/docs/design/00-backend-deep-dive.md](../../../backend/docs/design/00-backend-deep-dive.md) 的 API 地图。

## 统计概览

| 模块 | P1 遗留 | P2 遗留 | 总计 |
|------|---------|---------|------|
| AgentEnd (Runtime) | 1 | 2 | 3 |
| Backend (Go) | 0 | 0 | 0 |
| Frontend (React) | 2 | 3 | 5 |
| DevOps/部署 | 0 | 2 | 2 |
| 文档/交付 | 2 | 2 | 4 |
| **合计** | **5** | **9** | **14** |

> 本期相比 2026-06-02 审计已实现 8 项：MemorySaver 持久化、Conflict-Resolution、执行级 Retry、Dynamic Replanning、Durable Resume、Skills API、Service 层抽取、Docker Compose + Nginx 容器化部署。

---

## 一、AgentEnd (Python) — Runtime 升级

### P1 — 核心能力

| # | 功能 | 当前状态 | 说明 | 来源 |
|---|------|----------|------|------|
| 1 | **Profile 目录结构完善** | 🔧 部分 | SOUL.md 可编辑+注入已实现，但 `agentend/src/profiles/` 下缺少完整的 Profile 定义目录（capability、personality、constraints 等） | Phase 6 |

### P2 — 增强能力

| # | 功能 | 当前状态 | 说明 | 来源 |
|---|------|----------|------|------|
| 2 | **Capability Permission** | 📋 未实现 | 基于 SOUL Profile 的 Agent 权限检查（哪些工具可用、哪些目录可访问）未实现。现有规则引擎（SoulRule、PinRule、SafetyRule）但不包含能力/权限维度 | Phase 6 |
| 3 | **Prompt Renderer** | 📋 未实现 | 模板化 Prompt 组装未实现 | Phase 6 |

### ✅ 本期已实现

| # | 功能 | 实现说明 |
|---|------|----------|
| ~~MemorySaver 持久化~~ | ✅ 文件系统级持久化（conversation_memory.json + _pins.yaml），支持增量保存/替换 |
| ~~Conflict-Resolution Task~~ | ✅ `git_ops.py` merge_branch() 自动检测冲突文件，支持 merge --abort 回滚 |
| ~~执行级 Retry~~ | ✅ `graph.py` ask_agent 最多重试 3 次，固定延迟递增 |
| ~~Dynamic Replanning~~ | ✅ REVIEW 节点检查失败任务，触发重规划（max_iterations 控制） |
| ~~Durable Resume~~ | ✅ LangGraph MemorySaver checkpoint + is_resume 会话恢复逻辑 |

---

## 二、Backend (Go) — API 补全

> **本期全部完成，无遗留项。**

### ✅ 本期已实现

| # | 功能 | 实现说明 |
|---|------|----------|
| ~~Merge API~~ | ✅ `POST /api/workspace/task/:taskId/merge-to-main` 代理到 AgentEnd |
| ~~Skills API~~ | ✅ `agentend/src/api/v1/skills.py` + `backend/internal/service/impl/skill_service.go` + `backend/internal/controller/impl/skill_controller.go` |
| ~~Service 层抽取~~ | ✅ 后端已重构为 Controller + Service 分层（`controller/impl/` + `service/impl/`） |

---

## 三、Frontend (React) — UI/UX 打磨

### P1 — 核心体验

| # | 功能 | 当前状态 | 说明 | 来源 |
|---|------|----------|------|------|
| 6 | **响应式布局** | 📋 未实现 | 主聊天布局（ImPage 三栏）无 1280/1024/768 适配。Admin 页面有基础 responsive grid，但聊天界面未做断点适配 | Phase 7 |
| 7 | **网络错误处理** | 📋 未实现 | 无全局 Toast 通知系统。SSE 断连无用户可见提示，网络错误不保留已输入内容 | Phase 7 |

### P2 — 增强体验

| # | 功能 | 当前状态 | 说明 | 来源 |
|---|------|----------|------|------|
| 8 | **Agent 断连重连** | 🔧 部分 | SSE 层有自动重连（`lib/sse.ts`），但无 UI 反馈。需显示连接状态指示 + 重连进度 + 自动重试 | Phase 7 |
| 9 | **Agent 超时状态** | 📋 未实现 | Agent 长时间无响应时无超时 UI。需超时状态展示 + 手动重试按钮 | Phase 7 |
| 10 | **空状态引导** | 🔧 部分 | 有基础 PlaceholderPage 组件，但空状态设计较简单，引导性不足，缺少操作引导 | Phase 7 |

### 代码债务

| # | 项目 | 位置 | 说明 |
|---|------|------|------|
| 11 | API 类型迁移 | `frontend/src/lib/api.ts` | 4 处 `TODO: migrate to generated types from contracts/schemas` |
| 12 | 离开群聊 | `frontend/src/components/im/RightSidebar.tsx` | `/* TODO: leave group */` 未实现 |

---

## 四、DevOps / 部署

### ✅ 本期已实现

| # | 功能 | 实现说明 |
|---|------|----------|
| ~~Docker Compose~~ | ✅ `docker/` 目录含 docker-compose.yml + Backend/Frontend Dockerfile + Nginx 配置 + precheck 脚本 |
| ~~Nginx 反向代理~~ | ✅ `docker/frontend/nginx.conf` 已配置 SPA 路由 + /api 代理 + SSE 支持 |

### P2 — 遗留

| # | 功能 | 当前状态 | 说明 | 来源 |
|---|------|----------|------|------|
| 15 | **部署状态卡片** | 📋 未实现 | 前端无部署进度展示 | Phase 6 |

---

## 五、文档 / 交付物

### P1 — 关键缺失

| # | 项目 | 当前状态 | 说明 | 来源 |
|---|------|----------|------|------|
| 16 | **API 参考文档** | 📋 未写 | 无完整的 REST API 端点文档（请求/响应格式、错误码等）。`backend/docs/api/` 目录存在但为空 | Phase 7 |
| 17 | **产品功能说明书** | 📋 未写 | 缺少完整的产品功能说明（功能清单、交互流程、技术选型） | Phase 7 |

### P2 — 增强交付

| # | 项目 | 当前状态 | 说明 | 来源 |
|---|------|----------|------|------|
| 18 | **预置 Demo 数据脚本** | 📋 未写 | 无一键填充测试数据的脚本 | Phase 7 |
| 19 | **3 分钟 Demo 视频** | 📋 未做 | 演示脚本未编写，视频未录制 | Phase 7 |

---

## 六、稳定性 — 未做系统测试

以下项本期未做完整系统测试：

- [ ] 所有 API 端点正常响应（无 500）
- [ ] SSE 流稳定无断裂（连续运行 10 分钟）
- [ ] 会话切换无数据丢失
- [ ] 多 Agent 并发无竞态
- [ ] 错误恢复后可继续使用

---

## 后续迭代建议顺序

> 不构成本期承诺，仅作为下一期迭代的参考。

1. **响应式布局 + 网络错误处理** — Demo 演示必备体验
2. **API 参考文档 + 产品功能说明书** — 交付完整性
3. **Profile 目录结构 + Capability Permission** — SOUL 体系完整化
4. **Demo 视频录制** — 演示收尾
