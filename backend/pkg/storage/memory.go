package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"sync"
)

// MemoryStore is a deterministic Provider/ObjectReader used by unit tests.
// It follows the same immutable-key and metadata rules as MinIOStorage.
type MemoryStore struct {
	mu      sync.RWMutex
	objects map[string]memoryObject
}

type memoryObject struct {
	data        []byte
	contentType string
	sha256      string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{objects: make(map[string]memoryObject)}
}

func (s *MemoryStore) UploadBytes(ctx context.Context, key string, data []byte) (string, error) {
	if err := checkContext(ctx); err != nil {
		return "", err
	}
	if err := validateObjectKey(key); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.objects[key]; exists {
		return "", fmt.Errorf("%w: %s", ErrObjectExists, key)
	}
	digest := sha256.Sum256(data)
	s.objects[key] = memoryObject{
		data:        append([]byte(nil), data...),
		contentType: contentTypeForKey(key),
		sha256:      hex.EncodeToString(digest[:]),
	}
	return "/api/assets/" + strings.TrimPrefix(key, "/"), nil
}

func (s *MemoryStore) UploadReader(ctx context.Context, key string, reader io.Reader, size int64) (string, error) {
	if reader == nil || size < 0 || size == 1<<63-1 {
		return "", fmt.Errorf("invalid memory upload reader")
	}
	if err := validateObjectKey(key); err != nil {
		return "", err
	}
	data, err := io.ReadAll(io.LimitReader(contextReader{ctx: ctx, reader: reader}, size+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) != size {
		return "", fmt.Errorf("memory upload size mismatch: got %d want %d", len(data), size)
	}
	return s.UploadBytes(ctx, key, data)
}

func (s *MemoryStore) Open(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	if err := checkContext(ctx); err != nil {
		return nil, ObjectInfo{}, err
	}
	info, err := s.Stat(ctx, key)
	if err != nil {
		return nil, ObjectInfo{}, err
	}
	s.mu.RLock()
	object := s.objects[key]
	s.mu.RUnlock()
	return io.NopCloser(bytes.NewReader(object.data)), info, nil
}

func (s *MemoryStore) Stat(ctx context.Context, key string) (ObjectInfo, error) {
	if err := checkContext(ctx); err != nil {
		return ObjectInfo{}, err
	}
	if err := validateObjectKey(key); err != nil {
		return ObjectInfo{}, err
	}
	s.mu.RLock()
	object, ok := s.objects[key]
	s.mu.RUnlock()
	if !ok {
		return ObjectInfo{}, ErrNotFound
	}
	return ObjectInfo{
		Key:         key,
		Size:        int64(len(object.data)),
		SHA256:      object.sha256,
		ContentType: object.contentType,
		ETag:        object.sha256,
	}, nil
}

func (s *MemoryStore) Health(ctx context.Context) error {
	return checkContext(ctx)
}
