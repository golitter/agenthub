# taskctl — Agent 共享上下文管理工具

## 概述

`taskctl` 是一个 Go 编写的轻量 CLI 工具，用于多 Agent 协作场景下的共享上下文读写。它通过解析自身可执行文件的路径，自动识别当前 Agent 身份（taskID / agentName），无需额外配置。

## 源码位置

```
agentend/src/skills/builtin/taskctl/
├── main.go        # Go 源码
├── main_test.go   # Go 测试
├── go.mod         # Go module (go 1.22)
├── exe            # 编译产物（被分发到 agent worktree）
└── SKILL.md       # 使用说明（面向 Agent）
```

## 路径约定

`exe` 通过 `os.Executable()` 获取自身路径，然后向上回溯解析出 taskID 和 agentName：

```
<worktrees_root>/worktrees/<taskID>/<agentName>/<configDir>/skills/taskctl/exe
                 └─worktrees─┘ └─taskID─┘ └agentName┘  └configDir┘
```

共享目录定位：

```
<worktrees_root>/worktrees/<taskID>/shared/.agent/
├── config.yaml
├── plans/
│   └── plan-1.md
└── memory/
    ├── common/          # 所有 Agent 共享
    └── <agentName>/     # Agent 私有记忆
```

支持的 `configDir`：`.claude`（claude-code）、`.opencode`（opencode）。

## 命令

| 命令 | 说明 |
|------|------|
| `./exe help` | 打印帮助 |
| `./exe ls` | 递归列出共享目录结构 |
| `./exe summary` | 查看 config.yaml + plans |
| `./exe common-memory` | 读取公共记忆 |
| `./exe sub-memory` | 读取当前 Agent 私有记忆 |
| `./exe write-sub-memory <文件名> <内容>` | 写入私有记忆 |

## 分发机制

`taskctl` 由 `SkillProvisioner` 自动分发到 agent worktree，流程：

1. `SkillProvisioner.provision()` 读取 `builtin/manifest.yaml`
2. 按 manifest 中声明的 `file` / `dir` 列表复制到 `<worktree>/<configDir>/skills/taskctl/`
3. 已存在的 skill 不会被覆盖
4. 分发路径自动写入 `.git/info/exclude` 防止提交

manifest 声明（`builtin/manifest.yaml`）：

```yaml
taskctl:
  file:
    - SKILL.md
    - exe
```

## 编译与测试

```bash
cd agentend/src/skills/builtin/taskctl

# 运行测试
go test ./...

# 编译（macOS）
go build -o exe .

# 编译（Linux，用于部署）
GOOS=linux GOARCH=amd64 go build -o exe .
```

编译后 `exe` 文件替换到当前目录即可，下次 `provision` 会自动分发新版本。

## 修改指南

### 新增命令

1. 在 `main.go` 的 `switch cmd` 中添加 `case`
2. 实现对应函数（参考 `cmdLs` / `cmdCommonMemory` 等现有命令）
3. 在 `printHelp()` 中补充说明
4. 在 `main_test.go` 中添加测试
5. 更新 `SKILL.md` 中的命令列表
6. 重新编译 `exe`：`go build -o exe .`

### 路径解析变更

路径解析逻辑在 `parsePath()` 函数中。如果 worktree 目录结构发生变化，需同步修改此函数和对应测试（`TestParsePath` / `TestParsePathOpenCode` / `TestParsePathInvalid`）。

### 添加新 builtin skill

1. 在 `builtin/` 下创建新目录，放入 `SKILL.md` 和所需文件
2. 在 `builtin/manifest.yaml` 中添加声明：

```yaml
new-skill:
  file:
    - SKILL.md
    - exe
  dir:                    # 可选，按需声明
    - templates
```

3. `SkillProvisioner` 会自动读取 manifest 并分发
