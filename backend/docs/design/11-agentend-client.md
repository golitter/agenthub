# AgentEnd Client — AgentEnd 出站 HTTP 集成边界

## 实现了什么

`pkg/agentend_client` 是 Backend 访问 AgentEnd 的唯一出站通道：Run 触发与 SSE 订阅、PlanReview 审查、run 状态查询/取消、编排冲突恢复代理、评测（evals）透传、repo 路径校验/初始化、健康与资源探测、session/workspace/分支清理、技能安装/移除/扫描全部经由此客户端发出。客户端内建四条横切规则：服务间鉴权（`serviceAuthTransport` 注入 Bearer 服务令牌）、双 `http.Client` 超时策略（普通调用 60s 总超时，SSE 流式调用仅限响应头 30s）、永不跟随重定向，以及路径/查询参数强制转义防注入。

## 怎么实现的

### Client 结构与构造 (`pkg/agentend_client/client.go`)

```go
type Client struct {
	baseURL      string
	httpClient   *http.Client
	streamClient *http.Client
}

type serviceAuthTransport struct {
	base  http.RoundTripper
	token string
}

func (t serviceAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header = req.Header.Clone()
	if t.token != "" {
		clone.Header.Set("Authorization", "Bearer "+t.token)
	}
	return t.base.RoundTrip(clone)
}

func New(host string, port int) *Client {
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}
	host = strings.TrimRight(host, "/")
	token := strings.TrimSpace(os.Getenv("AGENTEND_SERVICE_TOKEN"))
	defaultTransport := http.DefaultTransport
	streamTransport := &http.Transport{
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 2 * time.Second,
	}
	return &Client{
		baseURL: fmt.Sprintf("%s:%d", host, port),
		// A redirect is not a successful Skill mutation: following it could
		// turn an intermediate/unknown AgentEnd outcome into an unrelated 2xx
		// response and make the Backend commit the wrong reservation state.
		httpClient: &http.Client{
			Transport: serviceAuthTransport{base: defaultTransport, token: token},
			Timeout:   60 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		streamClient: &http.Client{
			Transport: serviceAuthTransport{base: streamTransport, token: token},
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}
```

- 服务令牌从 `AGENTEND_SERVICE_TOKEN` 环境变量读取；`agentend.service_auth_enabled=true` 时 AgentEnd 侧要求该令牌（Backend 入站侧对应 `BACKEND_SERVICE_TOKEN` + `middleware.ServiceAuth`，见 [04-config.md](04-config.md)）
- `streamClient` 不设总超时：SSE 流可能持续很久，超时由调用方 context 控制（`TaskService.runStream` 传 30 分钟父 context，见 [03-stream.md](03-stream.md)、[09-run-lifecycle.md](09-run-lifecycle.md)）

### 错误语义：确定性失败 vs 未知结果

Outbox 补偿（Skill 安装/移除、Task 清理）要求区分「AgentEnd 明确拒绝、可安全回滚」与「结果未知、必须保持可重试」：

```go
// HTTPStatusError preserves the AgentEnd response status so callers that own
// a durable side effect can distinguish a deterministic request rejection
// (4xx, no install/remove mutation) from an unknown server/network outcome.
// The latter must remain retryable because AgentEnd may have committed the
// filesystem change before the connection failed.
type HTTPStatusError struct {
	Action     string
	StatusCode int
	Detail     string
}

// KnownFailure reports that AgentEnd rejected the request before a successful
// mutation.  Backend uses this marker to remove a failed install reservation;
// 5xx responses intentionally remain unknown and retryable.
func (e *HTTPStatusError) KnownFailure() bool {
	return e != nil && e.StatusCode >= http.StatusBadRequest && e.StatusCode < http.StatusInternalServerError
}

// SkillMutationError represents an explicit {success:false} application
// response. AgentEnd returns this only after rejecting the mutation, so it is
// safe to roll back the corresponding durable reservation.
type SkillMutationError struct {
	Action string
	Detail string
}

func (e *SkillMutationError) KnownFailure() bool { return e != nil }
```

非 2xx 响应由 `statusError` 统一转换（最多读取 4096 字节 body 作为 Detail）；技能安装/移除额外经 `decodeSkillMutationResult` 解析 `{success, error}` JSON，显式 `success:false` 返回 `SkillMutationError`。

### 方法清单

| 分组 | 方法 | AgentEnd 端点 |
|------|------|---------------|
| Run / 流式 | `StreamAgent` / `StreamAgentWithContext` | `POST /v1/agent/stream`（Accept: text/event-stream） |
| Run / 流式 | `ReviewAgent`（`ReviewRequest{SessionID, Action, Content}`） | `POST /v1/agent/review` |
| Run 生命周期 | `GetRun` / `CancelRun` | `GET/POST /v1/runs/{runID}[/cancel]` |
| 冲突恢复 | `GetConflictProjection` / `ApplyConflictAction` | `GET/POST /v1/internal/conflicts/{conflictID}/projection|actions` |
| 评测代理 | `GetEval` / `PostEval`（透传 `json.RawMessage`，GET 16MiB / POST 1MiB body 上限） | `GET/POST /v1/evals/{resourcePath}` |
| Git 工具 | `ValidateRepoPath` / `InitGitRepo` | `POST /v1/validate-repo-path`、`POST /v1/init-git-repo` |
| 探测 | `HealthCheck` / `GetResources` / `GetAgentConfigs`（`AgentConfigInfo`） | `GET /health`、`/v1/resources`、`/v1/agents/configs` |
| Session 清理 | `DestroySession` / `DestroySessionContext` | `DELETE /v1/session/{sessionID}` |
| Task 清理 | `CleanupByTask` / `CleanupTaskBranches`（含 Context 版本） | `DELETE /v1/workspace/task/{taskID}`、`POST .../cleanup-branches` |
| Workspace | `ListWorkspaces`（`WorkspaceInfo`）/ `CleanupWorkspace` | `GET /v1/workspace`、`DELETE /v1/workspace/{workspaceID}` |
| Skill | `FetchSkills`（`SkillInfo`）/ `InstallSkill[WithContext]` / `RemoveSkill[WithContext]` | `GET /v1/skills/{agentType}`、`POST /v1/skills/{agentType}/{skillName}/install`、`DELETE /v1/skills/{agentType}/{skillName}`（均带 `session_id` query） |

### 注入防护与超时

```go
func escapePathSegment(value string) string { return url.PathEscape(value) }
func escapeQueryValue(value string) string  { return url.QueryEscape(value) }
```

所有路径段（runID / conflictID / sessionID / taskID / workspaceID / agentType / skillName）经 `escapePathSegment`，query 值经 `escapeQueryValue`，防止 ID 参数注入路径穿越或额外路径段。`ReviewAgent` / `ValidateRepoPath` / `InitGitRepo` / `RemoveSkill` 等历史方法不带 ctx 参数（使用 context.Background()），新方法一律提供 `*WithContext` 变体供调用方传递取消信号（如 Skill Operation Worker 的操作超时）。

### 与其他模块的关系

- `TaskService`（run/review/conflict）、`SkillService` + `SkillOperationWorker`（install/remove）、`TaskCleanupWorker`（session/workspace/分支清理）、`AdminController`（evals 代理、资源/配置读取）、`WorkspaceController`（直接持有，代理工作区操作）共享同一实例，由 `cmd/server/main.go` 构造后经 `app.Dependencies.AgentClient` 注入（见 [05-wiring.md](05-wiring.md)）
- AgentEnd 不可用时：普通业务路径返回 503（`vo.ServiceUnavailable`），outbox 补偿路径按退避重试（见 [10-skill-storage.md](10-skill-storage.md)）
