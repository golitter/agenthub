# Git Auto Init — 新建对话支持非 Git 目录自动初始化

## 实现了什么

新建对话时，如果用户输入的目录路径存在但不是 Git 仓库，支持通过确认目录名后自动执行 `git init`，无需手动操作终端。前端 `RepoPathInput` 组件检测 "不是 git 仓库" 错误后进入确认态，用户输入目录名匹配后调用 `initGitRepo` API 完成初始化。链路跨三端：agentend 提供 `POST /v1/init-git-repo` 端点，Go Backend 代理 `/api/init-git-repo`，前端负责检测与确认交互。

## 怎么实现的

### 用户交互流程

1. 用户输入路径 → 点击"校验"
2. 路径存在但非 Git 仓库 → 显示黄色提示框："该目录不是 Git 仓库。输入目录名以确认初始化：**{name}**"（`UI_MESSAGES.GIT_INIT_PROMPT`）
3. 用户输入最后一个路径段（如 `repo`）→ "初始化 Git" 按钮亮起
4. 点击 → 后端执行 `git init && git add -A && git commit -m "init" && git branch -M main`
5. 成功 → 自动进入校验通过状态，继续创建对话

### Agentend 端点 (`agentend/src/api/v1/validate.py`)

- `InitGitRepoRequest` / `InitGitRepoResponse` Pydantic Model
- `init_git_repo()` 端点：校验路径存在且非 Git 仓库 → 调用 `GitOps.init_repo()` → 返回结果
- 直接 import `GitOps`（不经过 WorkspaceManager），因为这是独立操作

### Go Backend 代理路由 (`backend/pkg/agentend_client/client.go` + `backend/internal/controller/impl/task_controller.go`)

- `agentend_client/client.go`：`InitGitRepoResult` struct + `InitGitRepo()` 方法，代理到 agentend 的 `/v1/init-git-repo`
- `task_controller.go`：`InitGitRepoReq` struct + `InitGitRepo()` handler，在 `RegisterRoutes` 中注册 `rg.POST("/init-git-repo", ctrl.InitGitRepo)`

### Frontend API + UI 文本 (`frontend/src/lib/api.ts` + `frontend/src/lib/ui-text.ts`)

- `api.ts` 新增 `initGitRepo(repoPath)` 函数，调用 `POST /api/init-git-repo`
- `ui-text.ts`：
  - `UI_ACTIONS` 新增 `INIT_GIT: '初始化 Git'`
  - `UI_STATUS` 新增 `INITIALIZING_GIT: '正在初始化 Git...'`
  - `UI_MESSAGES` 新增 `GIT_INIT_PROMPT` / `GIT_INIT_SUCCESS` / `GIT_INIT_MISMATCH`
  - `UI_ERRORS` 新增 `GIT_INIT_FAILED`

### RepoPathInput (`src/components/im/RepoPathInput.tsx`)

- 状态：`needsGitInit` / `confirmInput` / `initError` / `initializing`
- `handleValidate` 中检测 "不是 git 仓库" 错误 → 进入确认态而非报错
- `handleInitGit` 函数：匹配目录名 → 调用 API → 成功后自动标记 validated
- JSX 内联在输入框下方的黄色确认框，含文本输入 + 确认按钮 + 取消按钮

### 关键设计决策

- **确认方式**：要求用户输入路径最后一段（如 `repo`），精确匹配才可提交，防止误操作
- **空目录处理**：`git init` + `git add -A` + `git commit` 在空目录上会失败（nothing to commit），API 返回错误，用户可见——这是预期行为
- **幂等性**：agentend 端点再次检查 `.git` 是否存在，防止并发重复初始化

### 验证方式

1. 启动三端服务（`make all`），用 `make status` 确认健康
2. 打开前端 → 新建对话
3. 输入一个存在但非 Git 的目录路径 → 点击"校验"
4. 确认出现黄色提示框，要求输入目录名
5. 输入错误名称 → 按钮不可点击
6. 输入正确名称 → 点击"初始化 Git"
7. 成功后自动进入校验通过状态 → 可正常选择 Agent 创建对话
8. 在终端 `ls -la {path}/.git` 确认 Git 仓库已初始化
