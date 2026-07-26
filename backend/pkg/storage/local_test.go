package storage

import "testing"

func TestValidateKeyRejectsTraversal(t *testing.T) {
	for _, key := range []string{
		"../avatar.png",
		"avatars/../../secret.txt",
		"/tmp/avatar.png",
		"",
		"   ",
	} {
		if _, err := validateKey(key); err == nil {
			t.Fatalf("validateKey(%q) error = nil, want error", key)
		}
	}
}

func TestValidateKeyAllowsDotsInsideFilename(t *testing.T) {
	key, err := validateKey("avatars/avatar..v2.png")
	if err != nil {
		t.Fatalf("validateKey: %v", err)
	}
	if key != "avatars/avatar..v2.png" {
		t.Fatalf("key = %q, want normalized key", key)
	}
}

func TestLocalStorageUploadNormalizesKey(t *testing.T) {
	store, err := NewLocalStorage(t.TempDir(), "http://localhost/uploads")
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}

	url, err := store.UploadBytes(t.Context(), "avatars/./avatar.png", []byte("image"))
	if err != nil {
		t.Fatalf("UploadBytes: %v", err)
	}
	if url != "http://localhost/uploads/avatars/avatar.png" {
		t.Fatalf("url = %q, want normalized URL", url)
	}
}
