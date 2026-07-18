# Orchestrator Merge 冲突处理机制 — 三道防线

## 实现了什么

当多个 Agent 并行工作并修改了相同文件时，最终合并分支会产生 Git merge conflict。Orchestrator 通过**三道防线**处理这类冲突：

1. **Git Worktree 物理隔离**（预防）— 每个 Agent 在独立 worktree 目录工作，从源头尽量避免冲突
2. **git merge --abort + Replan 循环**（自动修复）— 合并失败后中止，Orchestrator 重新规划任务拆解来规避冲突
3. **Aggregator 报告 + 人工介入**（兜底）— 重试耗尽后生成包含成功/失败状态的报告，由用户裁决

本系统**不做自动三路合并**、不做 LLM 辅助解决冲突——这是多 Agent 系统中的务实取舍。

## 怎么实现的

冲突处理按**三道防线**递进实现（预防 → 自动修复 → 兜底），下文依次展开涉及模块、三道防线机制、完整示例与配置。

## 涉及模块

```
冲突发生链路：

  Agent A worktree ──┐                     ┌── Agent B worktree
                     ▼                     ▼
              git merge (task branch)  ←  同一 task branch
                     │
              merge 冲突! → git_ops.merge_branch()
                     │
                     ▼
              Workspace API → task_result.success = false
                     │
                     ▼
              Orchestrator REVIEW Node → needs_replan = true
                     │
                     ▼
              skill_prepare → reason (重规划) → dispatch → execute → review
                     │
               (循环最多 replan_max_iterations 次)
                     │
                     ▼
              Aggregator → 生成报告 → 人工处理
```

## 相关文件

| 文件 | 职责 |
|---|---|
| `src/workspace/git_ops.py` | Git 底层操作，`merge_branch()` 执行合并/冲突中止 |
| `src/orchestrator/planning/graph.py` | LangGraph 状态机，`review_node()` 检测失败并触发重规划 |
| `src/orchestrator/reporting/aggregator.py` | `Aggregator` 汇总成功/失败结果，生成报告 |
| `src/adapters/orchestrator.py` | `OrchestratorAdapter` 编排完整流程，驱动 execute 和 review |
| `src/orchestrator/execution/engine.py` | `ExecutionEngine` 按波次执行任务，收集 task_result |
| `src/workspace/manager.py` | `WorkspaceManager.merge()` 调用 git_ops 并向上报告结果 |

---

## 第一道防线：Git Worktree 物理隔离（预防）

### 分支结构

采用两级分支策略避免 Agent 直接操作仓库默认分支：

```
<default-branch>
  └── task/task-123                              ← 集成分支（from detected default branch）
        ├── agent/sess-aaa/task-123              ← Agent A 的独立分支 + worktree
        └── agent/sess-bbb/task-123              ← Agent B 的独立分支 + worktree
```

### 目录隔离

每个 Agent 拥有独立的物理工作目录：

```
/repos/
  ├── project/                          ← 主仓库（检测到的默认分支）
  └── worktrees/
        └── task-123/
              ├── sess-aaa/             ← Agent A 的 worktree
              └── sess-bbb/             ← Agent B 的 worktree
```

### 合并顺序

分两步走，避免多 Agent 并发操作 main：

```
agent/sess-aaa/task-123 → task/task-123   (Agent → 任务内集成，默认行为)
agent/sess-bbb/task-123 → task/task-123   (Agent → 任务内集成，默认行为)
task/task-123            → main           (任务 → 主分支，显式触发)
```

当两个 Agent 修改了**不同文件**时，两步合并都能顺利完成。只有当多个 Agent 修改了**同一文件的同一区域**时，才会在 `agent → task` 这一步产生冲突。

> 详见 [08-workspace.md](08-workspace.md)。

---

## 第二道防线：merge --abort + Replan 循环（自动修复）

### Step 1: Git 层面 — 合并失败立即中止

`merge_branch()` 的核心策略：**merge 失败 → 立即 `--abort`，不做任何自动解决**。

```python
# src/workspace/git_ops.py

async def merge_branch(self, repo_path: str, branch: str, target: str = "main") -> bool:
    # 记录当前分支
    ok, current = await self._run_git("rev-parse", "--abbrev-ref", "HEAD", cwd=repo_path)
    if not ok:
        return False

    # 切到目标分支
    ok, _ = await self._run_git("checkout", target, cwd=repo_path)
    if not ok:
        return False

    # 尝试合并
    ok, err = await self._run_git("merge", branch, cwd=repo_path)
    if not ok:
        # 关键：冲突时不解决，直接 abort
        await self._run_git("merge", "--abort", cwd=repo_path)

    # 切回原分支
    await self._run_git("checkout", current.strip(), cwd=repo_path)
    return ok  # False = 合并失败
```

**安全性保证**：
- `merge --abort` 确保 target 分支（如 `task/task-123`）不会被冲突标记污染
- Agent 分支（如 `agent/sess-aaa/task-123`）保持原样，改动不丢失
- 返回 `False` 让上层知道合并失败了

### Step 2: 失败冒泡 → task_result 标记

`WorkspaceManager.merge()` 调用 `git_ops.merge_branch()` 后，结果会传递到 ExecutionEngine：

```python
# 简化的冒泡路径
git_ops.merge_branch()          → 返回 False
    ↓
WorkspaceManager.merge()        → 返回 False
    ↓
ExecutionEngine                 → 记录 task_result = {
                                    "success": False,
                                    "task_id": "task-001",
                                    "agent": "claude-code",
                                    "content": "merge conflict: ..."
                                  }
```

### Step 3: REVIEW Node — 冲突检测与重规划决策

`review_node()` 是冲突处理的核心决策点：

```python
# src/orchestrator/planning/graph.py

def review_node(state: GraphState) -> dict:
    task_results = state.get("task_results", [])
    failed = [tr for tr in task_results if not tr.get("success", True)]

    iteration = state.get("iteration", 0)
    max_iterations = state.get("max_iterations", settings.orchestrator.replan_max_iterations)

    # 路径 1: 全部成功 → 结束
    if not failed:
        return {"needs_replan": False, "replan_reason": ""}

    # 路径 2: 有失败 + 已达重试上限 → 妥协，接受部分结果
    if iteration >= max_iterations:
        logger.warning("Review: max_iterations=%d reached, accepting partial results", max_iterations)
        return {"needs_replan": False, "replan_reason": ""}

    # 路径 3: 有失败 + 未超上限 → 触发重规划
    failure_details = []
    for tr in failed:
        failure_details.append(
            f"- 任务 {tr.get('task_id', '?')} (agent: {tr.get('agent', '?')}): {tr.get('content', '')[:200]}"
        )
    replan_reason = "以下任务执行失败，请重新规划：\n" + "\n".join(failure_details)
    return {"needs_replan": True, "replan_reason": replan_reason, "iteration": 1}
```

三条路径的决策逻辑：

| 情况 | `needs_replan` | 后续动作 |
|---|---|---|
| 全部成功 | `False` | → EVOLVE → SAVE_MEM → **结束** |
| 有失败 + 未超上限 | `True` | → **回到 SKILL_PREPARE 重规划** |
| 有失败 + 已达上限 | `False` | → EVOLVE → SAVE_MEM → **妥协接受** |

### Step 4: 条件路由 — 回到起点重新规划

```python
# src/orchestrator/planning/graph.py

def route_by_review(state: GraphState) -> str:
    if state.get("needs_replan", False):
        return "skill_prepare"    # ← 回到起点，重新规划
    return "evolve"               # ← 成功或妥协：走向结束
```

### Step 5: REASON Node — 接收重规划请求

重规划请求会作为额外消息注入 LLM：

```python
# src/orchestrator/planning/graph.py (reason_node 内部)

if state.get("replan_reason"):
    messages.append(
        HumanMessage(
            content=f"[重规划请求] 以下任务执行失败，请重新规划：\n{state['replan_reason']}"
        )
    )
```

LLM 会根据失败信息（哪个任务失败了、失败原因是什么）重新规划任务拆解——可能：
- **换一个 Agent** 执行该任务
- **拆得更细**，将冲突文件分给不同 Agent
- **调整文件分工**，从根源规避冲突

### 完整的 LangGraph 状态机流转

```
skill_prepare → reason ──── [text → save_mem → END]
                     │
                   [plan]
                     │
                     ▼
                  dispatch → execute → review
                                        │
                                  needs_replan?
                                   │          │
                                  Yes         No
                                   │          │
                                   ▼          ▼
                          skill_prepare    evolve → save_mem → END
                               │
                               ▼
                            reason (带 replan_reason)
                               │
                             [plan]
                               │
                               ▼
                            dispatch → execute → review
                                              │
                                        (循环，最多 max_iterations 次)
```

---

## 第三道防线：Aggregator 报告 + 人工介入（兜底）

当重试次数耗尽（`iteration >= max_iterations`），系统不再重规划，进入 **Aggregator** 生成最终报告。

### Aggregator 汇总

```python
# src/orchestrator/reporting/aggregator.py

class Aggregator:
    async def aggregate(self, results: list[TaskResult], overview: str) -> str:
        # 使用 LLM 生成结构化报告
        # 包含：整体完成情况、各任务关键产出、后续建议
```

### 最终摘要块

```python
def build_final_summary_block(results: list[TaskResult]) -> str:
    completed = sum(1 for result in results if result.success)
    failed = len(results) - completed
    status = "success" if failed == 0 else "failed" if completed == 0 else "partial"
    next_action = (
        "可以继续验收结果。"
        if failed == 0
        else "请优先重试或人工检查失败任务，再合并最终结果。"
    )
    # ... 生成 JSON payload
```

报告会明确标注每个任务的成功/失败状态，并给出 `nextAction` 提示，引导用户人工处理冲突。

**报告示例**：

```
```aka_yhy
type: final_summary
json: {"status":"partial","completed":1,"failed":1,"nextAction":"请优先重试或人工检查失败任务，再合并最终结果。","details":[{"task_id":"task-001","agent":"claude-code","status":"completed","summary":"实现登录页面..."},{"task_id":"task-002","agent":"opencode","status":"failed","summary":"merge conflict: README.md"}]}
```

## 整体完成情况
...
## 各任务关键产出
- task-001 (claude-code): ✅ 完成
- task-002 (opencode): ❌ 失败 — merge conflict
## 后续建议
建议先手动解决 README.md 的合并冲突，再重新执行 task-002。
```

---

## 完整示例：冲突从发生到解决的流程

### 场景：两个 Agent 同时修改 `README.md`

```
初始状态：
  main: README.md = "# Project"

Step 1: Orchestrator 拆分任务
  task-001 → claude-code: 修改 README.md 添加使用说明
  task-002 → opencode:    修改 README.md 添加安装指南

Step 2: 两个 Agent 在各自 worktree 并行工作
  agent/sess-aaa/task-001: README.md = "# Project\n\n## Usage\n..."
  agent/sess-bbb/task-002: README.md = "# Project\n\n## Install\n..."

Step 3: claude-code 先合并成功
  git merge agent/sess-aaa/task-001 → task/task-xxx ✅
  task branch: README.md = "# Project\n\n## Usage\n..."

Step 4: opencode 合并时冲突
  git merge agent/sess-bbb/task-002 → CONFLICT (README.md)
  git merge --abort                    ← 安全回滚
  → task_result.success = False

Step 5: REVIEW Node 检测到失败
  needs_replan = True, iteration = 1 < max_iterations

Step 6: REASON Node 重新规划
  LLM 看到失败原因后决定：
  - task-003 → claude-code: 仅修改 docs/install.md
  - task-004 → opencode:    仅修改 docs/usage.md
  （通过拆分文件从根源规避冲突）

Step 7: 重新执行 → 全部成功 → Aggregator 生成报告 → 结束
```

如果 Step 6 重规划后仍然失败，循环继续直到 `max_iterations`，最终由人工处理。

---

## 配置项

| 配置 | 位置 | 默认值 | 说明 |
|---|---|---|---|
| `orchestrator.replan_max_iterations` | `config.yaml` | 3 | REVIEW 节点最大重规划次数 |
| `orchestrator.reason_max_iterations` | `config.yaml` | - | REASON 节点 LLM tool-calling 最大循环次数 |
| `orchestrator.llm_request_timeout` | `config.yaml` | - | LLM 请求超时（秒） |

---

## 已知限制与后续方向

1. **不做自动冲突解决**：merge 冲突时直接 abort，不尝试三路合并或 LLM 辅助修冲突。后续可考虑引入 LLM-based conflict resolution
2. **重规划依赖 LLM 理解力**：REASON Node 能否合理规避冲突，完全取决于 LLM 是否正确理解了失败原因
3. **无冲突预检**：合入 task branch 前无法预知是否冲突，只有执行 merge 才能发现
4. **Aggregator 不做冲突合并**：只汇总报告，不尝试融合多个 Agent 的冲突输出
5. **per-task 锁粒度**：同一 task 下的 merge 操作串行化，不同 task 之间并行——但同一 task 内的 Agent 仍可能产生冲突

## 相关文档

- [08-workspace.md](08-workspace.md) — Workspace 多 Agent 隔离实现（分支结构、worktree 管理）
- [11-orchestrator-planning.md](11-orchestrator-planning.md) — Orchestrator 规划 + 闭环编排实现
- [../testing/03-taskctl-merge.md](../testing/03-taskctl-merge.md) — taskctl merge 命令测试（含冲突路径测试用例）
