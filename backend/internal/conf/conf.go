package conf

import (
	"crypto/x509"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

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

type JWTConfig struct {
	Secret      string `yaml:"secret"`
	ExpireHours int    `yaml:"expire_hours"`
}

type AgentEndConfig struct {
	Host               string `yaml:"host"`
	Port               int    `yaml:"port"`
	ServiceAuthEnabled bool   `yaml:"service_auth_enabled"`
}

func (c *AgentEndConfig) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
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
	WriteProvider string             `yaml:"write_provider"`
	MinIO         AvatarMinIOConfig  `yaml:"minio"`
	Local         LocalStorageConfig `yaml:"local"`
}

// ArtifactStorageConfig controls the private object store used by builtin
// skills. Artifact data stays in a dedicated bucket, while deployments may
// reuse the same MinIO application credentials as avatar and Skill storage.
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

const maxArtifactObjectSize = 25 * 1024 * 1024

// SkillStorageConfig controls the feature-gated private Skill package store.
// Size and duration values remain strings so YAML accepts human-readable values
// such as "10MiB" and "15m"; the Skill package parses them at use sites.
type SkillStorageConfig struct {
	Enabled                  bool   `yaml:"enabled"`
	RequireAdmin             bool   `yaml:"require_admin"`
	Type                     string `yaml:"type"`
	Endpoint                 string `yaml:"endpoint"`
	Bucket                   string `yaml:"bucket"`
	AccessKey                string `yaml:"access_key"`
	SecretKey                string `yaml:"secret_key"`
	UseSSL                   bool   `yaml:"use_ssl"`
	CAFile                   string `yaml:"ca_file"`
	UploadSessionTTL         string `yaml:"upload_session_ttl"`
	ReceiptRetention         string `yaml:"receipt_retention"`
	ReadPreference           string `yaml:"read_preference"`
	ShadowWriteBlob          bool   `yaml:"shadow_write_blob"`
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

type RedisConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

func (c *RedisConfig) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
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

func Load(path string) (*Config, error) {
	// .env 是可选的 —— 不存在时不报错
	_ = godotenv.Load()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}
	var cfg Config
	// Skill management is privileged by default.  An explicit YAML false can
	// opt out for a trusted single-user development deployment.
	cfg.SkillStorage.RequireAdmin = true
	// Keep the migration rollback copy and old confirmation protocol enabled
	// unless an operator explicitly closes those gates after an observation
	// period.
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

func applyEnvOverrides(cfg *Config) error {
	if v := os.Getenv("MYSQL_HOST"); v != "" {
		cfg.MySQL.Host = v
	}
	if v := os.Getenv("MYSQL_PORT"); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("parse MYSQL_PORT: %w", err)
		}
		cfg.MySQL.Port = port
	}
	if v := os.Getenv("MYSQL_USER"); v != "" {
		cfg.MySQL.User = v
	}
	if v := os.Getenv("MYSQL_PASSWORD"); v != "" {
		cfg.MySQL.Password = v
	}
	if v := os.Getenv("MYSQL_DBNAME"); v != "" {
		cfg.MySQL.DBName = v
	}
	if v := os.Getenv("MYSQL_CHARSET"); v != "" {
		cfg.MySQL.Charset = v
	}

	if v := os.Getenv("JWT_SECRET"); v != "" {
		cfg.JWT.Secret = v
	}
	if v := os.Getenv("JWT_EXPIRE_HOURS"); v != "" {
		hours, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("parse JWT_EXPIRE_HOURS: %w", err)
		}
		cfg.JWT.ExpireHours = hours
	}

	if v := os.Getenv("AGENTEND_HOST"); v != "" {
		cfg.AgentEnd.Host = v
	}
	if v := os.Getenv("AGENTEND_PORT"); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("parse AGENTEND_PORT: %w", err)
		}
		cfg.AgentEnd.Port = port
	}
	if v := os.Getenv("AGENTEND_SERVICE_AUTH_ENABLED"); v != "" {
		enabled, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("parse AGENTEND_SERVICE_AUTH_ENABLED: %w", err)
		}
		cfg.AgentEnd.ServiceAuthEnabled = enabled
	}

	if v := os.Getenv("AVATAR_STORAGE_WRITE_PROVIDER"); v != "" {
		cfg.Storage.WriteProvider = v
	}
	if v := os.Getenv("ASSET_MINIO_ENABLED"); v != "" {
		enabled, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("parse ASSET_MINIO_ENABLED: %w", err)
		}
		cfg.Storage.MinIO.Enabled = enabled
	}
	if v := os.Getenv("ASSET_MINIO_ENDPOINT"); v != "" {
		cfg.Storage.MinIO.Endpoint = v
	}
	if v := os.Getenv("ASSET_MINIO_BUCKET"); v != "" {
		cfg.Storage.MinIO.Bucket = v
	}
	if v := os.Getenv("ASSET_MINIO_ACCESS_KEY"); v != "" {
		cfg.Storage.MinIO.AccessKey = v
	}
	if v := os.Getenv("ASSET_MINIO_SECRET_KEY"); v != "" {
		cfg.Storage.MinIO.SecretKey = v
	}
	if v := os.Getenv("ASSET_MINIO_USE_SSL"); v != "" {
		useSSL, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("parse ASSET_MINIO_USE_SSL: %w", err)
		}
		cfg.Storage.MinIO.UseSSL = useSSL
	}
	if v := os.Getenv("ASSET_MINIO_CA_CERT"); v != "" {
		cfg.Storage.MinIO.CAFile = v
	}
	if v := os.Getenv("ASSET_MINIO_REQUEST_TIMEOUT"); v != "" {
		cfg.Storage.MinIO.RequestTimeout = v
	}
	if v := os.Getenv("LOCAL_STORAGE_ENABLED"); v != "" {
		enabled, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("parse LOCAL_STORAGE_ENABLED: %w", err)
		}
		cfg.Storage.Local.Enabled = enabled
	}
	if v := os.Getenv("LOCAL_STORAGE_DIR"); v != "" {
		cfg.Storage.Local.Dir = v
	}
	if v := os.Getenv("LOCAL_STORAGE_URL_PREFIX"); v != "" {
		cfg.Storage.Local.URLPrefix = v
	}
	if v := os.Getenv("ARTIFACT_STORAGE_ENABLED"); v != "" {
		enabled, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("parse ARTIFACT_STORAGE_ENABLED: %w", err)
		}
		cfg.ArtifactStorage.Enabled = enabled
	}
	if v := os.Getenv("ARTIFACT_MINIO_ENDPOINT"); v != "" {
		cfg.ArtifactStorage.Endpoint = v
	}
	if v := os.Getenv("ARTIFACT_MINIO_BUCKET"); v != "" {
		cfg.ArtifactStorage.Bucket = v
	}
	if v := os.Getenv("ARTIFACT_MINIO_ACCESS_KEY"); v != "" {
		cfg.ArtifactStorage.AccessKey = v
	}
	if v := os.Getenv("ARTIFACT_MINIO_SECRET_KEY"); v != "" {
		cfg.ArtifactStorage.SecretKey = v
	}
	if v := os.Getenv("ARTIFACT_MINIO_USE_SSL"); v != "" {
		useSSL, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("parse ARTIFACT_MINIO_USE_SSL: %w", err)
		}
		cfg.ArtifactStorage.UseSSL = useSSL
	}
	if v := os.Getenv("ARTIFACT_MINIO_CA_CERT"); v != "" {
		cfg.ArtifactStorage.CAFile = v
	}
	if v := os.Getenv("ARTIFACT_MINIO_REQUEST_TIMEOUT"); v != "" {
		cfg.ArtifactStorage.RequestTimeout = v
	}
	if v := os.Getenv("ARTIFACT_MAX_OBJECT_SIZE"); v != "" {
		cfg.ArtifactStorage.MaxObjectSize = v
	}
	if v := os.Getenv("ARTIFACT_MAX_PER_MESSAGE"); v != "" {
		limit, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("parse ARTIFACT_MAX_PER_MESSAGE: %w", err)
		}
		cfg.ArtifactStorage.MaxArtifactsPerMsg = limit
	}
	if v := os.Getenv("ARTIFACT_UPLOAD_TOKEN_TTL"); v != "" {
		cfg.ArtifactStorage.UploadTokenTTL = v
	}
	if v := os.Getenv("ARTIFACT_CAPABILITY_SECRET"); v != "" {
		cfg.ArtifactStorage.CapabilitySecret = v
	}
	if v := os.Getenv("ARTIFACT_FAILED_RETENTION"); v != "" {
		cfg.ArtifactStorage.FailedRetention = v
	}

	if v := os.Getenv("REDIS_HOST"); v != "" {
		cfg.Redis.Host = v
	}
	if v := os.Getenv("REDIS_PORT"); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("parse REDIS_PORT: %w", err)
		}
		cfg.Redis.Port = port
	}
	if v := os.Getenv("REDIS_PASSWORD"); v != "" {
		cfg.Redis.Password = v
	}
	if v := os.Getenv("REDIS_DB"); v != "" {
		db, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("parse REDIS_DB: %w", err)
		}
		cfg.Redis.DB = db
	}

	if v := os.Getenv("CORS_ALLOW_ORIGINS"); v != "" {
		cfg.CORS.AllowOrigins = splitCSV(v)
	}
	if v := os.Getenv("ADMIN_PASSWORD"); v != "" {
		cfg.Admin.Password = v
	}
	if v := os.Getenv("API_AUTH_ENABLED"); v != "" {
		enabled, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("parse API_AUTH_ENABLED: %w", err)
		}
		cfg.Auth.Enabled = enabled
	} else if isProductionMode() {
		cfg.Auth.Enabled = true
	}
	if v := os.Getenv("SKILL_STORAGE_ENABLED"); v != "" {
		enabled, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("parse SKILL_STORAGE_ENABLED: %w", err)
		}
		cfg.SkillStorage.Enabled = enabled
	}
	if v := os.Getenv("SKILL_STORAGE_REQUIRE_ADMIN"); v != "" {
		requireAdmin, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("parse SKILL_STORAGE_REQUIRE_ADMIN: %w", err)
		}
		cfg.SkillStorage.RequireAdmin = requireAdmin
	}
	if v := os.Getenv("MINIO_ENDPOINT"); v != "" {
		cfg.SkillStorage.Endpoint = v
	}
	if v := os.Getenv("MINIO_BUCKET"); v != "" {
		cfg.SkillStorage.Bucket = v
	}
	if v := os.Getenv("MINIO_ACCESS_KEY"); v != "" {
		cfg.SkillStorage.AccessKey = v
	}
	if v := os.Getenv("MINIO_SECRET_KEY"); v != "" {
		cfg.SkillStorage.SecretKey = v
	}
	// MINIO_ACCESS_KEY/MINIO_SECRET_KEY are the shared application credentials.
	// Storage-specific YAML fields and environment variables remain supported;
	// the shared pair only fills credentials that were intentionally left empty.
	if cfg.Storage.MinIO.AccessKey == "" {
		cfg.Storage.MinIO.AccessKey = os.Getenv("MINIO_ACCESS_KEY")
	}
	if cfg.Storage.MinIO.SecretKey == "" {
		cfg.Storage.MinIO.SecretKey = os.Getenv("MINIO_SECRET_KEY")
	}
	if cfg.ArtifactStorage.AccessKey == "" {
		cfg.ArtifactStorage.AccessKey = os.Getenv("MINIO_ACCESS_KEY")
	}
	if cfg.ArtifactStorage.SecretKey == "" {
		cfg.ArtifactStorage.SecretKey = os.Getenv("MINIO_SECRET_KEY")
	}
	if v := os.Getenv("MINIO_USE_SSL"); v != "" {
		useSSL, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("parse MINIO_USE_SSL: %w", err)
		}
		cfg.SkillStorage.UseSSL = useSSL
	}
	if v := os.Getenv("MINIO_CA_CERT"); v != "" {
		cfg.SkillStorage.CAFile = v
	}
	if v := os.Getenv("SKILL_STORAGE_UPLOAD_SESSION_TTL"); v != "" {
		cfg.SkillStorage.UploadSessionTTL = v
	}
	if v := os.Getenv("SKILL_STORAGE_RECEIPT_RETENTION"); v != "" {
		cfg.SkillStorage.ReceiptRetention = v
	}
	if v := os.Getenv("SKILL_STORAGE_READ_PREFERENCE"); v != "" {
		cfg.SkillStorage.ReadPreference = v
	}
	if v := os.Getenv("SKILL_STORAGE_SHADOW_WRITE_BLOB"); v != "" {
		shadow, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("parse SKILL_STORAGE_SHADOW_WRITE_BLOB: %w", err)
		}
		cfg.SkillStorage.ShadowWriteBlob = shadow
	}
	if v := os.Getenv("SKILL_STORAGE_ALLOW_LEGACY_TMP_CONFIRM"); v != "" {
		allowed, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("parse SKILL_STORAGE_ALLOW_LEGACY_TMP_CONFIRM: %w", err)
		}
		cfg.SkillStorage.AllowLegacyTmpConfirm = allowed
	}
	if v := os.Getenv("SKILL_STORAGE_CONFIRM_LEASE"); v != "" {
		cfg.SkillStorage.ConfirmLease = v
	}
	if v := os.Getenv("SKILL_STORAGE_ORPHAN_GRACE_PERIOD"); v != "" {
		cfg.SkillStorage.OrphanGracePeriod = v
	}
	if v := os.Getenv("SKILL_STORAGE_INCOMING_TTL"); v != "" {
		cfg.SkillStorage.IncomingTTL = v
	}
	if v := os.Getenv("SKILL_STORAGE_TEMP_DIR"); v != "" {
		cfg.SkillStorage.TempDir = v
	}
	if v := os.Getenv("SKILL_STORAGE_MIN_TEMP_FREE_BYTES"); v != "" {
		cfg.SkillStorage.MinTempFreeBytes = v
	}
	if v := os.Getenv("SKILL_STORAGE_MAX_UPLOAD_SIZE"); v != "" {
		cfg.SkillStorage.MaxUploadSize = v
	}
	if v := os.Getenv("SKILL_STORAGE_MAX_PACKAGE_SIZE"); v != "" {
		cfg.SkillStorage.MaxPackageSize = v
	}
	if v := os.Getenv("SKILL_STORAGE_MAX_FILE_SIZE"); v != "" {
		cfg.SkillStorage.MaxFileSize = v
	}
	if v := os.Getenv("SKILL_STORAGE_MAX_UNPACKED_SIZE"); v != "" {
		cfg.SkillStorage.MaxUnpackedSize = v
	}
	if v := os.Getenv("SKILL_STORAGE_MAX_COMPRESSION_RATIO"); v != "" {
		ratio, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return fmt.Errorf("parse SKILL_STORAGE_MAX_COMPRESSION_RATIO: %w", err)
		}
		cfg.SkillStorage.MaxCompressionRatio = ratio
	}
	if v := os.Getenv("SKILL_STORAGE_MAX_FILE_COUNT"); v != "" {
		count, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("parse SKILL_STORAGE_MAX_FILE_COUNT: %w", err)
		}
		cfg.SkillStorage.MaxFileCount = count
	}
	if v := os.Getenv("SKILL_STORAGE_MAX_CONCURRENT_VALIDATIONS"); v != "" {
		concurrency, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("parse SKILL_STORAGE_MAX_CONCURRENT_VALIDATIONS: %w", err)
		}
		cfg.SkillStorage.MaxConcurrentValidations = concurrency
	}
	if v := os.Getenv("SKILL_STORAGE_VALIDATION_TIMEOUT"); v != "" {
		cfg.SkillStorage.ValidationTimeout = v
	}
	if v := os.Getenv("SKILL_STORAGE_REJECT_BINARIES"); v != "" {
		reject, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("parse SKILL_STORAGE_REJECT_BINARIES: %w", err)
		}
		cfg.SkillStorage.RejectBinaries = reject
	}
	if v := os.Getenv("SKILL_STORAGE_REJECT_EXECUTABLES"); v != "" {
		reject, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("parse SKILL_STORAGE_REJECT_EXECUTABLES: %w", err)
		}
		cfg.SkillStorage.RejectExecutables = reject
	}
	if v := os.Getenv("SKILL_STORAGE_CONTENT_SCAN_COMMAND"); v != "" {
		cfg.SkillStorage.ContentScanCommand = v
	}
	if v := os.Getenv("SKILL_STORAGE_CONTENT_SCAN_TIMEOUT"); v != "" {
		cfg.SkillStorage.ContentScanTimeout = v
	}
	if v := os.Getenv("SERVER_PORT"); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("parse SERVER_PORT: %w", err)
		}
		cfg.Server.Port = port
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	return nil
}

func validateConfig(cfg *Config) error {
	if cfg.MySQL.Host == "" {
		return fmt.Errorf("mysql host is required")
	}
	if cfg.MySQL.Port <= 0 || cfg.MySQL.Port > 65535 {
		return fmt.Errorf("mysql port must be between 1 and 65535")
	}
	if cfg.MySQL.User == "" {
		return fmt.Errorf("mysql user is required")
	}
	if cfg.MySQL.DBName == "" {
		return fmt.Errorf("mysql dbname is required")
	}
	if cfg.MySQL.Charset == "" {
		cfg.MySQL.Charset = "utf8mb4"
	}

	if cfg.JWT.Secret == "" {
		return fmt.Errorf("jwt secret is required")
	}
	if cfg.JWT.ExpireHours <= 0 {
		return fmt.Errorf("jwt expire_hours must be positive")
	}

	if cfg.AgentEnd.Host == "" {
		return fmt.Errorf("agentend host is required")
	}
	if cfg.AgentEnd.Port <= 0 || cfg.AgentEnd.Port > 65535 {
		return fmt.Errorf("agentend port must be between 1 and 65535")
	}
	if cfg.AgentEnd.ServiceAuthEnabled && strings.TrimSpace(os.Getenv("AGENTEND_SERVICE_TOKEN")) == "" {
		return fmt.Errorf("AGENTEND_SERVICE_TOKEN is required when agentend service auth is enabled")
	}
	if cfg.AgentEnd.ServiceAuthEnabled && strings.TrimSpace(os.Getenv("BACKEND_SERVICE_TOKEN")) == "" {
		return fmt.Errorf("BACKEND_SERVICE_TOKEN is required when agentend service auth is enabled")
	}
	if cfg.Redis.Host == "" {
		return fmt.Errorf("redis host is required")
	}
	if cfg.Redis.Port <= 0 || cfg.Redis.Port > 65535 {
		return fmt.Errorf("redis port must be between 1 and 65535")
	}
	if cfg.Redis.DB < 0 {
		return fmt.Errorf("redis db must be non-negative")
	}
	if cfg.Admin.Password == "" {
		return fmt.Errorf("admin password is required")
	}
	if err := validateAvatarStorageConfig(&cfg.Storage); err != nil {
		return err
	}
	if err := validateArtifactStorageConfig(&cfg.ArtifactStorage); err != nil {
		return err
	}
	// Keep upload staging on a dedicated volume even while MinIO is feature
	// gated off; the legacy DB-BLOB path still unpacks user archives locally.
	if strings.TrimSpace(cfg.SkillStorage.TempDir) == "" {
		cfg.SkillStorage.TempDir = "./data/skill-tmp"
	}
	// The DB-BLOB compatibility path still unpacks and validates archives when
	// MinIO is disabled, so the AgentEnd-compatible ZIP limits must be checked
	// and defaulted regardless of the storage feature gate.
	if err := validateSkillZipLimits(&cfg.SkillStorage); err != nil {
		return err
	}
	if cfg.SkillStorage.Enabled {
		if cfg.SkillStorage.Type == "" {
			cfg.SkillStorage.Type = "minio"
		}
		if cfg.SkillStorage.Type != "minio" {
			return fmt.Errorf("skill_storage.type must be minio when enabled")
		}
		if cfg.SkillStorage.Endpoint == "" || cfg.SkillStorage.Bucket == "" {
			return fmt.Errorf("skill_storage endpoint and bucket are required when enabled")
		}
		if cfg.SkillStorage.AccessKey == "" || cfg.SkillStorage.SecretKey == "" {
			return fmt.Errorf("skill_storage credentials are required when enabled")
		}
		if cfg.SkillStorage.ReadPreference == "" {
			cfg.SkillStorage.ReadPreference = "minio"
		}
		if cfg.SkillStorage.ReadPreference != "minio" && cfg.SkillStorage.ReadPreference != "db" {
			return fmt.Errorf("skill_storage.read_preference must be minio or db")
		}
		if cfg.SkillStorage.ReadPreference == "db" && !cfg.SkillStorage.ShadowWriteBlob {
			return fmt.Errorf("skill_storage.read_preference=db requires shadow_write_blob=true")
		}
		parsedDurations := map[string]time.Duration{}
		for field, raw := range map[string]string{
			"upload_session_ttl":   cfg.SkillStorage.UploadSessionTTL,
			"receipt_retention":    cfg.SkillStorage.ReceiptRetention,
			"confirm_lease":        cfg.SkillStorage.ConfirmLease,
			"orphan_grace_period":  cfg.SkillStorage.OrphanGracePeriod,
			"incoming_ttl":         cfg.SkillStorage.IncomingTTL,
			"validation_timeout":   cfg.SkillStorage.ValidationTimeout,
			"content_scan_timeout": cfg.SkillStorage.ContentScanTimeout,
		} {
			if raw == "" {
				continue
			}
			duration, err := time.ParseDuration(raw)
			if err != nil || duration <= 0 {
				return fmt.Errorf("skill_storage.%s must be a positive duration", field)
			}
			parsedDurations[field] = duration
		}
		// Incoming cleanup must retain an object beyond the Redis session and
		// confirmation lease.  Otherwise a slow confirmation could lose its
		// staging object while its session is still valid.
		sessionTTL := parsedDurations["upload_session_ttl"]
		if sessionTTL == 0 {
			sessionTTL = 15 * time.Minute
		}
		confirmLease := parsedDurations["confirm_lease"]
		if confirmLease == 0 {
			confirmLease = 2 * time.Minute
		}
		incomingTTL := parsedDurations["incoming_ttl"]
		if incomingTTL == 0 {
			incomingTTL = 24 * time.Hour
		}
		if incomingTTL < sessionTTL+confirmLease {
			return fmt.Errorf("skill_storage.incoming_ttl must exceed upload_session_ttl plus confirm_lease")
		}
		orphanGrace := parsedDurations["orphan_grace_period"]
		if orphanGrace == 0 {
			orphanGrace = 48 * time.Hour
		}
		if orphanGrace <= confirmLease {
			return fmt.Errorf("skill_storage.orphan_grace_period must exceed confirm_lease")
		}
		if cfg.Storage.MinIO.Enabled {
			if strings.TrimSpace(cfg.Storage.MinIO.Bucket) == strings.TrimSpace(cfg.SkillStorage.Bucket) {
				return fmt.Errorf("storage.minio.bucket and skill_storage.bucket must be different")
			}
		}
	}
	if cfg.ArtifactStorage.Enabled {
		capabilitySecret := strings.TrimSpace(cfg.ArtifactStorage.CapabilitySecret)
		for name, otherSecret := range map[string]string{
			"jwt.secret":                  cfg.JWT.Secret,
			"admin.password":              cfg.Admin.Password,
			"storage.minio.secret_key":    cfg.Storage.MinIO.SecretKey,
			"skill_storage.secret_key":    cfg.SkillStorage.SecretKey,
			"artifact_storage.secret_key": cfg.ArtifactStorage.SecretKey,
		} {
			if strings.TrimSpace(otherSecret) != "" && capabilitySecret == strings.TrimSpace(otherSecret) {
				return fmt.Errorf("artifact_storage.capability_secret must not reuse %s", name)
			}
		}
		if cfg.Storage.MinIO.Enabled {
			if strings.TrimSpace(cfg.ArtifactStorage.Bucket) == strings.TrimSpace(cfg.Storage.MinIO.Bucket) {
				return fmt.Errorf("storage.minio.bucket and artifact_storage.bucket must be different")
			}
		}
		if cfg.SkillStorage.Enabled {
			if strings.TrimSpace(cfg.ArtifactStorage.Bucket) == strings.TrimSpace(cfg.SkillStorage.Bucket) {
				return fmt.Errorf("skill_storage.bucket and artifact_storage.bucket must be different")
			}
		}
	}
	if err := validateSharedMinIOCredentials(cfg); err != nil {
		return err
	}
	if strings.ContainsAny(cfg.SkillStorage.ContentScanCommand, "\r\n") {
		return fmt.Errorf("skill_storage.content_scan_command must not contain newlines")
	}
	if cfg.SkillStorage.ContentScanCommand != "" && strings.TrimSpace(cfg.SkillStorage.ContentScanCommand) == "" {
		return fmt.Errorf("skill_storage.content_scan_command must not be blank")
	}
	if cfg.Server.Port <= 0 || cfg.Server.Port > 65535 {
		return fmt.Errorf("server port must be between 1 and 65535")
	}
	if isProductionMode() {
		if cfg.JWT.Secret == "agenthub-demo-secret" {
			return fmt.Errorf("jwt secret must be changed in production")
		}
		if cfg.Admin.Password == "123456" {
			return fmt.Errorf("admin password must be changed in production")
		}
		if cfg.SkillStorage.Enabled && !cfg.SkillStorage.UseSSL {
			return fmt.Errorf("skill_storage.use_ssl must be true in production")
		}
		if cfg.SkillStorage.Enabled && strings.HasPrefix(strings.ToLower(strings.TrimSpace(cfg.SkillStorage.Endpoint)), "http://") {
			return fmt.Errorf("skill_storage.endpoint must not use http in production")
		}
		if cfg.Storage.MinIO.Enabled {
			if !cfg.Storage.MinIO.UseSSL {
				return fmt.Errorf("storage.minio.use_ssl must be true in production")
			}
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(cfg.Storage.MinIO.Endpoint)), "http://") {
				return fmt.Errorf("storage.minio.endpoint must not use http in production")
			}
		}
		if cfg.ArtifactStorage.Enabled {
			if !cfg.ArtifactStorage.UseSSL {
				return fmt.Errorf("artifact_storage.use_ssl must be true in production")
			}
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(cfg.ArtifactStorage.Endpoint)), "http://") {
				return fmt.Errorf("artifact_storage.endpoint must not use http in production")
			}
		}
	}
	return nil
}

func validateSharedMinIOCredentials(cfg *Config) error {
	type credentials struct {
		name      string
		enabled   bool
		accessKey string
		secretKey string
	}
	stores := []credentials{
		{name: "storage.minio", enabled: cfg.Storage.MinIO.Enabled, accessKey: cfg.Storage.MinIO.AccessKey, secretKey: cfg.Storage.MinIO.SecretKey},
		{name: "skill_storage", enabled: cfg.SkillStorage.Enabled, accessKey: cfg.SkillStorage.AccessKey, secretKey: cfg.SkillStorage.SecretKey},
		{name: "artifact_storage", enabled: cfg.ArtifactStorage.Enabled, accessKey: cfg.ArtifactStorage.AccessKey, secretKey: cfg.ArtifactStorage.SecretKey},
	}
	for i := range stores {
		if !stores[i].enabled || strings.TrimSpace(stores[i].accessKey) == "" {
			continue
		}
		for j := i + 1; j < len(stores); j++ {
			if !stores[j].enabled || strings.TrimSpace(stores[i].accessKey) != strings.TrimSpace(stores[j].accessKey) {
				continue
			}
			if stores[i].secretKey != stores[j].secretKey {
				return fmt.Errorf("%s and %s use the same MinIO access_key but different secret_key values", stores[i].name, stores[j].name)
			}
		}
	}
	return nil
}

func validateArtifactStorageConfig(cfg *ArtifactStorageConfig) error {
	if cfg == nil {
		return fmt.Errorf("artifact storage config is required")
	}
	if strings.TrimSpace(cfg.RequestTimeout) == "" {
		cfg.RequestTimeout = "15s"
	}
	if _, err := ParsePositiveDuration(cfg.RequestTimeout, "artifact_storage.request_timeout", 15*time.Second); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.MaxObjectSize) == "" {
		cfg.MaxObjectSize = "25MiB"
	}
	maxSize, err := ParseByteSize(cfg.MaxObjectSize)
	if err != nil || maxSize <= 0 || maxSize > maxArtifactObjectSize {
		return fmt.Errorf("artifact_storage.max_object_size must be between 1 byte and 25MiB")
	}
	if cfg.MaxArtifactsPerMsg <= 0 {
		cfg.MaxArtifactsPerMsg = 20
	}
	if cfg.MaxArtifactsPerMsg > 1000 {
		return fmt.Errorf("artifact_storage.max_artifacts_per_message must not exceed 1000")
	}
	if strings.TrimSpace(cfg.UploadTokenTTL) == "" {
		cfg.UploadTokenTTL = "30m"
	}
	if _, err := ParsePositiveDuration(cfg.UploadTokenTTL, "artifact_storage.upload_token_ttl", 30*time.Minute); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.FailedRetention) == "" {
		cfg.FailedRetention = "24h"
	}
	if _, err := ParsePositiveDuration(cfg.FailedRetention, "artifact_storage.failed_retention", 24*time.Hour); err != nil {
		return err
	}
	if !cfg.Enabled {
		return nil
	}
	if strings.TrimSpace(cfg.Endpoint) == "" || strings.TrimSpace(cfg.Bucket) == "" {
		return fmt.Errorf("artifact_storage endpoint and bucket are required when enabled")
	}
	if strings.TrimSpace(cfg.AccessKey) == "" || strings.TrimSpace(cfg.SecretKey) == "" {
		return fmt.Errorf("artifact_storage credentials are required when enabled")
	}
	if len([]byte(cfg.SecretKey)) < 8 {
		return fmt.Errorf("artifact_storage secret_key must be at least 8 characters")
	}
	if strings.TrimSpace(cfg.CapabilitySecret) == "" || len([]byte(cfg.CapabilitySecret)) < 32 {
		return fmt.Errorf("artifact_storage capability_secret must be at least 32 characters when enabled")
	}
	if caFile := strings.TrimSpace(cfg.CAFile); caFile != "" {
		data, err := os.ReadFile(caFile)
		if err != nil {
			return fmt.Errorf("artifact_storage.ca_file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(data) {
			return fmt.Errorf("artifact_storage.ca_file contains no certificates")
		}
	}
	return nil
}

func validateAvatarStorageConfig(cfg *StorageConfig) error {
	if cfg == nil {
		return fmt.Errorf("storage config is required")
	}
	cfg.WriteProvider = strings.ToLower(strings.TrimSpace(cfg.WriteProvider))
	if cfg.WriteProvider == "" {
		cfg.WriteProvider = "minio"
	}
	if cfg.WriteProvider != "minio" && cfg.WriteProvider != "local" {
		return fmt.Errorf("storage.write_provider must be minio or local")
	}
	if strings.TrimSpace(cfg.MinIO.RequestTimeout) == "" {
		cfg.MinIO.RequestTimeout = "10s"
	}
	if _, err := ParsePositiveDuration(cfg.MinIO.RequestTimeout, "storage.minio.request_timeout", 10*time.Second); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.Local.Dir) == "" {
		cfg.Local.Dir = "./uploads"
	}
	if strings.TrimSpace(cfg.Local.URLPrefix) == "" {
		cfg.Local.URLPrefix = "/uploads"
	}
	if err := validateLocalURLPrefix(cfg.Local.URLPrefix); err != nil {
		return err
	}

	if cfg.MinIO.Enabled {
		if strings.TrimSpace(cfg.MinIO.Endpoint) == "" || strings.TrimSpace(cfg.MinIO.Bucket) == "" {
			return fmt.Errorf("storage.minio endpoint and bucket are required when enabled")
		}
		if strings.TrimSpace(cfg.MinIO.AccessKey) == "" || strings.TrimSpace(cfg.MinIO.SecretKey) == "" {
			return fmt.Errorf("storage.minio credentials are required when enabled")
		}
		if len([]byte(cfg.MinIO.SecretKey)) < 8 {
			return fmt.Errorf("storage.minio secret_key must be at least 8 characters")
		}
		if caFile := strings.TrimSpace(cfg.MinIO.CAFile); caFile != "" {
			data, err := os.ReadFile(caFile)
			if err != nil {
				return fmt.Errorf("storage.minio.ca_file: %w", err)
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(data) {
				return fmt.Errorf("storage.minio.ca_file contains no certificates")
			}
		}
	}
	if cfg.WriteProvider == "minio" && !cfg.MinIO.Enabled {
		return fmt.Errorf("storage.write_provider=minio requires storage.minio.enabled=true")
	}
	if cfg.WriteProvider == "local" && !cfg.Local.Enabled {
		return fmt.Errorf("storage.write_provider=local requires storage.local.enabled=true")
	}
	return nil
}

func validateLocalURLPrefix(prefix string) error {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" || !strings.HasPrefix(prefix, "/") || strings.HasPrefix(prefix, "//") {
		return fmt.Errorf("storage.local.url_prefix must be a same-origin path")
	}
	if strings.Contains(prefix, "\\") || strings.ContainsAny(prefix, "?#%") || strings.ContainsFunc(prefix, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return fmt.Errorf("storage.local.url_prefix must be a same-origin path")
	}
	trimmed := strings.Trim(prefix, "/")
	if trimmed == "" {
		return fmt.Errorf("storage.local.url_prefix must be a normalized path")
	}
	for _, segment := range strings.Split(trimmed, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return fmt.Errorf("storage.local.url_prefix must be a normalized path")
		}
	}
	clean := strings.TrimRight(prefix, "/")
	if clean == "" || clean == "/api" || strings.HasPrefix(clean, "/api/") {
		return fmt.Errorf("storage.local.url_prefix must not overlap /api")
	}
	return nil
}

// ParsePositiveDuration parses a human-readable positive duration and applies
// fallback only when the configuration field is empty.
func ParsePositiveDuration(raw, field string, fallback time.Duration) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(raw)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", field)
	}
	return duration, nil
}

func validateSkillZipLimits(cfg *SkillStorageConfig) error {
	parsedSizes := map[string]int64{}
	for field, raw := range map[string]string{
		"max_upload_size":   cfg.MaxUploadSize,
		"max_package_size":  cfg.MaxPackageSize,
		"max_file_size":     cfg.MaxFileSize,
		"max_unpacked_size": cfg.MaxUnpackedSize,
	} {
		if raw == "" {
			continue
		}
		value, err := ParseByteSize(raw)
		if err != nil {
			return fmt.Errorf("skill_storage.%s: %w", field, err)
		}
		if value > 512*1024*1024 {
			return fmt.Errorf("skill_storage.%s exceeds 512MiB safety ceiling", field)
		}
		parsedSizes[field] = value
	}
	if raw := cfg.MinTempFreeBytes; raw != "" {
		value, err := ParseByteSize(raw)
		if err != nil {
			return fmt.Errorf("skill_storage.min_temp_free_bytes: %w", err)
		}
		if value > 1<<40 {
			return fmt.Errorf("skill_storage.min_temp_free_bytes exceeds 1TiB safety ceiling")
		}
	}
	if value := parsedSizes["max_upload_size"]; value > 10*1024*1024 {
		return fmt.Errorf("skill_storage.max_upload_size cannot exceed AgentEnd's 10MiB limit")
	}
	if value := parsedSizes["max_package_size"]; value > 12*1024*1024 {
		return fmt.Errorf("skill_storage.max_package_size cannot exceed AgentEnd's 12MiB limit")
	}
	if value := parsedSizes["max_unpacked_size"]; value > 50*1024*1024 {
		return fmt.Errorf("skill_storage.max_unpacked_size cannot exceed AgentEnd's 50MiB limit")
	}
	if value := parsedSizes["max_file_size"]; value > 10*1024*1024 {
		return fmt.Errorf("skill_storage.max_file_size cannot exceed AgentEnd's 10MiB limit")
	}
	if cfg.MaxCompressionRatio < 0 || cfg.MaxCompressionRatio > 100 {
		return fmt.Errorf("skill_storage.max_compression_ratio must be between 1 and AgentEnd's 100:1 limit")
	}
	if cfg.MaxCompressionRatio == 0 {
		cfg.MaxCompressionRatio = 100
	}
	if cfg.MaxFileCount < 0 || cfg.MaxFileCount > 200 {
		return fmt.Errorf("skill_storage.max_file_count must be between 1 and AgentEnd's 200-file limit")
	}
	if cfg.MaxFileCount == 0 {
		cfg.MaxFileCount = 200
	}
	if upload, packageSize := parsedSizes["max_upload_size"], parsedSizes["max_package_size"]; upload > 0 && packageSize > 0 && upload > packageSize {
		return fmt.Errorf("skill_storage.max_upload_size cannot exceed max_package_size")
	}
	if cfg.MaxConcurrentValidations < 0 {
		return fmt.Errorf("skill_storage.max_concurrent_validations must be non-negative")
	}
	return nil
}

func isProductionMode() bool {
	appEnv := strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV")))
	ginMode := strings.ToLower(strings.TrimSpace(os.Getenv("GIN_MODE")))
	return appEnv == "production" || appEnv == "prod" || ginMode == "release"
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

// ParseByteSize accepts the human-readable values used by the Skill storage
// configuration (for example 10MiB, 512KiB and 1GiB).
func ParseByteSize(raw string) (int64, error) {
	raw = strings.TrimSpace(strings.ToUpper(raw))
	if raw == "" {
		return 0, fmt.Errorf("byte size is empty")
	}
	multiplier := int64(1)
	for suffix, value := range map[string]int64{"KIB": 1 << 10, "MIB": 1 << 20, "GIB": 1 << 30, "KB": 1e3, "MB": 1e6, "GB": 1e9} {
		if strings.HasSuffix(raw, suffix) {
			raw = strings.TrimSpace(strings.TrimSuffix(raw, suffix))
			multiplier = value
			break
		}
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("invalid byte size %q", raw)
	}
	if value > (1<<63-1)/multiplier {
		return 0, fmt.Errorf("byte size overflows int64")
	}
	return value * multiplier, nil
}
