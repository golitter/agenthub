package conf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDefaultsServerPort(t *testing.T) {
	clearConfigEnv(t)

	path := writeConfig(t, `
mysql:
  host: 127.0.0.1
  port: 3306
  user: root
  dbname: agenthub
jwt:
  secret: test-secret
  expire_hours: 24
agentend:
  host: http://localhost
  port: 8001
redis:
  host: 127.0.0.1
  port: 6379
  db: 0
admin:
  password: test-password
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Port != 8080 {
		t.Fatalf("Server.Port = %d, want 8080", cfg.Server.Port)
	}
	if cfg.MySQL.Charset != "utf8mb4" {
		t.Fatalf("MySQL.Charset = %q, want utf8mb4", cfg.MySQL.Charset)
	}
	if !cfg.SkillStorage.RequireAdmin {
		t.Fatal("SkillStorage.RequireAdmin defaulted to false")
	}
	if !cfg.SkillStorage.ShadowWriteBlob || !cfg.SkillStorage.AllowLegacyTmpConfirm {
		t.Fatal("migration rollback gates did not default to safe values")
	}
}

func TestLoadRejectsMissingJWTSecret(t *testing.T) {
	clearConfigEnv(t)

	path := writeConfig(t, `
mysql:
  host: 127.0.0.1
  port: 3306
  user: root
  dbname: agenthub
jwt:
  expire_hours: 24
agentend:
  host: http://localhost
  port: 8001
redis:
  host: 127.0.0.1
  port: 6379
  db: 0
admin:
  password: test-password
`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "jwt secret") {
		t.Fatalf("Load error = %v, want jwt secret error", err)
	}
}

func TestLoadRequiresBothServiceTokensWhenAgentEndAuthEnabled(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("AGENTEND_SERVICE_TOKEN", "agentend-token")

	path := writeConfig(t, `
mysql:
  host: 127.0.0.1
  port: 3306
  user: root
  dbname: agenthub
jwt:
  secret: test-secret
  expire_hours: 24
agentend:
  host: http://localhost
  port: 8001
  service_auth_enabled: true
redis:
  host: 127.0.0.1
  port: 6379
  db: 0
admin:
  password: test-password
`)

	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "BACKEND_SERVICE_TOKEN") {
		t.Fatalf("Load error = %v, want backend service token error", err)
	}
	t.Setenv("BACKEND_SERVICE_TOKEN", "backend-token")
	if _, err := Load(path); err != nil {
		t.Fatalf("Load with both service tokens: %v", err)
	}
}

func TestLoadAllowsSensitiveEnvOverrides(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("JWT_SECRET", "secret-from-env")
	t.Setenv("JWT_EXPIRE_HOURS", "48")
	t.Setenv("ADMIN_PASSWORD", "admin-from-env")
	t.Setenv("AGENTEND_HOST", "http://agentend")
	t.Setenv("AGENTEND_PORT", "9001")

	path := writeConfig(t, `
mysql:
  host: 127.0.0.1
  port: 3306
  user: root
  dbname: agenthub
jwt:
  secret: config-secret
  expire_hours: 24
agentend:
  host: http://localhost
  port: 8001
redis:
  host: 127.0.0.1
  port: 6379
  db: 0
admin:
  password: config-password
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.JWT.Secret != "secret-from-env" {
		t.Fatalf("JWT.Secret = %q, want env override", cfg.JWT.Secret)
	}
	if cfg.JWT.ExpireHours != 48 {
		t.Fatalf("JWT.ExpireHours = %d, want 48", cfg.JWT.ExpireHours)
	}
	if cfg.Admin.Password != "admin-from-env" {
		t.Fatalf("Admin.Password = %q, want env override", cfg.Admin.Password)
	}
	if cfg.AgentEnd.Host != "http://agentend" || cfg.AgentEnd.Port != 9001 {
		t.Fatalf("AgentEnd = %s:%d, want env override", cfg.AgentEnd.Host, cfg.AgentEnd.Port)
	}
}

func TestLoadRejectsDefaultSecretsInProduction(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("APP_ENV", "production")

	path := writeConfig(t, `
mysql:
  host: 127.0.0.1
  port: 3306
  user: root
  dbname: agenthub
jwt:
  secret: agenthub-demo-secret
  expire_hours: 24
agentend:
  host: http://localhost
  port: 8001
redis:
  host: 127.0.0.1
  port: 6379
  db: 0
admin:
  password: "123456"
`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "jwt secret") {
		t.Fatalf("Load error = %v, want production jwt secret error", err)
	}
}

func TestLoadEnablesAPIAuthByDefaultInProduction(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("GIN_MODE", "release")
	t.Setenv("JWT_SECRET", "production-secret")
	t.Setenv("ADMIN_PASSWORD", "production-admin")

	path := writeConfig(t, `
mysql:
  host: 127.0.0.1
  port: 3306
  user: root
  dbname: agenthub
jwt:
  secret: config-secret
  expire_hours: 24
agentend:
  host: http://localhost
  port: 8001
redis:
  host: 127.0.0.1
  port: 6379
  db: 0
admin:
  password: config-password
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Auth.Enabled {
		t.Fatal("Auth.Enabled = false, want true in production mode")
	}
}

func TestLoadRejectsArtifactCapabilitySecretReuse(t *testing.T) {
	clearConfigEnv(t)
	path := writeConfig(t, `
mysql:
  host: 127.0.0.1
  port: 3306
  user: root
  dbname: agenthub
jwt:
  secret: test-secret
  expire_hours: 24
agentend:
  host: http://localhost
  port: 8001
redis:
  host: 127.0.0.1
  port: 6379
  db: 0
admin:
  password: test-password
artifact_storage:
  enabled: true
  endpoint: minio:9000
  bucket: agenthub-artifacts
  access_key: artifact-user
  secret_key: artifact-secret-123
  capability_secret: artifact-secret-123
`)
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "capability_secret") {
		t.Fatalf("Load error = %v, want capability secret reuse error", err)
	}
}

func TestValidateArtifactStorageConfigCapsFirstPhaseMemorySize(t *testing.T) {
	cfg := &ArtifactStorageConfig{MaxObjectSize: "26MiB"}
	if err := validateArtifactStorageConfig(cfg); err == nil || !strings.Contains(err.Error(), "25MiB") {
		t.Fatalf("validateArtifactStorageConfig error = %v, want 25MiB cap", err)
	}
}

func TestLoadAllowsAPIAuthEnvOverride(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("GIN_MODE", "release")
	t.Setenv("JWT_SECRET", "production-secret")
	t.Setenv("ADMIN_PASSWORD", "production-admin")
	t.Setenv("API_AUTH_ENABLED", "false")

	path := writeConfig(t, `
mysql:
  host: 127.0.0.1
  port: 3306
  user: root
  dbname: agenthub
jwt:
  secret: config-secret
  expire_hours: 24
agentend:
  host: http://localhost
  port: 8001
redis:
  host: 127.0.0.1
  port: 6379
  db: 0
admin:
  password: config-password
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Auth.Enabled {
		t.Fatal("Auth.Enabled = true, want env override false")
	}
}

func TestParseByteSize(t *testing.T) {
	for raw, want := range map[string]int64{"10MiB": 10 << 20, "512KiB": 512 << 10, "1GB": 1_000_000_000} {
		got, err := ParseByteSize(raw)
		if err != nil || got != want {
			t.Fatalf("ParseByteSize(%q) = %d, %v; want %d", raw, got, err, want)
		}
	}
	if _, err := ParseByteSize("0MiB"); err == nil {
		t.Fatal("ParseByteSize accepted zero")
	}
}

func TestLoadRejectsOrphanGraceShorterThanConfirmLease(t *testing.T) {
	clearConfigEnv(t)
	path := writeConfig(t, `
mysql:
  host: 127.0.0.1
  port: 3306
  user: root
  dbname: agenthub
jwt:
  secret: test-secret
  expire_hours: 24
agentend:
  host: http://localhost
  port: 8001
redis:
  host: 127.0.0.1
  port: 6379
  db: 0
admin:
  password: test-password
skill_storage:
  enabled: true
  endpoint: minio:9000
  bucket: skill-packages
  access_key: app
  secret_key: secret
  upload_session_ttl: 15m
  confirm_lease: 2m
  orphan_grace_period: 1m
  incoming_ttl: 24h
`)
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "orphan_grace_period") {
		t.Fatalf("Load error = %v, want orphan grace validation error", err)
	}
}

func TestLoadRejectsDBReadPreferenceWithoutShadowWrites(t *testing.T) {
	clearConfigEnv(t)
	path := writeConfig(t, `
mysql:
  host: 127.0.0.1
  port: 3306
  user: root
  dbname: agenthub
jwt:
  secret: test-secret
  expire_hours: 24
agentend:
  host: http://localhost
  port: 8001
redis:
  host: 127.0.0.1
  port: 6379
  db: 0
admin:
  password: test-password
skill_storage:
  enabled: true
  endpoint: minio:9000
  bucket: skill-packages
  access_key: app
  secret_key: secret
  read_preference: db
  shadow_write_blob: false
`)
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "shadow_write_blob") {
		t.Fatalf("Load error = %v, want DB read preference gate", err)
	}
}

func TestLoadValidatesSkillZipLimitsWhenStorageDisabled(t *testing.T) {
	clearConfigEnv(t)
	path := writeConfig(t, `
mysql:
  host: 127.0.0.1
  port: 3306
  user: root
  dbname: agenthub
jwt:
  secret: test-secret
  expire_hours: 24
agentend:
  host: http://localhost
  port: 8001
redis:
  host: 127.0.0.1
  port: 6379
  db: 0
admin:
  password: test-password
skill_storage:
  max_upload_size: 11MiB
`)
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "max_upload_size") {
		t.Fatalf("Load error = %v, want disabled storage ZIP limit validation", err)
	}
}

func TestLoadDefaultsSkillZipLimitsWhenStorageDisabled(t *testing.T) {
	clearConfigEnv(t)
	path := writeConfig(t, `
mysql:
  host: 127.0.0.1
  port: 3306
  user: root
  dbname: agenthub
jwt:
  secret: test-secret
  expire_hours: 24
agentend:
  host: http://localhost
  port: 8001
redis:
  host: 127.0.0.1
  port: 6379
  db: 0
admin:
  password: test-password
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.SkillStorage.MaxCompressionRatio != 100 || cfg.SkillStorage.MaxFileCount != 200 {
		t.Fatalf("disabled storage ZIP defaults = ratio %d/files %d, want 100/200", cfg.SkillStorage.MaxCompressionRatio, cfg.SkillStorage.MaxFileCount)
	}
}

func TestLoadAvatarMinIOStorageDefaultsAndEnvOverrides(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("ASSET_MINIO_ACCESS_KEY", "asset-user")
	t.Setenv("ASSET_MINIO_SECRET_KEY", "asset-secret")
	path := writeConfig(t, `
mysql:
  host: 127.0.0.1
  port: 3306
  user: root
  dbname: agenthub
jwt:
  secret: test-secret
  expire_hours: 24
agentend:
  host: http://localhost
  port: 8001
redis:
  host: 127.0.0.1
  port: 6379
  db: 0
admin:
  password: test-password
storage:
  write_provider: minio
  minio:
    enabled: true
    endpoint: minio:9000
    bucket: agenthub-assets
  local:
    enabled: true
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Storage.WriteProvider != "minio" || cfg.Storage.MinIO.AccessKey != "asset-user" || cfg.Storage.MinIO.RequestTimeout != "10s" {
		t.Fatalf("avatar storage config = %+v, want explicit MinIO defaults and env credentials", cfg.Storage)
	}
	if cfg.Storage.Local.URLPrefix != "/uploads" {
		t.Fatalf("local URL prefix = %q, want /uploads", cfg.Storage.Local.URLPrefix)
	}
}

func TestLoadRejectsAvatarMinIOWithoutCredentials(t *testing.T) {
	clearConfigEnv(t)
	path := writeConfig(t, `
mysql:
  host: 127.0.0.1
  port: 3306
  user: root
  dbname: agenthub
jwt:
  secret: test-secret
  expire_hours: 24
agentend:
  host: http://localhost
  port: 8001
redis:
  host: 127.0.0.1
  port: 6379
  db: 0
admin:
  password: test-password
storage:
  write_provider: minio
  minio:
    enabled: true
    endpoint: minio:9000
    bucket: agenthub-assets
`)
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "storage.minio credentials") {
		t.Fatalf("Load error = %v, want avatar MinIO credential error", err)
	}
}

func TestLoadAllowsExplicitPureLocalAvatarStorage(t *testing.T) {
	clearConfigEnv(t)
	path := writeConfig(t, `
mysql:
  host: 127.0.0.1
  port: 3306
  user: root
  dbname: agenthub
jwt:
  secret: test-secret
  expire_hours: 24
agentend:
  host: http://localhost
  port: 8001
redis:
  host: 127.0.0.1
  port: 6379
  db: 0
admin:
  password: test-password
storage:
  write_provider: local
  minio:
    enabled: false
  local:
    enabled: true
    dir: ./uploads
    url_prefix: /uploads
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Storage.WriteProvider != "local" || cfg.Storage.Local.Dir != "./uploads" {
		t.Fatalf("local storage config = %+v", cfg.Storage)
	}
}

func TestLoadRejectsAvatarAndSkillStorageReuse(t *testing.T) {
	for name, storageBlock := range map[string]string{
		"bucket": `
storage:
  write_provider: minio
  minio:
    enabled: true
    endpoint: minio:9000
    bucket: skill-packages
    access_key: asset-user
    secret_key: asset-secret
`,
		"account": `
storage:
  write_provider: minio
  minio:
    enabled: true
    endpoint: minio:9000
    bucket: agenthub-assets
    access_key: shared-user
    secret_key: asset-secret
`,
	} {
		t.Run(name, func(t *testing.T) {
			clearConfigEnv(t)
			path := writeConfig(t, `
mysql:
  host: 127.0.0.1
  port: 3306
  user: root
  dbname: agenthub
jwt:
  secret: test-secret
  expire_hours: 24
agentend:
  host: http://localhost
  port: 8001
redis:
  host: 127.0.0.1
  port: 6379
  db: 0
admin:
  password: test-password
`+storageBlock+`
skill_storage:
  enabled: true
  type: minio
  endpoint: minio:9000
  bucket: skill-packages
  access_key: `+map[string]string{"bucket": "skill-user", "account": "shared-user"}[name]+`
  secret_key: skill-secret
`)
			if _, err := Load(path); err == nil {
				t.Fatal("avatar and Skill storage reuse was accepted")
			}
		})
	}
}

func TestLoadRejectsInvalidAvatarStorageValues(t *testing.T) {
	for name, storageBlock := range map[string]string{
		"provider": `
			write_provider: unsupported
  local:
    enabled: true
`,
		"timeout": `
  write_provider: local
  minio:
    request_timeout: 0s
  local:
    enabled: true
`,
		"prefix": `
  write_provider: local
  local:
    enabled: true
    url_prefix: https://cdn.example.com/uploads
`,
		"secret": `
  write_provider: minio
  minio:
    enabled: true
    endpoint: minio:9000
    bucket: agenthub-assets
    access_key: asset-user
    secret_key: short
`,
	} {
		t.Run(name, func(t *testing.T) {
			clearConfigEnv(t)
			path := writeConfig(t, `
mysql:
  host: 127.0.0.1
  port: 3306
  user: root
  dbname: agenthub
jwt:
  secret: test-secret
  expire_hours: 24
agentend:
  host: http://localhost
  port: 8001
redis:
  host: 127.0.0.1
  port: 6379
  db: 0
admin:
  password: test-password
storage:`+storageBlock)
			if _, err := Load(path); err == nil {
				t.Fatal("invalid avatar storage configuration was accepted")
			}
		})
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	// The general configuration fixtures use explicit pure-local storage so
	// unit tests do not require a live MinIO account. MinIO defaults are tested
	// by the dedicated storage validation cases below.
	if !strings.Contains(content, "\nstorage:") {
		content += `
storage:
  write_provider: local
  local:
    enabled: true
`
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func clearConfigEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"MYSQL_HOST",
		"MYSQL_PORT",
		"MYSQL_USER",
		"MYSQL_PASSWORD",
		"MYSQL_DBNAME",
		"MYSQL_CHARSET",
		"JWT_SECRET",
		"JWT_EXPIRE_HOURS",
		"AGENTEND_HOST",
		"AGENTEND_PORT",
		"AGENTEND_SERVICE_AUTH_ENABLED",
		"AGENTEND_SERVICE_TOKEN",
		"BACKEND_SERVICE_TOKEN",
		"REDIS_HOST",
		"REDIS_PORT",
		"REDIS_PASSWORD",
		"REDIS_DB",
		"CORS_ALLOW_ORIGINS",
		"ADMIN_PASSWORD",
		"API_AUTH_ENABLED",
		"SERVER_PORT",
		"APP_ENV",
		"GIN_MODE",
		"AVATAR_STORAGE_WRITE_PROVIDER",
		"ASSET_MINIO_ENABLED",
		"ASSET_MINIO_ENDPOINT",
		"ASSET_MINIO_BUCKET",
		"ASSET_MINIO_ACCESS_KEY",
		"ASSET_MINIO_SECRET_KEY",
		"ASSET_MINIO_USE_SSL",
		"ASSET_MINIO_CA_CERT",
		"ASSET_MINIO_REQUEST_TIMEOUT",
		"LOCAL_STORAGE_ENABLED",
		"LOCAL_STORAGE_DIR",
		"LOCAL_STORAGE_URL_PREFIX",
		"ARTIFACT_STORAGE_ENABLED",
		"ARTIFACT_MINIO_ENDPOINT",
		"ARTIFACT_MINIO_BUCKET",
		"ARTIFACT_MINIO_ACCESS_KEY",
		"ARTIFACT_MINIO_SECRET_KEY",
		"ARTIFACT_MINIO_USE_SSL",
		"ARTIFACT_MINIO_CA_CERT",
		"ARTIFACT_MINIO_REQUEST_TIMEOUT",
		"ARTIFACT_MAX_OBJECT_SIZE",
		"ARTIFACT_MAX_PER_MESSAGE",
		"ARTIFACT_UPLOAD_TOKEN_TTL",
		"ARTIFACT_CAPABILITY_SECRET",
		"ARTIFACT_FAILED_RETENTION",
		"MINIO_ENDPOINT",
		"MINIO_BUCKET",
		"MINIO_ACCESS_KEY",
		"MINIO_SECRET_KEY",
		"MINIO_USE_SSL",
		"MINIO_CA_CERT",
	} {
		t.Setenv(key, "")
	}
}
