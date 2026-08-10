# Config — 配置加载

## 实现了什么

通过 YAML 文件 + `.env` 环境变量双层机制加载配置，涵盖 MySQL、JWT、AgentEnd、Server、Auth、七牛云、Storage、SkillStorage、Redis、Admin、CORS 十一个模块。敏感信息（七牛云 / MinIO 密钥）从环境变量注入，不硬编码在 YAML 中。

## 怎么实现的

### Config 结构体 (`internal/conf/conf.go`)

```go
type Config struct {
	MySQL        MySQLConfig        `yaml:"mysql"`
	JWT          JWTConfig          `yaml:"jwt"`
	AgentEnd     AgentEndConfig     `yaml:"agentend"`
	Qiniu        QiniuConfig        `yaml:"qiniu"`
	Storage      StorageConfig      `yaml:"storage"`
	SkillStorage SkillStorageConfig `yaml:"skill_storage"`
	Redis        RedisConfig        `yaml:"redis"`
	Admin        AdminConfig        `yaml:"admin"`
	CORS         CORSConfig         `yaml:"cors"`
	Server       ServerConfig       `yaml:"server"`
	Auth         AuthConfig         `yaml:"auth"`
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
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
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

type QiniuConfig struct {
	AccessKey string `yaml:"access_key"`
	SecretKey string `yaml:"secret_key"`
	Bucket    string `yaml:"bucket"`
	Domain    string `yaml:"domain"`
	Region    string `yaml:"region"`
}

type LocalStorageConfig struct {
	Dir       string `yaml:"dir"`
	URLPrefix string `yaml:"url_prefix"`
}

type StorageConfig struct {
	Type  string             `yaml:"type"` // "qiniu" | "local" | "" (auto-detect)
	Local LocalStorageConfig `yaml:"local"`
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

`Load` 先尝试加载可选的 `.env` 文件，再预置 SkillStorage 安全默认值（`RequireAdmin=true`、`ShadowWriteBlob=true`、`AllowLegacyTmpConfirm=true`），读取 YAML 配置后覆盖七牛云密钥，执行 `applyEnvOverrides` 用环境变量覆盖 MySQL / JWT / AgentEnd / Redis / CORS / Admin / Server / SkillStorage 等连接参数（便于 Docker / CI 注入），最后通过 `validateConfig` 做启动前校验：

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

	cfg.Qiniu.AccessKey = os.Getenv("QINIU_ACCESS_KEY")
	cfg.Qiniu.SecretKey = os.Getenv("QINIU_SECRET_KEY")

	if err := applyEnvOverrides(&cfg); err != nil {
		return nil, err
	}
	if err := validateConfig(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
```

`validateConfig` 会校验 MySQL / Redis / AgentEnd / Server 端口范围、必填 host / user / dbname、JWT secret 和过期时间、Admin 密码等关键字段；`mysql.charset` 为空时默认回填 `utf8mb4`。启用 `skill_storage.enabled` 时还会校验 MinIO endpoint / bucket / 凭据、`read_preference`（`minio` / `db`）、各 duration 字段（`incoming_ttl` 须大于 `upload_session_ttl + confirm_lease`，`orphan_grace_period` 须大于 `confirm_lease`），以及 AgentEnd 兼容的 ZIP 大小 / 文件数上限（`validateSkillZipLimits`）。当 `APP_ENV=production` / `APP_ENV=prod` 或 `GIN_MODE=release` 时，默认 JWT secret（`agenthub-demo-secret`）和默认 Admin 密码（`123456`）会直接拒绝启动，启用 Skill 存储时还要求 `use_ssl=true` 且 endpoint 不得使用 `http://`，普通 API Auth 也会默认开启，除非显式设置 `API_AUTH_ENABLED=false`。这些问题会在启动阶段 fail-fast，而不是延迟到请求处理或数据库连接阶段。

`applyEnvOverrides` 支持的环境变量（非空时覆盖 YAML 值）：

| 段 | 环境变量 |
|----|----------|
| MySQL | `MYSQL_HOST`、`MYSQL_PORT`、`MYSQL_USER`、`MYSQL_PASSWORD`、`MYSQL_DBNAME`、`MYSQL_CHARSET` |
| JWT | `JWT_SECRET`、`JWT_EXPIRE_HOURS` |
| AgentEnd | `AGENTEND_HOST`、`AGENTEND_PORT` |
| Redis | `REDIS_HOST`、`REDIS_PORT`、`REDIS_PASSWORD`、`REDIS_DB` |
| CORS | `CORS_ALLOW_ORIGINS`（逗号分隔） |
| Admin | `ADMIN_PASSWORD` |
| Server | `SERVER_PORT` |
| Auth | `API_AUTH_ENABLED`（`true` / `false`） |
| SkillStorage | `SKILL_STORAGE_ENABLED`、`SKILL_STORAGE_REQUIRE_ADMIN`、`SKILL_STORAGE_READ_PREFERENCE`、`SKILL_STORAGE_SHADOW_WRITE_BLOB`、`SKILL_STORAGE_ALLOW_LEGACY_TMP_CONFIRM`、`SKILL_STORAGE_UPLOAD_SESSION_TTL`、`SKILL_STORAGE_RECEIPT_RETENTION`、`SKILL_STORAGE_CONFIRM_LEASE`、`SKILL_STORAGE_ORPHAN_GRACE_PERIOD`、`SKILL_STORAGE_INCOMING_TTL`、`SKILL_STORAGE_TEMP_DIR`、`SKILL_STORAGE_MIN_TEMP_FREE_BYTES`、`SKILL_STORAGE_MAX_UPLOAD_SIZE`、`SKILL_STORAGE_MAX_PACKAGE_SIZE`、`SKILL_STORAGE_MAX_FILE_SIZE`、`SKILL_STORAGE_MAX_UNPACKED_SIZE`、`SKILL_STORAGE_MAX_COMPRESSION_RATIO`、`SKILL_STORAGE_MAX_FILE_COUNT`、`SKILL_STORAGE_MAX_CONCURRENT_VALIDATIONS`、`SKILL_STORAGE_VALIDATION_TIMEOUT`、`SKILL_STORAGE_REJECT_BINARIES`、`SKILL_STORAGE_REJECT_EXECUTABLES`、`SKILL_STORAGE_CONTENT_SCAN_COMMAND`、`SKILL_STORAGE_CONTENT_SCAN_TIMEOUT` |
| MinIO 连接 | `MINIO_ENDPOINT`、`MINIO_BUCKET`、`MINIO_ACCESS_KEY`、`MINIO_SECRET_KEY`、`MINIO_USE_SSL`、`MINIO_CA_CERT` |

七牛云密钥仍走 `QINIU_ACCESS_KEY` / `QINIU_SECRET_KEY`，不通过 `applyEnvOverrides` 通道。

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

qiniu:
  bucket: "agenthub"
  domain: "http://tfj4mvkda.hd-bkt.clouddn.com"
  region: z0    # z0=华东 z1=华北 z2=华南 na0=北美

storage:
  type: ""          # "qiniu" | "local" | "" 自动检测（有 QINIU_ACCESS_KEY 则七牛云，否则本地）
  local:
    dir: "./uploads"
    url_prefix: "http://localhost:8080/uploads"

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

cors:
  allow_origins:
    - "http://localhost:5173"
```

七牛云 `access_key` / `secret_key` 不在 YAML 中配置，通过 `QINIU_ACCESS_KEY` / `QINIU_SECRET_KEY` 环境变量注入。

> `.env` 模板见 [`backend/.env.example`](../../../backend/.env.example)，首次运行前 `cp .env.example .env` 并填入实际密钥；留空则自动回退到本地磁盘存储。

存储层通过 `StorageConfig.Type` 控制策略：空字符串自动检测（有七牛云密钥则用七牛云，否则本地磁盘），`"qiniu"` 强制七牛云，`"local"` 强制本地磁盘。`pkg/storage/` 包提供统一的 `Provider` 接口，Controller 通过构造函数注入 `storage.Provider`。

技能包私有对象存储由独立的 `SkillStorageConfig` 控制（feature-gated，`enabled: false` 时走 DB blob 兼容路径）。启用时使用 `pkg/package_store/` 的 MinIO 实现（`MinIOStore`），配合 `pkg/skill_upload_session/` 的 Redis 上传会话与 `SkillOperationJob` outbox 做补偿。详见 [05-wiring.md](05-wiring.md)。
