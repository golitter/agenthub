package artifact_store

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestMemoryStoreKeepsImmutableObjectsAndMetadata(t *testing.T) {
	store := NewMemoryStore()
	body := []byte("<html><body>ok</body></html>")
	if err := store.Put(context.Background(), "artifacts/task/message/resource.html", bytes.NewReader(body), int64(len(body)), PutOptions{ContentType: "text/html; charset=utf-8"}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := store.Put(context.Background(), "artifacts/task/message/resource.html", bytes.NewReader(body), int64(len(body)), PutOptions{}); !errors.Is(err, ErrExists) {
		t.Fatalf("second put error = %v, want ErrExists", err)
	}
	info, err := store.Stat(context.Background(), "artifacts/task/message/resource.html")
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size != int64(len(body)) || info.ContentType != "text/html; charset=utf-8" || info.SHA256 == "" {
		t.Fatalf("unexpected object info: %+v", info)
	}
}

func TestMemoryStoreRejectsUnsafeKeys(t *testing.T) {
	store := NewMemoryStore()
	if err := store.Put(context.Background(), "../escape", bytes.NewReader([]byte("x")), 1, PutOptions{}); err == nil {
		t.Fatal("unsafe key unexpectedly accepted")
	}
}
