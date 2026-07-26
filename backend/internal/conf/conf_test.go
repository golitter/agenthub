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

func writeConfig(t *testing.T, content string) string {
	t.Helper()
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
		"QINIU_ACCESS_KEY",
		"QINIU_SECRET_KEY",
	} {
		t.Setenv(key, "")
	}
}
