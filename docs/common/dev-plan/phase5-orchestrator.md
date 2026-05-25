# Phase 5: Orchestrator 群聊协作 — LangGraph + 多 Agent 调度

> 目标: 群聊模式下用户发消息 → Orchestrator 拆解任务 → 多 Agent 依次执行 → 聊天流中汇报结果。
> 预估: 3-4 天
> 前置: Phase 4 完成 (产物卡片渲染可用)
> 设计文档: [orchestrator-plan-phase.md](../../internal/orchestrator-plan-phase.md)

## 交付标准

### AgentEnd 验证

```bash
# 准备 shared 目录
mkdir -p /tmp/orch-test/.agent/tasks

# 启动服务
cd agentend && uv run python -m src.app.main

# 调用 Orchestrator
curl -X POST http://localhost:8001/v1/agent/execute \
  -H "Content-Type: application/json" \
  -d '{
    "task_id": "test-001",
    "session_id": "orch-1",
    "message": "用 Claude Code 写一个 React 登录页，然后用 OpenCode 审查代码质量",
    "agent_type": "orchestrator",
    "config": {
      "agents": [
        {"id": "claude-code", "name": "Claude Code", "capabilities": "代码生成与编辑"},
        {"id": "opencode", "name": "OpenCode", "capabilities": "代码审查与优化"}
      ],
      "task_id": "test-001",
      "shared_dir": "/tmp/orch-test/.agent"
    }
  }'

# 验证产出
cat /tmp/orch-test/.agent/config.yaml       # 任务索引
cat /tmp/orch-test/.agent/overview.md       # 计划概述
cat /tmp/orch-test/.agent/tasks/task-001.md # Claude Code 的任务
cat /tmp/orch-test/.agent/tasks/task-002.md # OpenCode 的任务
```

### 前端群聊 UI 验证

1. 选择 Orchestrator Agent
2. 发送消息 "用 Claude Code 写一个 React 组件，OpenCode 审查"
3. 看到 Orchestrator 规划过程（PLANNING 事件）
4. 看到 Claude Code 执行结果（带 Agent 标签 + 颜色）
5. 看到 OpenCode 执行结果（带 Agent 标签 + 不同颜色）

## 实现步骤

### Step 1: config.yaml + LlmConfig

**修改**: `agentend/configs/config.yaml`

```yaml
# 新增 llm 段
llm:
  provider: "anthropic"
  model: "claude-sonnet-4-6"
  api_key: ""          # 留空则读 ANTHROPIC_API_KEY 环境变量
```

**修改**: `agentend/src/app/config.py`

```python
class LlmConfig(BaseModel):
    provider: str = "anthropic"
    model: str = "claude-sonnet-4-6"
    api_key: str = ""

class Settings(BaseSettings):
    # ... 现有字段
    llm: LlmConfig                   # 新增
```

### Step 2: orchestrator/models.py

**新增**: `agentend/src/orchestrator/models.py`

```python
from pydantic import BaseModel

class TaskDef(BaseModel):
    id: str
    agent: str
    title: str
    content: str

class PlanOutput(BaseModel):
    overview: str
    tasks: list[TaskDef]
```

### Step 3: orchestrator/prompts.py

**新增**: `agentend/src/orchestrator/prompts.py`

```python
PLAN_PROMPT = """\
你是多 Agent 协调器。根据用户需求，为每个 Agent 生成独立可执行的任务。

## 用户需求
{message}

## 可用 Agent
{agents}

## 规则
- 最多 5 个任务
- 每个任务必须指定给唯一的一个 agent
- 任务描述面向 coding agent，具体、可执行
- 如果需求简单，优先单 agent 完成
- tasks 数组顺序即为执行顺序

请生成执行计划。
"""
```

### Step 4: orchestrator/graph.py

**新增**: `agentend/src/orchestrator/graph.py`

LangGraph StateGraph：plan → write_shared

- `plan_node`: 调用 LLM `with_structured_output(PlanOutput)` 生成任务计划
- `write_shared_node`: 写入 `shared/.agent/` 目录（config.yaml + overview.md + tasks/*.md）
- 文件名后端生成 `task-{idx:03d}.md`，不信任 LLM 路径输出

### Step 5: adapters/orchestrator.py

**新增**: `agentend/src/adapters/orchestrator.py`

OrchestratorAdapter 实现 `BaseAgentAdapter`：
- `stream_chat()`: 通过 `graph.astream_events()` 获取流式事件，翻译为 StreamEvent
- `_translate()`: `on_chat_model_stream` → TEXT, `on_chain_end` → PLANNING
- `create_session()` / `destroy_session()`: 无状态，空实现

### Step 6: schemas 扩展

**修改**: `agentend/src/schemas/events.py` — 新增 `PLANNING = "planning"`
**修改**: `agentend/src/schemas/request.py` — 新增 `ORCHESTRATOR = "orchestrator"`

### Step 7: 注册接入

**修改**: `agentend/src/app/dependencies.py` — 注册 OrchestratorAdapter
**修改**: `agentend/src/api/v1/agent.py` — 透传 config 中的 agents/task_id/shared_dir

### Step 8: Go Backend Scheduler

**新增**: `backend/internal/scheduler/orchestrator.go`

```
职责: 读取 shared/.agent/config.yaml，按 tasks 数组顺序依次调度 Agent

流程:
  1. 接收到 Orchestrator 完成事件 (PLANNING done)
  2. 读取 shared/.agent/config.yaml 获取任务列表
  3. 按 task-001 → task-002 顺序：
     a. 读取 tasks/task-001.md 获取任务内容
     b. 调用 AgentEnd stream 接口，agent_type = task.agent
     c. SSE 透传 Agent 输出到前端
     d. 等待当前 Agent 完成后再调度下一个
  4. 所有任务完成后发送 AGGREGATE 事件

关键设计:
  - 顺序执行，不做并行（Phase 5）
  - 每个 Agent 的 SSE 流作为独立消息段透传
  - 前端通过 Agent 标签区分不同来源
```

**修改**: `backend/internal/handler/stream.go` — 当 agent_type=orchestrator 时，先走 Orchestrator 规划流，再由 Scheduler 依次调度

### Step 9: 前端群聊 UI

**修改**: `frontend/src/stores/chat.ts`

```
新增逻辑:
  - 识别 orchestrator 类型消息
  - PLANNING 事件 → 显示规划进度卡片
  - 每个 Agent 执行结果 → 带 Agent 标签的消息块
```

**修改**: `frontend/src/components/chat/MessageBubble.tsx`

```
群聊模式渲染:
  - 每个 assistant 消息块显示来源 Agent 名称
  - 不同 Agent 用不同颜色标签:
    - Claude Code: 蓝色
    - OpenCode: 绿色
    - Orchestrator: 紫色
  - Orchestrator 消息显示 PLANNING 状态图标
```

**新增**: `frontend/src/components/chat/PlanningCard.tsx`

```
规划进度卡片:
  - 显示 Orchestrator 正在分析任务
  - 显示拆解出的任务列表
  - 每个任务标注分配的 Agent
```

## 文件清单

```
AgentEnd:
├── configs/config.yaml                     # 修改: 加 llm 段
├── src/
│   ├── orchestrator/                       # 🆕 整个目录
│   │   ├── __init__.py
│   │   ├── models.py                       # PlanOutput, TaskDef
│   │   ├── prompts.py                      # PLAN_PROMPT
│   │   ├── graph.py                        # plan → write_shared
│   │   └── service.py                      # OrchestratorService (可选)
│   ├── adapters/
│   │   └── orchestrator.py                 # 🆕 OrchestratorAdapter
│   ├── schemas/
│   │   ├── request.py                      # 修改: AgentType.ORCHESTRATOR
│   │   └── events.py                       # 修改: EventType.PLANNING
│   └── app/
│       ├── config.py                       # 修改: LlmConfig
│       └── dependencies.py                 # 修改: 注册

Go Backend:
├── internal/
│   ├── scheduler/
│   │   └── orchestrator.go                 # 🆕 Scheduler
│   └── handler/
│       └── stream.go                       # 修改: Orchestrator 流程
└── cmd/server/
    └── main.go                             # 修改: 注册 Scheduler

Frontend:
├── src/
│   ├── stores/chat.ts                      # 修改: Orchestrator 消息处理
│   ├── components/chat/
│   │   ├── MessageBubble.tsx               # 修改: Agent 标签 + 颜色
│   │   └── PlanningCard.tsx                # 🆕 规划进度卡片
│   └── api/
│       └── client.ts                       # 修改: Orchestrator 配置透传
```

## 验证流程

```bash
# 1. 启动三端
make run-all

# 2. AgentEnd 单元验证 (Step 1-7 完成后)
curl -X POST http://localhost:8001/v1/agent/execute \
  -H "Content-Type: application/json" \
  -d '{ "task_id": "test-001", "session_id": "orch-1",
        "message": "写一个 hello world", "agent_type": "orchestrator",
        "config": { "agents": [...], "task_id": "test-001", "shared_dir": "/tmp/orch-test/.agent" } }'

# 3. 全链路验证 (Step 8-9 完成后)
# 浏览器打开 → 选 Orchestrator → 发消息 → 看到多 Agent 协作流
```
