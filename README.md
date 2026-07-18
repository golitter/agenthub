<p align="center">
  <img src="frontend/public/favicon.svg" alt="AgentHub" width="80">
</p>

<h1 align="center">AgentHub — 多 Agent 协作开发平台</h1>

AgentHub 是一个基于 IM 聊天范式的多 Agent 协作平台，统一接入 Claude Code、OpenCode、Codex 等 AI Coding CLI，通过 Orchestrator 完成任务拆解、Agent 分派与结果聚合，支撑多 Agent 安全、高效、可扩展协同开发。

## 核心能力

- IM 式单聊 / 群聊 / @Agent 协作
- Orchestrator 任务拆解、分派与聚合
- Claude Code / OpenCode / Codex 统一适配
- SSE 实时流式输出与断线恢复
- Git Worktree 工作区隔离
- Skills 分发与产物内联预览
- YAML 契约生成 TypeScript / Go / Python 类型

## 技术栈

| 模块 | 技术栈 | 职责 |
|------|--------|------|
| Frontend | React · Vite · TypeScript · Tailwind CSS | 聊天界面、会话管理、产物预览 |
| Backend | Go · Gin · GORM · MySQL · Redis Stream | API、SSE、消息持久化、管理能力 |
| AgentEnd | Python · FastAPI · LangGraph | Agent 适配、编排、工作区与技能管理 |
| Contracts | YAML Schema | 跨端协议单一来源 |

## 快速开始

环境要求：Node.js 22+、pnpm 9+、Go 1.24+、Python 3.12+、uv、MySQL 8.0、Redis 7+。

```bash
make                  # 启动前端 :5173、后端 :8080、AgentEnd :8001
make status           # 查看服务状态
make docker-up        # Docker 部署入口
```

## 目录结构

```text
frontend/     React 前端
backend/      Go 后端
agentend/     Python Agent Runtime
contracts/    跨端契约
docs/         项目文档
docker/       Docker 部署
scripts/      工程脚本
```

## License

MIT
