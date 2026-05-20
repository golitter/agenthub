from fastapi import Request

from src.adapters.registry import AdapterRegistry
from src.rules.engine import RuleEngine
from src.session.manager import SessionManager
from src.session.store import SessionMappingStore


def get_session_manager(request: Request) -> SessionManager:
    return request.app.state.session_manager


def get_adapter_registry(request: Request) -> AdapterRegistry:
    return request.app.state.adapter_registry


def get_rule_engine(request: Request) -> RuleEngine:
    return request.app.state.rule_engine


def get_session_store(request: Request) -> SessionMappingStore:
    return request.app.state.session_store
