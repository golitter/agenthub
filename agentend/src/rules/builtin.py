from src.rules.base import BaseRule

# Tools considered dangerous; blocked by SafetyRule
_DANGEROUS_TOOLS = {"dangerouslyDisableSandbox"}


class SafetyRule(BaseRule):
    name = "safety"
    description = "Injects safety constraints and blocks dangerous tools"
    phase = "pre"
    priority = 10

    def check(self, context: dict) -> bool:
        return True

    def enforce(self, context: dict) -> dict:
        existing_tools = set(context.get("allowed_tools", []))
        safe_tools = list(existing_tools - _DANGEROUS_TOOLS) if existing_tools else []

        result: dict = {
            "system_prompt_append": (
                "You are operating in a managed environment. "
                "Do not execute destructive commands (rm -rf, format, etc.). "
                "Do not access or modify system files outside the workspace."
            ),
        }
        if safe_tools:
            result["allowed_tools"] = safe_tools
        return result


class ScopeRule(BaseRule):
    name = "scope"
    description = "Validates workspace_path and injects scope constraints"
    phase = "pre"
    priority = 5

    def check(self, context: dict) -> bool:
        workspace_path = context.get("workspace_path")
        if not workspace_path:
            return True  # No workspace constraint is OK
        # Basic validation: workspace_path must be an absolute path
        return workspace_path.startswith("/")

    def enforce(self, context: dict) -> dict:
        workspace_path = context.get("workspace_path")
        if workspace_path:
            return {
                "system_prompt_append": f"Only modify files under: {workspace_path}",
            }
        return {}


class TaskctlRule(BaseRule):
    name = "taskctl"
    description = "Injects taskctl merge instruction when merge keywords detected"
    phase = "pre"
    priority = 3

    _MERGE_KEYWORDS = ("合并", "merge", "git merge")

    def check(self, context: dict) -> bool:
        return True

    def enforce(self, context: dict) -> dict:
        workspace_path = context.get("workspace_path", "")
        message = context.get("message", "").lower()
        if not workspace_path or not any(kw in message for kw in self._MERGE_KEYWORDS):
            return {}

        agent_type = context.get("agent_type")
        agent_dir = ".claude" if agent_type != "opencode" else ".opencode"
        taskctl_path = f"{workspace_path}/{agent_dir}/skills/taskctl/taskctl"

        return {
            "system_prompt_append": (
                f"合并分支时必须使用 `{taskctl_path} merge`，"
                "它会自动提交未保存改动、合并到 task 分支、切回 agent 分支。"
                "不要手动执行 git merge。"
            ),
        }


class SkillRule(BaseRule):
    name = "skill"
    description = "Injects output skill prompt"
    phase = "pre"
    priority = 1

    def check(self, context: dict) -> bool:
        return True

    def enforce(self, context: dict) -> dict:
        return {
            "system_prompt_append": (
                "## 输出技能\n"
                "\n"
                "workspace 中有 `render` 工具，可生成富媒体卡片（HTML 渲染、图片、附件、diff、预览）。\n"
                "需要时调用 `./render <子命令>`，将 stdout 包含在回复中。详情见 SKILL.md。"
            ),
        }
