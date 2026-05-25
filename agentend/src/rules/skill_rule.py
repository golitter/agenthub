from src.rules.base import BaseRule


class SkillRule(BaseRule):
    name = "skill"
    description = "Injects output skill prompt + workspace tool instructions"
    phase = "pre"
    priority = 1

    def check(self, context: dict) -> bool:
        return True

    def enforce(self, context: dict) -> dict:
        return {
            "system_prompt_append": (
                "## 输出技能\n"
                "\n"
                "workspace 中有 `render` 工具，提供 5 个子命令：html-render / image / attachment / diff / preview。\n"
                "调用后自动输出 aka_yhy 格式块，将 stdout 包含在回复中即可。详情见 render 的 SKILL.md。"
            ),
        }
