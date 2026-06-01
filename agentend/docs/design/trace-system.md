# agentend Trace 系统 — OpenTelemetry 可观测性设计

## 1. 目标

为 agentend 建立结构化的分布式 Trace 能力，使一次用户请求在多 Agent 编排中的完整生命周期可观测、可回溯、可分析。

### 核心诉求

| 维度 | 问题 |
|------|------|
| **端到端可追溯** | 一次用户提问 → Orchestrator 规划 → 多 Agent 并行执行 → 聚合回复，全链路耗时在哪？ |
| **跨 Agent 关联** | Orchestrator dispatch 了 3 个子任务，分别由哪些 Agent 执行？各自成功/失败？ |
| **LLM 调用观测** | Reason 节点调了几轮 LLM？每轮用了哪些 tool？token 消耗多少？ |
| **错误归因** | 某次编排失败，是规划阶段还是执行阶段？是哪个 Agent 超时？ |
| **性能分析** | dispatch → execute 的拓扑排序是否合理？并行 wave 的调度效率？ |

---

## 2. 技术选型：OpenTelemetry

选择 OpenTelemetry（OTel）作为 Trace 基础设施：

- **标准协议**：OTLP (OpenTelemetry Protocol) 是事实标准，后端可对接 Jaeger / Tempo / Datadog / SkyWalking
- **Python 原生支持**：`opentelemetry-sdk` + `opentelemetry-instrumentation-fastapi` 自动埋点 HTTP 层
- **异步友好**：基于 `contextvars` 的 Context 传播，天然适配 asyncio
- **LangChain 集成**：`opentelemetry-instrumentation-langchain` 可自动追踪 LLM 调用

### 依赖清单

```toml
# pyproject.toml [project.dependencies]
"opentelemetry-api>=1.30",
"opentelemetry-sdk>=1.30",
"opentelemetry-exporter-otlp>=1.30",
"opentelemetry-instrumentation-fastapi>=0.51b0",
"opentelemetry-instrumentation-httpx>=0.51b0",
"opentelemetry-instrumentation-asyncio>=0.51b0",
```

---

## 3. Span 树设计

一次完整请求的 Span 层级如下：

```
[ROOT] agent.request                    # HTTP /v1/agent/stream
  ├── agent.session.resolve             # 会话解析 & 创建
  ├── agent.workspace.resolve           # 工作区创建（如需）
  ├── agent.rules.evaluate              # 规则引擎评估
  │
  ├── [CLI Agent 路径]
  │   └── agent.chat                    # BaseAgentAdapter.stream_chat
  │       ├── agent.process.spawn       # 子进程启动
  │       └── agent.process.stream      # SSE 事件流
  │
  └── [Orchestrator 路径]
      └── orchestrator.plan             # OrchestratorAdapter.stream_chat
          ├── orchestrator.skill_prepare  # L1→L2 技能发现
          ├── orchestrator.reason         # LLM tool-calling 循环
          │   ├── llm.invoke             # 每轮 LLM 调用
          │   └── tool.call {name}       # 每次工具调用
          │       └── ask_agent {id}     # ask_agent 子调用
          │           ├── backend.run_task
          │           └── backend.stream_result
          ├── orchestrator.dispatch       # Plan → DispatchResult + 拓扑排序
          ├── orchestrator.execute        # 逐 wave 执行
          │   └── orchestrator.wave {n}   # 单个 wave
          │       ├── orchestrator.task {task_id}
          │       │   ├── workspace.worktree.create
          │       │   ├── backend.run_task
          │       │   └── backend.stream_result
          │       └── orchestrator.task {task_id}  # 并行
          ├── orchestrator.review         # 结果审查 & 重规划判断
          ├── orchestrator.evolve         # 经验记录
          └── orchestrator.save_mem       # 记忆持久化
```

---

## 4. Span 属性（Attributes）规范

每个 Span 携带结构化属性，便于后端查询和聚合。

### 4.1 通用属性（所有 Span）

| 属性 | 类型 | 说明 |
|------|------|------|
| `agent.session_id` | str | 会话 ID |
| `agent.task_id` | str | 任务 ID |
| `agent.type` | str | Agent 类型（claude-code / opencode / orchestrator） |

### 4.2 各节点特有属性

**`orchestrator.reason`**
| 属性 | 类型 | 说明 |
|------|------|------|
| `llm.model` | str | 模型名称 |
| `llm.iteration` | int | 第几轮 tool-calling |
| `llm.output_type` | str | "text" / "plan" / "error" |
| `llm.tool_calls_count` | int | 本轮调用的工具数 |
| `llm.token_usage.prompt` | int | prompt token 数 |
| `llm.token_usage.completion` | int | completion token 数 |

**`orchestrator.dispatch`**
| 属性 | 类型 | 说明 |
|------|------|------|
| `dispatch.task_count` | int | 拆分的任务数 |
| `dispatch.wave_count` | int | 执行波次 |
| `dispatch.tasks_per_wave` | str | JSON: `[3, 2, 1]` |

**`orchestrator.task`**
| 属性 | 类型 | 说明 |
|------|------|------|
| `task.id` | str | 任务 ID |
| `task.agent` | str | 执行 Agent |
| `task.agent_type` | str | Agent 类型 |
| `task.success` | bool | 是否成功 |
| `task.duration_ms` | float | 执行耗时 |
| `task.error_type` | str | 失败类型（timeout / error） |

**`ask_agent`**
| 属性 | 类型 | 说明 |
|------|------|------|
| `ask.source_agent` | str | 发起方 |
| `ask.target_agent` | str | 目标 Agent |
| `ask.question_id` | str | 问题 ID |
| `ask.status` | str | "completed" / "failed" |

### 4.3 事件（Span Events）

Span 内可记录离散事件（带时间戳但不产生子 Span）：

**`orchestrator.reason` 上的事件**
- `llm.retry`：LLM 调用重试
- `tool.error`：工具调用异常

**`orchestrator.task` 上的事件**
- `task.timeout`：任务超时
- `task.stream_error`：SSE 流错误

---

## 5. 核心模块设计

### 5.1 目录结构

```
src/trace/
├── __init__.py           # 导出 tracer / helpers
├── provider.py           # TracerProvider 初始化 & 配置
├── middleware.py          # FastAPI 中间件（request_id 注入）
├── context.py            # Context 传播工具（跨 async 边界）
├── spans.py              # 装饰器 & 上下文管理器
└── attributes.py         # 属性常量 & 构建器
```

### 5.2 `provider.py` — Tracer 初始化

```python
"""Trace provider — 应用启动时调用 init_tracing()。"""
from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.sdk.resources import Resource
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter


def init_tracing(service_name: str = "agentend", otlp_endpoint: str | None = None) -> TracerProvider:
    """初始化 TracerProvider，注册全局 tracer。

    Args:
        service_name: 服务名，出现在 Jaeger/Tempo 的 Service 列表
        otlp_endpoint: OTLP gRPC endpoint，如 "http://localhost:4317"
                        为 None 则使用 ConsoleExporter（开发模式）
    """
    resource = Resource.create({"service.name": service_name})

    provider = TracerProvider(resource=resource)

    if otlp_endpoint:
        exporter = OTLPSpanExporter(endpoint=otlp_endpoint, insecure=True)
        provider.add_span_processor(BatchSpanProcessor(exporter))
    else:
        from opentelemetry.sdk.trace.export import ConsoleSpanExporter
        provider.add_span_processor(BatchSpanProcessor(ConsoleSpanExporter()))

    trace.set_tracer_provider(provider)
    return provider


def get_tracer(name: str = "agentend") -> trace.Tracer:
    """获取命名 tracer。"""
    return trace.get_tracer(name)
```

### 5.3 `context.py` — 跨 async Context 传播

Orchestrator 已大量使用 `contextvars`（`_ask_event_queue_var` 等），trace 上下文同理。

```python
"""Trace context helpers — 在 contextvars 之上提供 trace_id / span_id 传播。"""
import contextvars
from opentelemetry import trace, context


# 保存当前 OTel context，以便在新的 asyncio.Task 中恢复
_trace_context_var: contextvars.ContextVar[context.Context | None] = contextvars.ContextVar(
    "trace_context", default=None
)


def capture_context() -> context.Context:
    """捕获当前 OTel context（在父 coroutine 中调用）。"""
    return context.get_current()


def attach_context(ctx: context.Context | None = None) -> None:
    """在子 coroutine/task 中恢复 OTel context。"""
    if ctx is None:
        ctx = _trace_context_var.get()
    if ctx is not None:
        context.attach(ctx)


def trace_context_token(ctx: context.Context | None = None) -> contextvars.Token:
    """保存 context 到 ContextVar，返回 token 用于恢复。"""
    return _trace_context_var.set(ctx or context.get_current())
```

### 5.4 `spans.py` — Span 创建工具

提供两种使用方式：**装饰器**（同步/异步函数）和 **上下文管理器**（代码块级控制）。

```python
"""Span 创建工具 — 装饰器 & 上下文管理器。"""
import functools
from collections.abc import AsyncIterator, Callable
from typing import Any, ParamSpec, TypeVar

from opentelemetry import trace

from src.trace.context import capture_context, attach_context

P = ParamSpec("P")
R = TypeVar("R")

tracer = trace.get_tracer("agentend")


def traced(
    span_name: str,
    attributes: dict[str, Any] | None = None,
    kind: trace.SpanKind = trace.SpanKind.INTERNAL,
) -> Callable[[Callable[P, R]], Callable[P, R]]:
    """装饰器：为 async 函数创建 Span。

    自动捕获异常并记录到 Span（status=ERROR + events）。
    """
    def decorator(fn: Callable[P, R]) -> Callable[P, R]:
        if asyncio.iscoroutinefunction(fn):
            @functools.wraps(fn)
            async def async_wrapper(*args: P.args, **kwargs: P.kwargs) -> R:
                ctx = capture_context()
                with tracer.start_as_current_span(span_name, kind=kind, attributes=attributes) as span:
                    try:
                        return await fn(*args, **kwargs)
                    except Exception as e:
                        span.set_status(trace.StatusCode.ERROR, str(e))
                        span.record_exception(e)
                        raise
            return async_wrapper
        else:
            @functools.wraps(fn)
            def sync_wrapper(*args: P.args, **kwargs: P.kwargs) -> R:
                with tracer.start_as_current_span(span_name, kind=kind, attributes=attributes) as span:
                    try:
                        return fn(*args, **kwargs)
                    except Exception as e:
                        span.set_status(trace.StatusCode.ERROR, str(e))
                        span.record_exception(e)
                        raise
            return sync_wrapper
    return decorator


class SpanBuilder:
    """流式 Span 构建器 — 用于手动控制 start/end 的场景。

    典型用法：
        with SpanBuilder("orchestrator.task", attributes={...}) as span:
            for event in stream:
                span.add_event("chunk_received")
    """
    def __init__(self, name: str, attributes: dict[str, Any] | None = None,
                 kind: trace.SpanKind = trace.SpanKind.INTERNAL):
        self._name = name
        self._attrs = attributes
        self._kind = kind
        self._span: trace.Span | None = None

    def __enter__(self) -> trace.Span:
        self._span = tracer.start_span(self._name, kind=self._kind, attributes=self._attrs)
        return self._span

    def __exit__(self, exc_type, exc_val, exc_tb):
        if exc_val and self._span:
            self._span.set_status(trace.StatusCode.ERROR, str(exc_val))
            self._span.record_exception(exc_val)
        if self._span:
            self._span.end()
```

### 5.5 `attributes.py` — 属性构建器

```python
"""Trace 属性常量 & 构建器。"""
from src.schemas.request import AgentType

# ── 常量 ──
ATTR_SESSION_ID = "agent.session_id"
ATTR_TASK_ID = "agent.task_id"
ATTR_AGENT_TYPE = "agent.type"
ATTR_AGENT_NAME = "agent.name"
ATTR_SUCCESS = "task.success"
ATTR_DURATION_MS = "task.duration_ms"
ATTR_ERROR_TYPE = "task.error_type"
ATTR_ERROR_MESSAGE = "task.error_message"

ATTR_LLM_MODEL = "llm.model"
ATTR_LLM_ITERATION = "llm.iteration"
ATTR_LLM_OUTPUT_TYPE = "llm.output_type"
ATTR_LLM_TOOL_CALLS = "llm.tool_calls_count"

ATTR_DISPATCH_TASK_COUNT = "dispatch.task_count"
ATTR_DISPATCH_WAVE_COUNT = "dispatch.wave_count"

ATTR_ASK_SOURCE = "ask.source_agent"
ATTR_ASK_TARGET = "ask.target_agent"
ATTR_ASK_STATUS = "ask.status"


def common_attrs(
    session_id: str = "",
    task_id: str = "",
    agent_type: str | AgentType = "",
) -> dict:
    """构建通用属性。"""
    return {
        ATTR_SESSION_ID: str(session_id),
        ATTR_TASK_ID: str(task_id),
        ATTR_AGENT_TYPE: str(agent_type),
    }


def task_result_attrs(result) -> dict:
    """从 TaskResult 构建属性。"""
    return {
        "task.id": result.task_id,
        ATTR_AGENT_NAME: result.agent,
        ATTR_SUCCESS: result.success,
        ATTR_DURATION_MS: result.duration * 1000,  # 秒 → 毫秒
        ATTR_ERROR_TYPE: result.error_type,
        ATTR_ERROR_MESSAGE: result.error_message,
    }
```

### 5.6 `middleware.py` — FastAPI 中间件

```python
"""FastAPI 中间件 — 为每个 HTTP 请求生成 trace_id 并注入 response header。"""
import uuid
from starlette.middleware.base import BaseHTTPMiddleware
from starlette.requests import Request
from starlette.responses import Response
from opentelemetry import trace, context


class TraceMiddleware(BaseHTTPMiddleware):
    """为每个请求创建根 Span，并将 trace_id 写入响应头。

    响应头：
      X-Trace-Id: <trace_id>   — 便于前端/客户端关联日志
    """
    async def dispatch(self, request: Request, call_next):
        # 如果 OTel 已有 span（instrumentation-fastapi 自动创建），直接复用
        span = trace.get_current_span()
        if not span.is_recording():
            tracer = trace.get_tracer("agentend")
            with tracer.start_as_current_span(
                f"HTTP {request.method} {request.url.path}",
                kind=trace.SpanKind.SERVER,
                attributes={
                    "http.method": request.method,
                    "http.url": str(request.url),
                    "http.route": request.url.path,
                },
            ) as root:
                root_ctx = context.get_current()
                response = await call_next(request)
                response.headers["X-Trace-Id"] = format(root.context.trace_id, "032x")
                return response

        # OTel instrumentation 已创建 span，补充 header
        response = await call_next(request)
        ctx = span.get_span_context()
        response.headers["X-Trace-Id"] = format(ctx.trace_id, "032x")
        return response
```

---

## 6. 埋点方案 — 逐文件改造指南

按调用链从外到内，标注每处需要的改动。

### 6.1 应用入口 — `src/app/main.py`

```python
# lifespan 中初始化
from src.trace.provider import init_tracing

@asynccontextmanager
async def lifespan(app: FastAPI):
    # ★ 新增：初始化 tracing
    init_tracing(
        service_name=settings.app.title,
        otlp_endpoint=settings.tracing.otlp_endpoint,  # 新增配置项
    )
    # ... 原有启动逻辑 ...

# ★ 新增：注册中间件（在 CORS 之后）
from src.trace.middleware import TraceMiddleware
app.add_middleware(TraceMiddleware)

# ★ 可选：自动 instrumentation
from opentelemetry.instrumentation.fastapi import FastAPIInstrumentor
FastAPIInstrumentor.instrument_app(app)
```

### 6.2 API 路由 — `src/api/v1/agent.py`

在 `_execute_stream` 函数中包裹根 Span：

```python
from src.trace import get_tracer, common_attrs, SpanBuilder

async def _execute_stream(...):
    tracer = get_tracer("agentend")
    attrs = common_attrs(request.session_id, request.task_id, request.agent_type)

    with tracer.start_as_current_span("agent.request", attributes=attrs) as root:
        root.set_attribute("agent.message_length", len(request.message))
        root.set_attribute("agent.workspace_path", workspace_path)

        session_mgr.update_state(session_id, SessionState.RUNNING)
        # ... 原有逻辑 ...

        try:
            async for event in adapter.stream_chat(session_id, request.message, **stream_kwargs):
                # ★ 每个关键事件类型记录到 span
                if event.type == EventType.ERROR.value:
                    root.add_event("stream_error", {"error": event.content.get("error", "")})
                yield {"event": event.type, "data": event.model_dump_json()}
        except Exception as e:
            root.set_status(trace.StatusCode.ERROR, str(e))
            raise
```

### 6.3 Orchestrator Adapter — `src/adapters/orchestrator.py`

**`stream_chat`** — 创建 `orchestrator.plan` Span：

```python
from src.trace import get_tracer, SpanBuilder, common_attrs

async def stream_chat(self, session_id, message, **kwargs):
    tracer = get_tracer("agentend")
    attrs = common_attrs(session_id, kwargs.get("task_id", ""), "orchestrator")

    with tracer.start_as_current_span("orchestrator.plan", attributes=attrs) as plan_span:
        plan_span.set_attribute("agent.count", len(kwargs.get("agents", [])))
        plan_span.set_attribute("agent.message_length", len(message))

        # ... 原有逻辑，在 node 输出处理中记录子 span ...

        # skill_prepare node
        if node_name == "skill_prepare":
            with tracer.start_as_current_span("orchestrator.skill_prepare") as sp:
                yield StreamEvent.create(EventType.PLANNING, node="skill_prepare")

        # reason node
        elif node_name == "reason":
            with tracer.start_as_current_span("orchestrator.reason") as sp:
                sp.set_attribute(ATTR_LLM_OUTPUT_TYPE, node_output.get("output_type", ""))
                for ev in await self._handle_reason(node_output):
                    yield ev

        # dispatch node
        elif node_name == "dispatch":
            drs = node_output.get("dispatch_results", [])
            waves = node_output.get("execution_waves", [])
            with tracer.start_as_current_span("orchestrator.dispatch") as sp:
                sp.set_attribute(ATTR_DISPATCH_TASK_COUNT, len(drs))
                sp.set_attribute(ATTR_DISPATCH_WAVE_COUNT, len(waves))
                for dr in drs:
                    yield StreamEvent.create(...)

        # execute node
        elif node_name == "execute":
            with tracer.start_as_current_span("orchestrator.execute") as sp:
                async for event in self._handle_execute(...):
                    yield event

        # review node
        elif node_name == "review":
            with tracer.start_as_current_span("orchestrator.review") as sp:
                sp.set_attribute("review.needs_replan", node_output.get("needs_replan", False))
                # ...
```

### 6.4 Graph Nodes — `src/orchestrator/planning/graph.py`

**`reason_node`** — 追踪 LLM 调用循环：

```python
from src.trace import get_tracer, ATTR_LLM_ITERATION

async def reason_node(state: GraphState) -> dict:
    tracer = get_tracer("agentend")

    for i in range(max_iterations):
        with tracer.start_as_current_span("llm.invoke") as llm_span:
            llm_span.set_attribute(ATTR_LLM_MODEL, settings.llm.model)
            llm_span.set_attribute(ATTR_LLM_ITERATION, i)

            response = await llm_with_tools.ainvoke(messages)

            # 记录 token 使用
            if hasattr(response, "usage_metadata") and response.usage_metadata:
                llm_span.set_attribute("llm.token_usage.prompt",
                    response.usage_metadata.get("input_tokens", 0))
                llm_span.set_attribute("llm.token_usage.completion",
                    response.usage_metadata.get("output_tokens", 0))

            if not response.tool_calls:
                llm_span.set_attribute(ATTR_LLM_OUTPUT_TYPE, "text")
                return {...}

            llm_span.set_attribute(ATTR_LLM_TOOL_CALLS, len(response.tool_calls))

            # 处理 tool calls...
            for tc in response.tool_calls:
                with tracer.start_as_current_span(f"tool.call.{tc['name']}") as tool_span:
                    tool_span.set_attribute("tool.name", tc["name"])
                    tool_span.set_attribute("tool.args", json.dumps(tc["args"], ensure_ascii=False))
                    # ... 执行工具 ...
```

### 6.5 Execution Engine — `src/orchestrator/execution/engine.py`

**`_execute_task`** — 追踪每个子任务执行：

```python
from src.trace import get_tracer, task_result_attrs

async def _execute_task(self, dispatch, timeout) -> AsyncIterator[...]:
    tracer = get_tracer("agentend")
    task_attrs = {
        "task.id": dispatch.task_id,
        "agent.name": dispatch.agent,
        "agent.type": dispatch.agent_type,
    }

    with tracer.start_as_current_span(f"orchestrator.task.{dispatch.task_id}",
                                       attributes=task_attrs) as task_span:
        start = time.monotonic()
        # ... 原有逻辑 ...

        # worktree 创建
        with tracer.start_as_current_span("workspace.worktree.create") as ws_span:
            agent_cwd = await self._ensure_worktree(dispatch)
            ws_span.set_attribute("workspace.path", agent_cwd)

        # backend run_task
        with tracer.start_as_current_span("backend.run_task") as rt_span:
            rt_span.set_attribute("backend.session_id", session_id)
            message_id = await asyncio.wait_for(
                self._backend_client.run_task(...), timeout=30.0,
            )
            rt_span.set_attribute("backend.message_id", message_id)

        # backend stream_result
        collected = []
        with tracer.start_as_current_span("backend.stream_result") as sr_span:
            async for event in self._backend_client.stream_result(...):
                # ... 原有 SSE 事件处理 ...
                pass

        duration = time.monotonic() - start
        task_span.set_attribute("task.success", success)
        task_span.set_attribute("task.duration_ms", duration * 1000)
        if error_type:
            task_span.set_attribute("task.error_type", error_type)
            task_span.add_event("task.failed", {"error": error_message})
```

---

## 7. 配置扩展

在 `src/app/config.py` 的 Settings 中新增 tracing 分区：

```python
class TracingSettings(BaseSettings):
    """OpenTelemetry tracing 配置。"""
    enabled: bool = False
    otlp_endpoint: str = ""          # gRPC endpoint，如 "http://localhost:4317"
    sample_rate: float = 1.0         # 采样率（0.0 ~ 1.0）
    console_export: bool = True      # 开发模式：console 输出
```

对应 `config.yaml`：

```yaml
tracing:
  enabled: true
  otlp_endpoint: "http://localhost:4317"
  sample_rate: 1.0
  console_export: false
```

---

## 8. 与 StreamEvent 的关系

**Trace 和 StreamEvent 是互补的两层观测**：

| 维度 | StreamEvent（已有） | Trace（新增） |
|------|---------------------|--------------|
| **面向** | 前端实时展示 | 后端运维分析 |
| **生命周期** | SSE 连接内 | 可跨请求持久化 |
| **数据** | SSE 事件流 | Span 树 + 指标 |
| **用途** | Chat UI 渲染 | 性能分析、错误归因、SLA 监控 |

StreamEvent 不变，Trace 是旁路（sidecar）观测。两者共享 `task_id` / `session_id` 作为关联键。

可在 `StreamEvent` 中新增可选字段 `trace_id`，方便前端在报错时展示 trace 链接：

```python
class StreamEvent(_StreamEvent):
    type: str
    content: dict = Field(default_factory=dict)
    timestamp: float = Field(default_factory=time.time)
    trace_id: str = Field(default="")  # ★ 新增
```

---

## 9. 实施路径

### Phase 1 — 基础骨架（1~2 天）

1. 创建 `src/trace/` 模块（provider / context / spans / attributes）
2. 在 `main.py` 初始化 TracerProvider
3. 在 `agent.py` 的 `_execute_stream` / `agent_execute` 加根 Span
4. 验证 ConsoleExporter 输出正常

### Phase 2 — Orchestrator 深度埋点（2~3 天）

5. `OrchestratorAdapter.stream_chat` 中为每个 node 创建子 Span
6. `reason_node` 内 LLM 调用循环追踪
7. `ExecutionEngine._execute_task` 完整生命周期追踪
8. `ask_agent` 跨 Agent 调用追踪

### Phase 3 — 自动 Instrumentation（1 天）

9. FastAPI auto-instrumentation（HTTP 层自动 Span）
10. httpx instrumentation（BackendClient HTTP 调用自动追踪）
11. Context propagation 到 asyncio.Task（并行 wave）

### Phase 4 — 可视化 & 告警（1~2 天）

12. 部署 Jaeger / Grafana Tempo 作为 OTel 后端
13. 配置 `docker-compose.otel.yaml`
14. 关键指标仪表盘（P95 延迟、成功率、重规划率）
15. 告警规则（连续失败、超时率飙升）

---

## 10. 性能考量

| 关注点 | 措施 |
|--------|------|
| **开销** | `BatchSpanProcessor` 批量异步导出，不阻塞请求主路径 |
| **采样** | 生产环境可设 `sample_rate=0.1`，开发环境 `1.0` |
| **内存** | 单次 Orchestrator 编排约产生 30~80 个 Span，每 Span ~1KB，总计 <100KB |
| **Context 传播** | 基于 `contextvars`，asyncio 原生支持，无需手动传递 |
| **Graceful degradation** | tracing disabled 时不创建任何 Span，零开销 |
