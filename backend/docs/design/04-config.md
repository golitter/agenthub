# Config — 配置加载

## 实现了什么

通过 YAML 文件 + `.env` 环境变量双层机制加载配置，涵盖 MySQL、JWT、AgentEnd、Server、Auth、头像 Storage、ArtifactStorage、SkillStorage、Redis、Admin、CORS 十一个模块。头像 MinIO、内置资源 Artifact MinIO 和 Skill MinIO 使用不同前缀、Bucket 与账号；敏感信息从环境变量注入，不硬编码在 YAML 中。

## 怎么实现的

### Config 结构体 (`internal/conf/conf.go`)

```go
type Config struct {
	MySQL           MySQLConfig           `yaml:"mysql"`
	JWT             JWTConfig             `yaml:"jwt"`
	AgentEnd        AgentEndConfig        `yaml:"agentend"`
	Storage         StorageConfig         `yaml:"storage"`
	ArtifactStorage ArtifactStorageConfig `yaml:"artifact_storage"`
	SkillStorage    SkillStorageConfig    `yaml:"skill_storage"`
	Redis           RedisConfig           `yaml:"redis"`
	Admin           AdminConfig           `yaml:"admin"`
	CORS            CORSConfig            `yaml:"cors"`
	Server          ServerConfig          `yaml:"server"`
	Auth            AuthConfig            `yaml:"auth"`
}
```

各子配置结构体均提供辅助方法：

```go
type MySQLConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	DBName   string `yaml:"dbname"`
	Charset  string `yaml:"charset"`
}

func (c *MySQLConfig) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=True&loc=Local",
		c.User, c.Password, c.Host, c.Port, c.DBName, c.Charset)
}
```

```go
type AgentEndConfig struct {
	Host               string `yaml:"host"`
	Port               int    `yaml:"port"`
	ServiceAuthEnabled bool   `yaml:"service_auth_enabled"`
}

func (c *AgentEndConfig) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}
```

```go
type RedisConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

func (c *RedisConfig) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}
```

```go
type JWTConfig struct {
	Secret      string `yaml:"secret"`
	ExpireHours int    `yaml:"expire_hours"`
}

type LocalStorageConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Dir       string `yaml:"dir"`
	URLPrefix string `yaml:"url_prefix"`
}

type AvatarMinIOConfig struct {
	Enabled        bool   `yaml:"enabled"`
	Endpoint       string `yaml:"endpoint"`
	Bucket         string `yaml:"bucket"`
	AccessKey      string `yaml:"access_key"`
	SecretKey      string `yaml:"secret_key"`
	UseSSL         bool   `yaml:"use_ssl"`
	CAFile         string `yaml:"ca_file"`
	RequestTimeout string `yaml:"request_timeout"`
}

type StorageConfig struct {
	WriteProvider string             `yaml:"write_provider"` // "minio" | "local"
	MinIO         AvatarMinIOConfig  `yaml:"minio"`
	Local         LocalStorageConfig `yaml:"local"`
}

// ArtifactStorageConfig 控制内置资源（Artifact）使用的私有对象存储。它与头像、
// external Skill 存储刻意分开，凭据与 Bucket 策略不会混用。
type ArtifactStorageConfig struct {
	Enabled            bool   `yaml:"enabled"`
	Endpoint           string `yaml:"endpoint"`
	Bucket             string `yaml:"bucket"`
	AccessKey          string `yaml:"access_key"`
	SecretKey          string `yaml:"secret_key"`
	UseSSL             bool   `yaml:"use_ssl"`
	CAFile             string `yaml:"ca_file"`
	RequestTimeout     string `yaml:"request_timeout"`
	MaxObjectSize      string `yaml:"max_object_size"`
	MaxArtifactsPerMsg int    `yaml:"max_artifacts_per_message"`
	UploadTokenTTL     string `yaml:"upload_token_ttl"`
	CapabilitySecret   string `yaml:"capability_secret"`
	FailedRetention    string `yaml:"failed_retention"`
}

// SkillStorageConfig 控制私有技能包对象存储（feature-gated）。大小与时长值
// 保留为字符串，以便 YAML 接受 "10MiB"、"15m" 等人类可读值；技能包在使用点解析。
type SkillStorageConfig struct {
	Enabled                  bool   `yaml:"enabled"`
	RequireAdmin             bool   `yaml:"require_admin"`
	Type                     string `yaml:"type"` // 启用时强制 "minio"
	Endpoint                 string `yaml:"endpoint"`
	Bucket                   string `yaml:"bucket"`
	AccessKey                string `yaml:"access_key"`
	SecretKey                string `yaml:"secret_key"`
	UseSSL                   bool   `yaml:"use_ssl"`
	CAFile                   string `yaml:"ca_file"`
	UploadSessionTTL         string `yaml:"upload_session_ttl"`
	ReceiptRetention         string `yaml:"receipt_retention"`
	ReadPreference           string `yaml:"read_preference"`        // "minio" | "db"
	ShadowWriteBlob          bool   `yaml:"shadow_write_blob"`      // 迁移期双写 DB blob
	AllowLegacyTmpConfirm    bool   `yaml:"allow_legacy_tmp_confirm"`
	ConfirmLease             string `yaml:"confirm_lease"`
	OrphanGracePeriod        string `yaml:"orphan_grace_period"`
	IncomingTTL              string `yaml:"incoming_ttl"`
	TempDir                  string `yaml:"temp_dir"`
	MinTempFreeBytes         string `yaml:"min_temp_free_bytes"`
	MaxUploadSize            string `yaml:"max_upload_size"`
	MaxPackageSize           string `yaml:"max_package_size"`
	MaxFileSize              string `yaml:"max_file_size"`
	MaxUnpackedSize          string `yaml:"max_unpacked_size"`
	MaxCompressionRatio      int64  `yaml:"max_compression_ratio"`
	MaxFileCount             int    `yaml:"max_file_count"`
	MaxConcurrentValidations int    `yaml:"max_concurrent_validations"`
	ValidationTimeout        string `yaml:"validation_timeout"`
	RejectBinaries           bool   `yaml:"reject_binaries"`
	RejectExecutables        bool   `yaml:"reject_executables"`
	ContentScanCommand       string `yaml:"content_scan_command"`
	ContentScanTimeout       string `yaml:"content_scan_timeout"`
}

type AdminConfig struct {
	Password string `yaml:"password"`
}

type CORSConfig struct {
	AllowOrigins []string `yaml:"allow_origins"`
}

type ServerConfig struct {
	Port int `yaml:"port"`
}

type AuthConfig struct {
	Enabled bool `yaml:"enabled"`
}
```

### 加载逻辑

`Load` 先尝试加载可选的 `.env` 文件，再预置 SkillStorage 安全默认值（`RequireAdmin=true`、`ShadowWriteBlob=true`、`AllowLegacyTmpConfirm=true`），读取 YAML 配置后执行 `applyEnvOverrides` 用环境变量覆盖 MySQL / JWT / AgentEnd / Redis / CORS / Admin / Server / 头像 Storage / SkillStorage 等连接参数（便于 Docker / CI 注入），最后通过 `validateConfig` 做启动前校验：

```go
func Load(path string) (*Config, error) {
	// .env 是可选的 —— 不存在时不报错
	_ = godotenv.Load()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}
	var cfg Config
	// 技能管理默认需要管理员权限；显式 YAML false 可在受信单机开发环境关闭。
	cfg.SkillStorage.RequireAdmin = true
	// 迁移回滚副本与旧确认协议在观察期内默认保持开启。
	cfg.SkillStorage.ShadowWriteBlob = true
	cfg.SkillStorage.AllowLegacyTmpConfirm = true
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if err := applyEnvOverrides(&cfg); err != nil {
		return nil, err
	}
	if err := validateConfig(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
```

`validateConfig` 会校验 MySQL / Redis / AgentEnd / Server 端口范围、必填 host / user / dbname、JWT secret 和过期时间、Admin 密码等关键字段；`agentend.service_auth_enabled=true` 时还要求同时设置 `AGENTEND_SERVICE_TOKEN` 与 `BACKEND_SERVICE_TOKEN` 两个环境变量；`mysql.charset` 为空时默认回填 `utf8mb4`。头像 Storage 的 `write_provider` 只能是 `minio` 或 `local`：MinIO Writer 必须启用 Asset MinIO 并提供 endpoint / Bucket / 独立凭据（SecretKey 至少 8 个字符），本地 Writer 必须启用本地存储；两套 Provider 可以同时启用读取，但不会自动 fallback 或双写。MinIO 的 request timeout 必须为正时长，`ca_file` 必须可读且包含证书，本地 URL prefix 必须是同源路径。`validateArtifactStorageConfig` 校验内置资源存储：`max_object_size` 不超过 25MiB、`max_artifacts_per_message` ≤ 1000、`upload_token_ttl` / `failed_retention` 为正时长；启用时还要求 endpoint / bucket / 凭据（SecretKey 至少 8 字符）、`capability_secret` 至少 32 字符，且禁止与头像或 Skill 存储共用 Bucket 或应用账号。启用 `skill_storage.enabled` 时还会校验 Skill MinIO endpoint / bucket / 凭据、`read_preference`（`minio` / `db`）、各 duration 字段和 AgentEnd 兼容的 ZIP 大小 / 文件数上限（`validateSkillZipLimits`）。当 `APP_ENV=production` / `APP_ENV=prod` 或 `GIN_MODE=release` 时，默认 JWT secret（`agenthub-demo-secret`）和默认 Admin 密码（`123456`）会直接拒绝启动，启用任一 MinIO 存储时还要求 `use_ssl=true` 且 endpoint 不得使用 `http://`，普通 API Auth 也会默认开启，除非显式设置 `API_AUTH_ENABLED=false`。这些问题会在启动阶段 fail-fast，而不是延迟到请求处理或数据库连接阶段。

`applyEnvOverrides` 支持的环境变量（非空时覆盖 YAML 值）：

| 段 | 环境变量 |
|----|----------|
| MySQL | `MYSQL_HOST`、`MYSQL_PORT`、`MYSQL_USER`、`MYSQL_PASSWORD`、`MYSQL_DBNAME`、`MYSQL_CHARSET` |
| JWT | `JWT_SECRET`、`JWT_EXPIRE_HOURS` |
| AgentEnd | `AGENTEND_HOST`、`AGENTEND_PORT`、`AGENTEND_SERVICE_AUTH_ENABLED`（启用后须配 `AGENTEND_SERVICE_TOKEN` / `BACKEND_SERVICE_TOKEN`） |
| Redis | `REDIS_HOST`、`REDIS_PORT`、`REDIS_PASSWORD`、`REDIS_DB` |
| CORS | `CORS_ALLOW_ORIGINS`（逗号分隔） |
| Admin | `ADMIN_PASSWORD` |
| Server | `SERVER_PORT` |
| Auth | `API_AUTH_ENABLED`（`true` / `false`） |
| ArtifactStorage | `ARTIFACT_STORAGE_ENABLED`、`ARTIFACT_MINIO_ENDPOINT`、`ARTIFACT_MINIO_BUCKET`、`ARTIFACT_MINIO_ACCESS_KEY`、`ARTIFACT_MINIO_SECRET_KEY`、`ARTIFACT_MINIO_USE_SSL`、`ARTIFACT_MINIO_CA_CERT`、`ARTIFACT_MINIO_REQUEST_TIMEOUT`、`ARTIFACT_MAX_OBJECT_SIZE`、`ARTIFACT_MAX_PER_MESSAGE`、`ARTIFACT_UPLOAD_TOKEN_TTL`、`ARTIFACT_CAPABILITY_SECRET`、`ARTIFACT_FAILED_RETENTION` |
| SkillStorage | `SKILL_STORAGE_ENABLED`、`SKILL_STORAGE_REQUIRE_ADMIN`、`SKILL_STORAGE_READ_PREFERENCE`、`SKILL_STORAGE_SHADOW_WRITE_BLOB`、`SKILL_STORAGE_ALLOW_LEGACY_TMP_CONFIRM`、`SKILL_STORAGE_UPLOAD_SESSION_TTL`、`SKILL_STORAGE_RECEIPT_RETENTION`、`SKILL_STORAGE_CONFIRM_LEASE`、`SKILL_STORAGE_ORPHAN_GRACE_PERIOD`、`SKILL_STORAGE_INCOMING_TTL`、`SKILL_STORAGE_TEMP_DIR`、`SKILL_STORAGE_MIN_TEMP_FREE_BYTES`、`SKILL_STORAGE_MAX_UPLOAD_SIZE`、`SKILL_STORAGE_MAX_PACKAGE_SIZE`、`SKILL_STORAGE_MAX_FILE_SIZE`、`SKILL_STORAGE_MAX_UNPACKED_SIZE`、`SKILL_STORAGE_MAX_COMPRESSION_RATIO`、`SKILL_STORAGE_MAX_FILE_COUNT`、`SKILL_STORAGE_MAX_CONCURRENT_VALIDATIONS`、`SKILL_STORAGE_VALIDATION_TIMEOUT`、`SKILL_STORAGE_REJECT_BINARIES`、`SKILL_STORAGE_REJECT_EXECUTABLES`、`SKILL_STORAGE_CONTENT_SCAN_COMMAND`、`SKILL_STORAGE_CONTENT_SCAN_TIMEOUT` |
| Avatar Storage | `AVATAR_STORAGE_WRITE_PROVIDER`、`ASSET_MINIO_ENABLED`、`ASSET_MINIO_ENDPOINT`、`ASSET_MINIO_BUCKET`、`ASSET_MINIO_ACCESS_KEY`、`ASSET_MINIO_SECRET_KEY`、`ASSET_MINIO_USE_SSL`、`ASSET_MINIO_CA_CERT`、`ASSET_MINIO_REQUEST_TIMEOUT`、`LOCAL_STORAGE_ENABLED`、`LOCAL_STORAGE_DIR`、`LOCAL_STORAGE_URL_PREFIX` |
| Skill MinIO | `MINIO_ENDPOINT`、`MINIO_BUCKET`、`MINIO_ACCESS_KEY`、`MINIO_SECRET_KEY`、`MINIO_USE_SSL`、`MINIO_CA_CERT` |

### YAML 文件 (`configs/config.yaml`)

```yaml
mysql:
  host: 127.0.0.1
  port: 3306
  user: root
  password: "123456"
  dbname: agenthub
  charset: utf8mb4

jwt:
  secret: agenthub-demo-secret
  expire_hours: 24

agentend:
  host: http://localhost
  port: 8001
  service_auth_enabled: false

server:
  port: 8080

auth:
  enabled: false

redis:
  host: 127.0.0.1
  port: 6379
  password: ""
  db: 0

admin:
  password: "123456"

storage:
  write_provider: minio
  minio:
    enabled: true
    endpoint: 127.0.0.1:19000
    bucket: agenthub-assets
    access_key: ""
    secret_key: ""
    use_ssl: false
    ca_file: ""
    request_timeout: 10s
  local:
    enabled: true
    dir: "./uploads"
    url_prefix: "/uploads"

skill_storage:
  enabled: false              # 启用 MinIO 私有技能包存储；false 时走 DB blob 兼容路径
  require_admin: true         # Skill Hub 写操作（upload/confirm/delete）需 Admin JWT
  type: "minio"               # 启用时强制 minio
  endpoint: ""                # MinIO 地址，生产环境必须 https
  bucket: ""
  use_ssl: false
  read_preference: "minio"    # "minio" | "db"（db 需配合 shadow_write_blob=true）
  shadow_write_blob: true     # 迁移期双写 DB blob，便于回滚
  allow_legacy_tmp_confirm: true
  temp_dir: "./data/skill-tmp"
  max_upload_size: "10MiB"    # 受 AgentEnd 10MiB 上限约束
  max_package_size: "12MiB"
  max_file_size: "10MiB"
  max_unpacked_size: "50MiB"
  max_compression_ratio: 100
  max_file_count: 200

artifact_storage:             # 内置资源（Artifact）私有对象存储，feature-gated
  enabled: false
  endpoint: ""
  bucket: ""                  # 必须与 storage.minio.bucket / skill_storage.bucket 不同
  access_key: ""
  secret_key: ""              # 至少 8 字符
  use_ssl: false
  ca_file: ""
  request_timeout: "15s"
  max_object_size: "25MiB"    # 上限 25MiB
  max_artifacts_per_message: 20
  upload_token_ttl: "30m"
  capability_secret: ""       # 至少 32 字符，且不得复用 jwt/admin/storage/skill 密钥
  failed_retention: "24h"

cors:
  allow_origins:
    - "http://localhost:5173"
```

> `.env` 模板见 [`backend/.env.example`](../../../backend/.env.example)，首次运行前 `cp .env.example .env` 并填入 Asset MinIO 应用凭据。没有 MinIO 时必须显式设置 `AVATAR_STORAGE_WRITE_PROVIDER=local`、`ASSET_MINIO_ENABLED=false` 和 `LOCAL_STORAGE_ENABLED=true`，不会根据凭据是否为空自动切换。

存储层通过 `StorageConfig.WriteProvider` 控制唯一写入目标：`minio` 是默认值，`local` 是显式本地模式。`minio.enabled` 与 `local.enabled` 分别控制 MinIO 代理和 `/uploads` 静态读取；两者可同时开启。`pkg/storage/` 包提供 `Provider`、`ObjectReader` 和 `Runtime`，Controller 通过构造函数注入所需接口。

技能包私有对象存储由独立的 `SkillStorageConfig` 控制（feature-gated，`enabled: false` 时走 DB blob 兼容路径）。启用时使用 `pkg/package_store/` 的 MinIO 实现（`MinIOStore`），配合 `pkg/skill_upload_session/` 的 Redis 上传会话与 `SkillOperationJob` outbox 做补偿。详见 [05-wiring.md](05-wiring.md)。

内置资源（Artifact）私有对象存储由独立的 `ArtifactStorageConfig` 控制（feature-gated，`enabled: false` 时 `ArtifactController` 不挂任何路由）。启用时使用 `pkg/artifact_store/` 的 MinIO 实现，Backend 用 `capability_secret` 签发短期 token，AgentEnd 凭 token 直传对象，元数据落在 `Artifact` 表；启动校验强制它与头像、Skill 存储使用不同 Bucket 与应用账号。详见 [05-wiring.md](05-wiring.md)。
