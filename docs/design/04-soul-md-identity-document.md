# SOUL.md — Agent 身份文档设计

## 实现了什么

每个 Session 保存一份最多 300 字的 `soul_md`，前端 Agent 详情页可读写。Backend 将当前运行 session 的 SOUL 注入 `AgentRequest.config.soul_md`；AgentEnd 在创建工作区或 shared 目录时写入对应 `SOUL.md`，再由规则或 Orchestrator prompt 读取。

## 怎么实现的

### Backend 持久化 (`backend/internal/service/impl/agent_profile_service.go`)

```go
func (svc *AgentProfileService) GetSoul(sessionID string) (string, error)
func (svc *AgentProfileService) UpdateSoul(sessionID, soulMD string) error
```

`AgentProfileController` 暴露 `GET/PUT /api/sessions/:sessionId/soul`，`TaskService.buildAgentRequest` 按运行 session 读取 `sessions.soul_md`。

### AgentEnd 写入 (`agentend/src/api/v1/agent.py`)

```python
soul_md = config.get("soul_md", "")
if soul_md:
    soul_path.write_text(soul_md, encoding="utf-8")
```

非 Orchestrator 写入 worktree 内对应 config 目录；Orchestrator 写入 shared `.agent/SOUL.md`，供 prompt 构造读取。

## 概述

每个 Agent 拥有一份独立的 SOUL.md 身份文档，由用户在 Agent 详情页编写（≤300 字），描述该 Agent 的人格、行为准则和专业领域。运行时由 Agent 自身读取并注入系统提示词。

## 核心原则

**每个 Agent 只注入自己的 SOUL.md**。orchestrator 不读取子 agent 的 SOUL.md，子 agent 的 SOUL.md 由各自运行时自注入。

## 数据流

```
前端 AgentProfilePage
  │ PUT /api/sessions/:sessionId/soul { soul_md: "..." }
  ▼
后端 Go (agent_profile_service.go)
  │ 1. 校验字数 ≤300
  │ 2. 写入 sessions.soul_md
  ▼
MySQL (sessions 表)
  │ task.go RunTask: 通过 session_id 读取 soul_md → 注入 AgentRequest.config
  ▼
Agentend Python (agent.py)
  │ 根据 agent_type 分流:
  │
  ├─ orchestrator:
  │   shared_dir = {repo}/../worktrees/{task_id}/shared/.agent/
  │   orchestrator.py → {shared_dir}/SOUL.md
  │   prompts.py 读取 {shared_dir}/SOUL.md → 注入 REASON_PROMPT
  │
  └─ 非 orchestrator (claude-code / opencode / codex):
      workspace = {repo}/../worktrees/{task_id}/{session_id}/
      agent.py → {workspace}/{config_dir}/SOUL.md
      rules/builtin.py SoulRule 读取 → system_prompt_append
```

## SOUL.md 注入方式

| Agent 类型 | 写入位置 | 读取方式 |
|-----------|---------|----------|
| Orchestrator | `{shared_dir}/SOUL.md` | `prompts.py` `build_reason_prompt` 读取 → `{soul_section}` |
| 非 orchestrator | `{workspace}/{config_dir}/SOUL.md` | `rules/builtin.py` `SoulRule` 读取 → `system_prompt_append` |

- orchestrator 通过 prompts.py 注入，因为 orchestrator 自建系统提示词，不使用 rules engine
- 非 orchestrator 通过 SoulRule（priority=8）注入，规则引擎将内容追加到 system_prompt_append

## 三端改动

### 1. 后端 (Go)

**model/session.go** — 新增字段:
```go
SoulMD string `gorm:"size:300" json:"soul_md,omitempty"`
```

**controller/impl/agent_profile_controller.go** + **service/impl/agent_profile_service.go** — 新增端点:
- `GET /api/sessions/:sessionId/soul` — 返回 `{ soul_md, session_id }`
- `PUT /api/sessions/:sessionId/soul` — 允许空字符串（清除），校验 ≤300 字

**service/impl/task_service.go** `RunTask` — 注入 soul_md 到 AgentRequest.config:
- orchestrator: config 包含 `soul_md`（自身）
- 非 orchestrator: config 包含 `{ soul_md }`

### 2. 前端 (React)

**lib/api.ts** — `AgentDetail` 增加 `soul_md`；新增 `fetchAgentSoul` / `updateAgentSoul`

**pages/AgentProfilePage.tsx** — SOUL.md 编辑模块: textarea + 实时字数统计 + 保存/清除

### 3. Agent 端 (Python)

**api/v1/agent.py** — workspace 创建后写入 SOUL.md 到 `{workspace}/{config_dir}/SOUL.md`

**adapters/orchestrator.py** `stream_chat` — 写入 `{shared_dir}/SOUL.md`

**orchestrator/planning/prompts.py** `build_reason_prompt` — 读取 orchestrator 自身 SOUL.md → `{soul_section}`

**rules/builtin.py** `SoulRule` (priority=8) — 读取 `{workspace}/{config_dir}/SOUL.md` → `system_prompt_append`

## 文件系统路径

```
{repo}/../worktrees/{task_id}/
├── shared/.agent/
│   └── SOUL.md                    ← orchestrator 自身身份
├── {session_1}/
│   └── .claude/SOUL.md            ← 非 orchestrator agent 的身份
└── {session_2}/
    └── .opencode/SOUL.md
```

## 后端路由

```
创建任务 → 每个 agent 分配 session_id → 写入 sessions 表含 soul_md
编辑 SOUL.md → PUT /api/sessions/:sessionId/soul
运行 agent → POST /api/tasks/:taskId/run → 后端通过 session_id 读取 soul_md → config → agentend
```

> 注意：未运行（无 workspace）时仅存 DB，下次 RunTask 自动写入文件系统。
