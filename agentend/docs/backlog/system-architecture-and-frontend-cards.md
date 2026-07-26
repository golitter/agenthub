# Agent 系统实现机制与前端卡片渲染方案（v2）

## 一、系统架构概览

```
Frontend (React)
    │
    ▼ SSE / WebSocket
Go Backend (API Gateway)
    │
    ▼ HTTP
AgentEnd Runtime (FastAPI, Python)
    │
    ├── Rule Engine（安全校验、工具白名单）
    ├── Adapter Registry → Claude / OpenCode / Orchestrator
    ├── Session Manager（状态机 + 会话持久化）
    ├── ArtifactManager（产物统一管理）
    └── Workspace Manager（Git worktree 隔离）
```

核心设计原则：**Runtime First · Event First · Adapter First**

所有 Agent 通过统一的 Adapter 模式接入，所有输出统一为 `StreamEvent`。这意味着前端、Agent、UI 三层完全解耦——后续切换 WebSocket、支持新模型、实现 Replay，都只需在这条流水线上扩展，无需改架构。

---

## 二、统一 Event 协议（核心抽象）

这是整个系统最关键的抽象：

```
Adapter → StreamEvent → Frontend Card
```

它保证了：
- 前后端解耦
- Agent 与 UI 解耦
- 多模型兼容
- SSE / WebSocket 无缝切换
- 消息回放（Replay）
- Event Sourcing

### 2.1 Event Envelope

当前模型只有 `type` + `content`，后续一定会因 metadata、tracing、latency、retry、chunk index 等需求而膨胀。建议立即升级为 **Envelope 结构**：

```json
{
  "event": {
    "id": "evt-abc123",
    "version": "v1",
    "type": "text",
    "source": "claude-code",
    "session_id": "sess-xyz",
    "task_id": "task-001",
    "timestamp": 1716345600.0,
    "ui_type": "markdown"
  },
  "payload": {
    "text": "这是回复内容"
  }
}
```

Envelope 的意义：
- `event` 是稳定的元数据层，前端路由和 replay 只依赖这一层
- `payload` 是自由的数据区，不同 `ui_type` 有不同的 schema
- `version` 保证老 session 可以 replay
- `id` 支持去重和乱序重组

### 2.2 升级后的 StreamEvent 模型

```python
class StreamEvent(BaseModel):
    id: str = Field(default_factory=lambda: str(uuid.uuid4()))
    version: str = "v1"

    type: EventType
    source: str                        # claude-code / opencode / orchestrator
    session_id: str
    task_id: str | None = None

    timestamp: float = Field(default_factory=time.time)

    ui_type: str | None = None         # UI Contract，见 2.3
    payload: dict = Field(default_factory=dict)
    metadata: dict = Field(default_factory=dict)
```

### 2.3 UI Type（UI Contract）

`ui_type` 不是"提示"，而是前后端之间的**正式协议**——后端声明用哪种卡片渲染，前端无需猜测。

| ui_type | 含义 | 对应卡片 |
|---|---|---|
| `markdown` | 自然语言文本 | TextCard |
| `code` | 代码块（含语言标识） | CodeBlockCard |
| `diff` | 代码差异 | DiffViewCard |
| `artifact.image` | 图片产物 | ImageCard |
| `artifact.file` | 文件产物 | FileAttachmentCard |
| `artifact.link_preview` | 链接预览 | LinkPreviewCard |
| `deploy.status` | 部署状态 | DeployStatusCard |
| `planning.step` | 规划步骤 | PlanningCard |
| `tool.progress` | 工具执行进度 | ToolProgressCard |

**核心原则：Markdown 只负责自然语言；所有结构化 UI 必须是显式 Event。**

不要让前端从 Markdown 文本中解析卡片类型（检测 ` ``` ` → code card、检测 URL → link card）。因为 Markdown 输出不稳定、模型输出不可控、嵌套格式容易炸、streaming 下无法稳定解析。结构化内容必须由后端显式发出对应的 `ui_type` Event。

---

## 三、三层 Adapter 机制

### 3.1 BaseAgentAdapter（抽象基类）

定义于 `agentend/src/adapters/base.py`，所有 Agent 必须实现 5 个接口：

| 方法 | 职责 |
|---|---|
| `create_session(session_id)` | 初始化会话 |
| `chat(session_id, message, **kwargs)` | 同步执行，返回 `AgentResponse` |
| `stream_chat(session_id, message, **kwargs)` | 流式执行，yield `StreamEvent` |
| `interrupt(session_id)` | 中断子进程 |
| `destroy_session(session_id)` | 销毁会话，清理资源 |

### 3.2 ClaudeCodeAdapter

文件：`agentend/src/adapters/claude.py`

通过 `asyncio.create_subprocess_exec` 启动 `claude` CLI，使用 `--output-format stream-json --verbose` 获取 JSON 流。

**命令构造**：
```python
cmd = [cli_path, "-p", message, "--output-format", "stream-json", "--verbose", "--include-partial-messages", "--dangerously-skip-permissions"]
# --session-id / --resume    会话管理
# --append-system-prompt     追加系统提示
# --allowedTools             工具白名单
# --max-turns                最大轮次
```

**CLI 事件映射**：

| CLI type | → EventType | payload |
|---|---|---|
| `system` | `INIT` | `{cli_session_id}` |
| `assistant` | `TEXT` | `{text}`（从 content blocks 提取） |
| `tool_use` | `TOOL_CALL` | `{tool, args}` |
| `tool_result` | `TOOL_RESULT` | `{tool, result}` |
| `result` | `DONE` | `{text, usage}` |

**进程管理**：`_processes` 字典按 session_id 存储活跃进程，`interrupt()` 先 terminate 后 kill（超时来自 config.yaml 的 `execution.process_terminate_timeout`）。危险工具通过 `SafetyRule` 在 `RuleEngine` 层过滤，不直接在适配器内拦截。

### 3.3 OpenCodeAdapter

文件：`agentend/src/adapters/opencode.py`

启动 `opencode run` 子进程，`--format json` 获取 NDJSON 输出。

**关键差异**：

| 维度 | Claude Code | OpenCode |
|---|---|---|
| CLI 命令 | `claude -p` | `opencode run` |
| 输出格式 | stream-json | NDJSON |
| 会话恢复 | `--resume` | `--session --fork` |
| 思考过程 | 无特殊处理 | reasoning → `[thinking]` 前缀 |
| 系统提示 | `--append-system-prompt` | 拼接到消息头部 |
| 完成信号 | `result` type 含 usage | 进程退出后手动 yield DONE |

**NDJSON 映射**：

| NDJSON type | → EventType | 特殊处理 |
|---|---|---|
| `error` | `ERROR` | 提取嵌套 error.data.message |
| `step_start` | `INIT` | 提取 sessionID |
| `text` | `TEXT` | 提取 part.text |
| `reasoning` | `TEXT` | 加 `[thinking]` 前缀 |
| `tool_use` | `TOOL_CALL` / `TOOL_RESULT` | status=error 时转 TOOL_RESULT |
| `step_finish` | 忽略 | — |

### 3.4 OrchestratorAdapter（规划层）

文件：`agentend/src/orchestrator/`

不管理子进程，而是通过 LangGraph 状态图调用 LLM（默认 DeepSeek）进行任务拆解。

**组件**：
- `models.py` — `TaskDef`（task_id, session_id, title, content）、`TaskResult`（含 message_id/error_type/conflict_files 等字段）
- `planning/prompts.py` — `REASON_PROMPT` 引导 LLM 通过 tool-calling 拆解与分派任务
- `planning/graph.py` — LangGraph 状态图：`skill_prepare` → `reason` → `human_review` → `dispatch` → `execute` → `review` → `evolve` → `save_mem`（8 节点）
- `execution/engine.py` — `ExecutionEngine` 执行引擎
- `execution/dispatcher.py` — `Dispatcher` 调度器
- `memory/pin_memory.py` — `PinMemory` 约束钉住
- `memory/evolution.py` — `EvolutionStore` 编排经验
- `reporting/aggregator.py` — `Aggregator` LLM 汇总

**输出文件结构**：
```
shared/.agent/
├── plans/
│   ├── overview.md        # 整体规划
│   ├── task-001.md        # 任务详情
│   └── task-002.md
└── config.yaml            # 声明式任务索引
```

**演进方向**：当前的 `plan → write_shared` 已经接近 Event-driven DAG Runtime。未来可升级为 `TaskGraph`（含 `TaskNode{id, deps, agent, state}`），支持 `task.completed → scheduler → dispatch next`，从而实现并行 Agent、自动调度、retry、approval、checkpoint。

---

## 四、请求处理流程

### 4.1 完整调用链

```
POST /v1/agent/stream 或 /v1/agent/execute
    │
    ▼
1. _resolve_workspace()
   - 有 workspace_path → 直接使用
   - 有 repo_path → 自动创建 Git worktree
    │
    ▼
2. RuleEngine.evaluate()
   - 安全校验、工具白名单、system_prompt 追加
    │
    ▼
3. AdapterRegistry.get(agent_type) → 实例化 Adapter
    │
    ▼
4. _resolve_session()
   - 首次 → 创建 Session
   - 恢复 → SessionMappingStore 获取 cli_session_id
    │
    ▼
5. adapter.stream_chat() → yield StreamEvent
    │
    ▼
6. ArtifactManager 注册产物（如需）
    │
    ▼
7. SSE 推送 Envelope：event: <type>\ndata: <json>\n\n
```

### 4.2 Session 状态机

```
IDLE → RUNNING → COMPLETED
                → INTERRUPTED
                → ERROR
```

`SessionManager` 管内存映射，`SessionMappingStore` 持久化 API session_id ↔ CLI session_id 映射。

### 4.3 Tool Event 生命周期（建议升级）

当前只有 `tool_call` 和 `tool_result` 两个事件，对长任务（shell command、docker build、deploy）无法实时展示。建议升级为：

| Event Type | 含义 | 用途 |
|---|---|---|
| `TOOL_START` | 工具开始执行 | 显示 spinner、工具名称 |
| `TOOL_STDOUT` | 标准输出增量 | 实时显示命令输出 |
| `TOOL_STDERR` | 标准错误增量 | 实时显示错误信息 |
| `TOOL_RESULT` | 最终结果 | 显示结果 / diff |
| `TOOL_ERROR` | 执行失败 | 显示错误详情 |

这样前端可以渲染完整的工具执行过程，而不是只看到一个"黑盒"的开始和结束。

---

## 五、ArtifactManager（产物统一管理）

当前产物直接暴露 workspace path（`/workspace/task-001/output/chart.png`），后续 worktree 清理、临时文件过期、权限控制、CDN、多节点部署时一定会出问题。

### 5.1 核心设计

```python
class ArtifactManager:
    """统一管理所有 Agent 产物，前端永远通过 artifact_id 访问"""

    async def register(self, task_id: str, path: str, mime_type: str, metadata: dict) -> str:
        """注册产物，返回 artifact_id"""

    async def get(self, artifact_id: str) -> ArtifactMeta:
        """获取产物元数据"""

    async def resolve_url(self, artifact_id: str) -> str:
        """解析为可访问的 URL（本地文件 / CDN / S3）"""

    async def cleanup(self, task_id: str) -> int:
        """清理 task 下所有产物"""
```

### 5.2 前端访问方式

```
前端只拿 artifact_id → GET /v1/artifacts/{artifact_id} → 后端解析真实路径返回
```

不在 SSE payload 中暴露任何 workspace 绝对路径。

### 5.3 产物 Event 格式

```json
{
  "event": {
    "type": "artifact",
    "ui_type": "artifact.image",
    "source": "claude-code"
  },
  "payload": {
    "artifact_id": "art-xyz789",
    "filename": "chart.png",
    "mime_type": "image/png",
    "size": 102400,
    "alt": "数据可视化图表"
  }
}
```

---

## 六、前端卡片渲染方案

### 6.1 Chunk 聚合层（MessageAggregator）

SSE 直出会导致 Claude/OpenCode 逐字符 chunk（`H` → `He` → `Hel` → ...），React 疯狂 rerender。必须在 SSE 和 React 之间加一层聚合：

```
SSE Stream → Event Buffer → Chunk Aggregator → Stable Message → React Render
```

**聚合规则**：

| Event Type | 聚合策略 |
|---|---|
| `TEXT`（连续 chunk） | 300ms debounce 合并 |
| `TOOL_START` / `TOOL_CALL` | 立即 flush，开始新的聚合组 |
| `ARTIFACT` | 立即 flush 为独立卡片 |
| `DONE` | 强制 finalize 当前所有 buffer |

实现方式：前端维护一个 `EventBuffer`，用 `requestAnimationFrame` 或 `setTimeout(debounce)` 批量提交。

### 6.2 前端组件架构

当前建议的组件树：

```
Conversation
  └── EventGroup（按 tool_call / 思考 / 回复 自动分组）
        ├── TextCard           # ui_type: markdown
        ├── CodeBlockCard      # ui_type: code
        ├── DiffViewCard       # ui_type: diff
        ├── ImageCard          # ui_type: artifact.image
        ├── FileAttachmentCard # ui_type: artifact.file
        ├── LinkPreviewCard    # ui_type: artifact.link_preview
        ├── DeployStatusCard   # ui_type: deploy.status
        ├── PlanningCard       # ui_type: planning.step
        ├── ToolProgressCard   # tool.start / tool.stdout / tool.stderr
        └── SystemCard         # init / error / done
```

**核心单位是 Event 而非 Message。** "消息"只是一组相关 Event 的聚合展示。

每个卡片组件接收统一的 Envelope prop，根据 `event.ui_type` 分发渲染。上层 `EventGroup` 负责：
- 将连续 `TEXT` chunk 合并为一个 TextCard
- 将 `TOOL_START → TOOL_STDOUT* → TOOL_RESULT` 组合为一个 ToolProgressCard
- 将 `ARTIFACT` 独立为一个卡片

### 6.3 各卡片实现方案

#### TextCard（ui_type: markdown）

```
┌─────────────────────────────────┐
│ Claude Code                     │
│                                 │
│ 回复内容支持 **Markdown** 渲染，│
│ 包括列表、表格、行内代码。      │
└─────────────────────────────────┘
```

- `react-markdown` + `remark-gfm` + `rehype-highlight`
- SSE 增量追加：chunk 拼接到缓冲区末尾
- 不同 `source` 用颜色/图标区分

#### CodeBlockCard（ui_type: code）

```
┌─ src/main.py ───────────── [复制] ┐
│                                    │
│  1  def hello():                   │
│  2      print("Hello, World!")     │
│                                    │
│────────────────────────────────────│
│ Python                             │
└────────────────────────────────────┘
```

- 后端显式发出 `ui_type: "code"` 的 Event，携带 `language`、`filename`、`code`
- 前端不再从 Markdown 围栏中解析
- `highlight.js` 或 `shiki` 语法高亮
- 复制按钮（`navigator.clipboard.writeText`）

#### DiffViewCard（ui_type: diff）

```
┌─ src/main.py ──────────── +1/-1 ──┐
│                                    │
│ - def hello():                     │
│ + def hello(name: str = 'World'): │
│                                    │
│ ✓ Successfully edited file        │
└────────────────────────────────────┘
```

- 后端从 `TOOL_RESULT` 中提取 `old_string` / `new_string`，生成标准 unified diff patch
- 显式发出 `ui_type: "diff"` Event：`{file, patch, stats}`
- 前端用 `react-diff-viewer-continued` 渲染 side-by-side 或 inline
- 前端无需关心是 Edit 工具还是 Write 工具——只看 `ui_type: "diff"`

#### ImageCard（ui_type: artifact.image）

```
┌─────────────────────────────────┐
│ ┌─────────────────────────────┐ │
│ │      图片预览区域            │ │
│ └─────────────────────────────┘ │
│ 数据可视化图表          [放大]  │
└─────────────────────────────────┘
```

- 后端通过 ArtifactManager 注册图片，发出 `{artifact_id, filename, mime_type, size, alt}`
- 前端懒加载、点击放大（lightbox）、错误 fallback
- URL 走 `/v1/artifacts/{artifact_id}`，不暴露 workspace 路径

#### FileAttachmentCard（ui_type: artifact.file）

```
┌─────────────────────────────────┐
│ report.pdf                      │
│ 1.0 MB · PDF 文档       [下载]  │
├─────────────────────────────────┤
│ data.csv                        │
│ 256 KB · CSV 数据       [下载]  │
└─────────────────────────────────┘
```

- 同样走 ArtifactManager，前端通过 artifact_id 下载
- 根据 MIME 类型显示图标

#### LinkPreviewCard（ui_type: artifact.link_preview）

```
┌─────────────────────────────────┐
│ github.com/org/repo             │
│ Fix: 修复登录超时问题 #42       │
│ 修复了 OAuth token 刷新时的     │
│ 竞态条件                [打开]  │
└─────────────────────────────────┘
```

- 后端增加 `/v1/preview?url=xxx` 端点，解析 OpenGraph 标签
- 缓存预览结果
- 前端不再正则匹配 URL——由后端/Adapter 显式发出

#### DeployStatusCard（ui_type: deploy.status）

```
┌─────────────────────────────────────┐
│ Deploy: frontend → staging         │
│                                     │
│ Commit: abc1234                     │
│ Status: Building...  [████░░] 67%  │
│ Duration: 2m 15s            [查看CI]│
└─────────────────────────────────────┘
```

- Agent 发出 `deploy.status` Event，后续状态通过 SSE 推送更新
- 状态流转：`building → deployed / failed`
- 前端进度条动画 + 状态图标

---

## 七、Agent Timeline

系统的 Event 化协议天然适合 Timeline 视图：

```
10:00  Planning...              [planning.step]
10:01  Claude editing file      [tool.start]
10:02  Tool call: grep          [tool.start]
10:03  Generated diff           [ui_type: diff]
10:05  Deploy staging           [deploy.status]
```

前端实现：一个按时间排序的 Event 列表，每条 Event 根据 `ui_type` 渲染为对应的小卡片或摘要行。可以折叠/展开详情。这对调试多 Agent 协作、排查问题非常有价值。

---

## 八、Agent Capability 模型

当前 `AdapterRegistry` 按 agent_type（claude-code、opencode）注册。未来多模型兼容时，前端和 Orchestrator 不应关心具体 Agent 类型，而应基于 **Capability** 匹配：

```python
CAPABILITIES = {
    "stream_text",      # 支持流式文本输出
    "tool_use",         # 支持工具调用
    "image_input",      # 支持图片输入
    "image_output",     # 支持图片输出
    "planning",         # 支持规划
    "diff_output",      # 支持结构化 diff
    "code_execution",   # 支持代码执行
    "long_running",     # 支持长任务
}
```

Orchestrator 调度时：`requirements: [tool_use, diff_output]` → Registry 匹配到 `claude-code`。
前端渲染时：根据 `event.source` 的 capabilities 决定展示哪些交互选项。

---

## 九、实现路线

### Phase 1：Event 协议升级（立即）

1. `StreamEvent` 增加 `id`、`version`、`source`、`session_id`、`task_id`、`ui_type` 字段
2. SSE 输出改为 Envelope 格式
3. Adapter 层补充 `ui_type` 标注（TEXT → markdown，TOOL_RESULT 中文件操作 → diff）

### Phase 2：前端聚合层 + 卡片组件（本周）

1. 实现 `MessageAggregator`（TEXT 300ms debounce，其他立即 flush）
2. 实现核心卡片：TextCard、CodeBlockCard、DiffViewCard
3. 实现 `EventGroup` 分组逻辑

### Phase 3：ArtifactManager（下周）

1. 后端实现产物注册/解析/清理
2. 新增 `/v1/artifacts/{id}` 端点
3. 前端 ImageCard、FileAttachmentCard 对接

### Phase 4：Tool 生命周期 + Timeline（后续）

1. 后端 `TOOL_START / TOOL_STDOUT / TOOL_STDERR` 事件
2. 前端 ToolProgressCard + Timeline 视图
3. DeployStatusCard 对接 CI/CD

### Phase 5：Capability + DAG（远期）

1. Agent Capability 注册与匹配
2. Orchestrator 升级为 TaskGraph DAG Runtime
3. 并行 Agent 调度、retry、checkpoint

---

## 十、成熟度评估

| 模块 | 当前水平 | 目标 |
|---|---|---|
| Adapter 抽象 | 很好 | 保持 |
| Session 管理 | 好 | 保持 |
| SSE 协议 | 好 | 升级为 Envelope |
| Event 统一 | 很好 | 增加 versioning + ui_type |
| Orchestrator | 好 | 远期升级为 DAG |
| UI Card 设计 | 好 | 基于 ui_type 显式协议 |
| Artifact 体系 | 需加强 | ArtifactManager |
| Event Schema | 需 versioning | Envelope v1 |
| Tool 生命周期 | 基础 | 5 阶段事件 |
| Runtime DAG | 下一阶段 | TaskGraph |

整体架构已经接近 Cursor / Devin / OpenHands 这类系统的雏形。核心优势是没有被 LangChain "大而全" 的抽象绑死，保持了 Runtime First、Event First、Adapter First 的清晰路线。
