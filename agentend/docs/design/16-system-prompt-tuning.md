# Orchestrator 系统提示词优化进程

记录 Orchestrator 系统提示词的持续优化项。每项包含动机、方案、改动文件和状态。

---

## ✅ 优化 1：Skill 按需加载

**状态**：已完成

**动机**：`skill_prepare_node` 额外调一次 LLM 做 `select_skills` 语义筛选，再将选中 skill 的完整 SKILL.md 正文（L2）全量注入系统提示词。导致系统提示词冗长浪费 token，且筛选 LLM 调用增加延迟。

**方案**：系统提示词 `## 可用 Skills` 只列出 L1 元数据（name + description），Orchestrator LLM 通过新增的 `load_skill_detail` 工具按需加载 L2（SKILL.md 正文）或 L3（资源文件）。

**改动文件**：

| 文件 | 改动 |
|------|------|
| `src/orchestrator/planning/prompts.py` | `l2_content` 参数 → `l1_skills`；skills_section 只渲染 name+description；tools_section 新增 load_skill_detail 说明 |
| `src/orchestrator/planning/graph.py` | `skill_prepare_node` 删除 `select_skills` / `load_l2_content` 调用，只做 `discover_skills` |
| `src/orchestrator/planning/tools.py` | 新增 `load_skill_detail(skill_name, level, resource_path)` 工具，支持 `level="l2"` / `level="l3"` |
| `src/orchestrator/planning/skill_loader.py` | 无改动（`discover_skills`、`load_skill_l2`、`load_skill_resource` 继续复用） |

**数据流对比**：

```
优化前：discover_skills → select_skills(LLM) → load_l2_content → 全量注入提示词
优化后：discover_skills → L1 元数据注入提示词 → LLM 按需调用 load_skill_detail
```

**效果**：`skill_prepare_node` 不再发起 LLM 调用（秒级 → 毫秒级）；系统提示词从 **~4.3k token 降至 ~3.0k token**（-30%）。

---

## ✅ 优化 2：精简 Agent 描述

**状态**：已完成

**动机**：`_build_agents_desc` 为每个 Agent 拼接 `capabilities` 列表（如 `代码生成, 代码审查`），但 agent type 本身已隐含能力信息，capabilities 多余。

**方案**：移除 `capabilities` / `cap_str` 逻辑，Agent 描述只保留 id、name、type。

```python
# 优化前
cap_str = ", ".join(caps) if caps else "通用"
lines.append(f"- **{aid}**（{name}，类型: {agent_type}）: {cap_str}")

# 优化后
lines.append(f"- **{aid}**（{name}，类型: {agent_type}）")
```

**改动文件**：

| 文件 | 改动 |
|------|------|
| `src/orchestrator/planning/graph.py` | `_build_agents_desc` 移除 capabilities 和 cap_str |

**效果**：每条 Agent 描述减少约 5-10 token。

---

## ✅ 优化 3：隐藏系统提示词中的绝对路径

**状态**：已完成

**动机**：`workspace_section` 包含 worktree 绝对路径（如 `/Users/yanghao/.../task-base`），暴露了用户目录结构。LLM 读取任务代码必须传绝对路径，因为 `_resolve_tool_path` 只有一个 base（`shared_dir`）。

**方案**：`read_file` / `list_dir` 新增 `workspace_type` 参数（`"shared"` / `"taskbase"`），根据 type 选择 base 目录。LLM 只需传相对路径，系统提示词不再暴露绝对路径。

**改动文件**：

| 文件 | 改动 |
|------|------|
| `src/orchestrator/planning/tools.py` | `build_tools` 新增 `task_base_dir` 参数；`read_file` / `list_dir` 新增 `workspace_type` 参数，按 type 选择 base |
| `src/orchestrator/planning/graph.py` | `reason_node` 传入 `task_base_path` 给 `build_tools` |
| `src/orchestrator/planning/prompts.py` | `workspace_section` 去掉绝对路径，改为说明 `workspace_type` 用法；`tools_section` 更新工具签名 |

**效果**：系统提示词不再暴露任何绝对路径信息。

---

## 🔲 优化 4：（待定）

---

## ✅ 优化 5：动态上下文拆分 + 对话记忆持久化（Cache 友好）

**状态**：已完成

**动机**：

1. **Cache 命中率低**：系统提示词包含 4 个动态区块（Pin、Evolution、replan、orchestrator_ctx），这些内容每轮都可能变化。LLM prompt cache 以 prefix 匹配，系统提示词前缀一变就全量 miss。静态部分（身份、规则、工具、技能）本可被缓存，却因拼在同一个 SystemMessage 中被动态内容拖累。
2. **跨轮丢失推理历史**：`memory_messages` 每轮从 `[]` 开始，LLM 无法回忆之前的规划决策、工具调用结果。

**方案**：

将系统提示词拆成**多层消息**，使静态部分和动态部分分离：

```
[SystemMessage(身份+规则+工具+技能)]   ← 高 cache 命中（几乎不变）
[SystemMessage(Pin 约束)]             ← 动态，每轮重建
[SystemMessage(编排经验)]              ← 动态，每轮重建
[HumanMessage(重规划反馈)]             ← 动态，仅重规划时
[HumanMessage(群聊上下文)]             ← 动态
[memory_messages(持久化历史)]          ← 跨轮保留的对话链
[HumanMessage(当前用户消息)]           ← 每轮新内容
```

关键设计：
- **系统提示词只保留静态内容**：身份、Agents 列表、SOUL、Skills L1、Tools、Workspace、规则。这些在 cache TTL 内几乎不变，prefix cache 命中率高。
- **动态上下文不持久化**：Pin/Evolution/replan/orchestrator_ctx 每轮从最新数据源重新构建注入消息列表，不写入 `memory_messages`，避免跨轮重复。
- **对话链持久化**：HumanMessage + AIMessage + ToolMessage 通过 `ConversationMemoryStore` 存入 `conversation_memory.json`，保留最近 10 轮。
- **用户 query 移出系统提示词**：`{message}` 从 `REASON_PROMPT` 模板移除，只作为独立 `HumanMessage` 注入，不再重复发送。

**改动文件**：

| 文件 | 改动 |
|------|------|
| `src/orchestrator/memory/conversation_memory.py` | **新建** `ConversationMemoryStore`：JSON 格式存储，`messages_to_dict` / `messages_from_dict` 序列化，`_trim_to_turns` 保留最近 10 轮 |
| `src/orchestrator/planning/prompts.py` | 移除 `{pin_context}` / `{evolution_context}` / `{replan_section}` / `{orchestrator_context}` / `{message}` 5 个插槽及相关计算逻辑；`build_reason_prompt` 参数精简 |
| `src/orchestrator/planning/graph.py` | GraphState 新增 `pin_context` / `evolution_context` / `orchestrator_context` 字段；`skill_prepare_node` 提取动态上下文到 state；`reason_node` 按 SystemMessage/HumanMessage 语义注入消息列表；`save_mem_node` 持久化 memory_messages |
| `src/adapters/orchestrator.py` | `stream_chat` 中 `memory_messages` 从 `[]` 改为 `ConversationMemoryStore.load_messages()` |

**数据流对比**：

```
优化前：
  SystemMessage(身份+规则+Pin+Evolution+replan+群聊+工具+技能+用户query)  ← 全部混在一起
  + memory_messages([])
  + HumanMessage(用户query)                                              ← 重复！

优化后：
  SystemMessage(身份+规则+工具+技能)    ← 静态，高 cache 命中
  SystemMessage(Pin)                    ← 动态，独立消息
  SystemMessage(Evolution)              ← 动态，独立消息
  HumanMessage(replan/群聊)             ← 动态，独立消息
  memory_messages(持久化对话链)          ← 跨轮保留
  HumanMessage(用户query)               ← 只发一次
```

**效果**：

- 系统提示词变为纯静态内容，LLM prompt cache 命中率大幅提升
- 动态上下文作为独立消息注入，变化时只影响自身，不拖累静态前缀
- Orchestrator 可跨轮回忆推理历史，规划连贯性提升
- 用户 query 不再重复发送，节省 token
