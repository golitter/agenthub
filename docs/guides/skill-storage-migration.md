# Skill MinIO 存储迁移操作指南

实现已按 `docs/design/10-skills-minio-storage-migration.md` 接入，但默认保持
`skill_storage.enabled: false`，不会改变现有数据库 BLOB 链路。启用前先准备私有
MinIO Bucket、应用级最小权限凭据和 Redis。

## 启用与灰度

```bash
export SKILL_STORAGE_ENABLED=true
export MINIO_ACCESS_KEY=agenthub
export MINIO_SECRET_KEY='replace-with-a-strong-secret'
export MINIO_USE_SSL=true                 # 生产必需
export MINIO_CA_CERT=/run/secrets/minio-ca.pem  # 使用自签 CA 时
```

观察期建议保持 `shadow_write_blob: true`、`read_preference: minio`，并保留
`allow_legacy_tmp_confirm: true`。确认所有旧实例退出、对账和回滚演练通过后，才关闭
旧 `tmp_dir` 兼容和影子 BLOB 写入。

Backend 的持久化 Worker 会按 `orphan_grace_period` 将无数据库引用的正式对象放入删除队列，
并在实际删除前再次检查引用；`skill-reconcile --verify` 默认不删除对象，发现哈希/大小异常
会标记 `storage_error` 并排入补偿，`--repair` 才会清理确认过的孤儿对象。

`min_temp_free_bytes` 默认保留 1 GiB；上传校验会在落盘前检查该水位，磁盘不足时拒绝新上传。
`max_file_size`、`max_compression_ratio` 和 `max_file_count` 也由 Backend 配置统一控制，且不会超过 AgentEnd 的独立上限。

## 迁移、校验与回滚

```bash
make skills migrate ARGS="--dry-run --batch-size 10"
make skills migrate ARGS="--resume --batch-size 10 --cursor-file /var/lib/agenthub/skill.cursor"
make skills migrate ARGS="--verify-only --batch-size 100"
make skills migrate ARGS="--reverse-to-db --resume"
make skills migrate ARGS="--clear-content --dry-run --batch-size 100"
# 观察期、全量对账和回滚演练完成后，且配置已关闭 shadow_write_blob：
make skills migrate ARGS="--clear-content --confirm-clear-content=CLEAR-SKILL-BLOBS --resume"
make skills reconcile ARGS="--verify"
make skills reconcile ARGS="--verify --repair"  # 明确确认后才允许删除候选对象
```

迁移任务使用 MySQL 持久化租约，重复执行按确定性对象键幂等；`--reverse-to-db` 在写回
前会校验对象大小和 SHA-256，发现已有不同 BLOB 时停止，不会覆盖不可判定内容。

备份必须把同一恢复点的 MySQL 快照标识与 MinIO 清单绑定：

```bash
MYSQL_SNAPSHOT_ID=mysql-bin.000123:456 \
  BACKUP_DIR=/srv/backups/agenthub \
  MINIO_ENDPOINT=https://minio.internal:9000 MINIO_ACCESS_KEY=agenthub \
  MINIO_SECRET_KEY='...' MINIO_BUCKET=skill-packages \
  sh docker/minio/backup.sh
```

恢复对象后先执行只读对账，再恢复确认、导入和删除写流量；`docker/minio/restore.sh`
只负责对象镜像恢复，不会替代数据库恢复或对账。

## 集成测试

默认单元测试不触碰外部服务。使用一次性 MinIO、Redis、MySQL 后显式开启：

```bash
MINIO_INTEGRATION=1 MINIO_INTEGRATION_ENDPOINT=127.0.0.1:9000 \
  MINIO_INTEGRATION_BUCKET=skill-packages MINIO_INTEGRATION_ACCESS_KEY=agenthub \
  MINIO_INTEGRATION_SECRET_KEY='...' \
  sh -c 'cd backend && go test ./pkg/package_store -run MinIOStoreIntegration'

REDIS_INTEGRATION=1 sh -c 'cd backend && go test ./pkg/skill_upload_session -run RedisUploadSessionIntegration'

MYSQL_INTEGRATION=1 MYSQL_INTEGRATION_HOST=127.0.0.1 \
  MYSQL_INTEGRATION_USER=root MYSQL_INTEGRATION_DBNAME=agenthub \
  sh -c 'cd backend && go test ./internal/dao/gorm -run SkillStorageMySQLIntegration'

# 完整全链路（upload→confirm→import→remove→delete）+ 多实例重启 + 故障注入 +
# 观察期影子写/双读回滚演练，一次连接真实 MinIO+Redis+MySQL 运行：
SKILL_E2E=1 \
  SKILL_E2E_MYSQL_HOST=127.0.0.1 SKILL_E2E_MYSQL_PORT=3307 SKILL_E2E_MYSQL_USER=root \
  SKILL_E2E_MYSQL_PASSWORD='...' SKILL_E2E_MYSQL_DBNAME=agenthub \
  SKILL_E2E_REDIS_HOST=127.0.0.1 SKILL_E2E_REDIS_PORT=6380 SKILL_E2E_REDIS_PASSWORD='...' \
  SKILL_E2E_MINIO_ENDPOINT=127.0.0.1:19000 SKILL_E2E_MINIO_BUCKET=e2e-skill-packages \
  SKILL_E2E_MINIO_ACCESS_KEY=agenthub SKILL_E2E_MINIO_SECRET_KEY='...' \
  sh -c 'cd backend && go test ./internal/service/impl -run TestE2E -timeout 600s'
```

分层覆盖：对象 Promote 幂等/不可覆盖、Redis 租约 fencing、显式元数据投影、receipt
持久化、限量 BLOB 读取扫描（`GetSkillContentLimited`/`GetSkillContentByIDLimited`）由各自的
门控测试覆盖；启用后，`TestE2E*` 会在真实 MinIO/Redis/MySQL 加可控 fake AgentEnd 的服务层
链路上验证全链路、并发确认 fencing、跨实例服务重启模拟、故障关闭导入与观察期双读回滚。真实 AgentEnd
和 Backend HTTP 多进程部署的外部验证仍是发布门禁，未设置对应开关时全部安全跳过。
