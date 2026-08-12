package storage

import (
	"context"
	"errors"
	"io"
	"strings"
	"unicode"
)

// Provider 是供 Service 层上传文件使用的存储抽象。
type Provider interface {
	// UploadBytes 上传内存中的数据，并返回公开访问 URL。
	UploadBytes(ctx context.Context, key string, data []byte) (string, error)
	// UploadReader 从 reader 读取数据上传，并返回公开访问 URL。
	UploadReader(ctx context.Context, key string, reader io.Reader, size int64) (string, error)
}

// ObjectReader is the read-only boundary used by the anonymous avatar proxy.
// The application deliberately keeps this separate from Provider so a reader
// cannot accidentally gain delete or write capabilities.
type ObjectReader interface {
	Open(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error)
	Stat(ctx context.Context, key string) (ObjectInfo, error)
	Health(ctx context.Context) error
}

// ObjectInfo contains the metadata needed to proxy an immutable avatar.
type ObjectInfo struct {
	Key         string
	Size        int64
	SHA256      string
	ContentType string
	ETag        string
}

var (
	// ErrNotFound is returned when an object does not exist.
	ErrNotFound = errors.New("storage object not found")
	// ErrTimeout identifies an upstream storage timeout.
	ErrTimeout = errors.New("storage operation timed out")
	// ErrPermission identifies an upstream storage authorization failure.
	ErrPermission = errors.New("storage permission denied")
	// ErrObjectExists is returned when an immutable object key is already used.
	ErrObjectExists = errors.New("storage object already exists")
)

func validateObjectKey(key string) error {
	if strings.TrimSpace(key) == "" || strings.HasPrefix(key, "/") || strings.Contains(key, "\\") || strings.ContainsFunc(key, unicode.IsControl) {
		return errors.New("invalid storage object key")
	}
	for _, part := range strings.Split(key, "/") {
		if part == "" || part == "." || part == ".." {
			return errors.New("invalid storage object key")
		}
	}
	return nil
}
