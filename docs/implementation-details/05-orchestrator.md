# Orchestrator 超详细实现

## 实现了什么

Orchestrator 是 AgentEnd 内的多 Agent 控制器。它使用 LangGraph 把“理解请求、发现成员、生成计划、人工审查、按依赖并行执行、集成 Git 结果、处理冲突、汇总、保存记忆”建模为可路由状态图，而不是一个无限循环的 prompt。

## 怎么实现的

### 模块边界

| 目录 | 责任 |
|---|---|
| `planning/` | GraphState、reason、tool、review、路由、Skill 渐进加载 |
| `execution/` | Dispatcher、ExecutionEngine、波次、协调通道、runtime state |
| `memory/` | pin、conversation summary、context compaction、evolution |
| `prompts/` | 群聊和 reasoning prompt 构造 |
| `reporting/` | TaskResult 汇总与 final summary block |
| `integration/` | 结构化 Git 操作、operation repository、冲突恢复 |

`adapters/orchestrator.py` 是统一 Adapter 边界：接收 AgentRequest，配置可观测性，调用 graph，将 graph 更新转换成 StreamEvent。

### 领域模型

`TaskDef` 表示计划中的一个子任务：稳定 task id、描述、目标 agent id、依赖和可能的执行约束。`PlanOutput` 包含任务列表、说明以及是否需要审查。`TaskResult` 保存 execution status、输出、错误、Agent 身份和 integration 结果。

执行状态包括 pending、running、completed、failed、timeout、cancelled、blocked。集成状态进一步区分没有操作、已集成、冲突、失败等。`DispatchResult` 把 TaskDef 与实际 Session/Workspace/Adapter 绑定。

### GraphState

GraphState 是整个运行的显式状态，主要包含：

- 用户消息、turn messages、conversation summary。
- 群聊成员和可分派 agent ids。
- Skill L1 列表、选择的 L2 内容、工具集合。
- reason iteration 与 replan iteration 计数。
- plan、review decision/content。
- dispatch results、task results、integration/conflict 状态。
- active pin snapshot、系统约束和 token 预算。
- 最终输出、错误和下一路由信号。

Reducer 对列表追加、turn message 合并和计数累加有显式定义，避免 LangGraph 节点并行更新时覆盖彼此。

### 主图节点

1. `skill_prepare`：发现 Skill 元数据，按请求选择 L1/L2，不一次性注入全部资源。
2. `compact_context`：估算 prompt、tools、summary 和输出 reserve；超过 trigger 时压缩旧 turn。
3. `reason`：调用 LLM；处理 agent discovery、ask_agent、plan 工具和直接回复。
4. `human_review`：把 plan 写成 review event，并等待外部决定。
5. `dispatch`：校验 agent id，绑定实际 Agent/Session/Workspace。
6. `execute`：按依赖图执行子任务并收集 TaskResult。
7. `review`：检查失败、冲突和是否需要 replan。
8. `evolve`：更新长期演化信息。
9. `save_mem`：以 revision guard 写 conversation summary。
10. `final_aggregate`：生成最终文本和 `final_summary` block。
11. `awaiting_user`：需要用户补充时形成明确暂停终态。

路由函数分别处理 reason 输出类型、compaction 后去向、review decision 和执行 review。iteration 上限是硬保护：reason 或 replan 超过配置次数必须失败/收敛，不能无限循环。

### Agent 发现与工具

Orchestrator 不信任模型凭空发明成员。reason 必须通过工具读取当前可分派 Agent，并用 `_plan_agent_id_error` 校验每个 TaskDef 的 agent id。只有发现成功或存在可证明的默认成员时才能生成可执行计划。

`planning/tools.py` 构造受限工具：

- 读取允许目录内文件和 Skill 资源。
- 获取当前时间。
- ask_agent：向群聊内目标 Agent 发起受监督调用。
- 计划提交工具。
- taskctl/render 等 builtin skill 可执行入口。

路径由 `_resolve_tool_path` 规范化，并用 allowed dirs 判断；Skill binary 必须在发现的 Skill 目录内。`filter_allowed_tools` 再按规则产物缩小工具集合。

### 计划审查

graph 为每个等待审查的 `session_id` 保存 pending handle。`wait_for_external_review` 在 `review_timeout` 内等待；Backend 的 `/tasks/:taskId/review` 转发到 AgentEnd `/v1/agent/review`，最终调用 `submit_plan_review`。

- approve：进入 dispatch。
- discuss：带用户内容回 reason。
- modify：把修改要求加入上下文并重做计划。

超时必须产生可观察结果，pending handle 在完成或取消后清理，防止后续请求误投旧审查。

### 拓扑分派与波次

Dispatcher 把计划任务解析成依赖 DAG。`topological_sort` 生成二维波次：同一波内没有未满足依赖，可并发执行；下一波等待前一波相关结果。循环依赖或引用不存在 task id 在执行前失败。

ExecutionEngine 对每个 DispatchResult：

1. 建立子 Run，parent/root 关联主 Orchestrator Run。
2. 获取目标 Adapter、Session Worktree 和上下文。
3. 发送 runtime status 与 coordination 事件。
4. 迭代子 Agent StreamEvent，持久化其 message ownership。
5. 应用 timeout 与 retry policy，最多使用配置的 execution retry attempts。
6. 将文本、错误、usage 和 integration result 合成 TaskResult。

父 Run 关闭后 repository 拒绝创建新子 Run，保证取消和终态不会被迟到任务重新打开。

### CoordinationChannel

协调通道把子 Agent 的开始、进度、消息、完成和失败变成统一事件。事件携带 root run、child run、source/target agent、task identity；Frontend 使用 CoordChannel/RuntimeStatus 呈现。协调事件也是消息归属事实，不能只保留一段无身份的文本。

### taskctl 与集成结果

builtin `taskctl` 是 Go CLI。子 Agent 用它记录结构化 Git 事实，不让 Orchestrator 从自然语言猜测 commit：

- task/session/root run 绑定。
- source/target branch、base/head commit。
- operation/idempotency/capability 信息。
- changed files、冲突文件和完成状态。

IntegrationResult V1/V2 兼容由 feature gate 控制写出；读取端支持契约指定版本。IntegrationService 校验 capability 和绑定后，创建 durable operation。`integration_service_execute_enabled=false` 时只记录/返回计划，不主动执行 Phase 2 操作。

### 冲突恢复

ConflictRecoveryCoordinator 基于 operation repository 和 WorkspaceManager：

- 为冲突生成 ConflictRecord，保存文件清单、attempt、状态和根 Run。
- 文本冲突可按开关交给 resolver Agent；二进制默认不自动解决。
- resolver 有独立 max attempts 和 timeout。
- 人工动作要求 action、task/session/root run/conflict 和 `expected_attempt` 全部匹配。
- action id 与数据库唯一性提供幂等；状态投影只允许一个终态。
- retry 会递增 attempt；accept ours/theirs、cancel 进入对应终态。

Backend 只代理冲突投影和动作，权威操作状态保存在 AgentEnd integration repository。

### Conversation Memory

`ConversationMemoryStore` 使用带 schema/revision 的 JSON envelope：

- 线程内 RLock + 文件原子替换。
- compare-and-swap revision，冲突抛 `RevisionConflict`。
- corruption policy 为 `fail` 或 `empty`；默认 fail，避免悄悄丢上下文。
- summary 记录内容和累计压缩消息数。

ContextCompactor 以近似 token 估算：system prompt + tools + messages + reserve。达到 trigger 后保留最近完整 turn，将较旧的完整 turn 批量摘要到 target 以下。不能拆散 tool call 与对应 tool result；若固定内容本身超预算则抛 ContextBudgetError。

默认预算：window 65536、trigger 46000、target 36000、recent turns 4、output reserve 8192、summary max 4096、active pin max 8192。配置 validator 保证 target < trigger < window，并为摘要输入和输出保留空间。

### Pin 与群聊上下文

PinMemory 把用户明确置顶的信息保存到共享目录。规划前构建 active pin snapshot，要求稳定字段并校验 token 上限。公告取消置顶也写入 pin 历史，避免刷新后恢复为旧状态。

Group chat prompt 包含成员列表、主 Session、有限消息窗口、公告和 active pins。BackendClient 获取的是受限窗口而不是全量 MySQL 历史。跨 Agent ask 的问题与回答通过 ask-card 事件返回并持久化身份。

### 汇总

Aggregator 对 TaskResult 排序，构造成功、失败、超时和阻塞概览。`build_final_summary_block` 生成前端可解析的 `aka_yhy` final_summary block；长子输出在进入总结前截断到固定范围，完整大资源通过 Artifact 交付。
