# 2026-05-29-backend-audit-fixes

## 变更原因

Backend 代码审计发现数据完整性、运行时健壮性和代码质量问题，进行批量修复。

## 变更文件

无 schema 文件变更。本次改动均为 backend 内部实现调整，不涉及 `contracts/schemas/*.yaml`。

## 跨端影响

| 改动 | Frontend | Backend | AgentEnd |
|------|----------|---------|----------|
| RunTask 响应格式统一 (`vo.Accepted`) | `json.data` 取值路径不变，兼容 | 格式统一为 `{code, data}` | 无影响 |
| Agent Profile skills 返回空数组 | 需适配 skills 为空的渲染 | 移除 mock 数据 | 无影响 |
| CreateTask/DeleteTask 事务化 | 无影响 | 内部数据完整性提升 | 无影响 |
| 连接池/超时/panic recovery | 无影响 | 运行时健壮性提升 | 无影响 |
| CORS 配置化 | 部署时需配置 `cors.allow_origins` | 可配置化 | 无影响 |

## 契约变更

无契约变更。本次修复不修改任何 schema 定义。
