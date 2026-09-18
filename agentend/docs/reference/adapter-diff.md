# 适配器差异：权限模型

## 权限模型

| 特性 | Claude CLI | OpenCode CLI | Codex CLI | Pi CLI |
|------|-----------|--------------|-----------|--------|
| 权限机制 | `--dangerously-skip-permissions` 跳过权限提示 + `--allowedTools` 命令行预授权 | 通过配置文件或内部机制管理，无命令行参数 | `--dangerously-bypass-approvals-and-sandbox` + `-s danger-full-access`（仅新会话路径；另加 `--disable apps/plugins`） | `--approve` + 可选 `--tools` |
| 适配器支持 | `allowed_tools` → `--allowedTools` | 接收但忽略 `allowed_tools` | 接收但忽略 `allowed_tools` | `allowed_tools` → Pi 小写工具名 |
| 非交互模式限制 | `-p` 无 TTY 无法弹权限确认；适配器统一加 `--dangerously-skip-permissions` 跳过审批 | 无此限制 | 无此限制（已跳过审批） | 使用 `--approve` 读取项目设置和 Skills |
| 危险工具过滤 | `SafetyRule` 过滤 `_DANGEROUS_TOOLS`（`dangerouslyDisableSandbox`），通过 `--allowedTools` 排除 | 无 | 无 | 由 Pi CLI 工具白名单和 AgentHub 规则共同约束 |

## 原因

Claude CLI 的 `-p` 非交互模式没有 TTY，无法弹出权限确认提示。适配器在命令中统一附加 `--dangerously-skip-permissions` 跳过审批，同时仍透传 `--allowedTools` 预授权列表（如 `Write`、`Edit`、`Bash`），两层叠加决定最终可执行的工具。也可通过 `.claude/settings.local.json` 配置持久权限。

OpenCode CLI 的权限管理不依赖命令行参数，适配器目前不做工具过滤，`allowed_tools` 参数传入后被忽略。

Codex CLI 通过 `--dangerously-bypass-approvals-and-sandbox` 跳过审批流程，`-s danger-full-access` 允许完全访问工作区。

Pi CLI 在非交互模式下使用 `--approve` 信任受管 worktree，并通过 `--no-extensions --no-prompt-templates` 禁止未受管扩展和提示模板；AgentHub 仅在 `allowed_tools` 非空时传递 `--tools`。
