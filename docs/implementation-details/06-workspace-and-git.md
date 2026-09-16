# Workspace、Git 与预览超详细实现

## 实现了什么

Workspace 子系统为每个 Agent Session 创建隔离 Git Worktree，提供文件读写、diff、commit、revert、分支合并、任务清理和静态预览。Backend 只做代理，实际文件系统和 Git 操作全部在 AgentEnd。

## 怎么实现的

### 对象模型

Workspace dataclass 保存 id、task id、session id、repo path、worktree path、branch、agent type、status、时间等。`WorkspaceStatus` 表示 active/inactive 等生命周期。所有 task/session/workspace 标识先通过 `validate_workspace_identifier`，拒绝路径分隔符、遍历和非法字符。

命名函数集中在 `workspace/models.py`：task base branch、session branch和 worktree path 都由 task/session 推导，避免调用方自行拼接。Worktree 位于配置的 `workspace.base_dir`，不是仓库源码目录中的随机子目录。

### 创建流程

`POST /v1/workspace/create`：

1. PathPolicy 验证 repo_path 位于 allowed roots，并解析真实路径。
2. 验证目录存在且是 Git 仓库。
3. WorkspaceManager 检查同 task/session 的现有 Workspace，支持幂等返回。
4. GitOps 确定默认分支/基线，创建 task base 与 session branch。
5. 执行 `git worktree add` 到受管目录。
6. 写 Workspace registry；写失败时回滚新 worktree/branch。
7. 返回 Workspace，不把任意宿主路径暴露成可写 API。

### 文件读取与写入

FastAPI 文件路由使用 `{file_path:path}`。`_resolve_worktree_file` 对 URL path 解码后规范化，并验证 resolved target 仍在 worktree 根内。它拒绝 `..`、绝对路径逃逸和符号链接导致的越界。

读取返回文件内容或合适的 not found/validation error。写入创建允许的父目录并覆盖目标文件；请求经过服务认证和大小限制。Go 代理层又执行一次 path segment 检查，形成纵深防御。

### Skill 文件排除

Workspace diff 会根据 agent type 获取 Skill 配置目录前缀，例如 `.claude`、`.opencode`、`.codex`、`.pi`。生成用户可见 diff 时排除由平台安装的 Skill/运行配置，避免把平台注入文件当成业务代码提交。

### Diff

`GET /v1/workspace/:id/diff` 调用 Git 获取 tracked 与必要的 untracked 变化并返回 unified diff。Frontend 通过 Backend session/workspace 代理消费。大 diff 受 HTTP/SSE 边界限制；持久化查看使用 DiffSnapshot，而不是把每次 diff 都写入 Message。

### Commit 与 Revert

Commit 接口校验 Workspace active，暂存允许的代码变更并创建 commit，返回 commit hash/摘要。Revert 是工作区操作，不等同于重写远端历史；它将未接受改动恢复到基线/规定状态，并更新前端 snapshot 状态。

所有 Git 命令通过 GitOps 的异步 subprocess 入口执行，传明确 cwd 和参数数组，不拼接 shell 字符串。stderr 作为错误上下文返回，但对外进行稳定化处理。

### Task base 与 Session 分支

一个群聊 Task 可能有多个 Session 分支。Task base 提供共享集成点，子 Agent 的提交先进入各自分支，Orchestrator 根据结构化 integration result 合并到 task base。最终 `merge-to-main` 名称为历史兼容，实际目标由仓库默认分支/配置决定。

这一区分避免多个 Agent 同时直接写默认分支，也使冲突可以在 task 范围内恢复。删除单个 Session Worktree 不应删除别的成员分支；删除整个 Task 才清理 task base 和全部 session branches。

### 合并

Workspace merge 与 task merge 接口分别用于子分支集成和最终集成：

- merge 前确认 source/target 与记录绑定。
- 工作区必须 clean 或处于明确允许状态。
- 成功返回 merge commit/fast-forward 等事实。
- 冲突返回文件列表，不用自然语言假装成功。
- Orchestrator Phase 2 路径将冲突交给 IntegrationService/RecoveryCoordinator。

### Registry 与 Git 恢复

`JsonFileWorkspaceStore` 用原子替换保存 registry。启动时 `recover_workspaces` 读取 `git worktree list --porcelain` 与 registry 对比：

- Git 存在而 registry 缺失的受管 worktree可恢复登记。
- registry 存在而 worktree 已消失的条目标记/移除。
- task base worktree 与普通 Session worktree通过 branch/path 规则区分。
- 重复或非法路径不盲目采纳。

恢复后 WorkspaceManager 再加载 store，保证内存视图是修复后的版本。

### Inactive cleanup

DBReader 只读查询 Backend MySQL 中 Session 状态。WorkspaceManager 按 `cleanup_interval` 周期检查 inactive Session，并清理对应 Worktree。清理与 TaskCleanupWorker 是两条互补路径：前者周期对账状态，后者消费 Backend 删除事务产生的 durable intent。

### 预览服务

PreviewManager 基于 aiohttp 为指定 Workspace 启动临时静态服务器：

- 根目录固定为已解析 Workspace path。
- 每个 workspace 只维护一个活动 preview。
- start 返回 URL/port；stop 释放 runner/site。
- AgentEnd shutdown 调用 stop_all。
- 预览只服务静态资源，不应获得任意命令执行能力。

Backend 代理 start/stop，Frontend `PreviewCard` 用 iframe 或外链展示。部署时必须考虑浏览器能否访问 AgentEnd 返回的 host/port。

### API 清单

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/v1/workspace/create` | 创建或返回 Workspace |
| GET/PUT | `/v1/workspace/:id/files/:path` | 文件读写 |
| GET | `/v1/workspace/:id/diff` | unified diff |
| POST | `/v1/workspace/:id/commit` | 提交 |
| POST | `/v1/workspace/:id/revert` | 撤销未接受变更 |
| POST | `/v1/workspace/:id/preview/start|stop` | 预览生命周期 |
| POST | `/v1/workspace/:id/merge` | Workspace 分支合并 |
| POST | `/v1/workspace/task/:taskId/merge-to-main` | Task 最终合并 |
| DELETE | `/v1/workspace/:id` | 删除单 Workspace |
| GET | `/v1/workspace` | 列表 |
| DELETE | `/v1/workspace/task/:taskId` | 清理 Task Workspaces |
| POST | `/v1/workspace/task/:taskId/cleanup-branches` | 清理 Task 分支 |
| GET | `/v1/workspace/by-session/:sessionId` | Session 解析 Workspace |
| GET | `/v1/workspace/task/:taskId/git-info` | 前端 Git Graph 数据 |

### 安全不变量

- API 永远从 workspace id/session id 解析受管根，不能接受任意绝对目标文件。
- `resolve()` 后再次检查 relative-to，不能只检查输入字符串。
- Git 命令使用 argv，不经 shell。
- 恢复只接受 base_dir 和命名规则内的 Worktree。
- 删除前精确解析目标，禁止对 base_dir、repo root 或空路径递归操作。
- Skill 注入目录不进入业务 diff/commit。
