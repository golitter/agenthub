# 2026-05-29 orchestrator 工具路径沙箱化

## 变更原因

`read_file` 和 `list_dir` 工具原为顶层 `@tool`，接受绝对路径且无边界检查，LLM 可读取文件系统任意文件（包括 agentend 自身配置）。现将其移入 `build_tools()` 闭包并添加路径校验，限制读取范围为 `shared_dir` 和 orchestrator 自身 session 工作区。

## 变更文件

- `agentend/src/orchestrator/planning/tools.py` — 删除顶层 `read_file`/`list_dir`，移入闭包并添加 `_is_allowed` 路径校验
- `agentend/src/orchestrator/planning/graph.py` — `GraphState` 新增 `allowed_read_dirs`，`plan_node` 传递给 `build_tools`
- `agentend/src/adapters/orchestrator.py` — 计算 `allowed_read_dirs`（shared_dir + cwd）并传入 graph state
- `agentend/src/orchestrator/planning/prompts.py` — 工具描述标注路径限制范围

## 对比结果

无 schema 变更。工具签名不变（`read_file(path)` / `list_dir(path)`），仅内部增加路径校验逻辑。

## 跨端影响

- **Frontend**: 无影响。SSE 事件流格式不变。
- **Backend**: 无影响。不涉及后端 API 或数据模型变更。
- **AgentEnd**: 内部安全增强。`read_file`/`list_dir` 新增 `allowed_read_dirs` 边界校验，`write_file` 行为不变。

## 契约变更

无。本次改动为 agentend 内部安全增强，不涉及 `contracts/schemas/` 中的任何契约定义。
