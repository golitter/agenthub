# 详细文档

## API 端点

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/v1/agent/stream` | 流式 Agent 响应（SSE） |
| POST | `/v1/agent/execute` | 同步 Agent 执行 |
| GET | `/v1/session` | 列出所有会话 |
| GET | `/v1/session/{id}` | 获取会话详情 |
| POST | `/v1/session/{id}/interrupt` | 中断运行中会话 |
| DELETE | `/v1/session/{id}` | 删除会话 |
| POST | `/v1/workspace/create` | 创建工作区（Git Worktree） |
| GET | `/v1/workspace` | 列出所有工作区 |
| POST | `/v1/workspace/{id}/commit` | 提交工作区变更 |
| POST | `/v1/workspace/{id}/merge` | 合并工作区到目标分支 |
| POST | `/v1/workspace/task/{task_id}/merge-to-main` | 合并任务分支到 main |
| DELETE | `/v1/workspace/{id}` | 清理工作区 |
| POST | `/v1/validate-repo-path` | 验证 repo 路径 |
| POST | `/v1/pin/add` | 添加 Pin 到共享内存 |
| POST | `/v1/pin/remove` | 移除 Pin |
| GET | `/v1/pin/list` | 列出所有 Pin |
| GET | `/health` | 健康检查 |

## 项目结构

```
agentend/
├── src/
│   ├── adapters/       # Adapter 适配器层
│   ├── api/            # FastAPI HTTP 端点
│   │   └── v1/         # v1 版本 API
│   ├── app/            # 应用入口、配置、DI
│   ├── orchestrator/   # Orchestrator 规划模块（LangGraph + LLM 任务拆解）
│   ├── rules/          # Rule Engine 规则引擎
│   ├── schemas/        # 数据模型
│   ├── session/        # Session 会话管理
│   ├── skills/         # 技能供给系统（taskctl 等）
│   └── workspace/      # 工作区管理（Git Worktree 隔离）
├── docs/
│   ├── common/         # 公共文档
│   ├── impl/           # 实现文档（按功能编号）
│   ├── playbooks/      # 手动测试流程
│   └── todos/          # 待办事项
├── tests/              # 测试
├── pyproject.toml      # 项目配置与依赖
└── ruff.toml           # 代码风格
```

## 核心架构

- **执行流程**：请求到达 → 规则引擎评估 → 适配器注册表解析 → 会话管理器跟踪状态 → 适配器执行 → 结果流式/同步返回
- **会话状态机**：`IDLE → RUNNING → COMPLETED / INTERRUPTED / ERROR`，另含 `INACTIVE` 状态用于标记不活跃会话
- **适配器模式**：通过抽象基类支持不同 Agent 类型，当前实现 Claude CLI、OpenCode CLI 与 Orchestrator 适配器
- **Orchestrator 规划**：通过 LangGraph + LLM 将用户需求拆解为多 Agent 子任务，写入 `shared/.agent/` 目录供各 agent 消费
- **规则引擎**：执行前评估 Safety（阻止危险工具）、Scope（校验工作区路径）等规则，可修改 system prompt 和工具白名单
- **会话持久化**：API session_id 与 CLI session_id 映射持久化至 `logs/session_mappings.json`
- **工作区管理**：基于 Git Worktree 的任务级隔离，支持自动创建任务分支（`task/{task_id}`）、提交、合并与清理，含 TTL 自动回收与启动恢复
- **Pin 内存系统**：通过 `/v1/pin` 端点管理共享内存中的固定条目，支持多 Agent 间共享上下文

## 配置

统一通过 `config.yaml` 管理，LLM 密钥通过 `.env` 读取。配置项分组如下：

- **server** — 监听地址、端口、CORS、热重载
- **cli** — Claude / OpenCode CLI 路径
- **workspace** — Worktree 根目录、TTL 过期、清理巡检间隔、存储路径、默认分支
- **session** — 会话映射持久化路径
- **execution** — 最大轮次、执行超时、进程终止超时
- **skills** — 内置技能目录与分发清单
- **llm** — Orchestrator LLM 配置（从 `.env` 读取 `DS_MODEL`、`DS_BASE_URL`、`DS_API_KEY`）

详见 [config.yaml](../../config.yaml) 中的注释。

## 文档索引

- [架构总览](../impl/architecture.md)
- [适配器差异对比](adapter-diff.md)
- [Orchestrator 规划实现](../impl/11-orchestrator-planning.md)
- [测试流程 Playbooks](../playbooks/)
