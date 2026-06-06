# Planning ToolMessage 结构化

## 实现了什么

将 `reason_node` 工具循环中的 ToolMessage 从纯文本包裹为 JSON 结构，让 LLM 在后续轮次中拥有完整的工具调用上下文（工具名、参数、输出）。改动范围仅限 `reason_node` 中 ToolMessage 构造，不改变 graph 拓扑。

## 怎么实现的

### 问题

`reason_node` 工具循环中，工具返回值为纯文本字符串，直接以 `ToolMessage(content=str(result))` 传回 LLM。LLM 缺乏工具调用的结构化上下文（哪个工具、传了什么参数），多轮调用后容易"迷失"，用自然语言总结而非输出 JSON 格式的计划。

`reason_node` 工具循环中，工具返回值为纯文本字符串，直接以 `ToolMessage(content=str(result))` 传回 LLM。LLM 缺乏工具调用的结构化上下文（哪个工具、传了什么参数），多轮调用后容易"迷失"，用自然语言总结而非输出 JSON 格式的计划。

### 根因

```
当前 ToolMessage 内容：

  "file contents here..."           ← read_file 返回的原始文本
  "Error: file not found: /path"    ← 错误信息也是纯文本

LLM 只看到一段文本，不知道：
- 这是哪个工具产生的（虽然有 tool_call_id，但 LLM 看不到）
- 调用时传了什么参数
- 结果是成功还是失败
```

## 方案

将工具返回值包裹为 JSON 结构，让 LLM 在后续轮次中拥有完整的工具调用上下文。

**改动范围**：只动 `reason_node` 中 ToolMessage 构造，不改 graph 拓扑。

### 结构化 ToolMessage

当前：`ToolMessage(content=str(result))` — 纯文本

改为：`ToolMessage(content=json.dumps({"tool": tc["name"], "args": tc["args"], "output": result}, ensure_ascii=False))` — JSON

包裹后的 LLM 视角：

```json
{"tool": "read_file", "args": {"path": "/shared/plans/overview.md"}, "output": "# 规划概述\n\n建立用户认证系统..."}
```

### 效果

| 方面 | 改前 | 改后 |
|------|------|------|
| LLM 对工具结果的理解 | 只看到原始文本 | 知道工具名、参数、输出 |
| 多轮后上下文连贯性 | 容易丢失 | 每轮 ToolMessage 自包含 |
| token 开销 | 基准 | 每次工具调用多 ~20-50 tokens（tool + args 结构） |
| 后续 JSON 输出成功率 | 偶尔退化为自然语言 | 预期提升（LLM 全程在结构化上下文中） |

## 修改文件

| 文件 | 改动 |
|------|------|
| `agentend/src/orchestrator/planning/graph.py` | `reason_node` 中 ToolMessage 构造改为 JSON 包裹 |

## 验证

1. `make run-agentend`
2. 发送需要规划的任务（如"帮我创建一个用户认证系统"）
3. 日志中 ToolMessage 应为 JSON 格式，LLM 多轮工具调用后仍能输出结构化 JSON 计划
