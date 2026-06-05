# Design: 新建对话时支持非 Git 目录自动初始化

## Context

当前创建新对话时，用户输入仓库路径后点"校验"，如果目录存在但不是 Git 仓库，校验直接失败，用户无法继续。本次改动让用户可以通过输入目录名确认后自动执行 `git init`，无需手动操作终端。

## 用户交互流程

1. 用户输入路径 → 点击"校验"
2. 路径存在但非 Git 仓库 → 显示黄色提示框："输入目录名以确认初始化 Git 仓库: **{name}**"
3. 用户输入最后一个路径段（如 `repo`）→ "初始化 Git" 按钮亮起
4. 点击 → 后端执行 `git init && git add -A && git commit -m "init" && git branch -M main`
5. 成功 → 自动进入校验通过状态，继续创建对话

## 改动清单

### 1. Agentend — 新增 `POST /v1/init-git-repo` 端点

**文件**: `agentend/src/api/v1/validate.py`

- 新增 `InitGitRepoRequest` / `InitGitRepoResponse` Pydantic Model
- 新增 `init_git_repo()` 端点：校验路径存在且非 Git 仓库 → 调用 `GitOps.init_repo()` → 返回结果
- 直接 import `GitOps`（不经过 WorkspaceManager），因为这是独立操作

### 2. Go Backend — 新增 `/init-git-repo` 代理路由

**文件**: `backend/pkg/agentend_client/client.go`

- 新增 `InitGitRepoResult` struct
- 新增 `InitGitRepo()` 方法，代理到 agentend 的 `/v1/init-git-repo`

**文件**: `backend/internal/controller/impl/task_controller.go`

- 新增 `InitGitRepoReq` struct + `InitGitRepo()` handler
- 在 `RegisterRoutes` 中注册 `rg.POST("/init-git-repo", ctrl.InitGitRepo)`

### 3. Frontend — API + UI 文本 + 组件

**文件**: `frontend/src/lib/api.ts`（~line 384 后）

- 新增 `initGitRepo(repoPath)` 函数，调用 `POST /api/init-git-repo`

**文件**: `frontend/src/lib/ui-text.ts`

- `UI_ACTIONS` 新增 `INIT_GIT: '初始化 Git'`
- `UI_STATUS` 新增 `INITIALIZING_GIT: '正在初始化 Git...'`
- `UI_MESSAGES` 新增 `GIT_INIT_PROMPT` / `GIT_INIT_SUCCESS` / `GIT_INIT_MISMATCH`
- `UI_ERRORS` 新增 `GIT_INIT_FAILED`

**文件**: `frontend/src/components/im/RepoPathInput.tsx`

- 新增状态: `needsGitInit` / `confirmInput` / `initError` / `initializing`
- `handleValidate` 中检测 "不是 git 仓库" 错误 → 进入确认态而非报错
- 新增 `handleInitGit` 函数：匹配目录名 → 调用 API → 成功后自动标记 validated
- JSX 新增黄色确认框（内联在输入框下方），含文本输入 + 确认按钮 + 取消按钮

## 关键设计决策

- **确认方式**：要求用户输入路径最后一段（如 `repo`），精确匹配才可提交，防止误操作
- **空目录处理**：`git init` + `git add -A` + `git commit` 在空目录上会失败（nothing to commit），API 返回错误，用户可见——这是预期行为
- **幂等性**：agentend 端点再次检查 `.git` 是否存在，防止并发重复初始化

## 验证方式

1. 启动三端服务 `make status`
2. 打开前端 → 新建对话
3. 输入一个存在但非 Git 的目录路径 → 点击"校验"
4. 确认出现黄色提示框，要求输入目录名
5. 输入错误名称 → 按钮不可点击
6. 输入正确名称 → 点击"初始化 Git"
7. 成功后自动进入校验通过状态 → 可正常选择 Agent 创建对话
8. 在终端 `ls -la {path}/.git` 确认 Git 仓库已初始化
