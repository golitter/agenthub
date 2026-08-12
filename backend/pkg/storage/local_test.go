package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	store, err := NewLocalStorage(t.TempDir(), "/uploads")
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}

	url, err := store.UploadBytes(t.Context(), "avatars/./avatar.png", []byte("image"))
	if err != nil {
		t.Fatalf("UploadBytes: %v", err)
	}
	if url != "/uploads/avatars/avatar.png" {
		t.Fatalf("url = %q, want normalized URL", url)
	}
}

func TestLocalStorageRejectsSymlinkRootAndReaderSizeMismatch(t *testing.T) {
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "uploads")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink is unavailable: %v", err)
	}
	if _, err := NewLocalStorage(link, "/uploads"); err == nil {
		t.Fatal("symlink storage root was accepted")
	}

	nestedRoot := t.TempDir()
	nestedTarget := t.TempDir()
	if err := os.Symlink(nestedTarget, filepath.Join(nestedRoot, "avatars")); err != nil {
		t.Skipf("symlink is unavailable: %v", err)
	}
	if _, err := NewLocalStorage(nestedRoot, "/uploads"); err == nil {
		t.Fatal("nested storage symlink was accepted")
	}

	parentRoot := t.TempDir()
	parentTarget := t.TempDir()
	parentLink := filepath.Join(parentRoot, "linked")
	if err := os.Symlink(parentTarget, parentLink); err != nil {
		t.Skipf("symlink is unavailable: %v", err)
	}
	if _, err := NewLocalStorage(filepath.Join(parentLink, "uploads"), "/uploads"); err == nil {
		t.Fatal("symlinked storage parent was accepted")
	}
	if _, err := os.Stat(filepath.Join(parentTarget, "uploads")); !os.IsNotExist(err) {
		t.Fatalf("storage directory was created through symlink, stat error = %v", err)
	}

	store, err := NewLocalStorage(t.TempDir(), "/uploads")
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}
	if _, err := store.UploadReader(t.Context(), "avatars/size.png", strings.NewReader("too-long"), 3); err == nil {
		t.Fatal("reader size mismatch was accepted")
	}
	if _, err := os.Stat(filepath.Join(store.Dir(), "avatars", "size.png")); !os.IsNotExist(err) {
		t.Fatalf("mismatched upload file still exists, stat error = %v", err)
	}
}

func TestLocalStorageRejectsExternalURLPrefix(t *testing.T) {
	if _, err := NewLocalStorage(t.TempDir(), "https://cdn.example.com/uploads"); err == nil {
		t.Fatal("external URL prefix was accepted")
	}
	if _, err := NewLocalStorage(t.TempDir(), "//cdn.example.com/uploads"); err == nil {
		t.Fatal("protocol-relative URL prefix was accepted")
	}
}
