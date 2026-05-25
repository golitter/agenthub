from fastapi import Request

from src.adapters.registry import AdapterRegistry
from src.preview.server import PreviewManager
from src.rules.engine import RuleEngine
from src.session.manager import SessionManager
from src.session.store import SessionMappingStore
from src.workspace.manager import WorkspaceManager


def get_session_manager(request: Request) -> SessionManager:
    return request.app.state.session_manager


def get_adapter_registry(request: Request) -> AdapterRegistry:
    return request.app.state.adapter_registry


def get_rule_engine(request: Request) -> RuleEngine:
    return request.app.state.rule_engine


def get_session_store(request: Request) -> SessionMappingStore:
    return request.app.state.session_store


def get_workspace_manager(request: Request) -> WorkspaceManager:
    return request.app.state.workspace_manager


def get_preview_manager(request: Request) -> PreviewManager:
    return request.app.state.preview_manager
