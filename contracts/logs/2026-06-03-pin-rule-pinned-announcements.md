# 2026-06-03 PinRule: Pinned Announcements → Agent 约束注入

**类型**: agentend + backend（无跨端契约变更）

## 变更说明

将 Orchestrator 的 Pin 约束源从文件系统 `PinMemory`（`_pins.yaml`）改为 Backend MySQL 的 pinned announcements，通过 Rules 引擎统一注入所有 Agent。

### 变更内容

1. **Backend**: `ListAnnouncements` 新增 `?pinned=true` query 参数过滤
2. **Agentend**: `BackendClient` 新增 `get_pinned_announcements(task_id)` 方法
3. **Agentend**: 新增 `PinRule`（priority=9），从 `rule_ctx["pinned_announcements"]` 读取并格式化为约束文本
4. **Agentend**: `/stream` 和 `/execute` 端点在 rule evaluation 前预获取 pinned announcements
5. **Agentend**: `OrchestratorAdapter.stream_chat` 提取 `system_prompt_append` 写入 `initial_state["pin_context"]`
6. **Agentend**: `skill_prepare_node` 移除 PinMemory 文件读取，改为从 state 透传

### 数据流

```
pinned announcement → Backend MySQL → BackendClient → PinRule → system_prompt_append
  ├─ 非 Orchestrator: CLI args
  └─ Orchestrator: initial_state["pin_context"] → reason_node → SystemMessage #2
```

## 影响范围

- **Backend**: 仅 handler 层增加 query parameter，无协议变更
- **Agentend**: 内部规则引擎 + adapter 层变更，无 API 协议变更
- **Frontend**: 无影响
- **contracts/schemas/**: 无变更
