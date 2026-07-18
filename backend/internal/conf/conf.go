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
	Type  string             `yaml:"type"` // "qiniu" | "local" | "" (auto-detect)
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

type Config struct {
	MySQL    MySQLConfig    `yaml:"mysql"`
	JWT      JWTConfig      `yaml:"jwt"`
	AgentEnd AgentEndConfig `yaml:"agentend"`
	Qiniu    QiniuConfig    `yaml:"qiniu"`
	Storage  StorageConfig  `yaml:"storage"`
	Redis    RedisConfig    `yaml:"redis"`
	Admin    AdminConfig    `yaml:"admin"`
	CORS     CORSConfig     `yaml:"cors"`
}

func Load(path string) (*Config, error) {
	// .env is optional — don't error if missing
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

	return nil
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
