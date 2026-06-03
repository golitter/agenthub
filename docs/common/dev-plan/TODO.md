# TODO — 未实现功能清单

> 基于 2026-06-03 代码审计结果，列出所有尚未实现的功能项。
> 按优先级和所属模块分类，标注对应 Phase 来源。

## 统计概览

| 模块 | P1 待实现 | P2 待实现 | 总计 |
|------|-----------|-----------|------|
| AgentEnd (Runtime) | 1 | 2 | 3 |
| Backend (Go) | 0 | 2 | 2 |
| Frontend (React) | 2 | 3 | 5 |
| DevOps/部署 | 0 | 3 | 3 |
| 文档/交付 | 2 | 2 | 4 |
| **合计** | **5** | **12** | **17** |

> 对比 2026-06-02 审计（22 项），本次已实现 5 项：MemorySaver 持久化、Conflict-Resolution、执行级 Retry、Dynamic Replanning、Durable Resume。

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

### ✅ 已实现（2026-06-03 确认）

| # | 功能 | 实现说明 | 完成时间 |
|---|------|----------|----------|
| ~~MemorySaver 持久化~~ | ✅ 文件系统级持久化（conversation_memory.json + _pins.yaml），支持增量保存/替换 | Phase 6 期间 |
| ~~Conflict-Resolution Task~~ | ✅ `git_ops.py` merge_branch() 自动检测冲突文件，支持 merge --abort 回滚 | Phase 6 期间 |
| ~~执行级 Retry~~ | ✅ `graph.py` ask_agent 最多重试 3 次，固定延迟递增 | Phase 6 期间 |
| ~~Dynamic Replanning~~ | ✅ REVIEW 节点检查失败任务，触发重规划（max_iterations 控制） | Phase 6 期间 |
| ~~Durable Resume~~ | ✅ LangGraph MemorySaver checkpoint + is_resume 会话恢复逻辑 | Phase 6 期间 |

---

## 二、Backend (Go) — API 补全

### P1 — 核心能力

> 无 P1 待实现项。

### ✅ 已实现（2026-06-03 确认）

| # | 功能 | 实现说明 | 完成时间 |
|---|------|----------|----------|
| ~~Merge API~~ | ✅ `POST /api/workspace/task/:taskId/merge-to-main` 代理到 AgentEnd | Phase 6 期间 |

### P2 — 增强能力

| # | 功能 | 当前状态 | 说明 | 来源 |
|---|------|----------|------|------|
| 4 | **Skills API** | 📋 未实现 | `agent_profile.go` 有 TODO 注释：`fetch skills from agentend when a skills API is available`。前端无法展示 Agent 可用 Skills | Phase 6 |
| 5 | **Service 层抽取** | 🔧 架构 | 业务逻辑直接写在 handler 中，`service/impl/` 目录预留但为空。不影响功能，但影响可维护性 | 架构优化 |

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

### P2 — 均未实现

| # | 功能 | 当前状态 | 说明 | 来源 |
|---|------|----------|------|------|
| 13 | **Docker Compose** | 📋 未实现 | 项目无 Dockerfile / docker-compose。三端均为本地开发模式启动 | Phase 6 |
| 14 | **Nginx 反向代理** | 📋 未实现 | 无生产环境代理配置 | Phase 6 |
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

## 六、稳定性 — 待验证

以下项需在 Phase 7 进行系统测试验证：

- [ ] 所有 API 端点正常响应（无 500）
- [ ] SSE 流稳定无断裂（连续运行 10 分钟）
- [ ] 会话切换无数据丢失
- [ ] 多 Agent 并发无竞态
- [ ] 错误恢复后可继续使用

---

## 建议执行顺序

### 第一批（P1 核心，约 2-3 天）

1. **响应式布局** — Demo 演示必备
2. **Profile 目录结构** — SOUL 体系完整性
3. **网络错误处理 + Toast 系统** — 基础体验

### 第二批（P1 文档 + P2 核心，约 2 天）

4. **API 参考文档** — 交付必备
5. **空状态引导 + 断连重连 UI** — 体验打磨

### 第三批（P2 增强 + Demo 交付，约 2-3 天）

6. **Docker Compose + Nginx** — 生产化
7. **Dynamic Replanning + Durable Resume** — Runtime 增强（已实现）
8. **Demo 数据脚本 + 视频** — 交付收尾
