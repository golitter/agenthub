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

## 🔲 优化 3：（待定）
