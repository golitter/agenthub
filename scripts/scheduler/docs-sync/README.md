# docs-sync

定时文档同步工作流：用 Claude Code 的 skill 自动同步全项目文档，在独立工作分支上提交，再合并回 main。单一闭环任务。

## 工作流（run.sh）

六个阶段，任一 skill 失败即中止：

| 阶段 | 动作 |
|------|------|
| 0 | 前置检查：必须在 `main` 分支、工作区完全干净（全部已提交） |
| 0.5 | 从最新 `main` 创建/重置工作分支 `auto-docs-update`（`git checkout -B`，幂等） |
| 1 | `/doc-linter` —— 全项目 `docs/` 文档同步精炼 |
| 2 | `/agentsmd-linter` —— 全项目 `AGENTS.md` 同步精炼（`--resume` 共享会话） |
| 3 | `/autogit` —— 若有文档变更，按项目规范生成中文 commit 并提交 |
| 4 | 合并 `auto-docs-update` → `main`，删除工作分支，停留在 `main` |

### 共享 session

阶段 1/2/3 共用同一个 Claude Code 会话，上下文跨阶段连贯：

- 阶段 1 用 `--output-format stream-json` 启动，从 init 消息捕获 `session_id`（UUID）
- 阶段 2/3 用 `--resume <session_id>` 恢复同一会话

## 运行

```bash
./scripts/scheduler/docs-sync/run.sh
```

日志写入 `logs/docs-sync.log`（stream-json 流，可事后用 `jq` 解析）。

## 配置

### `.env`（同目录，不入库）

| 变量 | 用途 |
|------|------|
| `CLAUDE_HOME` | Claude Code native binary 完整路径（run.sh 用） |
| `ANTHROPIC_BASE_URL` | 平台入口（quota-query.sh 用） |
| `ANTHROPIC_AUTH_TOKEN` | 认证凭证（quota-query.sh 用） |

从 `.env.example` 复制并填入真实值：

```bash
cp .env.example .env
```

### 可调参数（环境变量覆盖）

| 变量 | 默认 | 说明 |
|------|------|------|
| `ALLOWED_TOOLS` | `Read,Edit,Write,Glob,Grep,Bash` | Claude 工具白名单 |
| `MAX_TURNS` | `5000` | 单会话最大轮次 |
| `PER_SESSION_TIMEOUT` | `10000` | 单会话超时（秒），`0` = 不限 |
| `REQUIRE_BRANCH` | `main` | 前置检查 & 合并目标分支 |
| `WORK_BRANCH` | `auto-docs-update` | 临时工作分支名 |

## 挂定时（cron 示例）

```bash
# 每天凌晨 3:17 跑一次
17 3 * * * cd /home/leixu/yh/devprojects/agenthub && ./scripts/scheduler/docs-sync/run.sh >> logs/docs-sync.cron.log 2>&1
```

## 注意

- 三个 skill 都会**直接修改文档**，脚本用 `--dangerously-skip-permissions` 自动执行、不弹确认。
- 任一 skill 失败即中止；中止时停留在当前分支，残留改动用 `git restore .` 丢弃、`git checkout main` 切回。
- 工作分支 `auto-docs-update` 每次从最新 `main` 重建（`-B`），残留会被覆盖——它是临时分支，请勿在其中做手动工作。

## 目录其他脚本

- `quota-query.sh` —— 查询 GLM Coding Plan 额度，读 `.env` 的 `ANTHROPIC_*`。
