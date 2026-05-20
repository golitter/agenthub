from pydantic_settings import BaseSettings


class Settings(BaseSettings):
    CLAUDE_CLI_PATH: str = "claude"
    OPENCODE_CLI_PATH: str = "opencode"
    WORKSPACE_BASE_DIR: str = "./worktrees"
    DEFAULT_MAX_TURNS: int = 20
    EXECUTION_TIMEOUT: int = 300
    WORKSPACE_TTL_SECONDS: int = 3600
    WORKSPACE_TTL_CHECK_INTERVAL: int = 300
    HOST: str = "0.0.0.0"
    PORT: int = 8001

    model_config = {"env_prefix": "", "case_sensitive": True}


settings = Settings()
