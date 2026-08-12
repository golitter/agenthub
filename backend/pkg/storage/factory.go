package storage

import (
	"fmt"
	"strings"

	"agenthub/backend/internal/conf"
)

// NewRuntime constructs every enabled avatar storage component and selects a
// single writer.  A configured reader is never used as an implicit fallback
// for writes.
func NewRuntime(cfg *conf.StorageConfig) (*Runtime, error) {
	if cfg == nil {
		return nil, fmt.Errorf("storage config is required")
	}

	writeProvider := strings.ToLower(strings.TrimSpace(cfg.WriteProvider))
	if writeProvider == "" {
		writeProvider = "minio"
	}
	if writeProvider != "minio" && writeProvider != "local" {
		return nil, fmt.Errorf("unknown storage.write_provider: %s", writeProvider)
	}
	if cfg.MinIO.Enabled && (strings.TrimSpace(cfg.MinIO.AccessKey) == "" || strings.TrimSpace(cfg.MinIO.SecretKey) == "") {
		return nil, fmt.Errorf("storage.minio credentials are required when enabled")
	}
	requestTimeout, err := conf.ParsePositiveDuration(cfg.MinIO.RequestTimeout, "storage.minio.request_timeout", defaultMinIORequestTimeout)
	if err != nil {
		return nil, err
	}

	var minioStorage *MinIOStorage
	if cfg.MinIO.Enabled {
		minioStorage, err = NewMinIOStorage(MinIOConfig{
			Endpoint:       cfg.MinIO.Endpoint,
			Bucket:         cfg.MinIO.Bucket,
			AccessKey:      cfg.MinIO.AccessKey,
			SecretKey:      cfg.MinIO.SecretKey,
			UseSSL:         cfg.MinIO.UseSSL,
			CAFile:         cfg.MinIO.CAFile,
			RequestTimeout: requestTimeout,
		})
		if err != nil {
			return nil, err
		}
	}

	var localStorage *LocalStorage
	if cfg.Local.Enabled {
		if err := validateLocalPrefix(cfg.Local.URLPrefix); err != nil {
			return nil, err
		}
		dir := strings.TrimSpace(cfg.Local.Dir)
		if dir == "" {
			dir = "./uploads"
		}
		prefix := strings.TrimSpace(cfg.Local.URLPrefix)
		if prefix == "" {
			prefix = "/uploads"
		}
		var err error
		localStorage, err = NewLocalStorage(dir, prefix)
		if err != nil {
			return nil, err
		}
	}

	runtime := &Runtime{MinIO: minioStorage, Local: localStorage}
	switch writeProvider {
	case "minio":
		if minioStorage == nil {
			return nil, fmt.Errorf("storage.write_provider=minio requires storage.minio.enabled=true")
		}
		runtime.Writer = minioStorage
		runtime.AssetReader = minioStorage
	case "local":
		if localStorage == nil {
			return nil, fmt.Errorf("storage.write_provider=local requires storage.local.enabled=true")
		}
		runtime.Writer = localStorage
		if minioStorage != nil {
			runtime.AssetReader = minioStorage
		}
	default:
		// writeProvider is validated above; keep the switch exhaustive if a new
		// provider is added later.
		return nil, fmt.Errorf("unknown storage.write_provider: %s", writeProvider)
	}
	return runtime, nil
}

func validateLocalPrefix(prefix string) error {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return nil
	}
	if !strings.HasPrefix(prefix, "/") || strings.HasPrefix(prefix, "//") || strings.Contains(prefix, "\\") || strings.ContainsAny(prefix, "?#%") || strings.ContainsFunc(prefix, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
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
	if clean == "/api" || strings.HasPrefix(clean, "/api/") {
		return fmt.Errorf("storage.local.url_prefix must not overlap /api")
	}
	return nil
}

// NewProvider is a convenience wrapper for callers that only need the
// selected writer.  NewRuntime should be used by the server so both readers
// remain available for URL compatibility.
func NewProvider(cfg *conf.StorageConfig) (Provider, error) {
	runtime, err := NewRuntime(cfg)
	if err != nil {
		return nil, err
	}
	return runtime.Writer, nil
}
