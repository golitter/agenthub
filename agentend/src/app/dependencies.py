from src.adapters.claude import ClaudeCodeAdapter
from src.adapters.codex import CodexAdapter
from src.adapters.opencode import OpenCodeAdapter
from src.adapters.orchestrator import OrchestratorAdapter
from src.adapters.registry import AdapterRegistry
from src.app.config import settings
from src.preview.server import PreviewManager
from src.rules.builtin import SafetyRule, ScopeRule, SkillRule, TaskctlRule
from src.rules.engine import RuleEngine
from src.schemas.request import AgentType
from src.session.manager import SessionManager
from src.session.store import SessionMappingStore
from src.workspace.db import DBReader
from src.workspace.manager import WorkspaceManager
from src.workspace.store import JsonFileWorkspaceStore


def create_adapter_registry() -> AdapterRegistry:
    registry = AdapterRegistry()
    registry.register(AgentType.CLAUDE_CODE, ClaudeCodeAdapter)
    registry.register(AgentType.OPENCODE, OpenCodeAdapter)
    registry.register(AgentType.ORCHESTRATOR, OrchestratorAdapter)
    registry.register(AgentType.CODEX, CodexAdapter)
    return registry


def create_session_manager() -> SessionManager:
    return SessionManager()


def create_session_store() -> SessionMappingStore:
    return SessionMappingStore()


def create_rule_engine() -> RuleEngine:
    rules = [SafetyRule(), ScopeRule(), TaskctlRule(), SkillRule()]
    return RuleEngine(rules)


def create_workspace_manager() -> WorkspaceManager:
    store = JsonFileWorkspaceStore()
    return WorkspaceManager(store)


def create_preview_manager() -> PreviewManager:
    return PreviewManager()


def create_db_reader() -> DBReader:
    return DBReader(
        host=settings.database.host,
        port=settings.database.port,
        user=settings.database.user,
        password=settings.database.password,
        db=settings.database.dbname,
    )
