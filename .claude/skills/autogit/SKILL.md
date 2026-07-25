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

Co-Authored-By: <见下方「身份注入规则」，先反思再决定填入或整行省略>
EOF
)"
```

> **Co-Authored-By 身份注入规则**（防止伪造身份）：
>
> ❗ **提交前必须执行一次身份反思，确认"我到底是哪个模型"。** 直接跳到填名字 = 编造。
>
> ### 为什么必须反思
>
> harness 名 ≠ 模型身份。"Claude Code" 只是 harness，里面可以跑任何模型。凭"我在 Claude Code 里运行"就填 `Claude <noreply@anthropic.com>` 是伪造——上一版就因为这个出错。凭工具名、训练记忆、对话氛围猜厂商也都不算数。
>
> ### 反思步骤（提交前在脑内走一遍，不可跳过）
>
> 1. **找唯一可信来源**：harness 系统提示里实际声明的 model id 是什么？例如形如 "powered by the model `X`" / "model id: X"。逐字复述你读到的 id。
> 2. **自我质问来源**：这个 id 是我从系统提示里**读到**的，还是我"觉得应该是"的？是凭训练记忆或运行环境推断的吗？只要不是从系统提示逐字读到，身份即不可判定，停在此步。
> 3. **诚实归因**：根据读到的真实 model id，如实写出对应的模型/厂商身份与邮箱（格式 `Co-Authored-By: <Name> <noreply@<厂商域>>`）。不知道对应的邮箱域名时，不得生造。
> 4. **兜底**：系统提示未明确声明 model id、声明含糊、或无法确定对应邮箱域名时，**省略整个 Co-Authored-By 行**，绝不编造厂商或邮箱。
>
> ### 原则
>
> - 身份由**反思得来**，不是查表查来、更不是模板套来。
> - 编造身份比不写更糟——拿不准就省略 Co-Authored-By 行。
> - 每次提交都重新走一遍反思，禁止硬编码模型名。

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
