---
name: autogit
description: 自动 Git 提交。查看变更与最近提交风格，按项目 Conventional Commits + scope 规范生成中文 commit message 并提交。仅在用户明确要求提交时使用。
---

# Role

你是项目 Git 提交的执行者。按项目规范生成 commit message 并完成提交，不主动 push。

# When to Use

用户明确要求提交当前变更时（如"提交"、"commit 一下"、"把这些改动提交"）。不要在用户未要求时主动提交。

# Workflow

## 1. 查看变更

```bash
git status            # 当前变更概览
git diff              # 未暂存的具体改动
git diff --staged     # 已暂存的具体改动（若有）
git log --oneline -5  # 最近提交风格
```

## 2. 生成 commit message

严格遵循 [docs/guides/git-conventions.md](../../../docs/guides/git-conventions.md)：

- **格式**：`<type>(<scope>): <描述>`
- **scope 必填**，可取一个或多个值（逗号分隔）：`frontend` / `backend` / `agentend` / `common` / `docs` / `other`
- 跨多个子项目的改动用逗号分隔，如：`feat(frontend,backend,agentend): ...`
- **type** 遵循 Conventional Commits：`feat` / `fix` / `docs` / `refactor` / `chore` / `perf` 等
- **描述用中文**，简明概括改动的目的（why > what）

scope 判定依据改动落在哪个子项目：
- 只改 `frontend/` → `frontend`
- 只改 `backend/` → `backend`
- 只改 `agentend/` → `agentend`
- 改 `docs/` → `docs`
- 跨端 / 根目录工程文件（Makefile、scripts、docker）→ `common`
- 以上都不是 → `other`

## 3. 暂存与提交

按文件名精确暂存，**不要用 `git add .`**：

```bash
git add <file1> <file2> ...
```

提交（commit message 用 heredoc，末尾带 Co-Authored-By）：

```bash
git commit -m "$(cat <<'EOF'
<commit message>

Co-Authored-By: <执行提交的模型自行判断自身身份并注入>
EOF
)"
```

> **Co-Authored-By 身份注入**：不要硬编码模型名。提交时由当前执行提交的模型自行判断自己是谁（依据实际运行的模型/agent），按 `Co-Authored-By: <Name> <noreply@...>` 格式填入。若运行环境无法判定身份，则省略整个 Co-Authored-By 行，不要伪造。

## 4. 确认

```bash
git status            # 确认提交成功、工作区干净
```

# Strict Constraints

- **不使用 `--no-verify`**：若 pre-commit / commit-msg hook 失败，先修复问题再提交。
- **不提交密钥文件**：跳过 `.env`、`credentials`、私钥等敏感文件。
- **不使用 `--amend`**：不修改已有提交。
- **不主动 push**：除非用户明确要求推送到远程。
- **不在默认分支上直接提交大批改动**：如需开分支先与用户确认。
- **暂存精确到文件**：避免把无关变更混入同一次提交。
