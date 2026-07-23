import os
from pathlib import Path

from dotenv import load_dotenv
from pydantic import BaseModel, model_validator
from pydantic_settings import BaseSettings, SettingsConfigDict
from pydantic_settings.sources import YamlConfigSettingsSource

# 基于 __file__ 定位 config.yaml 和 .env，不依赖 CWD，任何目录启动都能找到
_CONFIG_PATH = Path(__file__).resolve().parent.parent.parent / "config.yaml"
_ENV_PATH = Path(__file__).resolve().parent.parent.parent / ".env"

# 启动时加载 .env 到 os.environ，供 LlmConfig.model_validator 读取
load_dotenv(_ENV_PATH)


class CorsConfig(BaseModel):
    origins: list[str]
    credentials: bool
    methods: list[str]
    headers: list[str]


class ServerConfig(BaseModel):
    host: str
    port: int
    reload: bool
    cors: CorsConfig


class AppConfig(BaseModel):
    title: str
    version: str


class WorkspaceConfig(BaseModel):
    base_dir: str
    cleanup_interval: int
    store_path: str
    git_default_branch: str


class SessionConfig(BaseModel):
    store_path: str


class DatabaseConfig(BaseModel):
    # 字段保留 YAML 注入的默认值；.env 中存在对应变量时强制覆盖（.env 优先级最高）
    host: str = ""
    port: int = 0
    user: str = ""
    password: str = ""
    dbname: str = ""

    @model_validator(mode="after")
    def resolve_from_env(self) -> "DatabaseConfig":
        # .env 变量存在（且非空）则覆盖，否则沿用 config.yaml 的默认值
        # 变量名与 backend 对齐（MYSQL_HOST / MYSQL_PORT / ...），便于跨端统一注入
        if os.environ.get("MYSQL_HOST"):
            self.host = os.environ["MYSQL_HOST"]
        if os.environ.get("MYSQL_PORT"):
            self.port = int(os.environ["MYSQL_PORT"])
        if os.environ.get("MYSQL_USER"):
            self.user = os.environ["MYSQL_USER"]
        if os.environ.get("MYSQL_PASSWORD"):
            self.password = os.environ["MYSQL_PASSWORD"]
        if os.environ.get("MYSQL_DBNAME"):
            self.dbname = os.environ["MYSQL_DBNAME"]
        return self


class BackendConfig(BaseModel):
    url: str = "http://localhost:8080"


class ExecutionConfig(BaseModel):
    max_turns: int
    timeout: int
    process_terminate_timeout: float


class SkillsConfig(BaseModel):
    builtin_dir: str
    block_marker: str = "aka_yhy"
    manifest: dict

    @property
    def builtin_dir_resolved(self) -> Path:
        """Resolve builtin_dir to absolute path relative to agentend project root."""
        p = Path(self.builtin_dir)
        if p.is_absolute():
            return p
        return _CONFIG_PATH.parent / p


class AgentPathsConfig(BaseModel):
    config_path: str = ""


AgentsConfig = dict[str, AgentPathsConfig]


class OrchestratorConfig(BaseModel):
    llm_request_timeout: float = 1200.0
    ask_agent_timeout: float = 180.0
    ask_agent_stream_chunk_timeout: float = 30.0
    review_timeout: float = 600.0
    reason_max_iterations: int = 10
    replan_max_iterations: int = 3
    skill_execution_timeout: float = 30.0


class LlmConfig(BaseModel):
    model: str = ""
    base_url: str = ""
    api_key: str = ""

    @model_validator(mode="after")
    def resolve_from_env(self) -> "LlmConfig":
        if not self.model:
            self.model = os.environ.get("DS_MODEL", "deepseek-chat")
        if not self.base_url:
            self.base_url = os.environ.get("DS_BASE_URL", "https://api.deepseek.com")
        if not self.api_key:
            self.api_key = os.environ.get("DS_API_KEY", "")
        return self


class Settings(BaseSettings):
    server: ServerConfig
    app: AppConfig
    workspace: WorkspaceConfig
    session: SessionConfig
    database: DatabaseConfig
    execution: ExecutionConfig
    backend: BackendConfig = BackendConfig()
    skills: SkillsConfig
    orchestrator: OrchestratorConfig = OrchestratorConfig()
    agents: AgentsConfig = {}
    llm: LlmConfig

    model_config = SettingsConfigDict(yaml_file=str(_CONFIG_PATH))

    @classmethod
    def settings_customise_sources(
        cls,
        settings_cls,
        init_settings,
        env_settings,
        dotenv_settings,
        file_secret_settings,
    ):
        # 纯 YAML 驱动，禁用环境变量 / dotenv / file secret 等默认源
        return (init_settings, YamlConfigSettingsSource(settings_cls))


# 模块加载时立即实例化，config.yaml 缺失或字段校验失败会直接崩溃退出（fail-fast）
settings = Settings()
