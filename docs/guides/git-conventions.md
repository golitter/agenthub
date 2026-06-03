# Git 规范

## Commit 格式

```
<type>(<scope>): <描述>
```

**scope 为必填项**，可以取一个或多个值（逗号分隔）：

| scope | 说明 |
|-------|------|
| frontend | 前端 |
| backend | 后端 |
| agentend | Agent 端 |
| common | 公共 |
| docs | 文档 |
| other | 其他 |

- 当改动涉及单个子项目时，使用单个 scope：`feat(frontend): ...`
- 当改动跨多个子项目时，使用逗号分隔的多个 scope：`feat(frontend,backend): ...`

type 遵循 [Conventional Commits](https://www.conventionalcommits.org/)（feat / fix / docs / refactor / chore 等）。

示例：

```
feat(frontend): 添加登录页面
fix(backend): 修复数据库连接超时
docs(common): 更新 monorepo 工程化说明
feat(frontend,backend,agentend): 实现通讯录分组+置顶会话+退群功能
fix(backend,agentend): 修复消息流式传输超时
```

## Git Hooks

| 钩子 | 触发时机 | 执行内容 |
|------|---------|---------|
| pre-commit | `git commit` 前 | lint-staged 检查暂存文件 |
| commit-msg | `git commit` 前 | commitlint 校验 commit message |

**不要使用 `--no-verify` 跳过钩子。** 如果钩子失败，先修复问题再提交。

## Lint-staged 规则

| 子项目 | 匹配文件 | 执行命令 |
|--------|---------|---------|
| frontend | `**/*.{ts,tsx}` | eslint --fix + prettier --write |
| backend | `**/*.go` | gofmt -w + goimports -w |
| agentend | `**/*.py` | ruff check --fix + ruff format |

各子项目的 lint 配置文件位于对应目录内。
