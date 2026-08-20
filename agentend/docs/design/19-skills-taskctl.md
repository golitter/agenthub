# taskctl — Agent 共享上下文管理工具

## 实现了什么

`taskctl` 是一个 Go 编写的轻量 CLI 工具，用于多 Agent 协作场景下的共享上下文读写。它通过解析自身可执行文件的路径，自动识别当前 Agent 身份（taskID / sessionID / agentType），无需额外配置。

核心能力：
- 任务级配置和内存管理
- 共享内存（common）和私有内存（sub）隔离
- 基于 Git 分支的多 Agent 协作
- Agent 分支 → 任务分支的安全合并

## 怎么实现的

### 源码位置

```
agentend/src/skills/builtin/taskctl/
├── main.go        # Go 源码
├── main_test.go   # Go 测试
├── go.mod         # Go module (go 1.22)
├── go.sum
├── taskctl        # 本地编译产物（被分发到 agent worktree，不入库）
└── SKILL.md       # 使用说明（面向 Agent）
```

### 路径解析 (`main.go`)

`taskctl` 通过 `os.Executable()` 获取自身路径，然后向上回溯解析出 taskID、sessionID 和 agentType：

```go
func main() {
    exePath, err := os.Executable()
    // ...
    exePath, err = filepath.EvalSymlinks(exePath)
    // ...
    taskID, sessionID, sharedDir, _, err := parsePath(exePath)
    // ...
    cmd := os.Args[1]
    switch cmd {
    case "help":    printHelp()
    case "ls":      cmdLs(sharedDir)
    case "summary": cmdSummary(sharedDir, sessionID)
    case "common-memory":    cmdCommonMemory(sharedDir, os.Args[2:])
    case "sub-memory":       cmdSubMemory(sharedDir, sessionID, os.Args[2:])
    case "write-sub-memory": cmdWriteSubMemory(sharedDir, sessionID)
    case "merge":   cmdMerge(taskID, sessionID, sharedDir)
    }
}
```

路径约定：

```
<worktrees_root>/worktrees/<taskID>/<sessionID>/<configDir>/skills/taskctl/taskctl
                 └─worktrees─┘ └─taskID─┘ └sessionID┘  └configDir┘
```

`parsePath` 签名：

```go
func parsePath(exePath string) (taskID, sessionID, sharedDir, agentType string, err error)
```

返回四个业务值（`taskID`、`sessionID`、`sharedDir`、`agentType`）外加 `err`。`main` 中 `agentType` 当前被忽略（用 `_` 接收），由 configDir 目录名映射得到：`.claude` → `claude-code`，`.opencode` → `opencode`，`.pi` → `pi`。

共享目录定位：

```
<worktrees_root>/worktrees/<taskID>/shared/.agent/
├── config.yaml
├── plans/
│   ├── overview.md
│   └── task-001.md
└── memory/
    ├── common/          # 所有 Agent 共享
    └── <sessionID>/     # Agent 私有记忆
```

### 命令

| 命令 | 说明 |
|------|------|
| `./taskctl help` | 打印帮助 |
| `./taskctl ls` | 递归列出共享目录结构 |
| `./taskctl summary` | 查看 config.yaml + 当前 agent 的 plans（按 sessionID 过滤） |
| `./taskctl common-memory [file]` | 读取公共记忆（指定文件名则只读单个文件） |
| `./taskctl sub-memory [file]` | 读取当前 Agent 私有记忆（指定文件名则只读单个文件） |
| `./taskctl write-sub-memory <file> [content...]` | 写入私有记忆（支持 stdin 管道输入） |
| `./taskctl merge` | 合并当前 agent 分支到 task 分支 |

### summary 过滤机制

`summary` 读取 `config.yaml` 中的 tasks 列表，只显示 `session_id` 匹配当前 agent 的 plan 文件 + `overview.md`（始终显示）。每个 agent 只能看到分配给自己的任务。

### merge 流程

合并不在当前 agent worktree 切换分支执行，而是在 `task-base` worktree 中直接合并 agent 分支，避免当前 agent worktree 抢占 task 分支。

1. 在 agent worktree 检测未提交改动，有则自动 `git add -A && git commit`
2. 校验 `task-base` worktree 存在
3. 在 `task-base` worktree 执行 `git merge agent/{sessionID}/{taskID}`
4. 合并成功：输出 `merged to task/{taskID}`
5. 合并冲突：列出冲突文件，执行 `git merge --abort` 回退 task-base，输出错误到 stderr，退出码 1

### 分发机制

`taskctl` 由 `SkillProvisioner` 自动分发到 agent worktree，流程：

1. `SkillProvisioner.provision()` 读取 `config.yaml` 的 `skills.manifest`
2. 按 manifest 中声明的 `file` / `dir` 列表复制到 `<worktree>/<configDir>/skills/taskctl/`
3. 已存在且与 builtin 逐文件一致（sha256 对比 manifest 声明的 file/dir）的 skill 跳过不重复复制；内容有差异时经 staging 目录 + 备份目录原子刷新（保留 manifest 之外的本地文件）。builtin 源目录缺失 manifest 声明的文件时抛 `FileNotFoundError`，要求先重跑 `make skills build`

> 注：将整个 `<configDir>`（如 `.claude`/`.opencode`/`.pi`）排除的逻辑由 `WorkspaceManager.create` 调用 `git_ops.setup_worktree_excludes` 完成，不属于 `SkillProvisioner`。该方法把 patterns 写入 worktree 本地 excludes 文件，并通过 `git config --worktree core.excludesFile` 注册（仅对当前 worktree 生效，不影响主仓库）；skill 分发路径位于 `<configDir>/skills/` 下，会被该 exclude 规则覆盖，自然不会被提交。

manifest 声明（`config.yaml` 的 `skills.manifest`）：

```yaml
taskctl:
  file:
    - SKILL.md
    - taskctl
```

### 编译与测试

```bash
# 运行测试
cd agentend/src/skills/builtin/taskctl
go test ./...

# 编译当前平台的内置 skill CLI
cd ../../../../..
make skills build

# 编译（Linux，用于部署）
cd agentend/src/skills/builtin/taskctl
GOOS=linux GOARCH=amd64 go build -o taskctl .
```

编译后 `taskctl` 文件生成在当前目录即可，下次 `provision` 会自动分发新版本。该文件是平台相关产物，已由 `.gitignore` 忽略，不应提交。
