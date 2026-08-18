# Pi Adapter 验收手册

## 自动测试

在仓库根目录执行：

```bash
make generate
cd agentend && uv run pytest tests/test_pi_adapter.py tests/test_skills.py
cd agentend && uv run pytest
cd backend && go test ./...
cd agentend/src/skills/builtin/taskctl && go test ./...
cd frontend && corepack pnpm lint && corepack pnpm build
```

`test_pi_adapter.py` 覆盖命令参数、真实 Pi session ID、增量文本、thinking/tool/error/DONE 映射、stderr 并发 drain、非零退出和进程中断；`test_skills.py` 与 taskctl 测试覆盖 `.pi/skills/` 路径和外部 Skill 边界。

## 手动验收

确保 `agentend/.env` 中的 `PI_CLI_PATH` 指向 Pi 0.82.1，并启动三端服务；`agents.json` 保持通用命令名 `pi`。

1. 创建一个 Pi 单聊，发送首条消息。检查 SSE 首个有效事件为 `init`，其 `content.cli_session_id` 为 Pi `session` header 的 `id`。
2. 继续发送第二条消息，确认命令使用 `--session <id>`，且上下文来自同一个 Pi session。
3. 让 Pi 读取、编辑文件，确认出现 `tool_call`、`tool_result` 和增量 `text`；检查大工具结果已被出站 sanitizer 裁剪。
4. 检查 worktree 中的 `.pi/skills/taskctl` 和 `.pi/skills/render` 可执行，并通过 Skills API 安装/移除外部 Skill。
5. 在 Pi 正在运行时取消 Run，确认 Pi 及其通过 `bash` 启动的子进程均退出，且 AgentEnd 的 `_processes` 已清理。
6. 并行创建同一仓库的两个 Pi 会话，确认各自的 `cli_session_id` 不串线；检查 `.pi` 不进入用户提交的 diff。

## 失败排查

- Pi 没有读取 `.pi/settings.json` 或 Skills：确认命令含 `--approve`，且未误加 `--no-skills`。
- 没有恢复上下文：检查 `logs/session_mappings.json` 是否保存 Pi `session.id`，不要使用 `--continue`。
- 子进程残留：检查 AgentEnd 日志中的 Run 取消记录和 `execution.process_terminate_timeout`，确认服务账户有权限发送进程组信号。
- 配置读取失败：检查 `agentend/.env` 的 `PI_CONFIG_PATH` 以及该路径是否可读；`config.yaml` 中只保留空的通用配置。
