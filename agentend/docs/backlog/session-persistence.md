# TODO: Session 存储从本地文件迁移到持久化存储

## 当前状态

Session 映射存储在本地 JSON 文件（路径来自 `config.yaml` 的 `session.store_path`），使用 `session_id::task_id` 复合键。仅适合单实例开发环境。

## 待办事项

### 1. 将 SessionMappingStore 替换为抽象接口（Protocol）

当前 `SessionMappingStore` 是一个具体类（直接读写 JSON 文件）。需要抽象出接口，支持多种后端实现：

```python
class SessionMappingStoreProtocol(Protocol):
    def get_cli_session_id(self, session_id: str, task_id: str = "") -> str | None: ...
    async def set_cli_session_id(self, session_id: str, cli_session_id: str, task_id: str = "") -> None: ...
    async def delete(self, session_id: str, task_id: str = "") -> None: ...
```

### 2. 实现 Redis 后端

- Key 格式：`agentend:session:{session_id}` → `cli_session_id`
- 支持设置 TTL（可选）
- 通过环境变量配置连接

### 3. 实现 MySQL 后端

- 表结构：`session_mappings(request_session_id, cli_session_id, created_at, updated_at)`
- 通过配置文件或环境变量配置连接

### 4. 配置化选择存储后端

在 `config.yaml` 中添加 session 存储后端配置：

```yaml
session:
  store_path: logs/session_mappings.json
  store_backend: file  # file | redis | mysql
  store_redis_url: redis://localhost:6379
  store_mysql_url: ""
```

### 5. ~~修复：不传 session_id 时也应建立映射~~（已解决）

`AgentRequest.session_id` 现为必填（`min_length=1`，见 `src/schemas/request.py`），不传 `session_id` 的请求会在参数校验阶段被拒绝（422），不再出现"内部 UUID 无法用于后续调用"的路径。
