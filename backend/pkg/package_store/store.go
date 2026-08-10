package package_store

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"
)

var (
	ErrNotFound       = errors.New("package object not found")
	ErrIntegrity      = errors.New("package object integrity mismatch")
	ErrTargetConflict = errors.New("package object target conflict")
)

// ObjectInfo is metadata required to compare an immutable package object.
// SHA256 may be empty when reading a legacy object that has not been verified yet.
type ObjectInfo struct {
	Key          string
	Size         int64
	SHA256       string
	LastModified time.Time
}

// PackageStore is the private object storage boundary for external Skill packages.
type PackageStore interface {
	Put(ctx context.Context, key string, body io.Reader, size int64, sha256 string) error
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	Stat(ctx context.Context, key string) (*ObjectInfo, error)
	Promote(ctx context.Context, sourceKey, targetKey string, expected ObjectInfo) error
	List(ctx context.Context, prefix, cursor string, limit int) (items []ObjectInfo, nextCursor string, err error)
	Delete(ctx context.Context, key string) error
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

func contextOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func validateObjectKey(key string) error {
	if strings.TrimSpace(key) == "" || strings.HasPrefix(key, "/") || strings.Contains(key, "\\") {
		return errors.New("invalid package object key")
	}
	for _, part := range strings.Split(key, "/") {
		if part == ".." || part == "." || part == "" {
			return errors.New("invalid package object key")
		}
	}
	return nil
}
