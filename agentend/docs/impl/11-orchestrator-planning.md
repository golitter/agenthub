# Orchestrator 规划实现

## 实现了什么

Orchestrator 作为 "AI 项目经理"，接收用户需求 + agent 列表，通过 LLM 拆解为多个子任务，将 `config.yaml`、`plans/*.md` 写入 `shared/.agent/` 目录。各 agent 通过 `taskctl summary` 只能看到分配给自己的任务。

## 整体架构

```
POST /v1/agent/execute (agent_type=orchestrator)
        │
        ▼
  OrchestratorAdapter
        │
        ▼
  LangGraph StateGraph
   ┌────┴────┐
   │  plan   │ ← LLM structured output (DeepSeek)
   │  node   │
   └────┬────┘
   ┌────▼────┐
   │ write_  │ ← 文件 IO
   │ shared  │
   └────┬────┘
        │
        ▼
  shared/.agent/
  ├── config.yaml
  └── plans/
      ├── overview.md
      ├── task-001.md
      └── task-002.md
```

## 文件结构

```
src/
├── orchestrator/
│   ├── __init__.py
│   ├── models.py      # TaskDef, PlanOutput Pydantic model
│   ├── prompts.py     # PLAN_PROMPT（引导 LLM 输出 JSON）
│   └── graph.py       # LangGraph StateGraph（plan → write_shared）
├── adapters/
│   └── orchestrator.py # OrchestratorAdapter（BaseAgentAdapter 子类）
└── api/v1/
    └── agent.py        # _orchestrator_kwargs() config 透传
```

## 怎么实现的

### 数据模型 (`src/orchestrator/models.py`)

```python
class TaskDef(BaseModel):
    task_id: str       # 任务标识，如 task-001
    session_id: str    # agent id，如 claude-code
    title: str         # 任务标题
    content: str       # 任务详细描述

class PlanOutput(BaseModel):
    overview: str           # 整体规划概述
    tasks: list[TaskDef]    # 任务列表（按执行顺序）
```

### LangGraph 流程 (`src/orchestrator/graph.py`)

**GraphState** — 图状态：

```python
class GraphState(TypedDict):
    message: str              # 用户需求
    agents: list[dict]        # 可用 agent 列表
    task_id: str              # 任务 ID
    shared_dir: str           # 写入路径
    plan: PlanOutput | None   # LLM 规划结果
```

**plan node** — 单次 LLM 调用生成规划：

1. 从 `.env` 读取 `DS_MODEL`、`DS_BASE_URL`、`DS_API_KEY` 构建 `ChatOpenAI`
2. 将 agents 列表格式化为 prompt 中的描述
3. 调用 LLM，从响应中提取 JSON（支持 markdown 代码块包裹），解析为 `PlanOutput`

**write\_shared node** — 将规划结果写入文件系统：

1. 将 agents config 中的 `id → session_id` 映射（`claude-code → cc-orch-test`）
2. 写 `plans/overview.md`
3. 写 `plans/task-NNN.md`（文件名后端生成，不信任 LLM）
4. 写 `config.yaml`（声明式任务索引，`session_id` 用真实 session）

### LLM 配置 (`config.yaml` + `.env`)

`config.yaml` 中 `llm` 段为空，实际值全部从 `.env` 环境变量读取：

```yaml
llm: {}
```

```bash
# .env
DS_MODEL=deepseek-chat
DS_BASE_URL=https://api.deepseek.com
DS_API_KEY=sk-xxx
```

`LlmConfig` 通过 `model_validator` 在 `os.environ` 中解析，启动时通过 `load_dotenv()` 加载 `.env`。

### Adapter 层 (`src/adapters/orchestrator.py`)

`OrchestratorAdapter` 实现 `BaseAgentAdapter` 接口：

- `__init__`：编译 LangGraph graph
- `stream_chat`：传入 `agents`、`task_id`、`shared_dir`，yield `PLANNING` → `TEXT` → `DONE` 事件
- `chat`：收集 stream 事件，返回 `AgentResponse`
- `create_session` / `interrupt` / `destroy_session`：无操作（Orchestrator 无会话状态）

### API 接入 (`src/api/v1/agent.py`)

`_orchestrator_kwargs()` 从 `request.config` 提取 orchestrator 专用参数：

```python
{
    "agents": config.get("agents", []),
    "task_id": config.get("task_id", request.task_id),
    "shared_dir": config.get("shared_dir", f"{task_id}/shared/.agent"),
}
```

`shared_dir` 默认值为 `{task_id}/shared/.agent`，调用方可通过 config 覆盖。

### Schema 扩展

- `AgentType` 枚举新增 `ORCHESTRATOR = "orchestrator"`
- `EventType` 枚举新增 `PLANNING = "planning"`

### taskctl 联动

`taskctl summary` 按 `session_id` 过滤任务文件：

1. 读取 `config.yaml`，解析 tasks 列表
2. 从可执行文件路径解析出当前 agent 的 `sessionID`
3. 只显示 `session_id` 匹配的 task 文件 + `overview.md`

每个 agent 只能看到分配给自己的任务。

## 产出文件格式

### config.yaml（声明式任务索引）

```yaml
task_id: orch-test
tasks:
- task_id: task-001
  session_id: cc-orch-test
  file: plans/task-001.md
- task_id: task-002
  session_id: oc-orch-test
  file: plans/task-002.md
```

### plans/task-001.md

```markdown
# 生成登录页面代码

> agent: claude-code

使用 HTML、CSS 和 JavaScript 生成一个简洁的登录页面...
```

## 调用示例

```bash
curl -X POST http://localhost:8001/v1/agent/execute \
  -H 'Content-Type: application/json' \
  -d '{
    "task_id": "orch-test",
    "session_id": "orch-planner",
    "message": "用 Claude Code 写登录页，用 OpenCode 审查代码",
    "agent_type": "orchestrator",
    "config": {
      "agents": [
        {"id": "claude-code", "session_id": "cc-orch-test", "name": "Claude Code", "capabilities": ["代码生成"]},
        {"id": "opencode", "session_id": "oc-orch-test", "name": "OpenCode", "capabilities": ["代码审查"]}
      ],
      "shared_dir": "/path/to/worktrees/orch-test/shared/.agent"
    }
  }'
```
