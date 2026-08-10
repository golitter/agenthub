package package_store

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

func TestMemoryStorePromoteIsIdempotentAndImmutable(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	data := []byte("skill package")
	sha := hashBytes(data)
	if err := store.Put(ctx, "incoming/u1.zip", bytesReader(data), int64(len(data)), sha); err != nil {
		t.Fatal(err)
	}
	expected := ObjectInfo{Size: int64(len(data)), SHA256: sha}
	if err := store.Promote(ctx, "incoming/u1.zip", "skills/demo/"+sha+".zip", expected); err != nil {
		t.Fatal(err)
	}
	if err := store.Promote(ctx, "incoming/u1.zip", "skills/demo/"+sha+".zip", expected); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, "skills/demo/"+sha+".zip", bytesReader([]byte("other")), 5, hashBytes([]byte("other"))); !errors.Is(err, ErrTargetConflict) {
		t.Fatalf("Put conflict error = %v, want ErrTargetConflict", err)
	}
}

func TestMemoryStoreListPaginates(t *testing.T) {
	store := NewMemoryStore()
	for _, key := range []string{"incoming/a", "incoming/b", "skills/a"} {
		data := []byte(key)
		if err := store.Put(context.Background(), key, bytesReader(data), int64(len(data)), hashBytes(data)); err != nil {
			t.Fatal(err)
		}
	}
	items, cursor, err := store.List(context.Background(), "incoming/", "", 1)
	if err != nil || len(items) != 1 || cursor == "" {
		t.Fatalf("first page = %#v cursor=%q err=%v", items, cursor, err)
	}
	items, next, err := store.List(context.Background(), "incoming/", cursor, 1)
	if err != nil || len(items) != 1 || next != "" {
		t.Fatalf("second page = %#v cursor=%q err=%v", items, next, err)
	}
}

func bytesReader(data []byte) io.Reader { return bytes.NewReader(data) }
