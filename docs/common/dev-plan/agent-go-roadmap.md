# Agent & Go 后续技术路线图

> 范围：只讨论 Go 后端与 AgentEnd / Orchestrator / Agent Runtime，不包含 Web 端 UI 改造。
> 目标：在现有 Gin + GORM + MySQL + Redis Stream、FastAPI + LangGraph + Langfuse、Git Worktree 隔离和契约生成基础上，继续引入 Agent 与 Go 方向的主流工程技术。

## 一、当前基线

项目当前已经具备一个可继续演进的多 Agent Runtime 雏形：

- **Go Backend**：`backend/` 使用 Gin + GORM + MySQL，按 Controller → Service → DAO 分层；`backend/internal/stream/` 已实现 RuntimeHub + Redis Stream + MySQL 批量刷写的 SSE 中转。
- **AgentEnd**：`agentend/` 使用 FastAPI，桥接 Claude CLI / OpenCode CLI / Codex CLI / Orchestrator，包含规则引擎、session 管理、workspace 管理、技能供给和 LangGraph 编排。
- **Orchestrator**：已有 plan review、dispatch、execute、review、replan、memory、aggregation 等闭环能力，但仍有持久化、权限、可观测性、工作流恢复等增强空间。
- **Contracts**：`contracts/schemas/` 是跨端协议单一来源，已经形成 YAML → Python / TypeScript / Go 的生成链路。

可以把后续方向理解为：从“多 Agent 聊天系统”升级成 **Agent Runtime + Go Control Plane**。

```text
用户请求
  │
  ▼
Go Backend Control Plane
  │  任务路由 / 权限 / 状态 / 流式中转 / 持久化
  │
  ├── Durable Workflow / Job Queue
  │       │
  │       ▼
  │   AgentEnd Runtime Workers
  │       │
  │       ├── Orchestrator / LangGraph
  │       ├── Claude / OpenCode / Codex CLI Adapter
  │       ├── MCP Tool Gateway
  │       └── Workspace / Sandbox
  │
  └── Observability / Evals / Audit
```

## 二、技术选择原则

1. **先补可靠性、安全、观测，再追求复杂编排。** Agent 系统失败模式多，先能恢复、能看见、能限制，再谈更智能。
2. **优先渐进式引入。** 当前 Go Backend 与 AgentEnd 已能工作，不建议为了“主流技术”直接大重写。
3. **保留契约优先原则。** 所有跨服务字段、状态、事件都应优先落在 `contracts/schemas/` 或后续 Protobuf 契约中。
4. **工具能力标准化。** Agent 能调用的 Git、文件、workspace、资源、技能操作应变成统一工具层，而不是散落在 Adapter 或 prompt 里。
5. **运行时能力可审计。** 每次 Agent 执行、工具调用、审批、重试、失败恢复都要能在日志、trace、metric 或数据库中追踪。

## 三、推荐路线

| 优先级 | 方向 | 主流技术 | 建议落点 | 收益 |
|--------|------|----------|----------|------|
| P0 | Agent 安全与权限治理 | OPA / Rego、路径白名单、tool approval | AgentEnd rules + Go Backend audit | 阻止任意路径写入、危险工具、越权 workspace 操作 |
| P1 | Session / Runtime 状态持久化 | Redis / MySQL store、LangGraph checkpointer | `agentend/src/session/`、`agentend/src/orchestrator/` | 支持多实例、重启恢复、人工审批恢复 |
| P1 | OpenTelemetry 全链路观测 | OpenTelemetry Go/Python、OTLP、Prometheus、Tempo/Jaeger | Backend + AgentEnd + Orchestrator | 串起 API、SSE、Redis、LLM、CLI、workspace 的 trace |
| P1 | Agent Job Queue | Redis Streams Consumer Groups，后续 NATS JetStream | Backend task runner + AgentEnd workers | 支持 ack、retry、dead letter、worker 横向扩容 |
| P2 | MCP 工具网关 | Model Context Protocol | workspace/git/taskctl/resources/skills tools | 统一 Agent 工具生态，减少 Adapter 私有逻辑 |
| P2 | Durable Workflow | Temporal Go SDK 或 LangGraph 持久化 | Orchestrator 执行链路 | 长任务、审批、重规划、失败恢复更稳 |
| P2 | Agent Evals | Langfuse evals / LangSmith evals、golden tasks | `agentend/tests/` + eval datasets | 评估规划质量、工具轨迹、输出质量、成本和延迟 |
| P3 | 内部 RPC 契约升级 | Protobuf、Buf、gRPC Go / Connect RPC | Backend ↔ AgentEnd | 强类型流式 RPC、breaking change 检测 |
| P3 | Go 测试工程化 | testcontainers-go、Go fuzzing、race test | Backend DAO / stream / validator | 提升 Redis/MySQL/SSE/安全边界测试可信度 |

## 四、分阶段落地

### Phase 0：安全底座优先

当前 `agentend/docs/backlog/orchestrator-drawbacks.md` 已指出 `shared_dir` 路径注入、LLM 输出直接写文件、Orchestrator 与 workspace 系统割裂等问题。建议先做这部分。

可落地任务：

- 为 `shared_dir`、`workspace_path`、`repo_path` 建立统一路径解析器，只允许落在当前 task 的标准 workspace/shared 目录下。
- 将 Orchestrator 文件写入动作收敛到 Workspace API，不直接接收任意绝对路径。
- 在 AgentEnd rule engine 中新增 Capability / Permission rule，声明每类 Agent 可用工具、可写目录、可访问网络范围。
- 在 Go Backend 记录 tool call / approval / denied audit log，后续可用于安全审计和 eval。
- 对路径校验、zip skill 校验、SSE event parser 加 Go fuzz / Python property test。

建议技术：

- **OPA / Rego**：适合把“哪些工具、哪些目录、哪些操作需要审批”从代码中抽出来，作为 policy-as-code 管理。
- **gVisor / 容器沙箱**：后续如果允许 Agent 执行更危险命令，可用作强隔离运行时。短期可以先做进程级限制和路径白名单。

### Phase 1：状态持久化与多实例准备

当前 `agentend/docs/backlog/session-persistence.md` 已记录 session mapping 仍依赖本地 JSON 文件，适合单机开发，不适合多实例。

可落地任务：

- 抽象 `SessionMappingStoreProtocol`，提供 file / Redis / MySQL 三种实现。
- Redis key 使用 `agentend:session:{task_id}:{session_id}`，支持 TTL 与主动删除。
- MySQL 表记录 `request_session_id`、`task_id`、`agent_type`、`cli_session_id`、`created_at`、`updated_at`。
- 不传 `session_id` 时也要生成并保存完整映射，避免后续 resume 找不到 CLI session。
- Orchestrator memory、pin memory、conversation memory 从散落文件逐步迁移到可配置持久化后端。

建议顺序：

1. 先补 Redis store，成本最低，适合 Runtime 临时状态。
2. 再补 MySQL store，适合审计和长期查询。
3. 最后考虑 LangGraph checkpointer，解决 Orchestrator graph step 级恢复。

### Phase 2：OpenTelemetry + Metrics

Agent 系统最怕“失败了但不知道卡在哪里”。当前已有 Langfuse，用于 LLM / Orchestrator trace；后续建议补 OpenTelemetry 做全链路技术 trace，不替换 Langfuse。

可落地任务：

- Go Backend 增加 request id / trace id middleware，并注入到 AgentEnd 请求头。
- `StreamWriter` 对 run、Redis XADD、MySQL flush、session status update 打 span。
- AgentEnd FastAPI 增加 OTel middleware，保留 SSE 长连接特殊处理。
- Orchestrator 节点打 span：reason、human_review、dispatch、execute、review、evolve、save_mem。
- CLI Adapter 打 span：process start、first token latency、exit code、interrupt、timeout。
- 暴露 Prometheus metrics：active streams、stream duration、flush lag、Redis stream lag、agent run duration、LLM call latency、tool failure count。

注意：

- 不把 prompt、用户输入、文件内容直接写入 trace attribute。
- Langfuse 继续负责 LLM 语义观测；OTel 负责系统链路观测。

### Phase 3：Agent Job Queue

现在 Redis Stream 已用于 SSE 重连与事件补偿。后续可以新增独立 job stream，把 Agent 执行从 HTTP 同步触发升级为异步任务消费。

可落地任务：

- 新增 job keyspace，例如 `agenthub:jobs:{queue}`，不要复用 SSE stream key。
- Backend 创建任务时写入 job stream，记录 `task_id`、`session_id`、`agent_type`、`message_id`、`attempt`。
- AgentEnd worker 使用 Consumer Group 消费任务，执行完成后 ack。
- 增加 retry policy、dead letter stream、max attempts、backoff。
- Backend 查询 job 状态，SSE 仍通过现有 stream 通道推送。

技术选择：

- **短期：Redis Streams Consumer Groups**。项目已经依赖 Redis，改造成本低。
- **中长期：NATS JetStream**。如果需要更清晰的 worker queue、durable consumer、多主题路由和更好的消息治理，再迁移或并行引入。

### Phase 4：MCP Tool Gateway

MCP 是 Agent 工具生态的主流方向之一。项目已经有 skills、workspace、taskctl、resource API，适合抽象成 MCP server。

可落地工具：

| MCP 能力 | 对应现有模块 | 说明 |
|----------|--------------|------|
| `workspace.read_file` / `workspace.write_file` | `agentend/src/workspace/` | 统一文件读写与路径校验 |
| `workspace.diff` / `workspace.commit` / `workspace.merge` | `agentend/src/workspace/git_ops.py` | 统一 Git 操作 |
| `task.summary` / `task.merge` | `agentend/src/skills/builtin/taskctl` | 复用内置 taskctl |
| `resources.inspect` | `agentend/src/api/v1/resources.py` | 暴露磁盘、内存、运行状态 |
| `skills.list` / `skills.install` | `agentend/src/api/v1/skills.py` | 统一技能发现和安装 |
| `session.interrupt` / `session.destroy` | `agentend/src/session/manager.py` | Agent 生命周期控制 |

建议：

- 先做只读 tools/resources，再开放写操作。
- 写操作默认需要 approval。
- MCP tool schema 应由 Pydantic model 或 contracts 生成，避免手写漂移。

### Phase 5：Durable Workflow

Orchestrator 当前已经有规划、执行、审查、重规划，但真正的 durable execution 还不完整。

两个选择：

| 方案 | 适合场景 | 优点 | 代价 |
|------|----------|------|------|
| LangGraph checkpointer | 先增强现有 Python Orchestrator | 改造小，贴合当前 graph | 跨 Go Backend / AgentEnd 的全局编排能力弱 |
| Temporal Go SDK | 把 Go Backend 升级为可靠控制平面 | 长任务、重试、审批、恢复、可视化成熟 | 引入 Temporal server 与 worker 运维成本 |

推荐路径：

1. 先用 LangGraph checkpointer 解决 Orchestrator 节点级恢复。
2. 当任务队列、审批、超时、重试逻辑变复杂后，再用 Temporal Go SDK 接管跨服务工作流。
3. Temporal workflow 只负责编排和状态，不直接跑不可信代码；具体 Agent 执行仍在 AgentEnd worker / sandbox 中完成。

### Phase 6：Agent Evals

只靠单元测试很难判断 Agent 改动是否变好。建议建立 eval 套件，把“规划质量”和“工具轨迹”变成可比较指标。

可落地任务：

- 建立 golden task 数据集：小修 bug、跨文件重构、冲突合并、文档生成、权限拒绝、长任务中断恢复。
- 记录每次 Orchestrator 的 plan、dispatch、tool calls、final summary、实际 diff。
- 增加评估维度：任务拆分是否合理、Agent 分配是否正确、是否越权、是否重复执行、是否按要求产出、成本和耗时。
- 在 CI 或本地命令中跑 smoke eval，不要求每次全量跑。
- 将失败样例沉淀为 regression cases。

可选平台：

- **Langfuse evals**：贴合当前已有 Langfuse trace。
- **LangSmith evals**：生态成熟，适合 LangChain / LangGraph 工作流评测。
- **自建轻量 eval runner**：先用 pytest + 固定输入输出 + judge prompt 起步，避免过早平台化。

### Phase 7：内部 RPC 契约升级

现有 REST + YAML contracts 已能支撑开发。若 Backend ↔ AgentEnd 交互继续变复杂，可引入 Protobuf 体系。

推荐技术：

- **Buf**：Protobuf lint、breaking change detection、代码生成统一入口。
- **gRPC Go**：服务间 RPC 主流方案，适合双向流、服务发现和高性能内部通信。
- **Connect RPC**：兼容 gRPC / gRPC-Web / Connect 协议，HTTP 友好，Go 客户端和服务端体验较轻。

建议落地边界：

- 不立即替换对外 REST。
- 先把 Backend ↔ AgentEnd 的内部接口抽成 proto：run agent、stream event、workspace operation、session operation。
- 保留 `contracts/schemas/` 管 SSE 与前端兼容协议；内部服务协议可以逐步迁移到 proto。

### Phase 8：Go 测试工程化

Go 后端已经有一批单测，后续可以补更接近真实依赖的测试层。

可落地任务：

- 使用 `testcontainers-go` 启 MySQL / Redis，测试 DAO、StreamWriter、cleanup、reconnect。
- 对 `StreamWriter` 增加并发、断连、error event、plan review、agent switch 的集成测试。
- 用 Go fuzzing 测：SSE 行解析、runtime block 解析、路径清洗、skill zip 校验。
- 在 CI 中跑 `go test -race ./...`。
- 为 AgentEnd 增加 async pytest，覆盖 Orchestrator reason/dispatch/review/replan、workspace recovery、session store。

## 五、不建议优先做的事

- **不建议立刻重写 Gin / GORM。** 当前后端结构清晰，除非遇到明显性能或类型安全痛点，否则迁移收益不如观测、队列、权限。
- **不建议一开始就上 Kubernetes 全套。** 没有 job queue、metrics、trace 和健康检查之前，编排平台只会放大排障难度。
- **不建议把 Orchestrator 完全塞进 AdapterRegistry。** 现有文档已经指出它更像 planner / workflow engine，后续应逐步拆出独立编排层。
- **不建议让 LLM 直接决定高危工具执行。** 所有写文件、shell、merge、删除、网络访问都应经过策略与审批层。
- **不建议只堆 prompt。** Agent 能力的瓶颈往往在状态、工具、权限、恢复、评估，而不是更长的系统提示词。

## 六、建议的下一期 Backlog

| 编号 | 任务 | 类型 | 优先级 |
|------|------|------|--------|
| AG-01 | 统一路径校验器，修复 Orchestrator `shared_dir` 任意路径风险 | 安全 | P0 |
| AG-02 | Capability / Permission Rule：按 Agent 类型限制工具和目录 | 安全 | P0 |
| AG-03 | SessionMappingStore Redis 实现，替代单机 JSON 默认方案 | 可靠性 | P1 |
| AG-04 | Backend + AgentEnd trace id 透传，接入 OTel 基础 span | 可观测性 | P1 |
| AG-05 | Prometheus metrics：active streams、agent duration、LLM latency、Redis lag | 可观测性 | P1 |
| AG-06 | Redis Streams Consumer Group 版 Agent job queue spike | 架构 | P1 |
| AG-07 | MCP tool gateway spike：先暴露 workspace read/diff/resource 只读工具 | Agent 工具 | P2 |
| AG-08 | LangGraph checkpointer spike，验证 plan review 后恢复执行 | Durable workflow | P2 |
| AG-09 | Golden task eval runner，覆盖规划、工具轨迹、最终结果 | Agent eval | P2 |
| AG-10 | testcontainers-go 集成测试：MySQL + Redis + StreamWriter | 测试 | P2 |

## 七、推荐总路线

```text
安全边界
  ↓
状态持久化
  ↓
OpenTelemetry + Metrics
  ↓
Agent Job Queue
  ↓
MCP Tool Gateway
  ↓
Durable Workflow
  ↓
Agent Evals
  ↓
RPC 契约升级
```

这条路线能最大化利用现有代码：Go Backend 继续作为控制平面，AgentEnd 继续作为运行时，Redis/MySQL 继续承接短期状态与持久化，再逐步引入 MCP、Temporal、OpenTelemetry、evals 等 Agent 和 Go 生态里的主流技术。

## 八、参考资料

- OpenTelemetry Go：<https://opentelemetry.io/docs/languages/go/>
- OpenTelemetry Docs：<https://opentelemetry.io/docs/>
- Redis Streams：<https://redis.io/docs/latest/develop/data-types/streams/>
- NATS JetStream：<https://docs.nats.io/nats-concepts/jetstream>
- Temporal Docs：<https://docs.temporal.io/>
- Temporal Go SDK：<https://pkg.go.dev/go.temporal.io/sdk/workflow>
- Model Context Protocol：<https://modelcontextprotocol.io/specification/draft/server/tools>
- LangGraph Persistence：<https://docs.langchain.com/oss/python/langgraph/persistence>
- Open Policy Agent：<https://www.openpolicyagent.org/docs>
- gVisor：<https://gvisor.dev/docs/>
- Buf CLI：<https://buf.build/docs/cli/quickstart/>
- gRPC Go：<https://grpc.io/docs/languages/go/quickstart/>
- Connect RPC Go：<https://github.com/connectrpc/connect-go>
- Testcontainers for Go：<https://golang.testcontainers.org/quickstart/>
- Go Fuzzing：<https://go.dev/doc/security/fuzz/>
