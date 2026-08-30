from src.rules.base import BaseRule


class RuleEngine:
    def __init__(self, rules: list[BaseRule] | None = None) -> None:
        self._rules: list[BaseRule] = sorted(rules or [], key=lambda r: r.priority, reverse=True)

    def evaluate(self, context: dict) -> tuple[bool, dict]:
        merged: dict = {
            "system_constraints": [],
            "active_pins": [],
            "reference_context": [],
            "capability_hints": [],
            "allowed_tools": None,
            "max_turns": None,
        }

        for rule in self._rules:
            if not rule.check(context):
                return False, {"error": f"Rule '{rule.name}' check failed", "rule": rule.name}

            result = rule.enforce(context)

            for key in (
                "system_constraints",
                "active_pins",
                "reference_context",
                "capability_hints",
            ):
                value = result.get(key)
                if value:
                    if isinstance(value, list):
                        merged[key].extend(value)
                    else:
                        merged[key].append(value)

            if "allowed_tools" in result and result["allowed_tools"] is not None:
                allowed = list(dict.fromkeys(result["allowed_tools"]))
                if merged["allowed_tools"] is None:
                    merged["allowed_tools"] = allowed
                else:
                    allowed_set = set(allowed)
                    merged["allowed_tools"] = [
                        name for name in merged["allowed_tools"] if name in allowed_set
                    ]
            if result.get("max_turns") is not None and merged["max_turns"] is None:
                merged["max_turns"] = result["max_turns"]
            if result.get("blocked"):
                return False, {"error": result.get("error", "blocked by rule"), "rule": rule.name}

        return True, merged
