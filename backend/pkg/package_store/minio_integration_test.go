package package_store

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// Run explicitly with MINIO_INTEGRATION=1.  The test never writes to a
// developer's bucket by accident; all integration settings must be supplied
// through the dedicated *_INTEGRATION_* variables.
func TestMinIOStoreIntegration(t *testing.T) {
	if os.Getenv("MINIO_INTEGRATION") != "1" {
		t.Skip("set MINIO_INTEGRATION=1 to run against a real MinIO service")
	}
	endpoint := os.Getenv("MINIO_INTEGRATION_ENDPOINT")
	bucket := os.Getenv("MINIO_INTEGRATION_BUCKET")
	access := os.Getenv("MINIO_INTEGRATION_ACCESS_KEY")
	secret := os.Getenv("MINIO_INTEGRATION_SECRET_KEY")
	if endpoint == "" || bucket == "" || access == "" || secret == "" {
		t.Fatal("MINIO_INTEGRATION_ENDPOINT, _BUCKET, _ACCESS_KEY and _SECRET_KEY are required")
	}
	store, err := NewMinIOStore(MinIOConfig{
		Endpoint: endpoint, Bucket: bucket, AccessKey: access, SecretKey: secret,
		UseSSL: os.Getenv("MINIO_INTEGRATION_USE_SSL") == "1", CAFile: os.Getenv("MINIO_INTEGRATION_CA_FILE"),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := store.Health(ctx); err != nil {
		t.Fatal(err)
	}
	prefix := "incoming/integration-" + strings.ReplaceAll(time.Now().UTC().Format("20060102T150405.000000000Z"), ":", "")
	source := prefix + ".zip"
	target := "skills/integration/" + strings.TrimPrefix(prefix, "incoming/") + ".zip"
	data := []byte("minio package store integration")
	sha := hashBytes(data)
	defer func() {
		_ = store.Delete(context.Background(), source)
		_ = store.Delete(context.Background(), target)
	}()
	if err := store.Put(ctx, source, bytes.NewReader(data), int64(len(data)), sha); err != nil {
		t.Fatal(err)
	}
	info, err := store.Stat(ctx, source)
	if err != nil || info.Size != int64(len(data)) || !strings.EqualFold(info.SHA256, sha) {
		t.Fatalf("source stat = %+v, err = %v", info, err)
	}
	if err := store.Promote(ctx, source, target, ObjectInfo{Size: int64(len(data)), SHA256: sha}); err != nil {
		t.Fatal(err)
	}
	if err := store.Promote(ctx, source, target, ObjectInfo{Size: int64(len(data)), SHA256: sha}); err != nil {
		t.Fatalf("idempotent promote: %v", err)
	}
	reader, err := store.Open(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	got, readErr := io.ReadAll(reader)
	_ = reader.Close()
	if readErr != nil || !bytes.Equal(got, data) {
		t.Fatalf("read target = %q, err = %v", got, readErr)
	}
	items, _, err := store.List(ctx, "skills/integration/", "", 100)
	if err != nil || len(items) == 0 {
		t.Fatalf("list target = %#v, err = %v", items, err)
	}
}
