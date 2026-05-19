# Git 规范

## Commit 格式

```
<type>(<scope>): <描述>
```

**scope 为必填项**，只能取以下值：

| scope | 说明 |
|-------|------|
| frontend | 前端 |
| backend | 后端 |
| agentend | Agent 端 |
| common | 公共 |
| docs | 文档 |
| other | 其他 |

type 遵循 [Conventional Commits](https://www.conventionalcommits.org/)（feat / fix / docs / refactor / chore 等）。

示例：

```
feat(frontend): 添加登录页面
fix(backend): 修复数据库连接超时
docs(common): 更新 monorepo 工程化说明
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
