# Workspace 多 Agent 隔离实现

## 实现了什么

使用 git worktree 实现多 Agent 并行编码的物理目录隔离。每个 Agent 拥有独立的工作目录和分支，互不干扰。配合两级分支结构（`<default-branch> -> task/{task_id} -> agent/{session_id}/{task_id}`）、JSON 文件持久化存储、per-task 并发锁、启动恢复和 DB inactive 清理，构成完整的多 Agent 隔离方案。

解决了以下问题：

1. **分支污染**：所有 Agent branch 直接 merge 到仓库默认分支，多 Agent 并发会互相覆盖
2. **状态丢失**：workspace 状态纯内存存储，进程崩溃或重启后全部丢失，git worktree 变成无人管理的孤儿
3. **竞态条件**：create/merge/cleanup 无并发保护，高并发下 worktree 状态不一致
4. **资源泄漏**：无自动清理机制，worktree 目录和分支会无限积累占用磁盘

## 怎么实现的

## 分支结构设计

采用两级分支策略：`<default-branch> → task/{task_id} → agent/{session_id}/{task_id}`。默认分支由 `GitOps.default_branch()` 检测：优先使用本地可用的 `origin/HEAD` 目标分支，其次当前分支，再回退到本地 `main`/`master` 或第一个本地分支。

```
<default-branch>
  └── task/task-123                              ← 集成分支（from detected default branch）
        ├── agent/sess-aaa/task-123              ← Session A 的 Agent 独立分支 + worktree 目录
        └── agent/sess-bbb/task-123              ← Session B 的 Agent 独立分支 + worktree 目录
```

- **task branch**：同一 task 下所有 Agent 的公共父分支，从检测到的默认分支创建。作为任务内的集成分支
- **agent branch**：每个 Agent 的私有分支，从对应的 task branch 创建，拥有独立的 worktree 工作目录

### Merge 流程

分两步走，避免多 Agent 直接操作仓库默认分支：

```
agent/sess-aaa/task-123 → task/task-123   (Agent → 任务内集成，默认行为)
agent/sess-bbb/task-123 → task/task-123   (Agent → 任务内集成，默认行为)
task/task-123            → <default-branch> (任务 → 默认分支，显式触发)
```

### 目录结构

假设 `repo_path = /repos/project`，worktree 目录生成规则：

```
/repos/
  ├── project/                          ← 主仓库（检测到的默认分支）
  └── worktrees/
        └── task-123/
              ├── sess-aaa/             ← Session A 的 worktree
              └── sess-bbb/             ← Session B 的 worktree
```

## 模块结构

```
src/workspace/
  ├── __init__.py      # 模块导出
  ├── models.py        # 数据模型（Workspace、WorkspaceStatus）
  ├── store.py         # 持久化存储（WorkspaceStoreProtocol + JsonFileWorkspaceStore）
  ├── git_ops.py       # Git 底层操作封装（GitOps）
  ├── manager.py       # 核心管理器（WorkspaceManager）
  └── recovery.py      # 启动恢复（parse_worktree_list + recover_workspaces）
```

## 改动文件

| 文件 | 说明 |
|---|---|
| `src/workspace/models.py` | **修改** — 新增 `container_id` 字段、`task_branch_name()` 函数 |
| `src/workspace/store.py` | **新建** — 持久化存储抽象接口与 JSON 文件实现 |
| src/workspace/git_ops.py | **修改** — 新增 ensure_ready_repo、default_branch、	ask_branch_create、worktree_list，worktree_add 增加 ase_branch 参数 |
| `src/workspace/manager.py` | **重写** — 接受 store 参数，增加 per-task 锁、DB inactive 清理 |
| `src/workspace/recovery.py` | **新建** — 启动时 worktree 恢复与孤儿清理 |
| `src/workspace/db.py` | **新建** — DBReader 只读查询（inactive session + task 状态） |
| `src/app/config.py` | **修改** — 新增 database 配置分区 |
| `src/app/dependencies.py` | **修改** — 组装 store → manager 的依赖注入 |
| `src/app/main.py` | **修改** — lifespan 中加入恢复和 TTL 启动/停止 |
| `src/api/v1/workspace.py` | API 端点（已有，未修改） |

---

## 关键实现细节

### 1. 数据模型（`src/workspace/models.py`）

#### WorkspaceStatus

```python
class WorkspaceStatus(str, Enum):
    ACTIVE = "active"    # 正常工作中
    MERGED = "merged"    # 已合并到仓库默认分支
    CLEANED = "cleaned"  # 已清理（worktree 已删除）
```

#### Workspace dataclass

```python
@dataclass
class Workspace:
    id: str                    # UUID，自动生成
    task_id: str               # 任务 ID
    agent_name: str            # Agent 名称（如 frontend、backend）
    agent_type: AgentType | None  # Agent 类型（如 claude-code、opencode）
    repo_path: str             # 主仓库路径
    worktree_path: str         # worktree 目录路径（自动生成）
    branch_name: str           # agent 分支名（自动生成）
    session_id: str            # 关联的会话 ID
    container_id: str | None   # 容器 ID（预留，为后续 Docker 隔离用）
    status: WorkspaceStatus = WorkspaceStatus.ACTIVE    # 当前状态
    created_at: datetime       # 创建时间
```

`__post_init__` 中自动计算：
- `branch_name` → `"agent/{session_id}/{task_id}"`
- `worktree_path` → `"{repo_parent}/worktrees/{task_id}/{session_id}"`

#### MergeResult dataclass

```python
@dataclass
class MergeResult:
    success: bool
    source_branch: str
    target_branch: str
    conflict_files: list[str] = field(default_factory=list)
    error: str = ""
    aborted: bool = False
```

由 `WorkspaceManager.merge()` 和 `merge_task_to_main()` 返回，通过 `asdict()` 序列化为 API 响应。

#### task_branch_name 辅助函数

```python
def task_branch_name(task_id: str) -> str:
    return f"task/{task_id}"
```

集中管理 task branch 命名规则，避免字符串硬编码散落各处。

---

### 2. 持久化存储（`src/workspace/store.py`）

#### WorkspaceStoreProtocol 抽象接口

```python
class WorkspaceStoreProtocol(Protocol):
    async def load_all(self) -> dict[str, Workspace]: ...
    async def save(self, workspace: Workspace) -> None: ...
    async def delete(self, workspace_id: str) -> None: ...
    async def query_by_task(self, task_id: str) -> list[Workspace]: ...
    async def query_by_status(self, status: WorkspaceStatus) -> list[Workspace]: ...
```

抽象接口设计，后续迁移 SQLite 只需实现新的 Store 类，不需要修改 Manager。

#### JsonFileWorkspaceStore 实现

存储路径：`logs/workspaces.json`（与 `logs/session_mappings.json` 对称）。

**核心机制：**

- 初始化时从文件加载，文件不存在或损坏则从空 dict 开始
- `save()` 将 Workspace 序列化为 dict（`status` 转为字符串、`created_at` 转为 ISO 格式），写入 JSON 文件
- `delete()` 从内存 dict 中移除并写盘
- `query_by_task()` / `query_by_status()` 从内存 dict 中过滤
- 内部 `asyncio.Lock` 保护 `save()` 和 `delete()` 的文件写操作，避免并发写损坏

**序列化细节：**

```python
# 保存时
raw = asdict(workspace)
raw["status"] = workspace.status.value          # 枚举 → 字符串
raw["created_at"] = workspace.created_at.isoformat()  # datetime → ISO 字符串

# 读取时反序列化
raw["status"] = WorkspaceStatus(raw["status"])         # 字符串 → 枚举
raw["created_at"] = datetime.fromisoformat(raw["created_at"])  # ISO → datetime
```

**存储格式示例：**

```json
{
  "uuid-abc": {
    "id": "uuid-abc",
    "task_id": "task-123",
    "agent_name": "frontend",
    "agent_type": "claude-code",
    "session_id": "sess-aaa",
    "repo_path": "/repos/project",
    "worktree_path": "/repos/worktrees/task-123/sess-aaa",
    "branch_name": "agent/sess-aaa/task-123",
    "container_id": null,
    "status": "active",
    "created_at": "2026-05-21T00:30:00.123456"
  }
}
```

---

### 3. Git 底层操作（`src/workspace/git_ops.py`）

`GitOps` 封装所有 git 命令，统一通过 `asyncio.create_subprocess_exec` 调用，所有方法都是 async。

#### _run_git 基础方法

```python
async def _run_git(self, *args: str, cwd: str | None = None) -> tuple[bool, str]:
```

执行 git 命令，返回 `(成功与否, stdout 或 stderr 内容)`。失败时记录 warning 日志但不抛异常。

#### task_branch_create — 创建集成分支

```python
async def task_branch_create(self, repo_path: str, task_id: str) -> bool:
```

等价的 git 命令：

```bash
# 先检查是否已存在
git branch --list task/task-123

# 不存在则创建（from detected default branch）
git branch task/task-123 <default-branch>
```

已存在时直接返回 `True`，幂等操作。

#### worktree_add — 创建工作目录

```python
async def worktree_add(
    self, repo_path: str, path: str, branch: str, base_branch: str | None = None
) -> bool:
```

等价的 git 命令：

```bash
# 检查分支是否已存在
git branch --list agent/sess-aaa/task-123

# 分支不存在：创建新分支 + worktree
git worktree add /path/to/worktree -b agent/sess-aaa/task-123 task/task-123
#                                                新分支名 ↑            ↑ 起始点

# 分支已存在：直接检出已有分支
git worktree add /path/to/worktree agent/sess-aaa/task-123
```

`base_branch` 参数指定新分支的起始点（即 task branch），不传时从 HEAD 创建。

#### worktree_list — 列出物理 worktree

```python
async def worktree_list(self, repo_path: str) -> list[tuple[str, str]]:
```

解析 `git worktree list --porcelain` 输出。porcelain 格式是稳定的机器可读格式：

```
worktree /repos/project
HEAD abc123def
branch refs/heads/main

worktree /repos/worktrees/task-123/sess-aaa
HEAD def456abc
branch refs/heads/agent/sess-aaa/task-123
```

解析逻辑：按空行分隔条目，提取 `worktree`（路径）和 `branch`（分支名，去掉 `refs/heads/` 前缀）。

#### merge_branch — 合并分支

```python
async def merge_branch(self, repo_path: str, branch: str, target: str | None = None) -> MergeResult:
```

等价的 git 命令：

```bash
# 记录当前分支
git rev-parse --abbrev-ref HEAD

# 切到目标分支
git checkout <target-branch>

# 合并
git merge agent/sess-aaa/task-123

# 冲突时回滚
git merge --abort
git checkout <原分支>
```

冲突时不抛异常，执行 `--abort` 回滚后返回 `False`。

#### 其他方法

| 方法 | 说明 |
|---|---|
| `worktree_remove(path)` | `git worktree remove --force` |
| `branch_create(repo_path, name, base)` | `git branch <name> <base>` |
| `add_and_commit(path, message)` | `git add -A && git commit -m`，无变更时返回 `False` |
| `get_current_branch(path)` | `git rev-parse --abbrev-ref HEAD` |

---

### 4. 核心管理器（`src/workspace/manager.py`）

`WorkspaceManager` 是 workspace 模块的门面类，协调 GitOps（git 操作）和 Store（持久化）。

#### 构造函数

```python
def __init__(self, store: WorkspaceStoreProtocol):
    self._store = store          # 持久化存储
    self._git = GitOps()         # git 操作
    self._provisioner = SkillProvisioner()  # 技能分发
    self._workspaces: dict[str, Workspace] = {}  # 内存缓存
    self._locks: dict[str, asyncio.Lock] = {}    # per-task 并发锁
    self._cleanup_task: asyncio.Task | None = None  # inactive 清理后台任务
```

#### 并发锁机制

按 task_id 分组加锁。同一 task 下的 create/merge/cleanup 串行化，不同 task 之间并行：

```python
def _get_lock(self, task_id: str) -> asyncio.Lock:
    if task_id not in self._locks:
        self._locks[task_id] = asyncio.Lock()
    return self._locks[task_id]
```

为什么按 task 分组而不是按 workspace：同一 task 下多个 agent workspace 共享 task branch，并发创建 task branch 会冲突。

**锁的清理**：cleanup 完成后检查该 task 是否还有 ACTIVE workspace，如果没有则删除 Lock 对象，避免内存泄漏。

#### create() — 创建 Workspace

```
1. 获取 per-task lock
2. 检查是否已有 ACTIVE workspace（同一 task_id + session_id）→ 有则直接返回
3. 确保仓库有可用 HEAD；空仓库会自动创建 init 提交
4. 创建 task branch（task/task-123 from detected default branch）   ← 幂等，已存在则跳过
5. 构建 Workspace 对象（自动生成 branch_name 和 worktree_path）
6. 创建 agent worktree（agent/frontend/task-123 from task/task-123）
7. 分发技能到 worktree（SkillProvisioner.provision）
8. 初始化 shared 目录（memory/common/ 等）
9. 写入 git exclude 排除 agent 配置目录
10. 存入内存 dict
11. 持久化到 store
12. 释放 lock
```

失败时抛出 `RuntimeError`。

#### merge() — 合并分支

```python
async def merge(self, workspace_id: str, target_branch: str | None = None) -> bool:
```

- **不传 target_branch**（默认）→ 合到 `task/{task_id}`（Agent → 任务内集成），状态不变
- **未传 target_branch 且由 task 合并接口触发** → 合到仓库默认分支（任务 → 默认分支），状态变为 MERGED

这种设计让 merge 到 task branch 是安全的中间步骤，不会改变 workspace 的生命周期状态。只有最终合到仓库默认分支才标记为完成。

#### cleanup() — 清理 Workspace

```
1. 检查 workspace 存在且状态为 ACTIVE
2. 获取 per-task lock
3. 执行 git worktree remove --force
4. 状态改为 CLEANED + 持久化
5. 检查该 task 是否还有 ACTIVE workspace，没有则删除 lock
```

#### cleanup_by_task() — 批量清理

遍历指定 task_id 下所有 ACTIVE workspace，逐个调用 `cleanup()`。

#### Inactive 自动清理

基于 DB 查询的 inactive session 清理：

```python
async def _inactive_cleanup_loop(self, db_reader: DBReader, interval: int) -> None:
    while True:
        await asyncio.sleep(interval)
        inactive_pairs = await db_reader.query_inactive_sessions()
        task_statuses = await db_reader.query_task_session_statuses()

        for session_id, task_id in inactive_pairs:
            # 清理该 session 对应的 ACTIVE workspace
            ...

        for task_id, statuses in task_statuses.items():
            if statuses == {"inactive"}:
                # 该 task 下所有 session 都 inactive，清理 task 分支
                await self.cleanup_by_task(task_id)
```

- `start_inactive_cleanup(db_reader, interval)` — 启动后台 asyncio task
- `stop_inactive_cleanup()` — 取消后台 task
- 通过 `DBReader.query_inactive_sessions()` 查询 inactive 状态的 session
- 通过 `DBReader.query_task_session_statuses()` 判断 task 是否可整体清理
- 日志输出：`Inactive cleanup: scanned X sessions, cleaned Y sessions, cleaned Z tasks`

---

### 5. 启动恢复（`src/workspace/recovery.py`）

服务重启后，内存中的 workspace 数据丢失，但 git worktree 物理目录仍然存在。需要将持久化记录与物理状态进行 reconcile。

#### parse_worktree_list — 解析 worktree 列表

```python
def parse_worktree_list(output: str) -> list[tuple[str, str]]:
```

纯函数，解析 `git worktree list --porcelain` 的输出文本，返回 `[(path, branch), ...]`。

#### recover_workspaces — 恢复逻辑

```python
async def recover_workspaces(
    git_ops: GitOps, store: WorkspaceStoreProtocol, repo_path: str
) -> tuple[int, int, int]:
```

Reconcile 规则：

| Store 记录 | 物理 Worktree | 处理 | 返回值 |
|---|---|---|---|
| 有（ACTIVE） | 有 | 恢复为 ACTIVE，加载到内存 | `recovered += 1` |
| 有（ACTIVE） | 无 | 标记为 CLEANED，更新 store | `cleaned += 1` |
| 无 | 有 | orphan，执行 `git worktree remove --force` | `orphans_removed += 1` |

日志输出：`Workspace recovery: X recovered, Y cleaned, Z orphans removed`

---

### 6. 应用接入

#### 配置项（`src/app/config.py`）

配置来自 `config.yaml`，主要字段：

```yaml
workspace:
  base_dir: ...              # worktree 根目录
  cleanup_interval: 7200     # inactive 清理检查间隔（秒）
  store_path: logs/workspaces.json
  git_default_branch: main

database:
  host: ...
  port: 3306
  user: ...
  password: ...
  dbname: ...
```

#### 依赖注入（`src/app/dependencies.py`）

```python
def create_workspace_manager() -> WorkspaceManager:
    store = JsonFileWorkspaceStore()
    return WorkspaceManager(store)

def create_db_reader() -> DBReader:
    return DBReader(
        host=settings.database.host,
        port=settings.database.port,
        user=settings.database.user,
        password=settings.database.password,
        db=settings.database.dbname,
    )
```

组装 Store → Manager 和 DBReader 的依赖链。

#### 生命周期（`src/app/main.py`）

```
Startup:
  1. create_workspace_manager()      → 实例化 store + manager
  2. _load_from_store()              → 从 JSON 文件加载 workspace 到内存
  3. recover_workspaces()            → 逐 repo_path 执行 reconcile
  4. create_db_reader() + connect()  → 连接 MySQL
  5. start_inactive_cleanup()        → 启动 DB inactive 后台扫描

Shutdown:
  6. stop_inactive_cleanup()         → 取消清理后台 task
  7. db_reader.close()               → 关闭 DB 连接
```

#### API 端点（`src/api/v1/workspace.py`）

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/v1/workspace/create` | 创建 workspace |
| GET | `/v1/workspace/{id}/files/{path}` | 读取文件 |
| PUT | `/v1/workspace/{id}/files/{path}` | 写入文件 |
| GET | `/v1/workspace/{id}/diff` | 获取 diff |
| POST | `/v1/workspace/{id}/commit` | 提交变更 |
| POST | `/v1/workspace/{id}/revert` | 撤销变更（`git checkout HEAD -- .`） |
| POST | `/v1/workspace/{id}/merge` | 合并分支（默认合到 task branch） |
| POST | `/v1/workspace/{id}/preview/start` | 启动预览服务器 |
| POST | `/v1/workspace/{id}/preview/stop` | 停止预览服务器 |
| POST | /v1/workspace/task/{task_id}/merge-to-main | 合并 task branch 到仓库默认分支（路径名保留 main 兼容旧前端） |
| DELETE | `/v1/workspace/{id}` | 清理 workspace |
| GET | `/v1/workspace` | 列出所有 workspace |
| DELETE | `/v1/workspace/task/{task_id}` | 清理 task 下所有 workspace |
| POST | `/v1/workspace/task/{task_id}/cleanup-branches` | 强制清理 task 分支（无活跃 workspace 时） |
| GET | `/v1/workspace/by-session/{session_id}` | 按 session 查找 workspace |

---

## 完整生命周期示例

### 场景：为 task-123 创建两个 Agent Session

**Step 1: 创建 Session A 的 workspace**

```
POST /v1/workspace/create
{"repo_path": "/repos/project", "task_id": "task-123", "agent_name": "frontend", "session_id": "sess-aaa", "agent_type": "claude_code"}
```

Manager 内部执行：
```bash
git branch task/task-123 <default-branch>                  # 从检测到的默认分支创建集成分支
git worktree add /repos/worktrees/task-123/sess-aaa \
    -b agent/sess-aaa/task-123 task/task-123               # 创建 agent worktree
```

**Step 2: 创建 Session B 的 workspace**

```
POST /v1/workspace/create
{"repo_path": "/repos/project", "task_id": "task-123", "agent_name": "backend", "session_id": "sess-bbb", "agent_type": "opencode"}
```

Manager 内部执行：
```bash
git branch --list task/task-123                             # 检查已存在，跳过
git worktree add /repos/worktrees/task-123/sess-bbb \
    -b agent/sess-bbb/task-123 task/task-123               # 从同一 task branch 创建
```

**Step 3: Agent 工作中**

两个 Agent 分别在各自的 worktree 目录中独立工作，互不影响。

**Step 4: 提交变更**

```
POST /v1/workspace/{workspace_id}/commit
{"message": "feat: implement login page"}
```

等价于 `git add -A && git commit -m "feat: implement login page"`，提交到当前 agent 分支。

**Step 5: 合并 Session A 到 task branch**

```
POST /v1/workspace/{workspace_a_id}/merge
{"target_branch": "task/task-123"}
```

等价于 `git checkout task/task-123 && git merge agent/sess-aaa/task-123`。workspace 状态保持 ACTIVE。

**Step 6: 合并 task branch 到仓库默认分支**

```
POST /v1/workspace/task/task-123/merge-to-main
{"repo_path": "/repos/project"}
```

等价于 `git checkout <default-branch> && git merge task/task-123`。此时所有该 task 下的变更一次性合入仓库默认分支。

**Step 7: 清理 workspace**

```
DELETE /v1/workspace/{workspace_a_id}
DELETE /v1/workspace/{workspace_b_id}
```

等价于 `git worktree remove --force <path>` + `git branch -D agent/sess-aaa/task-123`，状态变为 CLEANED，Lock 对象被回收。

---

## 已知限制与后续方向

1. **container_id 预留字段**：已添加但未使用，为后续 Docker 容器隔离预留
2. **JSON 文件存储**：适合单实例开发环境，生产环境需替换为 SQLite/Redis
3. **Inactive 清理依赖 DB**：需要 MySQL 中 sessions 表的 status 字段准确标记，否则清理不触发
4. **冲突处理**：merge 冲突时直接 abort 返回错误，不做自动冲突解决
5. **recovery 按 repo_path 粒度**：当前逐个 repo 执行恢复，多 repo 场景下可优化为并行
