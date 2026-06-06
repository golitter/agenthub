查看 contracts 目录，进行 schema 修改或变更日志更新。

1. 查看当前变更：`git status` + `git diff`
2. 修改 `contracts/schemas/` 下的 YAML 文件（契约优先，不要手改 `generated/`）
3. 运行 `make generate` 生成三端类型文件
4. 确认生成文件已更新（对照 [contracts/AGENTS.md](../../contracts/AGENTS.md) 的映射表）
5. 在 `contracts/logs/` 写入变更记录（格式：`YYYY-MM-DD-<kebab-case>.md`，包含变更原因、变更文件、对比结果、跨端影响）
6. 执行 `git log --oneline -5` 查看最近提交风格
7. 根据 [docs/guides/git-conventions.md](../guides/git-conventions.md) 的规范生成 commit message（scope 取 `common`）
8. 展示所有待提交的变更文件列表和生成的 commit message，由用户自行决定是否提交（不要执行 `git add` 或 `git commit`）。示例：
```shell
git add <file1> <file2> ...
git commit -m "$(cat <<'EOF'
<commit message>

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
EOF
)"
```
