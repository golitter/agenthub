# Config — 配置加载

## 实现了什么

通过 YAML 文件 + `.env` 环境变量双层机制加载配置，涵盖 MySQL、JWT、AgentEnd、Server、Auth、七牛云、Storage、Redis、Admin、CORS 十个模块。敏感信息（七牛云密钥）从环境变量注入，不硬编码在 YAML 中。

## 怎么实现的

### Config 结构体 (`internal/conf/conf.go`)

```go
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

`Load` 先尝试加载可选的 `.env` 文件，再读取 YAML 配置，然后覆盖七牛云密钥，执行 `applyEnvOverrides` 用环境变量覆盖 MySQL / JWT / AgentEnd / Redis / CORS / Admin / Server 等连接参数（便于 Docker / CI 注入），最后通过 `validateConfig` 做启动前校验：

```go
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
	if err := validateConfig(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
```

`validateConfig` 会校验 MySQL / Redis / AgentEnd / Server 端口范围、必填 host / user / dbname、JWT secret 和过期时间、Admin 密码等关键字段；`mysql.charset` 为空时默认回填 `utf8mb4`。当 `APP_ENV=production` / `APP_ENV=prod` 或 `GIN_MODE=release` 时，默认 JWT secret（`agenthub-demo-secret`）和默认 Admin 密码（`123456`）会直接拒绝启动，普通 API Auth 也会默认开启，除非显式设置 `API_AUTH_ENABLED=false`。这些问题会在启动阶段 fail-fast，而不是延迟到请求处理或数据库连接阶段。

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

cors:
  allow_origins:
    - "http://localhost:5173"
```

七牛云 `access_key` / `secret_key` 不在 YAML 中配置，通过 `QINIU_ACCESS_KEY` / `QINIU_SECRET_KEY` 环境变量注入。

> `.env` 模板见 [`backend/.env.example`](../../../backend/.env.example)，首次运行前 `cp .env.example .env` 并填入实际密钥；留空则自动回退到本地磁盘存储。

存储层通过 `StorageConfig.Type` 控制策略：空字符串自动检测（有七牛云密钥则用七牛云，否则本地磁盘），`"qiniu"` 强制七牛云，`"local"` 强制本地磁盘。`pkg/storage/` 包提供统一的 `Provider` 接口，Controller 通过构造函数注入 `storage.Provider`。
