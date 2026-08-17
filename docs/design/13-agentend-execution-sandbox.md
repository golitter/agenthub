# 13 — AgentEnd 执行沙盒与控制面安全实施规划

> **状态**：首个受控执行竖切已实施；严格 ExecutionSandbox 尚未实现并保持失败关闭，不具备生产 / Eval 隔离条件
> **日期**：2026-08-12
> **范围**：AgentEnd、Backend、Frontend、Contracts、Config Center、部署与测试
> **前置决策**：Agent 默认采用自治执行；当前任务 Workspace 内无需逐工具确认并拥有完整开发权限，宿主机边界、跨 Workspace 隔离和资源上限由系统强制执行
> **关联文档**：[三层架构设计](01-three-tier-design.md)、[Agent 路由与自动分派](09-agent-routing-and-dispatch.md)、[Agent Go 化路线图](../common/dev-plan/agent-go-roadmap.md)、[AgentEnd 架构](../../agentend/docs/design/00-architecture.md)、[AgentEnd Adapter](../../agentend/docs/design/02-adapters.md)、[AgentEnd Workspace](../../agentend/docs/design/08-workspace.md)

## 1. 背景

AgentEnd 已经承担统一 Agent Harness 的核心职责：适配 Claude Code、Codex、OpenCode 与
Orchestrator，准备 Git Worktree、注入规则与技能、维护会话状态，并将执行事件通过 SSE 返回
Backend。它也是未来批量 Agent Eval 的实际执行入口。

规划启动时的实现把 Agent CLI 当作普通子进程直接运行，基线问题如下：

- `ClaudeCodeAdapter`、`CodexAdapter`、`OpenCodeAdapter` 分别调用
  `asyncio.create_subprocess_exec()`，并各自维护 `session_id → Process` 内存映射。
- Adapter 的 `interrupt()` 只对父进程执行 `terminate()` / `kill()`，无法证明其创建的 Shell、
  测试服务器或后台进程已全部退出。
- `POST /v1/session/{session_id}/interrupt` 当前只把 Session 状态改为 `interrupted`，没有调用
  对应 Adapter，也没有终止真实进程。
- 每次请求临时实例化 Adapter，进程所有权没有提升到应用级组件；SessionManager 中虽然保留
  `Session.process` 字段，实际 Adapter 执行链路没有统一写入。
- Claude Code 使用跳过权限确认参数，Codex 使用跳过审批与 `danger-full-access`。现有
  SafetyRule 和 ScopeRule 主要通过 Prompt 约束行为，不构成操作系统安全边界。
- Workspace 是 Git 分支与目录隔离，不是进程、文件系统、资源或密钥隔离。
- Orchestrator 子任务通过 AgentEnd → Backend → AgentEnd 回环启动新的 Agent 请求，父任务取消
  目前不能可靠传播到所有子任务。
- AgentEnd 默认配置允许监听 `0.0.0.0`，业务端点没有独立的 Backend → AgentEnd 服务认证。
- `repo_path`、`workspace_path` 等路径缺少统一的授权根目录和真实路径校验。

在单次人工任务下，这些问题可能表现为无法可靠停止或误操作；在批量 Eval 下，同一个问题会被
并发放大为进程泄漏、端口占用、宿主机越界和资源耗尽。因此执行沙盒是 Eval Harness 之前的
P0 基础设施。

### 1.1 当前落地快照（2026-08-13）

本次实现完成了可以独立验收的第一条纵向链路，但没有把“进程组控制”冒充为“操作系统沙盒”：

- Contracts 已加入 `run_id / root_run_id / parent_run_id`、Run 状态、预算和结构化终止原因，并生成
  Python、TypeScript、Go 三端类型。
- AgentEnd 已加入 SQLite WAL RunRepository、EventJournal、RunSupervisor、幂等 RunSpec、父子 admission
  fence、状态 / 事件 / 取消 API；`/stream` 与 `/execute` 均由 Supervisor 启动，消费者断开不再直接
  取消 Run，事件可按 `after_seq` 续读。
- 三种 CLI 在本地不安全后端中使用独立进程组，取消执行 TERM → grace → KILL；子进程环境过滤
  AgentEnd / Backend 服务密钥。Session interrupt 已接入 Run 取消。
- AgentEnd 与 Backend 已支持独立 Bearer 服务密钥；非 loopback 且未启用 AgentEnd 服务认证时启动
  失败。PathPolicy 对授权根、canonical path、目录边界和 symlink 逃逸执行统一校验。
- Backend 已持久化 Message 与 `run_id` 的关联，提供用户权限范围内的状态 / 取消 API，并为
  AgentEnd 回调提供内部路由。Frontend 流式发送按钮可切换为停止 / 正在停止状态。
- 已实现 wall time、事件数量和输出字节上限、调用方预算上限收紧、根 Run 的子节点总量 / 活动并行度
  约束，以及父取消向已登记子 Run（含 queued 分支）的传播；CPU、内存、pids、空闲、磁盘和跨子 Run
  的资源消耗汇总尚未实现。
- readiness 会探测 Linux、bubblewrap、user namespace、cgroup v2 等能力。凭据代理、受控出口、Git
  元数据隔离和磁盘 quota 未实现，因此 `strict` 模式拒绝执行且 readiness 返回 HTTP 503 / not ready；本地
  `unsafe_process` 必须显式启用，只提供生命周期控制，不提供宿主机隔离。

仍未完成的关键闭环包括：linux_bwrap / cgroup 执行后端、短期委托与凭据代理、网络和 Git 元数据
隔离、Backend EventJournal 自动断点续接、Adapter 彻底移除独立进程表、Orchestrator 独立执行域、
完整预算、重启后遗留 cgroup 回收、Config Center / Docker precheck 和真实四类 Agent 端到端验收。

## 2. 核心设计决策

### 2.1 默认自治，不做逐工具审批

AgentHub 的目标是无人值守的多 Agent 执行与评测。默认模式为 `autonomous`：

- Agent 在系统分配的当前 Workspace 内可读写文件、删除文件、安装依赖、运行测试、启动预览、
  执行 Git 命令和创建子进程。
- Claude Code、Codex、OpenCode 不因普通文件或命令操作暂停等待用户确认。
- 安全边界由服务认证、PathPolicy、操作系统沙盒、RunSupervisor 和 ExecutionBudget 提供，
  不依赖 Agent 自觉遵守 Prompt。
- 自治权限只覆盖工作区内的文件与开发命令，不包含读取宿主机长期凭据、直连宿主机控制面、
  直接修改共享 Git 元数据或绕过受控网络出口。

需要用户确认的是 Harness 边界外的高影响动作，例如合并到真实主分支、推送远程、生产部署、
调用真实外部账号和切换至宿主机不受限模式，而不是每一次内部工具调用。

### 2.2 沙盒内全权限，不等于宿主机全权限

```text
宿主机
┌──────────────────────────────────────────────────────────┐
│ AgentEnd                                                 │
│   │                                                      │
│   └── RunSupervisor                                      │
│       └── Execution Sandbox                              │
│           ├── 当前 Worktree：读写                        │
│           ├── 当前 Task Shared：读写                     │
│           ├── Task 独立 Git 元数据 / Git Broker          │
│           ├── Run 级短期 CLI 凭据或凭据代理              │
│           ├── 独立临时目录：读写                         │
│           ├── 受控模型 / 依赖网络出口                    │
│           ├── 其他 Workspace / AgentEnd 密钥：不可见     │
│           └── CPU / 内存 / 进程 / 时间 / 输出：有限额    │
└──────────────────────────────────────────────────────────┘
```

### 2.3 AgentEnd 是全部 Run 的生命周期所有者

所有 CLI Agent 和它们创建的完整进程树必须由 AgentEnd 创建、登记、观察、取消和回收。Adapter
只负责命令构造与事件解析，不再拥有独立进程表。

### 2.4 严格模式失败关闭

远程部署、生产环境和 Eval 模式必须使用真实沙盒。如果运行环境不支持所需内核能力或沙盒程序，
AgentEnd 必须拒绝执行，不能悄悄降级为宿主机直接执行。

本地开发可显式使用 `unsafe_process` 后端，但必须：

- 只绑定 loopback。
- 在启动日志和健康状态中明确标记不安全。
- 禁止运行批量 Eval。
- 仍使用进程组终止、密钥过滤和基本资源限制。

## 3. 目标与非目标

### 3.1 目标

- Backend 与 AgentEnd 建立明确的内部服务身份，AgentEnd 业务端点拒绝匿名访问。
- 所有 Repo、Worktree、Shared、Skill 和文件操作通过统一 PathPolicy。
- 引入应用级 RunSupervisor，统一管理单 Agent、Orchestrator 和子 Agent Run。
- 引入可替换 ExecutionSandbox 接口，首个严格后端面向 Linux / WSL2。
- 默认在 Workspace 内自治执行，无逐工具审批。
- 支持按 `run_id` 幂等取消完整进程树，并把父 Run 取消传播到所有子 Run。
- 统一执行时间、空闲时间、并发、CPU、内存、进程数、输出和磁盘预算。
- AgentEnd 异常重启后能识别、终止和回收遗留沙盒。
- 提供结构化安全审计、终止原因、健康信息和自动化攻击场景测试。
- 为后续 Eval Runner 提供稳定的 Run、预算和取消基础模型。

### 3.2 非目标

- 本阶段不实现 Eval Dataset、Trial、Grader、Baseline 或评测 UI。
- 不实现用户级 OAuth、SSO、RBAC 或多租户 IAM。
- 不实现 Kubernetes 调度或跨主机沙盒集群。
- 不实现 Windows 原生强隔离；Windows 用户以 WSL2 为严格模式目标。
- 不实现细粒度逐命令人工审批状态机。
- 不承诺撤销已经完成的外部副作用，例如已推送 Commit、已发送消息或已成功调用的第三方 API。
- 第一阶段不实现面向任意业务的完整网络策略语言，但严格模式必须隔离宿主 loopback、私网和云元数据
  地址，并通过受控出口满足模型与依赖源访问；不得共享宿主网络 namespace 后直接开放任意网络。

## 4. 信任边界与威胁模型

### 4.1 信任边界

| 组件 | 信任级别 | 说明 |
|---|---|---|
| Frontend 用户 | 已登录但不可信输入 | 任务文本、仓库路径、文件内容均需验证 |
| Backend | 可信控制面 | 负责用户身份、任务、消息和服务间调用 |
| AgentEnd 主进程 | 高信任执行控制面 | 持有宿主机工作区与 Agent CLI 调度能力 |
| Agent CLI / 模型输出 | 不可信执行主体 | 可能误判、遭 Prompt Injection 或产生危险命令 |
| Repository 内容 | 不可信数据 | README、脚本、Skill、测试均可能诱导或攻击 Agent |
| Sandbox | 可丢弃执行域 | 允许当前任务内完全自治，但必须限制爆炸半径 |
| 外部网络 | 不可信 | 模型 API、包管理源和任意远端响应 |

### 4.2 P0 必须覆盖的风险

- 未授权客户端直接调用 AgentEnd 执行 CLI、写文件或初始化 Git 仓库。
- `../`、绝对路径或 symlink 逃逸到授权根目录之外。
- Agent 读取 AgentEnd `.env`、数据库密码、MinIO 凭据或其他任务内容。
- Agent 读取 CLI 的长期登录凭据或模型 API Key，并通过任意外网连接外传。
- Agent 通过共享宿主网络访问 Backend、AgentEnd、MySQL、Redis、Config Center、云元数据或其他
  本地控制面。
- Agent 修改其他仓库、用户 Home、系统目录或其他 Workspace。
- Agent 通过 Git common dir 修改其他 Task 的 refs、config、hooks 或 worktree 元数据。
- Agent 创建后台进程，父 CLI 退出或被中断后仍继续运行。
- Agent 无限输出、无限循环、fork bomb、内存/磁盘耗尽或高并发占满宿主机。
- Orchestrator 被取消后子 Agent 继续执行。
- AgentEnd 崩溃后留下孤儿进程、端口、cgroup、临时目录或 Run 记录。
- 沙盒不可用时静默回退到宿主机全权限执行。

## 5. 目标组件架构

```text
Frontend
   │ 用户任务 / 停止
   ▼
Backend
   │ 用户鉴权、生成 run_id、服务凭证、取消转发
   ▼
AgentEnd API
   ├── ServiceAuthMiddleware
   ├── PathPolicy
   └── Run API
          │
          ▼
     RunSupervisor  ─────────────── RunRepository
          │                         状态 / 父子关系 / 资源用量
          ├── BudgetManager
          ├── EventJournal
          ├── AuditLogger
          ├── CredentialBroker
          ├── NetworkPolicy
          └── ExecutionSandbox
                    │
                    ▼
             ManagedProcess
             ├── stdout / stderr
             ├── process group
             ├── PID namespace
             └── cgroup / resource limits
                    │
                    ▼
         Claude Code / Codex / OpenCode / Orchestrator Worker
                    │
                    └── Shell / tests / preview / child processes
```

建议新增模块：

```text
agentend/src/security/
├── authentication.py
├── path_policy.py
├── audit.py
├── startup_validation.py
├── credential_broker.py
└── network_policy.py

agentend/src/execution/
├── models.py
├── errors.py
├── budget.py
├── repository.py
├── event_journal.py
├── supervisor.py
├── sandbox.py
└── backends/
    ├── linux_bwrap.py
    └── unsafe_process.py
```

## 6. Run 领域模型

### 6.1 RunIdentity

每次执行使用独立 `run_id`，不能继续把 `session_id` 同时当会话、进程和取消标识。

```text
RunIdentity
├── run_id                 本次执行唯一 UUID
├── root_run_id            整棵执行树根 Run
├── parent_run_id          Orchestrator 子任务的父 Run
├── task_id
├── session_id
├── message_id
├── workspace_id
├── agent_type
└── requested_by           backend / eval
```

约束：

- Backend 在接收用户执行请求时生成根 `run_id`，并随 AgentRequest 传给 AgentEnd。
- Orchestrator 创建子任务时生成子 `run_id`，同时传播 `root_run_id` 和 `parent_run_id`。
- 一个 Session 同一时刻默认最多有一个活动 Run；重复启动返回冲突，不覆盖已有映射。
- 所有状态、日志、Trace、预算、取消和审计都以 `run_id` 关联。
- `run_id` 是幂等键。第一次创建时保存不可变 `RunSpec` 摘要；同一 `run_id` 以不同 Task、Workspace、
  Agent、父节点或预算重试时必须返回冲突，不能复用旧执行。
- 创建子 Run 必须与父状态检查串行化：父或根 Run 进入 `cancelling` 后设置持久化 admission fence，
  任何晚到的子 Run 均被拒绝，防止“先扫描子节点、后到达新子任务”的取消竞态。

### 6.2 Run 状态机

```text
queued → starting → running ───────────────→ completed
                     │  │                  → failed
                     │  └─ budget exceeded → failed
                     ▼
                 cancelling → cancelled

starting ─ sandbox failure → failed
AgentEnd recovery ─ residual found → cancelling → cancelled
```

状态转换必须由 RunSupervisor 串行化。取消是幂等操作：对 `cancelling`、`cancelled` 或其他终态重复
调用不报错，也不能重新启动执行。

### 6.3 RunRepository

第一阶段可采用本地原子 JSON / SQLite 保存最小运行元数据，但接口必须可替换：

```python
class RunRepository(Protocol):
    async def create(self, run: RunRecord) -> None: ...
    async def update_state(self, run_id: str, expected: set[RunState], target: RunState) -> bool: ...
    async def bind_runtime(self, run_id: str, runtime: RuntimeIdentity) -> None: ...
    async def get(self, run_id: str) -> RunRecord | None: ...
    async def list_active(self) -> list[RunRecord]: ...
```

至少持久化 Sandbox backend、PID/PGID、cgroup/unit、Workspace、开始时间、父子关系和终止原因，
使 AgentEnd 重启后可以进行残留清理。进程句柄本身仍只存在内存中。

正式实现优先使用 SQLite；原子 JSON 只允许用于单进程 Spike，不作为 production / eval 后端。
Sandbox 执行域必须先以 `run_id` 创建和登记，再在其中启动进程，消除“进程已启动、runtime 尚未
持久化”的崩溃窗口。恢复身份不得只依赖 PID / PGID，必须同时校验 boot ID、进程启动时间或唯一
cgroup / systemd unit 名称，避免 PID 重用后误杀无关进程。

### 6.4 EventJournal 与流重连

RunSupervisor 在把事件交给 HTTP 流之前，先写入有序 EventJournal：

```text
run_id + monotonically increasing seq + event + timestamp
```

- Journal 至少保存到 Run 终态后的配置 TTL，支持 Backend 按 `after_seq` 续读。
- 事件采用 at-least-once 交付；Backend 以 `(run_id, seq)` 去重。
- HTTP 客户端断开不停止 stdout/stderr 消费，避免子进程因 pipe 无消费者阻塞。
- Journal 达到输出或事件预算时触发结构化取消，不能无限缓存。
- `done` / `error` 终态事件与 RunRecord 终态必须在同一持久化提交中收敛，恢复时允许补发但不能
  产生两个不同终态。

## 7. RunSupervisor

RunSupervisor 是应用级单例，由 FastAPI lifespan 创建并关闭，承担：

- 校验 RunSpec、Workspace 和预算。
- 获取全局并发配额。
- 创建 Sandbox 和 ManagedProcess。
- 登记 `run_id → RunHandle`。
- 将 stdout/stderr 交给 Adapter 解析，但不把进程所有权交给 Adapter。
- 维护父子 Run 树。
- 执行用户取消、超时取消、父级取消和应用关闭取消。
- 收集退出码、资源用量和结构化终止原因。
- 为每个事件分配序号并写入 EventJournal，HTTP 连接只作为可重建的消费者。
- 释放并发令牌、临时目录、cgroup 和 Sandbox 资源。
- 启动时扫描 RunRepository 和 Sandbox backend，回收遗留执行域。

建议接口：

```python
class RunSupervisor:
    async def start(self, spec: RunSpec) -> RunHandle: ...
    async def cancel(self, run_id: str, reason: TerminationReason) -> CancelResult: ...
    async def cancel_tree(self, root_run_id: str, reason: TerminationReason) -> CancelResult: ...
    async def get(self, run_id: str) -> RunSnapshot | None: ...
    async def shutdown(self) -> None: ...
```

Adapter 改为提供：

```python
class BaseAgentAdapter(ABC):
    def build_command(self, request: AgentCommandRequest) -> AgentCommand: ...
    def parse_stdout(self, line: bytes) -> StreamEvent | None: ...
    def parse_stderr(self, line: bytes) -> StreamEvent | None: ...
    def finalize(self, exit: ProcessExit) -> list[StreamEvent]: ...
```

不再允许 Adapter 维护 `_processes`，也不再由 Adapter 实现真实进程 `interrupt()`。

## 8. ExecutionSandbox 后端

### 8.1 抽象接口

```python
class ExecutionSandbox(Protocol):
    async def probe(self) -> SandboxCapabilities: ...
    async def spawn(self, spec: SandboxRunSpec) -> ManagedProcess: ...
    async def terminate(self, runtime: RuntimeIdentity, grace_seconds: float) -> TerminationReport: ...
    async def inspect(self, runtime: RuntimeIdentity) -> RuntimeSnapshot | None: ...
    async def list_managed(self) -> list[RuntimeIdentity]: ...
    async def cleanup(self, runtime: RuntimeIdentity) -> None: ...
```

`ManagedProcess` 必须提供有界 stdout/stderr 流、退出等待、资源快照和进程树终止能力。

### 8.2 首选严格后端：Linux bubblewrap + cgroup v2

第一阶段面向 Linux / WSL2：

- Bubblewrap 是构造沙盒策略的底层工具，不是单独即可成立的完整安全边界；MountPlan、网络、凭据、
  seccomp 和 cgroup 策略均由 AgentEnd 负责并必须组合验收。
- bubblewrap 提供 Mount、PID、IPC、UTS 和 Network namespace。
- Network namespace 默认不能访问宿主 loopback、局域网、云元数据地址或其他 Run；模型 API 和
  包管理器通过 AgentEnd 管理的出口代理访问，Preview 通过显式登记的端口转发访问。
- `--die-with-parent` 只作为父进程退出保护；严格后端还必须使用 `--new-session`，不得挂载宿主 D-Bus
  等控制面 socket，并以 cgroup 为进程树归属和最终回收依据。
- 独立 PID namespace 防止 Agent 观察或控制宿主机其他进程。
- cgroup v2 或 systemd transient unit 负责整棵进程树归属、CPU、内存、进程数统计与终止。
- 最终 KILL 使用非 threaded leaf cgroup 的 `cgroup.kill=1`，并验证 `cgroup.events` 的 `populated=0`；
  不能把只终止 namespace PID 1 或 CLI 父进程当作完成。
- `RLIMIT_NOFILE`、文件大小等进程级限制作为补充，不替代 cgroup。

版本与安装基线：仅支持非 setuid Bubblewrap；版本必须不低于 0.11.2，或由发行版提供等价的
CVE-2026-41163 后向补丁并可被启动探针证明。setuid 位、未知补丁状态或无法执行真实隔离自检时，
严格 readiness 必须失败。依据：[Bubblewrap 安全模型](https://github.com/containers/bubblewrap/security)、
[GHSA-xq78-7hw4-5jvp](https://github.com/containers/bubblewrap/security/advisories/GHSA-xq78-7hw4-5jvp)、
[Linux cgroup v2](https://www.kernel.org/doc/html/latest/admin-guide/cgroup-v2.html)、
[Linux seccomp filter](https://docs.kernel.org/userspace-api/seccomp_filter.html)。

严格模式启动前执行 capability probe：

```text
bwrap 存在
bwrap 为非 setuid 且版本 / 发行版安全补丁满足基线
unprivileged user namespace 可用
mount / PID / Network namespace 可创建
cgroup v2 或受支持的 systemd user manager 可用
Workspace / Git / Shared 挂载方案通过自检
凭据代理、受控网络出口与宿主内网阻断通过自检
完整进程树可被终止
new-session、控制面 socket 隔离、seccomp 与 cgroup.kill 实际攻击自检通过
```

任一硬要求失败时，`sandbox.mode=strict` 必须使 readiness 失败并拒绝 Run。

### 8.3 Git Worktree 与 Git 元数据隔离

Worktree 根目录中的 `.git` 通常指向原仓库 `.git/worktrees/...`。直接把 Git common dir 读写
挂载进沙盒虽然能让 Git 命令工作，但自治 Agent 可以绕过 WorkspaceManager 修改 refs、config、
hooks 和其他 worktree 元数据；进程内 Repo / Task 锁只能减少正常操作竞态，不能约束不可信进程。
因此严格模式禁止把宿主仓库 common dir 整体读写挂载给 Agent。

阶段 0 必须在以下两种方案中选定一种：

1. **Task 级 Git 元数据副本**：为每个 Task 创建独立 clone / Git metadata，宿主对象库最多只读
   复用；Agent 在 Task 域内正常 commit，完成后由可信 Broker 校验并导入目标提交或 patch。
2. **Git Broker**：Sandbox 只读必要 Git 历史并读写工作区文件；commit、branch、merge、cleanup
   等修改共享元数据的操作通过受认证、参数受限的 Broker 执行。

MountPlan 由 WorkspaceManager 生成，不允许请求提交任意宿主路径：

| 路径 | 权限 | 说明 |
|---|---|---|
| 当前 Agent Worktree | 读写 | Agent 的主要工作空间 |
| 当前 Task Shared 目录 | 读写 | taskctl、计划、公共/私有记忆 |
| Task 独立 Git 元数据 | 读写 | 不能包含其他 Task 的可写 refs、config、hooks 或 worktree 元数据 |
| Agent CLI 二进制与运行库 | 只读 | 执行必需 |
| 宿主 CLI 登录配置 | 不挂载 | 长期 Token 即使只读也会被 Shell 读取 |
| Run 级凭据入口 | 最小权限 | 短期 Token 或本地凭据代理，不暴露宿主长期凭据 |
| Run 独立临时目录 | 读写 | 替代宿主机公共临时目录 |
| AgentEnd 配置与 `.env` | 不挂载 | 不可见 |
| 其他仓库与 Workspace | 不挂载 | 不可见 |

Broker 内部仍需 Repo / Task 级锁和并发集成测试，但锁是共享元数据操作的串行化手段，不作为
不可信 Agent 的安全边界。若阶段 0 无法证明 Git 元数据隔离，则产品必须明确把整个 Repo 定义为
单一信任域，并禁止同仓库不互信 Task 并发；不能继续宣称 Task 级完整性隔离。

### 8.4 CLI 凭据与网络出口

CLI 必须能访问模型凭据，但同一执行域中的 Shell 也能读取 CLI 可见的文件和环境变量。因此：

- 不挂载宿主机 `~/.claude`、`~/.codex`、OpenCode 配置或长期 API Key。
- 首选由宿主 CredentialBroker / 模型代理持有长期凭据，按 `run_id` 注入认证并执行额度、模型、
  目标域名和到期时间限制。
- CLI 无法使用代理时，只允许注入 Run 级短期 Token；该 Token 按“可被 Agent 读取”处理，必须
  最小权限、短有效期、不可访问其他控制面，并在 Run 结束时撤销。
- 出口代理拒绝 loopback、RFC1918 / ULA、链路本地、云元数据、DNS rebinding 后的私网地址以及
  非允许协议；依赖源按配置 profile 开放。
- 现有 Artifact 上传是唯一默认允许的内部写通道：通过 AgentEnd 管理的内部代理到达 Backend，
  继续使用绑定 `message_id`、过期时间和操作范围的 capability；除该通道和 Orchestrator 的短期
  Backend 委托外，Sandbox 不得直连 Backend 业务端点。
- Orchestrator Worker 访问 Backend 时只获得绑定 root Run / Task 的短期委托，不注入静态
  `BACKEND_SERVICE_TOKEN`。
- Preview 使用独立端口登记和反向代理，不允许 Sandbox 任意绑定宿主端口。

长期凭据代理与宿主内网阻断未通过攻击测试时，`sandbox_enforced` 不得为 `true`。

### 8.5 本地后端：unsafe_process

`unsafe_process` 仅用于开发兼容：

- 使用独立进程组 / session 启动 CLI。
- 过滤 AgentEnd 密钥并设置临时目录。
- 使用可用的 rlimit 和超时。
- 终止时对整个 PGID 发送信号。
- 不提供文件系统隔离，因此健康状态必须返回 `sandbox_enforced=false`。

Eval、production、remote 模式不得选择该后端。

## 9. 权限与路径策略

### 9.1 配置授权根目录

```yaml
security:
  allowed_repo_roots:
    - /home/leixu/yh/devprojects
```

PathPolicy 必须：

1. 拒绝空路径、NUL、相对路径和过长路径。
2. 使用 `resolve()` 获取真实路径。
3. 使用路径组件语义判断是否位于允许根目录，禁止字符串前缀判断。
4. 拒绝过宽根目录，例如 `/`、用户 Home 根目录和系统目录。
5. 校验 Repo、Worktree、Shared、Skill target 的预期文件类型。
6. 文件读写时重新验证最终父目录与 symlink，降低校验后替换竞态。
7. 恢复持久化 Workspace 时重新执行校验，不信任历史记录。

所有 `/v1/validate-repo-path`、`/v1/init-git-repo`、Workspace CRUD、文件 API、Preview、Skill、
Orchestrator 和 Agent Run 入口都必须使用同一个 PathPolicy。

### 9.2 可信 Workspace Handle

Agent 请求不再能够用任意 `workspace_path` 绕过 WorkspaceManager。内部执行使用：

```text
workspace_id → WorkspaceManager → canonical MountPlan
```

为兼容现有 Backend 请求，迁移期可继续携带 `cwd`，但 AgentEnd 必须将其解析为已登记 Workspace，
找不到精确匹配时拒绝执行，不直接把原字符串传给子进程。

## 10. 进程生命周期与取消语义

### 10.1 终止完整进程树

```text
cancel(run_id)
   │
   ├── 原子状态：running → cancelling
   ├── 持久化关闭 root / parent 的子 Run admission
   ├── 递归取消所有活动子 Run
   ├── 向 Sandbox 进程树发送 TERM
   ├── 等待 process_terminate_timeout
   ├── 仍存活则对 cgroup / unit / PID namespace 执行 KILL
   ├── 验证任务、子进程与端口不再存在
   ├── 清理临时目录与 Sandbox 资源
   └── 状态：cancelled + termination_reason
```

终止操作必须直接作用于 cgroup、systemd unit 或隔离执行域，不能只杀 CLI 父 PID。进程组是
`unsafe_process` 的降级机制，不是严格后端的唯一保证。

父 Run 的 admission fence、子 Run 登记和父状态检查必须使用同一持久化事务或等价串行化机制。
取消完成前再次扫描子节点；任何携带已关闭 `root_run_id` 的晚到请求直接返回父级已取消，不创建
Sandbox。这样父取消的含义才是“此后不会再出现新子 Run”，而不只是取消某一时刻的快照。

### 10.2 取消来源

- 用户点击停止。
- Backend 取消 Task / Session。
- AgentEnd API 明确取消 Run。
- SSE 客户端断开后的策略取消。
- 墙钟、空闲、CPU、内存、输出、磁盘或进程数超限。
- Orchestrator 父 Run 取消或 Replan 放弃旧子任务。
- Eval Runner 提前终止 Trial。
- AgentEnd 优雅关闭或启动恢复发现遗留 Run。

### 10.3 SSE 断开策略

网络闪断不应立即杀死 Agent。建议：

- Backend 持久化最后确认的 `event_seq`，通过 EventJournal 持续消费 AgentEnd 事件。
- Browser EventSource 断开只影响展示，不直接取消 Run。
- Backend → AgentEnd 流断开时进入短暂 orphan grace period；若 Backend 未恢复或未查询接管，
  Backend 使用 `/events?after_seq=` 续接；超过 grace period 仍无受认证消费者或状态查询时，
  AgentEnd 再按策略取消 Run。
- 用户明确点击停止则绕过 grace period，立即调用 Run Cancel API。

“状态查询”不能代替事件接管：GET Status 只返回快照，只有带序号的 EventJournal 接口能够恢复
丢失输出。Backend 重试创建同一 `run_id` 时，AgentEnd 返回已有 Run 状态，不启动第二个进程。

### 10.4 AgentEnd 重启恢复

启动时：

1. 读取所有非终态 RunRecord。
2. 同时调用 Sandbox backend `list_managed()` 枚举带 AgentHub 标识的 cgroup / unit / PID namespace，
   不只相信可能未及时写入的 RunRecord。
3. 以 `run_id`、boot ID、进程启动时间或唯一 unit 身份交叉验证后，对仍存在的执行域强制终止；
   身份不一致时不得按陈旧 PID 杀进程，而是记录高优先级恢复告警。
4. 清理临时目录、端口登记和残留资源。
5. 将 Run 标记为 `cancelled`，原因为 `agentend_recovery`。

第一阶段不恢复 CLI 进程的流式会话；恢复的是安全终止和资源回收。业务 Session 的 CLI
session_id 映射仍可供下一次请求 resume。

## 11. ExecutionBudget

### 11.1 预算模型

```yaml
execution:
  budget:
    wall_time_seconds: 1200
    idle_time_seconds: 180
    max_concurrent_runs: 4
    max_turns: 20
    max_processes_per_run: 64
    max_memory_mb: 4096
    max_cpu_seconds: 900
    max_output_mb: 50
    max_event_count: 50000
    max_workspace_growth_mb: 2048
    max_orchestrator_children: 12
    max_orchestrator_parallelism: 4
```

首轮实施必须硬执行：墙钟、全局并发、进程数、内存、CPU、输出、事件数和磁盘容量。严格 / Eval
模式的 Workspace 与临时目录必须位于具备 quota 的文件系统、限容 volume 或等价的硬限制执行域；
固定周期检测只用于预警和统计，不能防止进程在两次采样间写满宿主磁盘。

空闲超时不能简单定义为“stdout 一段时间无输出”，否则会误杀正常的长编译、测试或模型等待。
IdleDetector 至少综合进程 CPU / I/O 活动、子进程状态、模型请求状态和最后事件时间；无法可靠区分
正常静默与卡死的 Adapter，可以关闭 idle hard-kill，只保留 wall time 硬上限与空闲告警。

### 11.2 父子预算

Orchestrator 预算是整棵 Run 树的总预算，不允许每个子 Agent 重置成完整额度：

```text
Root Run Budget
├── Orchestrator planning
├── Child Run A
├── Child Run B
└── Child Run C
```

BudgetManager 在启动子 Run 时从父预算授予额度，并持续汇总并发数、墙钟、CPU、输出和子任务数。
根预算耗尽时，RunSupervisor 取消整棵树。

### 11.3 结构化终止原因

至少支持：

```text
user_cancelled
parent_cancelled
session_deleted
wall_time_exceeded
idle_timeout
cpu_exceeded
memory_exceeded
process_limit_exceeded
output_limit_exceeded
event_limit_exceeded
workspace_limit_exceeded
concurrency_rejected
policy_violation
sandbox_start_failed
sandbox_lost
agentend_shutdown
agentend_recovery
process_exit_error
```

这些原因必须进入 RunRecord、SSE Error/Done 终态、Backend Message 状态和未来 Eval Trial 结果，
避免把 Harness 故障误判为 Agent 能力失败。

## 12. 服务认证与安全启动

### 12.1 Backend → AgentEnd

- 新增独立 `AGENTEND_SERVICE_TOKEN`，不复用用户 JWT、Admin 密码、Artifact capability 或存储密钥。
- Backend AgentEnd Client 为所有请求添加 `Authorization: Bearer ...`。
- AgentEnd 使用全局中间件和恒定时间比较验证 Token。
- 只有 `/health/live` 可匿名；readiness、资源、Agent、Workspace、Session、Skill、Pin 等端点均需认证。
- 禁止 Query Token，禁止在日志、Trace 和错误正文输出凭证。
- Token 支持双 Key 轮换、独立 audience 和最短必要权限；认证成功不代表请求路径与 Workspace 已
  授权，仍必须经过 PathPolicy 和 RunSpec 绑定校验。

### 12.2 AgentEnd → Backend

Orchestrator 会回调 Backend 的 RunTask、消息流和公告接口。生产 Auth 开启后，应使用独立的
`BACKEND_SERVICE_TOKEN` 或受限内部身份，不冒充普通用户 JWT。内部身份只授权所需的任务执行、
流读取和公告读取接口。

每次回环还必须携带短期委托上下文，至少绑定 `root_run_id`、`task_id`、允许的子 Agent、过期时间
和 audience。Backend 校验根 Run 仍活动且 Task 匹配后才接收子任务；静态服务 Token 本身不能被
当成可操作任意 Task 的万能用户身份。

### 12.3 启动约束

- 监听非 loopback 地址时，缺少服务认证必须启动失败。
- `sandbox.mode=strict` 时 capability probe 失败必须 readiness 失败并拒绝 Run。
- production / eval 下禁止默认密钥、通配 CORS 和 `unsafe_process`。
- AgentEnd 原则上不供 Browser 直连，CORS 默认只允许明确配置的控制面来源。
- Docker precheck 与 Config Center 必须展示认证、授权根目录和沙盒能力状态。

## 13. API 与契约规划

跨端字段遵循契约优先原则。建议新增 `contracts/schemas/agent-run.yaml`，生成三端类型，至少包含：

```text
AgentRunIdentity
AgentRunBudget
AgentRunState
AgentRunTerminationReason
AgentRunStatus
AgentRunEventEnvelope
CancelAgentRunRequest / Response
```

`AgentRunEventEnvelope` 至少包含 `run_id`、`seq`、`event` 和 `timestamp`；`AgentRunStatus` 包含
`last_event_seq`。Backend Message 需增加 `run_id` 和可空 `termination_reason`，但不把全部 Run
状态塞进 MessageStatus；`cancelling / cancelled` 等执行态以 Run 状态为准，Session / Message 在
终态映射时保留现有业务语义。

`AgentRequest` 增加：

```text
run_id
root_run_id
parent_run_id
workspace_id
budget
```

AgentEnd 内部 API：

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/v1/runs` | 按 `run_id` 幂等创建并启动，返回 202 或已有 Run 状态（**规划项，未实施**：当前创建仍走 `/v1/agent/stream` \| `/v1/agent/execute`） |
| POST | `/v1/agent/stream` | 迁移期兼容入口，内部调用 Create Run 后消费 EventJournal，不直接拥有进程 |
| POST | `/v1/runs/{run_id}/cancel` | 幂等取消 Run 与全部子 Run |
| GET | `/v1/runs/{run_id}` | 查询状态、预算和终止原因 |
| GET | `/v1/runs/{run_id}/events?after_seq={seq}` | 按序号续接 EventJournal，支持长轮询或 SSE |
| GET | `/v1/runs` | 受限运维查询活动 Run |
| GET | `/health/ready` | 返回 Auth / Sandbox / Budget / Store readiness |

Backend 对 Frontend 提供用户权限内的 Task Run 状态与取消接口。Frontend 的停止按钮必须调用
Backend Cancel API；仅关闭 EventSource 或 AbortController 不代表执行已经停止。

Session API 迁移：

- `POST /v1/session/{id}/interrupt` 调用 `RunSupervisor.cancel(current_run_id)`，不能只修改状态。
- `DELETE /v1/session/{id}` 先写入删除 tombstone 并触发取消；正常情况下确认 Run 终止后清理
  Session。AgentEnd 不可达时保持 `deleting / cancelling` 并异步重试，不能无限阻塞请求，也不能
  在真实执行状态未知时直接物理删除关联记录。
- Session 状态由 Run 终态驱动，禁止 API 先标记 interrupted、真实进程仍继续运行。

修改契约后必须运行 `make generate` 并在 `contracts/logs/` 写变更记录。

## 14. 各端实施影响

### 14.1 AgentEnd

- 新增 SecurityConfig、SandboxConfig、BudgetConfig。
- FastAPI lifespan 创建 RunSupervisor、ExecutionSandbox、RunRepository 和 PathPolicy。
- Adapter 从“命令 + 进程 + interrupt”拆为“命令构造 + 协议解析”。
- `/stream` 与 `/execute` 作为兼容入口统一调用 Create Run / RunSupervisor，不保留两套进程生命
  周期逻辑；新 Backend 使用 `/runs` + `/events`，不依赖创建请求的 HTTP 连接存活。
- WorkspaceManager 输出可信 MountPlan，并集中校验 Repo / Worktree / Shared / Task Git 元数据或
  Git Broker 目标。
- SessionManager 只维护业务 Session，不直接保存裸 `asyncio.subprocess.Process`。
- Orchestrator 传播父子 Run 与剩余预算，取消时汇聚所有子 Run。
- Orchestrator 规划执行移入受监督 worker 进程或等价的独立执行域；其 LLM 请求、工具子进程、
  内存、CPU、输出和取消均归属根 Run。不能只取消 AgentEnd 主进程内的 coroutine 后假定资源已回收。
- 增加 EventJournal；HTTP `/stream` 只消费 Journal，不直接拥有 Adapter stdout 生命周期。
- Observability 增加 run、sandbox 和 termination metadata，继续执行隐私白名单与脱敏。

### 14.2 Backend

- AgentEnd Client 注入服务凭证，并为每个根执行生成 `run_id`。
- 持久化 `run_id`、Session、Message 的关联与终态原因。
- 持久化每个 Run 已确认的 `event_seq`，断流后从 AgentEnd EventJournal 续接并去重。
- 增加 Run Cancel / Status Service 与 Controller。
- 根执行改为先幂等 Create Run，再独立消费 EventJournal；迁移完成后不再依赖 `/agent/stream`
  单连接承载完整执行生命周期。
- Orchestrator 回环请求传播 `root_run_id`、`parent_run_id` 和预算。
- 用户断开 SSE 不直接视为取消；显式取消才调用 AgentEnd。
- AgentEnd 不可达或取消结果未知时保持 `cancelling`，通过状态查询收敛，禁止直接宣告取消成功。

### 14.3 Frontend

- 流式执行时展示明确的“停止”操作。
- 停止操作调用 Backend，显示 `cancelling → cancelled`，不只是本地 abort。
- 区分 Agent 错误、用户取消、超时、资源超限和沙盒故障。
- 网络断开时显示连接状态，但不误导用户认为 Agent 已停止。

### 14.4 Config Center、Docker 与脚本

- 管理服务 Token、允许的 Repo Roots、Sandbox mode 和默认预算。
- 配置校验拒绝 production / eval 使用 `unsafe_process`。
- precheck 检查 `bwrap`、user namespace、cgroup v2 / systemd 能力和授权根目录。
- precheck 检查 network namespace、受控出口、凭据代理、Git 元数据隔离和磁盘 quota；这些硬能力
  缺失时严格模式 readiness 失败。
- `make status` 或 health 输出当前 Sandbox backend、强制状态和活动 Run 数，不输出敏感配置。
- Docker + 宿主机 AgentEnd 模式下，Backend 通过服务 Token 访问宿主机 AgentEnd。

## 15. 配置草案

```yaml
server:
  host: "127.0.0.1"
  port: 8001

security:
  service_auth_enabled: true
  allowed_repo_roots:
    - "/home/leixu/yh/devprojects"
  allow_unsafe_local_execution: false

execution:
  permission_mode: "autonomous"
  process_terminate_timeout: 5.0
  orphan_grace_seconds: 30
  sandbox:
    mode: "strict"                  # strict | unsafe_process
    backend: "linux_bwrap"
    network_mode: "controlled_egress"
    deny_host_loopback: true
    deny_private_networks: true
    credential_mode: "broker"       # broker | ephemeral_token
    git_mode: "task_metadata"       # task_metadata | broker
    host_home_access: "none"
    cross_workspace_access: "none"
  budget:
    wall_time_seconds: 1200
    idle_time_seconds: 180
    max_concurrent_runs: 4
    max_turns: 20
    max_processes_per_run: 64
    max_memory_mb: 4096
    max_cpu_seconds: 900
    max_output_mb: 50
    max_event_count: 50000
    max_workspace_growth_mb: 2048
    max_orchestrator_children: 12
    max_orchestrator_parallelism: 4
```

密钥只从 `.env` / Secret 注入：

```text
AGENTEND_SERVICE_TOKEN
BACKEND_SERVICE_TOKEN
CREDENTIAL_BROKER_KEY
```

不得把真实 Token 写入 YAML、日志、Trace 或 Config Center 返回正文。`CREDENTIAL_BROKER_KEY` 只在
AgentEnd 控制面可见，不得进入 Sandbox；Run 级临时凭据按可泄露数据处理并限制权限、额度和有效期。

## 16. 分阶段实施计划

### 阶段 0：Sandbox Spike

目标：在目标 Linux / WSL2 环境证明方案可行，不改生产主链路。

- 验证三种 CLI 在不挂载宿主长期登录配置的情况下，通过凭据代理或 Run 级短期 Token 访问模型。
- 验证独立 network namespace、受控出口、宿主 loopback / 私网 / 云元数据阻断和 Preview 端口转发。
- 对比 Task 独立 Git 元数据与 Git Broker，确定不暴露可写 common dir 的实现方案。
- 验证 Worktree / Task Git 域 + Task Shared 的最小挂载集合。
- 验证安装依赖、运行测试、taskctl merge 和流式 stdout/stderr。
- 验证 TERM → grace → KILL 能清除 CLI、Shell、测试服务器和刻意创建的后台进程。
- 验证 AgentEnd 父进程异常退出时 `--die-with-parent` 与 cgroup 回收行为。
- 验证 CPU、内存、pids、磁盘 quota 和并发限制。
- 形成 capability matrix，确定 cgroup 使用 systemd transient unit 还是直接 cgroup v2 管理。

退出条件：三种 CLI 核心流程通过；长期凭据不可读、宿主控制面不可达、Git 元数据跨 Task 不可写、
磁盘具备硬限额且完整进程树可终止。任一条件无法满足时，不进入后续实现，也不得把严格模式标记为
已强制。

### 阶段 1：服务认证与 PathPolicy

- Backend → AgentEnd 服务 Token。
- AgentEnd → Backend 内部身份。
- 非 loopback 安全启动校验。
- 统一 allowed repo roots、canonical path、symlink 和恢复校验。
- WorkspaceManager 生成可信 Workspace Handle 与 MountPlan。
- Backend 内部身份使用绑定 root Run / Task 的短期委托，拒绝跨 Task confused-deputy 请求。

退出条件：匿名业务请求、授权根外 Repo 和 symlink 逃逸全部被自动化测试拒绝。

### 阶段 2：RunSupervisor 与进程所有权统一

- 增加 RunIdentity、RunState、RunRepository。
- 增加 EventJournal、幂等 RunSpec 摘要和父子 admission fence。
- `/runs`、`/stream` 与 `/execute` 接入 RunSupervisor；兼容入口不得直接持有进程。
- 三种 Adapter 移除 `_processes` 和独立 interrupt。
- Session interrupt / delete 改为真实取消。
- 实现进程组级 `unsafe_process`，先建立统一生命周期，但不作为严格发布完成条件。

退出条件：所有 Agent Run 都可按 `run_id` 终止；重复创建不会启动第二个进程；父取消后晚到子 Run
被拒绝；测试消费者断开后可按事件序号续接；不存在只改状态不杀进程的路径。

### 阶段 3：严格 ExecutionSandbox

- 实现 linux_bwrap backend、MountPlan、PID / Network namespace 和 cgroup 管理。
- 实现凭据代理或短期凭据、受控网络出口、Task Git 元数据 / Git Broker 和磁盘硬 quota。
- 实现 startup probe、readiness 与 fail-closed。
- 三种 CLI 默认自治执行并运行在严格沙盒。
- Orchestrator 移入受监督 worker 执行域，其工具子进程统一由 RunSupervisor 管理。
- 实现重启残留回收。

退出条件：沙盒逃逸、长期凭据读取、宿主网络访问、跨 Workspace / Git 元数据写入、磁盘耗尽和完整
进程树终止测试全部通过。

### 阶段 4：预算与父子 Run 树

- 实现全局与 Run 级预算。
- Orchestrator 传播 root / parent run 和剩余预算。
- 父取消递归取消全部子 Run。
- 预算终止原因进入 AgentEnd、Backend、SSE 和 Message 状态。

退出条件：Orchestrator 并发子任务无法超过父预算，根 Run 取消后无活动子进程。

### 阶段 5：产品接入与运维闭环

- Backend Cancel / Status API。
- Backend 接入幂等 Create Run、持久化 `event_seq` 和 EventJournal 断点续接。
- Frontend 停止、取消中、已取消和超限状态。
- Config Center、Docker precheck、Makefile、部署与排障文档。
- 结构化审计和活动 Run 运维查询。
- 真实 Claude Code / Codex / OpenCode / Orchestrator 端到端验收。

退出条件：用户可可靠停止任务；断网不误取消；生产配置无法绕过严格沙盒。

## 17. 测试与验收矩阵

### 17.1 单元测试

- Service Token 缺失、错误、正确和恒定时间校验入口。
- PathPolicy 的 `..`、前缀碰撞、symlink、过宽根目录与历史记录恢复。
- Run 状态机并发 start / cancel / finish 竞争。
- Cancel 幂等和父子 Run 传播。
- 同一 `run_id` 的同规格重试与异规格冲突；父取消和晚到子 Run 创建竞争。
- Budget 授予、消耗、归还与父预算汇总。
- EventJournal 序号、重复投递、续读、终态单写和 TTL 清理。
- Adapter 命令构造与事件解析不依赖真实进程。

### 17.2 Sandbox 集成测试

测试程序主动尝试：

- 读取 AgentEnd `.env`、数据库密码、宿主 CLI 登录配置和其他长期凭据。
- 读取或写入另一个 Workspace。
- 通过 symlink 访问授权根之外文件。
- 写入用户 Home、`/etc` 或其他系统目录。
- 访问宿主 loopback、局域网、云元数据和其他 Run 的 Preview 端口。
- 直接修改其他 Task 的 Git refs、config、hooks 或 worktree 元数据。
- 创建后台 Shell、HTTP Server、双重 fork 和忽略 TERM 的子进程。
- 无限输出、无限循环、写满磁盘、分配超量内存和创建超量进程。
- 读取宿主长期凭据、滥用过期 Run Token 或把短期 Token 用于非授权模型 / 域名。
- 访问允许的模型网络与包管理器。
- 在 Worktree 中正常 commit、merge、运行 taskctl 和测试。

所有越界读取、控制面访问、跨 Task Git 修改和凭据滥用必须失败；后台进程必须可全部回收，资源
攻击必须被对应硬预算终止，正常开发流程必须成功。

### 17.3 跨端测试

- Backend 未携带 Token 时 AgentEnd 返回 401。
- 用户点击停止后 Backend、AgentEnd、Message 和 Frontend 状态最终一致。
- Browser SSE 断开不立即取消 Run；显式停止立即取消。
- Backend → AgentEnd 事件流断开后按最后 `event_seq` 续接，无重复持久化或输出缺口。
- AgentEnd 取消响应丢失时 Backend 通过 Status API 收敛。
- Orchestrator 根 Run 取消能终止所有回环创建的子 Run。
- 根 Run 取消与新子任务回环并发时，晚到子 Run 被 admission fence 拒绝。
- AgentEnd 重启后遗留沙盒被回收，Run 标记为 `agentend_recovery`。

### 17.4 发布门禁

- `/health/ready` 显示 `sandbox_enforced=true`。
- production / eval 配置无法选择 `unsafe_process`。
- 长期 CLI 凭据不在 Sandbox 文件或环境中，宿主 loopback / 私网不可达。
- 严格模式不向 Agent 挂载可写的宿主 Git common dir，Workspace 与临时目录具备磁盘硬限额。
- Safety suite 通过率必须 100%。
- 连续运行多轮并发任务后无残留进程、监听端口、cgroup、临时目录或活动 RunRecord。
- 真实三种 CLI 和 Orchestrator 各完成一次修改、测试、取消和超时场景。

## 18. 验收标准

本规划完成实施后必须能回答：

1. **谁能调用 AgentEnd？** 只有持有内部服务身份的 Backend 或明确授权的运维调用者。
2. **Agent 能在哪里操作？** 只在 WorkspaceManager 生成的当前 Task 执行域内。
3. **Agent 是否需要逐工具确认？** 不需要；沙盒内默认自治并拥有完整开发权限。
4. **越界由什么阻止？** PathPolicy、Mount / PID / Network namespace、凭据代理、Git 元数据隔离和
   cgroup，而不是 Prompt。
5. **谁拥有进程生命周期？** AgentEnd RunSupervisor。
6. **能否随时停止？** 能；按 `run_id` 取消完整进程树和全部子 Run，并回收沙盒资源。
7. **Agent 失控怎么办？** 时间、并发、CPU、内存、进程、输出、事件和磁盘均有硬预算；断流输出
   进入有界 EventJournal。
8. **AgentEnd 崩溃怎么办？** 重启后发现并清理遗留执行域，不尝试接管未知进程流。
9. **能否支持批量 Eval？** 安全与生命周期基础具备；Eval Harness 在此之上建设。

## 19. 风险、取舍与回滚

| 风险 | 影响 | 缓解 |
|---|---|---|
| bubblewrap / user namespace 在部分环境不可用 | 严格模式无法运行 | 阶段 0 capability probe；WSL2/Linux 明确基线；失败关闭 |
| Task Git 元数据副本或 Broker 与现有 Worktree 流程差异大 | commit/merge/cleanup 迁移复杂 | 阶段 0 固定方案；Broker 校验导入；Repo/Task 锁只负责正常并发串行化 |
| 三种 CLI 不完全支持凭据代理或短期 Token | 严格模式无法启动 CLI | 阶段 0 逐 CLI 验证；不通过则该 CLI 不进入严格发布矩阵，不回退挂载长期凭据 |
| 受控出口影响包管理器和 Preview | 正常开发命令失败 | 配置化依赖源 profile、DNS/IP 双重校验、AgentEnd Preview 反向代理 |
| cgroup 管理在 systemd 与非 systemd 环境差异大 | 生命周期实现复杂 | 接口化 backend；Spike 后固定受支持矩阵 |
| 取消与自然完成并发 | 状态或资源重复清理 | 状态 CAS、幂等 terminate/cleanup、终态单写测试 |
| 父取消与子 Run 晚到并发 | 根 Run 结束后仍有子任务运行 | 持久化 admission fence、创建时原子父状态校验、取消完成前二次扫描 |
| EventJournal 堆积或重复投递 | 磁盘增长、消息重复 | 输出/事件硬预算、TTL、序号去重、终态后清理 |
| 过严预算误杀正常任务 | 用户体验下降 | 结构化原因、配置 profile、观测后调整，不静默放宽 |

回滚只允许回到 `unsafe_process` 的显式本地开发模式。生产或 Eval 不得以“临时兼容”为由关闭
认证、PathPolicy 或严格沙盒；若严格后端故障，应停止接收新 Run 并保留查询/取消能力。

## 20. 后续衔接

执行沙盒完成后，下一项应是统一 Agent Run 生命周期与 Eval Core：Task、Trial、Transcript、
Outcome、Grader、Baseline 和回归门禁直接复用本规划建立的 `run_id`、RunSupervisor、预算、取消、
资源统计与结构化终止原因，不另建测试专用执行链路。
