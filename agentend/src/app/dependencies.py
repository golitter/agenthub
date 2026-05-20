from src.adapters.claude import ClaudeCodeAdapter
from src.adapters.registry import AdapterRegistry
from src.rules.builtin import SafetyRule, ScopeRule
from src.rules.engine import RuleEngine
from src.session.manager import SessionManager
from src.session.store import SessionMappingStore


def create_adapter_registry() -> AdapterRegistry:
    registry = AdapterRegistry()
    registry.register("claude-code", ClaudeCodeAdapter)
    return registry


def create_session_manager() -> SessionManager:
    return SessionManager()


def create_session_store() -> SessionMappingStore:
    return SessionMappingStore()


def create_rule_engine() -> RuleEngine:
    rules = [SafetyRule(), ScopeRule()]
    return RuleEngine(rules)
