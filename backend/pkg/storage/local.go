package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// LocalStorage 将文件存储在本地文件系统，并通过 HTTP 提供访问。
type LocalStorage struct {
	dir       string
	urlPrefix string
}

// NewLocalStorage 创建一个以 dir 为根目录的 LocalStorage，URL 以 urlPrefix 为前缀。
// 该函数会确保 dir 在磁盘上存在。
func NewLocalStorage(dir, urlPrefix string) (*LocalStorage, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, fmt.Errorf("storage dir is required")
	}
	prefix := strings.TrimSpace(urlPrefix)
	if prefix == "" {
		prefix = "/uploads"
	}
	if err := validateLocalPrefix(prefix); err != nil {
		return nil, err
	}
	prefix = strings.TrimRight(prefix, "/")
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve storage dir: %w", err)
	}
	// Check existing ancestors before MkdirAll so a configured path cannot
	// create new files through a symlinked parent and only then be rejected.
	if err := ensureNoSymlinkComponents(absDir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return nil, fmt.Errorf("create storage dir: %w", err)
	}
	if err := ensureNoSymlinkComponents(absDir); err != nil {
		return nil, err
	}
	if err := ensureNoSymlinks(absDir); err != nil {
		return nil, err
	}
	return &LocalStorage{dir: absDir, urlPrefix: prefix}, nil
}

// Dir 返回磁盘上的存储目录（main.go 用它来注册 Gin 静态路由）。
func (s *LocalStorage) Dir() string { return s.dir }

// URLPrefix returns the same-origin URL prefix returned by uploads.
func (s *LocalStorage) URLPrefix() string { return s.urlPrefix }

func (s *LocalStorage) UploadBytes(ctx context.Context, key string, data []byte) (string, error) {
	if err := checkContext(ctx); err != nil {
		return "", err
	}
	cleanKey, fullPath, err := s.resolveKey(key)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return "", fmt.Errorf("create directory: %w", err)
	}
	if err := ensureNoSymlinkPath(s.dir, cleanKey); err != nil {
		return "", err
	}
	if err := checkContext(ctx); err != nil {
		return "", err
	}
	f, err := os.OpenFile(fullPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return "", fmt.Errorf("%w: %s", ErrObjectExists, cleanKey)
		}
		return "", fmt.Errorf("create file: %w", err)
	}
	written, writeErr := f.Write(data)
	if writeErr != nil || written != len(data) {
		_ = f.Close()
		_ = os.Remove(fullPath)
		if writeErr != nil {
			return "", fmt.Errorf("write file: %w", writeErr)
		}
		return "", fmt.Errorf("write file: short write: got %d want %d", written, len(data))
	}
	if err := checkContext(ctx); err != nil {
		_ = f.Close()
		_ = os.Remove(fullPath)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(fullPath)
		return "", fmt.Errorf("close file: %w", err)
	}
	return s.publicURL(cleanKey), nil
}

func (s *LocalStorage) UploadReader(ctx context.Context, key string, reader io.Reader, size int64) (string, error) {
	if reader == nil || size < 0 || size == 1<<63-1 {
		return "", fmt.Errorf("invalid upload reader or size")
	}
	if err := checkContext(ctx); err != nil {
		return "", err
	}
	cleanKey, fullPath, err := s.resolveKey(key)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return "", fmt.Errorf("create directory: %w", err)
	}
	if err := ensureNoSymlinkPath(s.dir, cleanKey); err != nil {
		return "", err
	}
	if err := checkContext(ctx); err != nil {
		return "", err
	}
	f, err := os.OpenFile(fullPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return "", fmt.Errorf("%w: %s", ErrObjectExists, cleanKey)
		}
		return "", fmt.Errorf("create file: %w", err)
	}
	written, copyErr := io.Copy(f, io.LimitReader(contextReader{ctx: ctx, reader: reader}, size+1))
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(fullPath)
		return "", fmt.Errorf("write file: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(fullPath)
		return "", fmt.Errorf("close file: %w", closeErr)
	}
	if written != size {
		_ = os.Remove(fullPath)
		return "", fmt.Errorf("upload size mismatch: got %d want %d", written, size)
	}
	if err := checkContext(ctx); err != nil {
		_ = os.Remove(fullPath)
		return "", err
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
	if err := ensureNoSymlinkPath(s.dir, cleanKey); err != nil {
		return "", "", err
	}
	return cleanKey, fullPath, nil
}

func validateKey(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", fmt.Errorf("invalid key: empty")
	}
	if filepath.IsAbs(key) || strings.HasPrefix(key, "/") || strings.Contains(key, "\\") {
		return "", fmt.Errorf("invalid key: absolute path")
	}
	for _, segment := range strings.Split(key, "/") {
		if segment == ".." {
			return "", fmt.Errorf("invalid key: path traversal")
		}
	}
	cleanKey := filepath.ToSlash(filepath.Clean(key))
	if cleanKey == "." || cleanKey == "" {
		return "", fmt.Errorf("invalid key: empty")
	}
	for _, segment := range strings.Split(cleanKey, "/") {
		if segment == ".." || strings.ContainsFunc(segment, unicode.IsControl) {
			return "", fmt.Errorf("invalid key: path traversal")
		}
	}
	return cleanKey, nil
}

func ensureNoSymlinkPath(root, key string) error {
	current := root
	for _, segment := range strings.Split(key, "/") {
		current = filepath.Join(current, segment)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("inspect storage path: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("invalid key: symlink path")
		}
	}
	return nil
}

func ensureNoSymlinkComponents(path string) error {
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			parent := filepath.Dir(current)
			if parent == current {
				return nil
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect storage path: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("storage dir must not contain symlink: %s", current)
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	return nil
}

// ensureNoSymlinks protects the static compatibility route as well as writes:
// a symlink that already exists below the storage root would otherwise let
// http.Dir serve a file outside the configured directory.
func ensureNoSymlinks(root string) error {
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("inspect storage path %s: %w", path, walkErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("storage dir must not contain symlink: %s", path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

func checkContext(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := checkContext(r.ctx); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
