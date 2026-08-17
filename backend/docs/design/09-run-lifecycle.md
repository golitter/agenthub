# Run Lifecycle — Agent Run 生命周期控制面

## 实现了什么

为每次 Agent 执行建立可追溯、可取消的 **run 生命周期**：`RunTask` 为每轮执行分配 `run_id` 并落库到 Message（`RunKey` 唯一索引实现创建幂等，`RunRequestHash` 实现同 run_id 请求一致性校验）；内部 run 入口可透传执行沙盒身份（`root_run_id` / `parent_run_id`）与资源预算（`budget`）给 AgentEnd；前端可通过 `GET .../run` 查询、`POST .../run/cancel` 取消一个 run；AgentEnd 上报的 `termination_reason` 由 StreamWriter 持久化到 Message，并影响流结束后的 Session 收尾状态。执行沙盒本体在 AgentEnd 侧，Backend 只承担控制面（幂等、鉴权、代理、状态收敛）。

## 怎么实现的

### 契约类型 (`internal/generated/agent_run.go`)

由 `contracts/schemas/` 生成，定义 Run 状态机、终止原因与预算：

```go
type AgentRunState string
// queued / starting / running / cancelling / completed / failed / cancelled

type AgentRunTerminationReason string
// user_cancelled / parent_cancelled / session_deleted / wall_time_exceeded /
// idle_timeout / cpu_exceeded / memory_exceeded / process_limit_exceeded /
// output_limit_exceeded / event_limit_exceeded / workspace_limit_exceeded /
// concurrency_rejected / policy_violation / sandbox_start_failed / sandbox_lost /
// agentend_shutdown / agentend_recovery / process_exit_error

type AgentRunBudget struct {
    WallTimeSeconds         int `json:"wall_time_seconds,omitempty"`
    IdleTimeSeconds         int `json:"idle_time_seconds,omitempty"`
    MaxTurns                int `json:"max_turns,omitempty"`
    MaxProcesses            int `json:"max_processes,omitempty"`
    MaxMemoryMb             int `json:"max_memory_mb,omitempty"`
    MaxCpuSeconds           int `json:"max_cpu_seconds,omitempty"`
    MaxOutputBytes          int `json:"max_output_bytes,omitempty"`
    MaxEventCount           int `json:"max_event_count,omitempty"`
    MaxWorkspaceGrowthBytes int `json:"max_workspace_growth_bytes,omitempty"`
    MaxChildren             int `json:"max_children,omitempty"`
    MaxParallelism          int `json:"max_parallelism,omitempty"`
}

type AgentRunStatus struct {
    RunId             string           `json:"run_id"`
    RootRunId         string           `json:"root_run_id"`
    ParentRunId       *string          `json:"parent_run_id,omitempty"`
    State             AgentRunState    `json:"state"`
    TerminationReason *string          `json:"termination_reason,omitempty"`
    Budget            AgentRunBudget   `json:"budget"`
    LastEventSeq      int              `json:"last_event_seq,omitempty"`
    CreatedAt         string           `json:"created_at"`
    StartedAt         *string          `json:"started_at,omitempty"`
    FinishedAt        *string          `json:"finished_at,omitempty"`
}

type CancelAgentRunResponse struct {
    RunId    string        `json:"run_id"`
    State    AgentRunState `json:"state"`
    Accepted bool          `json:"accepted"`
}
```

### 数据模型 (`internal/model/message.go`)

Run 状态附着在 Agent Message 行上（不单独建表）：

```go
RunID             string    `gorm:"column:run_id;size:36;index" json:"run_id,omitempty"`
RunKey            *string   `gorm:"column:run_key;size:36;uniqueIndex" json:"-"`
RunRequestHash    string    `gorm:"column:run_request_hash;size:64" json:"-"`
TerminationReason string    `gorm:"column:termination_reason;size:64" json:"termination_reason,omitempty"`
```

- `RunKey` 唯一索引是幂等锚点：并发重复提交同一 `run_id` 时，第二个插入失败转为查询既有行
- `RunRequestHash` 是 `RunTaskInput` 的 SHA256（`hashRunTaskInput`）：同一 run_id 携带不同请求返回 409，相同请求直接返回既有结果

### Service 层 (`internal/service/impl/task_service.go`)

接口签名（`internal/service/service.go`）：

```go
type TaskService interface {
    RunTask(taskID string, input RunTaskInput) (*RunTaskResult, error)
    GetRun(taskID, messageID string) (*generated.AgentRunStatus, error)
    CancelRun(taskID, messageID string) (*generated.CancelAgentRunResponse, error)
    // ...
}

type RunTaskInput struct {
    Message         string                 `json:"message" binding:"required"`
    AgentType       string                 `json:"agent_type"`
    SessionID       string                 `json:"session_id" binding:"required"`
    Cwd             string                 `json:"cwd"`
    SkipUserMessage bool                   `json:"skip_user_message"`
    RootRunID       string                 `json:"root_run_id"`
    ParentRunID     string                 `json:"parent_run_id"`
    Budget          map[string]interface{} `json:"budget"`
    RunID           string                 `json:"run_id"`
}
```

`RunTask` 的 run 相关流程：

1. `hashRunTaskInput(input)` 计算请求哈希；调用方带 `run_id` 时先 `findRunMessage`（通过 `MessageDao` 的 `FindByRunID` 能力断言）查既有 run：归属其他 task 或请求哈希不一致返回 409，一致则直接复用既有结果
2. 未指定 `run_id` 时生成新 UUID；Agent Message 以 `Status=streaming`、`RunID`、`RunKey=&runID`、`RunRequestHash` 创建
3. 创建唯一索引冲突（并发重复提交）时回查既有行，同请求幂等返回，不同请求 409

`buildAgentRequest` 把 run 身份与预算注入 AgentEnd 请求：

```go
rootRunID := runID
if input.RootRunID != "" {
    rootRunID = input.RootRunID
}
agentReq.RunId = &runID
agentReq.RootRunId = &rootRunID
if input.ParentRunID != "" {
    agentReq.ParentRunId = &input.ParentRunID
}
if input.Budget != nil {
    budget := interface{}(input.Budget)
    agentReq.Budget = &budget
}
```

`GetRun` / `CancelRun` 都先经 `authorizedRunMessage` 鉴权：校验 `task_id`、`message_id`（UUID 格式），确认消息归属该 task 且带 `RunID`，否则 404；随后带 10s（查询）/ 15s（取消）超时代理 AgentEnd。`CancelRun` 以 `user_cancelled` 作为取消原因。

### Controller 层 (`internal/controller/impl/task_controller.go`)

```
--- RegisterRoutes（/api）---
GET  /tasks/:taskId/messages/:messageId/run         GetRun（run 状态）
POST /tasks/:taskId/messages/:messageId/run/cancel  CancelRun（返回 202）

--- RegisterInternalRoutes（/api/internal）---
POST /tasks/:taskId/run                             runTask（内部 run 入口）
```

浏览器入口与内部入口的边界：`RunTask`（用户路由）在绑定请求后立即清空 `RootRunID` / `ParentRunID` / `Budget` / `RunID`，浏览器请求无法伪造父 run 身份或预算；内部入口 `runTask` 保留这些字段，供编排方（Orchestrator 链路）声明沙盒身份。

### AgentEnd 客户端 (`pkg/agentend_client/client.go`)

```go
func (c *Client) GetRun(ctx context.Context, runID string) (*generated.AgentRunStatus, error)
// GET {baseURL}/v1/runs/{escapePathSegment(runID)}

func (c *Client) CancelRun(ctx context.Context, runID string, reason generated.AgentRunTerminationReason) (*generated.CancelAgentRunResponse, error)
// POST {baseURL}/v1/runs/{escapePathSegment(runID)}/cancel，body 为 CancelAgentRunRequest{Reason}
```

run id 作为路径段传递前经 `escapePathSegment` 转义，防路径注入；两个调用都复用带 `serviceAuthTransport`（AgentEnd service auth 启用时附带服务令牌）的 `httpClient`。

### 终止原因与收尾 (`internal/stream/writer.go` + `task_service.go`)

StreamWriter 在 Error 事件携带 `termination_reason` 时：

```go
func (sw *StreamWriter) persistTerminationReason(reason string) {
    messageIDs := []string{sw.messageID}
    if sw.originalMessageID != sw.messageID {
        messageIDs = append(messageIDs, sw.originalMessageID)
    }
    for _, messageID := range messageIDs {
        sw.messageDao.UpdateMessageRunState(messageID, string(generated.MessageStatusFailed), reason)
    }
}
```

`MessageDao.UpdateMessageRunState(messageID, status, terminationReason string) error` 一次性更新状态与终止原因，覆盖当前子消息与原始消息。

`TaskService.runStream` 在流结束后的默认（completed）分支会先查 `message.TerminationReason`：非空说明 run 以终止原因收尾，跳过把 Session 置为 `completed` 的收尾更新，交由取消/超时语义决定最终状态。
