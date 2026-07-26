package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// LocalStorage 将文件存储在本地文件系统，并通过 HTTP 提供访问。
type LocalStorage struct {
	dir       string
	urlPrefix string
}

// NewLocalStorage 创建一个以 dir 为根目录的 LocalStorage，URL 以 urlPrefix 为前缀。
// 该函数会确保 dir 在磁盘上存在。
func NewLocalStorage(dir, urlPrefix string) (*LocalStorage, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve storage dir: %w", err)
	}
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return nil, fmt.Errorf("create storage dir: %w", err)
	}
	return &LocalStorage{dir: absDir, urlPrefix: strings.TrimRight(urlPrefix, "/")}, nil
}

// Dir 返回磁盘上的存储目录（main.go 用它来注册 Gin 静态路由）。
func (s *LocalStorage) Dir() string { return s.dir }

func (s *LocalStorage) UploadBytes(_ context.Context, key string, data []byte) (string, error) {
	cleanKey, fullPath, err := s.resolveKey(key)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return "", fmt.Errorf("create directory: %w", err)
	}
	if err := os.WriteFile(fullPath, data, 0o644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}
	return s.publicURL(cleanKey), nil
}

func (s *LocalStorage) UploadReader(_ context.Context, key string, reader io.Reader, _ int64) (string, error) {
	cleanKey, fullPath, err := s.resolveKey(key)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return "", fmt.Errorf("create directory: %w", err)
	}
	f, err := os.Create(fullPath)
	if err != nil {
		return "", fmt.Errorf("create file: %w", err)
	}
	defer f.Close()
	if _, err := io.Copy(f, reader); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}
	return s.publicURL(cleanKey), nil
}

func (s *LocalStorage) publicURL(key string) string {
	return s.urlPrefix + "/" + key
}

func (s *LocalStorage) resolveKey(key string) (string, string, error) {
	cleanKey, err := validateKey(key)
	if err != nil {
		return "", "", err
	}
	fullPath := filepath.Join(s.dir, filepath.FromSlash(cleanKey))
	rel, err := filepath.Rel(s.dir, fullPath)
	if err != nil {
		return "", "", fmt.Errorf("resolve storage key: %w", err)
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return "", "", fmt.Errorf("invalid key: path traversal")
	}
	return cleanKey, fullPath, nil
}

func validateKey(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", fmt.Errorf("invalid key: empty")
	}
	if filepath.IsAbs(key) || strings.HasPrefix(key, "/") {
		return "", fmt.Errorf("invalid key: absolute path")
	}
	cleanKey := filepath.ToSlash(filepath.Clean(key))
	if cleanKey == "." || cleanKey == "" {
		return "", fmt.Errorf("invalid key: empty")
	}
	for _, segment := range strings.Split(cleanKey, "/") {
		if segment == ".." {
			return "", fmt.Errorf("invalid key: path traversal")
		}
	}
	return cleanKey, nil
}
