# Rule Engine — 规则引擎

## 实现了什么

可插拔规则系统，在 Agent 请求执行前进行安全检查和约束注入。规则通过修改 CLI 参数控制 Agent 行为，不侵入 Agent 内部逻辑。

## 怎么实现的

### BaseRule (`src/rules/base.py`)

抽象基类，定义规则接口：

```python
class BaseRule(ABC):
    name: str = ""           # 规则名称
    description: str = ""    # 描述
    phase: str = "pre"       # 阶段："pre" | "post"
    priority: int = 0        # 优先级，数值越大越先执行

    def check(context: dict) -> bool     # 校验是否通过
    def enforce(context: dict) -> dict   # 通过后注入约束
```

### RuleRegistry (`src/rules/registry.py`)

简单的注册表，支持动态注册和查找规则实例。

### RuleEngine (`src/rules/engine.py`)

执行核心逻辑：

```
evaluate(context) → (bool, dict)
```

1. 按 `priority` 降序排列所有 Rule
2. 对每个 Rule 依次执行：
   - `check(context)` → 如果返回 `False`，立即终止，返回 `(False, {"error": ..., "rule": ...})`
   - `enforce(context)` → 如果通过，合并约束结果
3. 全部通过返回 `(True, merged_context)`

合并规则：
- `system_prompt_append`：追加合并（所有 Rule 的提示词拼接）
- `allowed_tools`：追加合并
- `max_turns`：取第一个非 None 值
- `blocked`：任一 Rule 阻断则整体失败

### 内置规则 (`src/rules/builtin.py`)

#### SafetyRule（priority=10）

- `check`：始终通过
- `enforce`：
  - 注入安全提示词："You are operating in a managed environment..."
  - 从 `allowed_tools` 中过滤危险工具（如 `dangerouslyDisableSandbox`）

#### ScopeRule（priority=5）

- `check`：
  - `workspace_path` 为空 → 通过（不限制）
  - `workspace_path` 非绝对路径 → 失败
  - 绝对路径 → 通过
- `enforce`：
  - 注入约束："Only modify files under: {workspace_path}"

#### TaskctlRule（priority=3）

- `check`：始终通过
- `enforce`：当消息包含合并关键词（"合并"、"merge"、"git merge"）且有 workspace_path 时，注入 taskctl merge 指令
  - 根据 agent_type 选择 `.claude` 或 `.opencode` 配置目录
  - 注入提示："合并分支时必须使用 `{taskctl_path} merge`..."

#### SkillRule（priority=1）

- `check`：始终通过
- `enforce`：注入输出技能提示，告知 Agent workspace 中有 `render` 工具，可生成富媒体卡片（HTML 渲染、图片、附件、diff、预览）

#### SoulRule（priority=8）

- `check`：始终通过
- `enforce`：当 workspace_path 和 agent_type 存在且对应配置目录下有 `SOUL.md` 文件时，读取其内容作为身份文档注入到 system prompt
  - 根据 agent_type 选择配置目录（如 `.claude` / `.opencode`）
  - 注入格式：`## 你的身份文档 (SOUL.md)\n\n{content}`

#### GroupChatRule（priority=6）

- `check`：始终通过
- `enforce`：当请求中携带 `group_chat_messages`（来自其他 Agent 的上下文消息）时，通过 `build_group_chat_context()` 构建跨 Agent 对话上下文，注入到 system prompt
  - 用于 Orchestrator 多 Agent 协作场景，让每个 Agent 了解其他 Agent 的对话窗口
  - 不含 group_chat_messages 时返回空 dict，无副作用

### 约束注入流程

```
AgentRequest
  ↓
RuleEngine.evaluate(context)
  ↓
SafetyRule.enforce → system_prompt_append: "You are operating in a managed environment..."
                    allowed_tools: [...]
  ↓
SoulRule.enforce → system_prompt_append: "## 你的身份文档 (SOUL.md)\n\n{content}"
  ↓
GroupChatRule.enforce → system_prompt_append: "{cross_agent_context}"
  ↓
ScopeRule.enforce  → system_prompt_append: "Only modify files under: /workspace"
  ↓
TaskctlRule.enforce → system_prompt_append: "合并分支时必须使用 taskctl merge..."
  ↓
SkillRule.enforce → system_prompt_append: "workspace 中有 render 工具..."
  ↓
合并结果 → 传入 Adapter._build_command()
  ↓
CLI 参数: --append-system-prompt "managed environment...\n身份文档...\n跨Agent上下文...\nOnly modify...\n合并时..."
          --allowedTools Read,Write
```
