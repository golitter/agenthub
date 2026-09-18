# AgentEnd Runtime 超详细实现

## 实现了什么

AgentEnd 是 FastAPI 执行面。它把 Backend 的统一 AgentRequest 转换为 Claude Code、OpenCode、Codex、Pi 或 Orchestrator 执行，负责工作目录、规则、Session、CLI 子进程、Run journal、资源预算、预览和结果净化。

## 怎么实现的

### FastAPI 生命周期

`agentend/src/app/main.py` 的 lifespan 负责所有长生命周期依赖：

1. 启动安全校验：非 loopback 监听与服务认证、unsafe process 的显式允许。
2. 创建 AdapterRegistry、SessionManager、SessionMappingStore、RuleEngine。
3. 创建 WorkspaceManager、PreviewManager、BackendClient、PathPolicy。
4. 打开 SQLiteRunRepository 与 IntegrationOperationRepository。
5. 创建 RunSupervisor，并保留集成恢复依赖的 root run 后恢复未完成 Run。
6. 从 JSON registry 加载 Workspace，与 `git worktree list` 对账后再次加载。
7. 恢复未完成 integration operation 与 conflict recovery。
8. 恢复 Skill staging/backup，启动周期清理。
9. 连接 MySQL DBReader，启动 inactive workspace cleanup。
10. 异步向 Backend 报告 builtin skills。

关闭顺序取消周期任务、停止 inactive cleanup 和所有 preview、关闭 RunSupervisor、两个 repository、BackendClient、DBReader 与 Langfuse。

### 依赖获取

`api/dependencies.py` 从 `request.app.state` 获取 manager、registry、supervisor 等对象，路由不自行 new 全局依赖。配置 `settings` 在模块加载时实例化，缺失或非法直接 fail fast。

### 请求执行主链路

`POST /v1/agent/stream` 在 `api/v1/agent.py` 中执行：

1. 校验 execution backend 可用；Phase 2 integration 时校验必要凭据。
2. 对 repo/workspace/session/request 生成规范化指纹，用于幂等 Run 验证。
3. 解析并验证预算，构造只暴露必要值的子进程环境。
4. `_resolve_workspace` 用 PathPolicy 验证路径，查找或创建 Worktree。
5. `_resolve_session` 读取/创建本地 Session，并关联 CLI session id。
6. 写入受限长度的 SOUL 文档或 prompt append。
7. RuleEngine 顺序应用规则，得到 system prompt、allowed tools、constraints 等结果。
8. 根据 agent_type 从 registry 选择 Adapter。
9. 向 RunSupervisor start 或 attach run；journal 包装所有事件。
10. `_execute_stream` 迭代 Adapter 事件，通过 sanitizer 后以 SSE 返回。
11. 取消、超时、进程错误和正常 done 分别写唯一终态。

`POST /v1/agent/execute` 复用同一执行内核，但收集事件并返回一次性 AgentResponse，适合非流式调用。`POST /v1/agent/review` 把计划审查结果投递给等待中的 Orchestrator graph。

### Adapter 抽象

`adapters/base.py` 定义 BaseAgentAdapter 和共同的进程治理：

- `child_process_env` 只继承/注入允许的环境。
- 子进程创建独立 process group，取消时先 terminate，等待配置的 grace timeout 后 kill。
- stderr 独立 drain，避免管道写满导致死锁。
- Adapter 对外产出统一 StreamEvent，不让 CLI 私有 JSON 泄漏到上层。

Registry 根据 agent type 返回：

| Agent | Adapter | 会话/协议特点 |
|---|---|---|
| claude-code | `ClaudeCodeAdapter` | Claude CLI stream JSON，会话 ID 写回 |
| opencode | `OpenCodeAdapter` | OpenCode 事件协议与 session 延续 |
| codex | `CodexAdapter` | Codex JSONL，done 和工具事件归一 |
| pi | `PiAdapter` | Pi RPC/消息数组，合并 usage 与 assistant 内容 |
| orchestrator | `OrchestratorAdapter` | 不启动单一 CLI，进入规划、分派、汇总图 |

各 Adapter 必须处理：命令构造、系统 prompt、工作目录、allowed tools、resume session、stdout 解析、usage、异常行、取消和终态。

### 规则引擎

RuleEngine 按 registry 中顺序执行 BaseRule，每条规则接收 request/context 并返回对执行参数的增量。内置规则：

- SafetyRule：加入危险操作与边界约束。
- ScopeRule：限制任务与工作目录范围。
- TaskctlRule：为 Orchestrator/子 Agent 暴露结构化 Git 集成工具。
- SkillRule：发现和注入 builtin/external Skill。
- PinRule：加载共享 pin memory，构造稳定快照。
- SoulRule：注入 Agent 身份文档。
- GroupChatRule：加入群聊成员、历史和协调上下文。

规则产物合并时必须保持确定顺序。CLI 支持 system prompt append 时使用原生参数；不支持时走兼容注入路径。

### Session 管理

SessionManager 维护运行中的 Session 状态；SessionMappingStore 把 AgentHub session ID 映射到 CLI 原生 session ID。映射写入 JSON 时使用 `atomic_write_text`，通过同目录临时文件 + replace 避免崩溃留下半文件。

Session API：GET 列表/单项，POST interrupt，DELETE 删除。interrupt 会调用正在运行 Adapter/Run 的取消路径；delete 还要清理 mapping，但 Workspace 删除由对应生命周期接口负责，不能假设删除 Session 自动等于删除全部 Git 数据。

### Run 生命周期

RunSpec 是不可变启动事实，含 run/root/parent、task/session/workspace、Agent、requester、budget。RunRecord 增加状态、时间、结果和 error。

SQLiteRunRepository 持久化：

- Run record 与 parent/root 关系。
- 单调递增事件 seq 和 journal payload。
- 合法状态转换与唯一终态。
- 父 Run 已关闭时拒绝新子 Run。
- 幂等 request fingerprint 冲突检测。

RunSupervisor 管理 asyncio task 与并发 semaphore。默认 `max_concurrent_runs=4`。取消先写意图并通知 task；恢复时把不应继续的旧 active run 转换为中断/失败，同时为 integration 指定 root 保留恢复空间。

Runs API 支持 active list、单 run、按 `after_seq` 读 events、cancel、resume。resume 只能用于实现允许的可恢复状态，不能把任意终态重新打开。

### 预算与超时

Execution 配置包括 max turns、总体 timeout、process terminate timeout、最大并发与 sandbox。AgentRunBudget 可进一步约束 wall time、turn/tool/资源维度。路由将原始预算转换为校验后的对象，非法/负数值在启动前拒绝。

超时层次：

- CLI/Run 总执行 timeout。
- 进程终止 grace timeout。
- Orchestrator LLM、ask-agent、stream chunk、review、skill、resolver 各自 timeout。
- Backend/AgentEnd HTTP client timeout。

这些超时不应互相替代；外层超时必须触发内层取消和 journal 终态。

### 出站净化

`transport/sanitizer.py` 在事件离开 AgentEnd 前处理超大字段，尤其是 tool_call args、tool_result 和 text。目标是保护 SSE、Redis 和浏览器内存，但保留事件类型、标识、状态与足够的摘要。Artifact 大内容不应进入 SSE，而应上传对象存储并只发送 resource metadata。

### BackendClient

`clients/backend_client.py` 封装 AgentEnd 回调 Go Backend：读取消息窗口、群聊上下文、公告、内部 stream/run，以及 builtin skill report 等。启用 service auth 时附加 token。Client 在 lifespan 中单例复用连接池并在 shutdown 关闭。

### AgentEnd API 分组

| 前缀 | 功能 |
|---|---|
| `/health`,`/health/live`,`/health/ready` | 存活与就绪 |
| `/v1/agent` | stream、review、execute |
| `/v1/agents/configs` | Agent 配置发现 |
| `/v1/session` | Session 查询、interrupt、delete |
| `/v1/workspace` | Worktree 文件/Git/preview/cleanup |
| `/v1/validate-repo-path`,`/v1/init-git-repo` | 仓库校验与初始化 |
| `/v1/resources` | 磁盘与内存指标 |
| `/v1/runs` | Run 与 journal |
| `/v1/skills` | 扫描、原子安装、移除 |
| `/v1/pin` | pin add/remove/list、公告取消置顶 |
| `/v1/internal/integration-operations` | 集成操作诊断与执行 |
| `/v1/internal/conflicts` | 冲突详情、投影与动作 |
| `/v1/evals` | 评测 datasets / experiments / trials / compare 查询与 trial review 写入（供 Backend 管理面板代理） |
### 本地持久化文件

| 配置 | 默认 | 内容 |
|---|---|---|
| `workspace.store_path` | `logs/workspaces.json` | Workspace registry |
| `session.store_path` | `logs/session_mappings.json` | AgentHub → CLI session mapping |
| `execution.run_store_path` | `logs/runs.sqlite3` | Run、events、integration、conflict |
| `workspace.base_dir` | `./worktrees` | Git Worktree 父目录 |

相对路径以 AgentEnd 项目配置位置解析。JSON 写入必须原子化；SQLite 变更必须走 repository 事务；Worktree registry 必须与 Git 命令输出对账。
