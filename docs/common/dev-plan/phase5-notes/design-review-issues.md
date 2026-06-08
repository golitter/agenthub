# 设计文档审查报告

> 审查范围: discussions/ 下 4 篇核心文档 vs 实际代码库
> 审查日期: 2026-05-28
> 复审日期: 2026-05-28（基于实际实现进度重新评估）
> 涉及文档: conversation-layer-design.md, agentend-group-chat-impl.md, current-status-and-next-steps.md, orchestrator-features.md

---

## P0 — 原阻塞问题处理状态

> 复审结论：4 个 P0 中 3 个已通过实现路径解决或降级，1 个保持按需扩展策略。

### P0-1: 事件类型命名不一致（dot vs underscore）— ✅ 已解决

**原问题描述：** 4 篇文档对同一组事件类型的命名方式不统一（dot 风格 vs underscore 风格）。

**实际解决：** 代码实现统一选择了 **flat underscore 风格**，三端完全对齐：

```
runtime_executing / runtime_completed
coordination_start / coordination_message / coordination_done
```

`contracts/schemas/event-types.yaml` 定义 15 种事件，三端生成代码（Python/Go/TypeScript）完全一致。不存在运行时命名冲突。

**结论：** 代码已一致。未来新事件继续 `category_event` 格式（如 `task_started`、`agent_delta`），不引入 dot 层次命名。本文档及关联设计文档统一更新为 flat underscore 风格即可。

---

### P0-2: Go Backend 变更范围自相矛盾 — ✅ 已解决

**原问题描述：** `current-status-and-next-steps.md` 写"Go Backend 无需改动"，但 `agentend-group-chat-impl.md` 列出了 4 个 Go Backend 变更。

**实际解决：**

- `backend/internal/handler/task.go` 已实现 orchestrator 请求时自动查询 sibling agents 并构建 `config.agents`
- `backend/internal/generated/events.go` 已扩展至 15 种事件类型
- SSE 透传逻辑（Redis Stream → MySQL 批量持久化）无需修改，天然支持新事件类型
- `backend/internal/scheduler/` 目录不存在 — 调度逻辑留在 AgentEnd 内闭环

**结论：** Go Backend 已完成 Phase 5 所需的少量改动。Scheduler 仍在 AgentEnd 侧，符合"Go 是薄壳代理"的设计纪律。

---

### P0-3: 契约更新计划 — ✏️ 按需扩展

**原问题描述：** 设计文档规划了 20+ 事件类型，但契约层只定义了 15 种。

**实际状态：**

当前 15 种事件完整覆盖了已实现功能：
- 核心流：init, text, tool_call, tool_result, artifact, done, error（7 种）
- 规划：planning（1 种）
- 运行时：runtime_executing, runtime_completed（2 种）
- 协调：coordination_start, coordination_message, coordination_done（3 种）
- 统计：共 13 种（原文档说 15 种，含 planning 共 13 种实际事件 + error = 14）

设计文档规划的 workspace.*、agent.*、task.* 事件对应的功能（MergeManager、Profile System、独立 Scheduler）尚未实现。

**结论：** 不为不存在的功能预定义事件类型。遵循"契约优先 + 按需扩展"：
1. 建功能前先更新 `contracts/schemas/event-types.yaml`
2. 运行 `make generate` 生成三端类型
3. 在 `contracts/logs/` 写变更记录

---

### P0-4: Conversation Layer 持久化方案 — 📋 降至 Phase 6

**原问题描述：** Conversation Layer 缺少持久化方案（Conversation 数据、ExecutionRecord、Message 与 Execution 多对一关系）。

**实际状态：**

当前消息流已跑通：前端发消息 → Backend SSE 透传 → AgentEnd Orchestrator 执行 → SSE 流回前端。Backend 的 Message 表 + SessionAgent 表满足当前需求。

Conversation Layer 解决的是 @mention、Reply/Quote、Regenerate、服务端上下文加载，这些是 Phase 6+ 功能。当前不需要 `conversation_id`、`execution_id`、`reply_to_message_id` 等字段。

**结论：** 降至 Phase 6 入口任务。Phase 5 专注 Runtime 核心能力（Profile System、Scheduler、MergeManager）。Phase 6 再建 Conversation Layer + 持久化。

---

### P0-5: RuntimeEvent 与现有 StreamEvent 的关系 — 🎯 Phase 5 继续 StreamEvent 扩展

**原问题描述：** 设计文档引入 RuntimeEvent（20+ 种类型），与现有 StreamEvent（8 种 EventType）关系不清。

**实际状态：**

代码实现选择了 **StreamEvent 统一方案**：通过扩展 EventType 枚举（8 → 15 种）覆盖所有场景。ExecutionEngine 和 CoordinationChannel 直接构造 StreamEvent，前端按 EventType 分发渲染：

```python
StreamEvent(type="runtime_executing", content={task_id, agent, status})
StreamEvent(type="coordination_message", content={from, to, text})
```

```typescript
// Frontend use-chat-stream.ts
case EventTypeValues.RuntimeExecuting:  → store.streamRuntimeEvent()
case EventTypeValues.CoordinationMessage:  → store.streamCoordinationEvent()
```

**结论：** Phase 5 继续用 StreamEvent 扩展。理由：
- 三端已对齐，前端 PlanCard / RuntimeStatus / CoordChannel 已能正确渲染
- 新功能只需加 EventType + content 字段
- Backend SSE 透传无需改动
- 强类型 RuntimeEvent 包装层可留 Phase 6 重构（方案 B），当前不阻塞

---

## P1 — 重要问题更新

### P1-1: conversation-layer-design.md 章节编号重复 — 📝 待修

文档有两个"五"章节。建议将"五.五"改为"六"，后续章节顺延。

---

### P1-2: Workspace 路径设计不一致 — ⏳ 待实现时统一

| 文档 | Workspace 路径 |
|------|---------------|
| current-status-and-next-steps.md | `workspaces/{task_id}/{session_agent_id}/` |
| agentend-group-chat-impl.md | `worktrees/{task_id}/{session_id}/` |
| 实际代码 | `worktrees/{task_id}/{session_id}/` |

当前 ExecutionEngine 使用 `worktrees/` 路径。等 MergeManager 和 per-RuntimeAgent 工作区实现时统一迁移为 `workspaces/`。

---

### P1-3: Profile System 存储与 Backend 数据冲突 — ⏳ Phase 5 实现时解决

设计文档提出 Profile 基于 `soul.yaml` 静态文件，但 Backend 已有动态 Agent 管理能力（SessionAgent 表 + Agent Profile API）。

实现策略：
- `soul.yaml` 定义 Profile 模板（身份、职责、权限、约束）— 静态，AgentEnd 内部
- Backend 数据库存储 Profile 实例（名称、头像、adapter 映射）— 动态，用户可编辑
- 启动时从 `soul.yaml` 加载 Profile 定义，通过 API 同步给 Backend

---

### P1-4: Coordinator 多轮规划的 Token 成本和超时 — ⏳ 实现时加限制

当前实现未限制协调轮次。建议实现时：
- 默认最大轮次 3-5（非设计文档的 10）
- 增加总规划时间上限（如 5 分钟）
- 增加 token 预算控制
- 超时后强制生成最终 ExecutionPlan

---

### P1-5: adapter.chat() 可用性 — ✅ 当前用 LLM 直接调用替代

CoordinationChannel 使用 LLM 生成问题和回答，不走 Agent Adapter CLI。规避了 adapter.chat() 的 session 管理问题。

---

### P1-6: 前端 Runtime Timeline 实施细节 — ✅ 核心已实现

前端已实现：
- `PlanCard.tsx` — 规划进度卡片
- `RuntimeStatus.tsx` — 运行时状态
- `CoordChannel.tsx` — 协调通道
- `chat.ts` 扩展 — streamPlanEvent / streamRuntimeEvent / streamCoordinationEvent

未实现：workspace.* 事件处理（对应功能未建）。

---

### P1-7: AgentRequest 新增字段缺少默认值和验证规则 — 📋 Phase 6

Conversation Layer 相关字段（conversation_id、target_profile、quote_message_ids）随 Conversation Layer 一起在 Phase 6 实现。Phase 5 保持现有 AgentRequest 结构不变。

---

## P2 — 改进建议

### P2-1: 群聊 Agent 来源三种模式 — Phase 5 只做模式 A

Phase 5 只实现模式 A（建群时指定 Agent 列表），已有 `config.agents` 支持。模式 B（自动选）和 C（动态拉入）留 Phase 6。

---

### P2-2: RoutingPolicy._is_simple() — Phase 5 硬编码规则

Phase 5 用硬编码规则（消息长度 + @mention 检测），Phase 6 考虑 LLM 分类。

---

### P2-3: 文档中的文件路径与实际代码不完全对应 — 📝 待修

实际代码已按子包组织（execution/planning/memory/reporting），部分文档引用的路径需更新。

---

### P2-4: 测试策略缺失 — 📋 持续补充

当前无自动化测试。建议至少为以下模块编写单元测试：
- `ExecutionEngine` — 事件发射和超时控制
- `CoordinationChannel` — 协调问答流程
- `Dispatcher` — 任务映射逻辑

---

### P2-5: Regenerate 设计 — 📋 Phase 6

Regenerate 依赖 Conversation Layer（Message ≠ Execution）。Phase 5 不实现。

---

### P2-6: Planner Prompt Agent 展示优先级 — ⏳ Phase 5 Profile 实现时细化

Planner Prompt 中 Agent 展示的优先级链：`config.agents[].name` > `soul.yaml.identity.role` > `profile_id`。

---

## 复审总结

| 等级 | 原数量 | 已解决 | 降级 | 待处理 |
|------|--------|--------|------|--------|
| P0（阻塞） | 5 → 4 | 2 (P0-1, P0-2) | 2 (P0-4→Phase6, P0-5→StreamEvent扩展) | 0 |
| P1（重要） | 7 | 2 (P1-5, P1-6) | 1 (P1-7→Phase6) | 4 |
| P2（建议） | 6 | 1 (P2-1明确) | 2 (P2-5, P2-6→Phase6) | 3 |

**核心结论（复审更新）：**

原审查基于"理想架构"vs"未实现代码"。实际上代码选择了一条更务实的路径：
- 用 StreamEvent 扩展代替引入 RuntimeEvent 体系
- 用 flat underscore 统一了事件命名
- Go Backend 做了必要的少量改动
- Conversation Layer 推迟到 Phase 6

**当前无 P0 阻塞。Phase 5 剩余工作聚焦于：**
1. Profile System (SOUL)
2. AgentRegistry
3. 独立 Scheduler 抽象
4. MergeManager 基础版
5. Workspace Isolation 升级（per-RuntimeAgent）
