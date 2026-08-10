package package_store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
)

type memoryObject struct {
	data         []byte
	sha256       string
	lastModified time.Time
}

// MemoryStore is a deterministic in-memory store for Service tests.
type MemoryStore struct {
	mu      sync.RWMutex
	objects map[string]memoryObject
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{objects: make(map[string]memoryObject)}
}

func (s *MemoryStore) Health(ctx context.Context) error {
	return checkContext(ctx)
}

func (s *MemoryStore) Put(ctx context.Context, key string, body io.Reader, size int64, expectedSHA string) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if err := validateObjectKey(key); err != nil || body == nil || size < 0 || size == 1<<63-1 {
		return fmt.Errorf("invalid package object: key=%q size=%d", key, size)
	}
	data, err := io.ReadAll(io.LimitReader(body, size+1))
	if err != nil {
		return err
	}
	if int64(len(data)) != size {
		return fmt.Errorf("package size mismatch: got %d want %d", len(data), size)
	}
	actual := hashBytes(data)
	if expectedSHA != "" && !strings.EqualFold(actual, expectedSHA) {
		return fmt.Errorf("%w: got %s want %s", ErrIntegrity, actual, expectedSHA)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.objects[key]; ok {
		if len(existing.data) == len(data) && existing.sha256 == actual {
			return nil
		}
		return fmt.Errorf("%w: %s", ErrTargetConflict, key)
	}
	s.objects[key] = memoryObject{data: append([]byte(nil), data...), sha256: actual, lastModified: time.Now().UTC()}
	return nil
}

func (s *MemoryStore) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if err := validateObjectKey(key); err != nil {
		return nil, err
	}
	s.mu.RLock()
	obj, ok := s.objects[key]
	s.mu.RUnlock()
	if !ok {
		return nil, ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(append([]byte(nil), obj.data...))), nil
}

func (s *MemoryStore) Stat(ctx context.Context, key string) (*ObjectInfo, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if err := validateObjectKey(key); err != nil {
		return nil, err
	}
	s.mu.RLock()
	obj, ok := s.objects[key]
	s.mu.RUnlock()
	if !ok {
		return nil, ErrNotFound
	}
	return &ObjectInfo{Key: key, Size: int64(len(obj.data)), SHA256: obj.sha256, LastModified: obj.lastModified}, nil
}

func (s *MemoryStore) Promote(ctx context.Context, sourceKey, targetKey string, expected ObjectInfo) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if err := validateObjectKey(sourceKey); err != nil {
		return err
	}
	if err := validateObjectKey(targetKey); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	src, ok := s.objects[sourceKey]
	if !ok {
		return ErrNotFound
	}
	if expected.Size >= 0 && int64(len(src.data)) != expected.Size {
		return fmt.Errorf("%w: source size", ErrIntegrity)
	}
	if expected.SHA256 != "" && !strings.EqualFold(src.sha256, expected.SHA256) {
		return fmt.Errorf("%w: source sha256", ErrIntegrity)
	}
	if dst, exists := s.objects[targetKey]; exists {
		if len(dst.data) == len(src.data) && dst.sha256 == src.sha256 {
			return nil
		}
		return fmt.Errorf("%w: %s", ErrTargetConflict, targetKey)
	}
	s.objects[targetKey] = memoryObject{data: append([]byte(nil), src.data...), sha256: src.sha256, lastModified: time.Now().UTC()}
	return nil
}

func (s *MemoryStore) List(ctx context.Context, prefix, cursor string, limit int) ([]ObjectInfo, string, error) {
	if err := checkContext(ctx); err != nil {
		return nil, "", err
	}
	if prefix != "" {
		if err := validateObjectKey(strings.TrimSuffix(prefix, "/")); err != nil {
			return nil, "", err
		}
	}
	if limit <= 0 {
		limit = 100
	} else if limit > 1000 {
		limit = 1000
	}
	s.mu.RLock()
	keys := make([]string, 0, len(s.objects))
	for key := range s.objects {
		if strings.HasPrefix(key, prefix) && key > cursor {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	hasMore := len(keys) > limit
	if len(keys) > limit {
		keys = keys[:limit]
	}
	items := make([]ObjectInfo, 0, len(keys))
	for _, key := range keys {
		obj := s.objects[key]
		items = append(items, ObjectInfo{Key: key, Size: int64(len(obj.data)), SHA256: obj.sha256, LastModified: obj.lastModified})
	}
	s.mu.RUnlock()
	if hasMore {
		return items, items[len(items)-1].Key, nil
	}
	return items, "", nil
}

func (s *MemoryStore) Delete(ctx context.Context, key string) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if err := validateObjectKey(key); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.objects[key]; !ok {
		return ErrNotFound
	}
	delete(s.objects, key)
	return nil
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
