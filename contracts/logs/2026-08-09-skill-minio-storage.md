# Skill MinIO 存储契约

## 变更原因

Skill 上传确认从服务器临时目录引用迁移为服务端生成的 `upload_id`，并增加规范 ZIP 的
SHA-256、压缩包大小和存储类型字段，供 MinIO staging/promote 流程使用。

## 变更文件

- `contracts/schemas/skill-storage.yaml`
- `scripts/generate_contracts.py`
- 三端 `generated/skill_storage.*`

## 跨端影响

- Backend 上传接口返回 `upload_id`；MinIO 模式确认只依赖该 ID。
- Frontend 使用生成的上传/确认类型，不再在 MinIO 模式回传 `tmp_dir`。
- `tmp_dir` 字段仅保留为 DB 兼容模式的过渡字段。

## 契约变更

- 新增 `SkillUploadResponse`、`SkillConfirmRequest`、`SkillConfirmResponse`。
- `SkillUploadResponse` 增加 `upload_id`、`sha256`、`package_size`、`storage_type`、文件清单和可执行/二进制内容提示。
- 生成器补齐 JSON Schema `integer` 到 Python/TypeScript/Go 的映射。
