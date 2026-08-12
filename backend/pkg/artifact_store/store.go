// Package artifact_store provides the private object boundary for builtin
// skill artifacts. It deliberately has no public URL or bucket-management
// concerns in its interface.
package artifact_store

import (
	"context"
	"errors"
	"io"
	"strings"
	"unicode"
)

var (
	ErrNotFound   = errors.New("artifact object not found")
	ErrExists     = errors.New("artifact object already exists")
	ErrPermission = errors.New("artifact storage permission denied")
	ErrTimeout    = errors.New("artifact storage operation timed out")
)

type PutOptions struct {
	ContentType string
	SHA256      string
}

type ObjectInfo struct {
	Key         string
	Size        int64
	ContentType string
	SHA256      string
	ETag        string
}

type Store interface {
	Put(ctx context.Context, key string, body io.Reader, size int64, options PutOptions) error
	Open(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error)
	Stat(ctx context.Context, key string) (ObjectInfo, error)
	Delete(ctx context.Context, key string) error
	Health(ctx context.Context) error
}

func ValidateObjectKey(key string) error {
	if strings.TrimSpace(key) == "" || strings.HasPrefix(key, "/") || strings.Contains(key, "\\") || strings.ContainsFunc(key, unicode.IsControl) {
		return errors.New("invalid artifact object key")
	}
	for _, part := range strings.Split(key, "/") {
		if part == "" || part == "." || part == ".." {
			return errors.New("invalid artifact object key")
		}
	}
	return nil
}

func CheckContext(ctx context.Context) error {
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
