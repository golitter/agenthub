自动 git 提交流程：

1. 先执行 `git status` 查看当前变更
2. 执行 `git diff` 查看具体改动内容
3. 执行 `git log --oneline -5` 查看最近提交风格
4. 根据 [docs/guides/git-conventions.md](../guides/git-conventions.md) 的规范生成 commit message：
   - 格式：`<type>(<scope>): <描述>`
   - scope 必填，可取一个或多个值（逗号分隔）：frontend / backend / agentend / common / docs / other
   - 跨多个子项目的改动使用逗号分隔，如：`feat(frontend,backend,agentend): ...`
   - type 遵循 Conventional Commits（feat / fix / docs / refactor / chore 等）
   - 描述用中文，简明概括改动的目的（why > what）
5. 对变更文件执行 `git add`（按文件名指定，不要用 `git add .`）
6. 执行提交：
```shell
git commit -m "$(cat <<'EOF'
<commit message>

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```
7. 提交后执行 `git status` 确认提交成功

注意事项：
- 不要使用 `--no-verify` 跳过钩子
- 不要提交可能包含密钥的文件（.env、credentials 等）
- 不要使用 `--amend` 修改已有提交
- 不要主动 push 到远程，除非明确要求
