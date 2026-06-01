# CodeGraph & OpenSpec

> 参考：[CodeGraph 深度解析](https://zhuanlan.zhihu.com/p/2043160358018348348)

## CodeGraph — 代码知识图谱 MCP 服务器

基于 SQLite + tree-sitter 的本地代码知识图谱，让 Agent 直接查询预构建索引，无需从零探索代码库。

**常用工具：**

| 工具 | 用途 |
|------|------|
| `codegraph_context` | 任务全景上下文（主工具，优先用） |
| `codegraph_trace` | 调用链追踪：从 A 到 B 的完整路径 |
| `codegraph_impact` | 变更影响分析 |
| `codegraph_search` | 按名称搜索符号 |
| `codegraph_files` | 项目文件树 |

## OpenSpec — 规格驱动开发（SDD）

先写规格再写代码的工作流，通过 5 个 Skill 完成：

```
/openspec-explore    → 探索/澄清需求
/openspec-propose    → 创建提案（设计 + 规格 + 任务）
/openspec-apply      → 按任务列表实施
/openspec-verify     → 验证实现是否匹配规格
/openspec-archive    → 归档已完成的变更
```
