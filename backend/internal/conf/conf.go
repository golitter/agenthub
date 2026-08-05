package conf

import (
	"fmt"
	"os"
	"strconv"
	"strings"

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
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

func (c *AgentEndConfig) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
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
	Type  string             `yaml:"type"` // 存储类型："qiniu" | "local" | ""（留空则自动判定）
	Local LocalStorageConfig `yaml:"local"`
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
	MySQL    MySQLConfig    `yaml:"mysql"`
	JWT      JWTConfig      `yaml:"jwt"`
	AgentEnd AgentEndConfig `yaml:"agentend"`
	Qiniu    QiniuConfig    `yaml:"qiniu"`
	Storage  StorageConfig  `yaml:"storage"`
	Redis    RedisConfig    `yaml:"redis"`
	Admin    AdminConfig    `yaml:"admin"`
	CORS     CORSConfig     `yaml:"cors"`
	Server   ServerConfig   `yaml:"server"`
	Auth     AuthConfig     `yaml:"auth"`
}

func Load(path string) (*Config, error) {
	// .env 是可选的 —— 不存在时不报错
	_ = godotenv.Load()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}
	var cfg Config
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
