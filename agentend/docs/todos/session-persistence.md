# TODO: Session 存储从本地文件迁移到持久化存储

## 当前状态

Session 映射存储在本地 JSON 文件 `logs/session_mappings.json`，仅适合单实例开发环境。

## 待办事项

### 1. 将 SessionMappingStore 替换为抽象接口

当前 `SessionMappingStore` 直接读写文件。需要抽象出接口，支持多种后端实现：

```python
class SessionMappingStore(Protocol):
    def get_cli_session_id(self, session_id: str) -> str | None: ...
    def set_cli_session_id(self, session_id: str, cli_session_id: str) -> None: ...
    def delete(self, session_id: str) -> None: ...
```

### 2. 实现 Redis 后端

- Key 格式：`agentend:session:{session_id}` → `cli_session_id`
- 支持设置 TTL（可选）
- 通过环境变量配置连接

### 3. 实现 MySQL 后端

- 表结构：`session_mappings(request_session_id, cli_session_id, created_at, updated_at)`
- 通过配置文件或环境变量配置连接

### 4. 配置化选择存储后端

在 `src/app/config.py` 的 Settings 中添加：

```python
SESSION_STORE_BACKEND: str = "file"  # file | redis | mysql
SESSION_STORE_REDIS_URL: str = "redis://localhost:6379"
SESSION_STORE_MYSQL_URL: str = ""
```

### 5. 修复：不传 session_id 时也应建立映射

当前不传 `session_id` 时不会建立 CLI session 映射，返回的内部 UUID 无法用于后续调用。需要统一处理，使所有调用都走完整的映射流程。
