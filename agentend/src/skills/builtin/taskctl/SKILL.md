---
name: taskctl
description: 多 Agent 协作上下文与状态管理工具。自动识别 Agent 身份，提供任务级配置/记忆的读写能力，并基于 Git 分支机制实现多 Agent 间的安全状态隔离与合并同步。
---

## 概述

`taskctl` 专为多 Agent 协作场景设计，解决多实例间的信息隔离与同步问题。它通过统一的文件系统管理任务配置、执行计划及共享/私有记忆，并结合 Git 分支策略（`task` 分支与 `agent` 分支）实现安全、原子性的上下文合并，确保协作过程不丢失、不冲突。

## 命令

### `help`

打印所有可用命令及说明。

```bash
./taskctl help
```

### `ls`

递归列出共享目录下的文件结构（按名称排序，目录以 `/` 结尾）。

```bash
./taskctl ls
```

输出示例：

```
config.yaml
memory/
memory/common/
memory/common/notes.md
plans/
plans/plan-1.md
```

如果共享目录为空，输出 `(空)`。

### `summary`

查看任务概览，输出 `config.yaml` 内容和 `plans/` 目录下所有计划文件。

```bash
./taskctl summary
```

输出示例：

```
=== config.yaml ===
name: my-task

=== plans/plan-1.md ===
step 1
```

如果 `plans/` 不存在或读取失败，输出错误信息并以非零退出码退出。

### `common-memory [file]`

读取公共记忆目录 `memory/common/` 下的文件。

```bash
# 读取全部公共记忆（按文件名排序）
./taskctl common-memory

# 读取指定文件
./taskctl common-memory notes.md
```

所有 Agent 共享同一份公共记忆。不指定文件时，输出所有文件内容；指定文件名时，仅输出该文件内容。如果目录为空或不存在，输出 `(无公共记忆)`；指定文件不存在时，输出错误信息并以非零退出码退出。

### `sub-memory [file]`

读取当前 Agent 的私有记忆。

```bash
# 读取全部私有记忆（按文件名排序）
./taskctl sub-memory

# 读取指定文件
./taskctl sub-memory log.md
```

每个 Agent 只能看到自己的私有记忆，不同 Agent 之间隔离。不指定文件时，输出所有文件内容；指定文件名时，仅输出该文件内容。如果为空，输出 `(无私有记忆)`；指定文件不存在时，输出错误信息并以非零退出码退出。

### `write-sub-memory <file> [content...]`

向当前 Agent 的私有记忆写入文件。

```bash
# 通过参数写入
./taskctl write-sub-memory log.md 完成了代码审查

# 通过 stdin 写入（适合长内容）
echo "# 审查报告\n全部通过" | ./taskctl write-sub-memory review.md

# 多词内容会自动拼接
./taskctl write-sub-memory note.md hello world foo bar
```

特性：
- 内容来源优先级：stdin 管道输入 > 命令行参数（多个参数以空格拼接）
- 原子写入：先写临时文件再 rename，保证不会出现半截文件
- 目录不存在时自动创建
- 未提供内容（无 stdin 且无参数）时输出错误并以非零退出码退出

### `merge`

将当前 agent 分支合并到 task 分支（`task/{taskID}`），合并后自动切回 agent 分支。

```bash
./taskctl merge
```

流程：
1. 检测未提交改动，有则自动 `git add -A && git commit`
2. 切换到 `task/{taskID}` 分支
3. 执行 `git merge agent/{sessionID}/{taskID}`
4. 合并成功：切回 agent 分支，输出 `merged to task/{taskID}`
5. 合并冲突：执行 `git merge --abort`，切回 agent 分支，输出错误到 stderr，退出码 1
