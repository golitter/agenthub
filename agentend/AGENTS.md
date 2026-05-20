# AGENTS.md — agentend

基于 FastAPI 的 Agent Runtime 服务，桥接外部 Agent（当前为 Claude CLI），提供会话管理、规则引擎和流式响应。Python >=3.10，包管理 uv，代码检查 ruff，测试 pytest。

## 目录结构

```
src/
├── adapters/     # Agent 适配器（插件式）
├── api/v1/       # API 路由
├── app/          # 应用入口与配置
├── rules/        # 规则引擎
├── schemas/      # 数据模型
└── session/      # 会话管理
```

## 常用命令

> 需在 `agentend/` 目录下执行。

```bash
uv sync                        # 安装依赖
uv run python -m src.app.main  # 启动开发服务器（热重载）
```

## 详细文档

- [API 端点、核心架构、配置](docs/common/details.md)
