---
name: taskctl
description: Agent 共享上下文管理工具。自动识别当前 Agent 身份，提供任务级共享目录的读写能力。
---

## 概述

`taskctl` 是一个轻量级 CLI 工具，用于在多 Agent 协作场景下访问和管理共享上下文。

## 命令

### `help`

打印所有可用命令及说明。

```bash
./exe help
```

### `ls`

递归列出共享目录下的文件结构（按名称排序，目录以 `/` 结尾）。

```bash
./exe ls
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
./exe summary
```

输出示例：

```
=== config.yaml ===
name: my-task

=== plans/plan-1.md ===
step 1
```

如果 `plans/` 不存在，输出 `(无 plans)`。

### `common-memory`

读取公共记忆目录 `memory/common/` 下的所有文件（按文件名排序）。

```bash
./exe common-memory
```

所有 Agent 共享同一份公共记忆。如果目录为空或不存在，输出 `(无公共记忆)`。

### `sub-memory`

读取当前 Agent 的私有记忆（按文件名排序）。

```bash
./exe sub-memory
```

每个 Agent 只能看到自己的私有记忆，不同 Agent 之间隔离。如果为空，输出 `(无私有记忆)`。

### `write-sub-memory`

向当前 Agent 的私有记忆写入文件。

```bash
./exe write-sub-memory <文件名> <内容>
```

示例：

```bash
./exe write-sub-memory log.md "完成了代码审查"
```

如果目录不存在会自动创建。参数不足时输出用法提示。