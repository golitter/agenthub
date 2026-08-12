package artifact_store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sync"
)

type MemoryStore struct {
	mu      sync.RWMutex
	objects map[string]memoryObject
}

type memoryObject struct {
	data []byte
	info ObjectInfo
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{objects: make(map[string]memoryObject)}
}

func (s *MemoryStore) Put(ctx context.Context, key string, body io.Reader, size int64, options PutOptions) error {
	if err := CheckContext(ctx); err != nil {
		return err
	}
	if err := ValidateObjectKey(key); err != nil || body == nil || size < 0 || size == 1<<63-1 {
		if err != nil {
			return err
		}
		return fmt.Errorf("invalid artifact upload")
	}
	data, err := io.ReadAll(io.LimitReader(body, size+1))
	if err != nil {
		return err
	}
	if int64(len(data)) != size {
		return fmt.Errorf("artifact size mismatch: got %d want %d", len(data), size)
	}
	digest := sha256.Sum256(data)
	actual := hex.EncodeToString(digest[:])
	if options.SHA256 != "" && options.SHA256 != actual {
		return fmt.Errorf("artifact sha256 mismatch")
	}
	contentType := options.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.objects[key]; exists {
		return ErrExists
	}
	s.objects[key] = memoryObject{data: append([]byte(nil), data...), info: ObjectInfo{
		Key: key, Size: size, ContentType: contentType, SHA256: actual, ETag: actual,
	}}
	return nil
}

func (s *MemoryStore) Open(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	if err := CheckContext(ctx); err != nil {
		return nil, ObjectInfo{}, err
	}
	if err := ValidateObjectKey(key); err != nil {
		return nil, ObjectInfo{}, err
	}
	s.mu.RLock()
	object, ok := s.objects[key]
	s.mu.RUnlock()
	if !ok {
		return nil, ObjectInfo{}, ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(object.data)), object.info, nil
}

func (s *MemoryStore) Stat(ctx context.Context, key string) (ObjectInfo, error) {
	if err := CheckContext(ctx); err != nil {
		return ObjectInfo{}, err
	}
	if err := ValidateObjectKey(key); err != nil {
		return ObjectInfo{}, err
	}
	s.mu.RLock()
	object, ok := s.objects[key]
	s.mu.RUnlock()
	if !ok {
		return ObjectInfo{}, ErrNotFound
	}
	return object.info, nil
}

func (s *MemoryStore) Delete(ctx context.Context, key string) error {
	if err := CheckContext(ctx); err != nil {
		return err
	}
	if err := ValidateObjectKey(key); err != nil {
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

func (s *MemoryStore) Health(ctx context.Context) error { return CheckContext(ctx) }
