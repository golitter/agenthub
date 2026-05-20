from src.adapters.claude import ClaudeCodeAdapter
from src.adapters.opencode import OpenCodeAdapter
from src.adapters.registry import AdapterRegistry
from src.app.config import settings
from src.rules.builtin import SafetyRule, ScopeRule
from src.rules.engine import RuleEngine
from src.session.manager import SessionManager
from src.session.store import SessionMappingStore
from src.workspace.manager import WorkspaceManager
from src.workspace.store import JsonFileWorkspaceStore


def create_adapter_registry() -> AdapterRegistry:
    registry = AdapterRegistry()
    registry.register("claude-code", ClaudeCodeAdapter)
    registry.register("opencode", OpenCodeAdapter)
    return registry


def create_session_manager() -> SessionManager:
    return SessionManager()


def create_session_store() -> SessionMappingStore:
    return SessionMappingStore()


def create_rule_engine() -> RuleEngine:
    rules = [SafetyRule(), ScopeRule()]
    return RuleEngine(rules)


def create_workspace_manager() -> WorkspaceManager:
    store = JsonFileWorkspaceStore()
    return WorkspaceManager(store, ttl_seconds=settings.WORKSPACE_TTL_SECONDS)
