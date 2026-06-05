# 2026-06-06: 新增 InitGitRepo 契约

## 变更原因

新增 Git 自动初始化功能：当用户输入的目录路径不是 git 仓库时，前端弹出确认流程，经三端联动完成 `git init`。需要在契约层定义 `InitGitRepoRequest` / `InitGitRepoResponse` 协议。

## 变更文件

- `contracts/schemas/validate-repo-path.yaml` — 新增两个 definition

## 契约变更

| 类型 | 变更 |
|------|------|
| `InitGitRepoRequest` | 新增，字段：`repo_path: string`（必填） |
| `InitGitRepoResponse` | 新增，字段：`success: boolean`（必填）、`errors: string[]`（必填） |

## 生成文件

| 语言 | 文件路径 |
|------|----------|
| Python | `agentend/src/generated/validate_repo_path.py` |
| TypeScript | `frontend/src/generated/validate-repo-path.ts` |
| Go | `backend/internal/generated/validate_repo_path.go` |

## 跨端影响

- **AgentEnd**：`src/api/v1/validate.py` 新增 `/v1/init-git-repo` 端点，内联定义与契约一致
- **Backend**：`pkg/agentend_client/client.go` 新增 `InitGitRepo` 方法，`internal/controller/impl/task_controller.go` 新增路由
- **Frontend**：`src/lib/api.ts` 新增 `initGitRepo()` 函数，`src/components/im/RepoPathInput.tsx` 添加确认初始化 UI
