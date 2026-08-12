package storage

import (
	"context"
	"errors"
	"testing"

	"agenthub/backend/internal/conf"
)

func TestNewRuntimeSelectsLocalWriterWithoutMinIO(t *testing.T) {
	runtime, err := NewRuntime(&conf.StorageConfig{
		WriteProvider: "local",
		Local:         conf.LocalStorageConfig{Enabled: true, Dir: t.TempDir(), URLPrefix: "/uploads"},
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if _, ok := runtime.Writer.(*LocalStorage); !ok || runtime.AssetReader != nil || runtime.Local == nil {
		t.Fatalf("runtime = %+v, want local writer without MinIO reader", runtime)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := runtime.Writer.UploadBytes(ctx, "avatars/cancelled.png", []byte("x")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled local upload error = %v, want context.Canceled", err)
	}
}

func TestNewRuntimeKeepsMinIOReaderWhenLocalIsWriter(t *testing.T) {
	runtime, err := NewRuntime(&conf.StorageConfig{
		WriteProvider: "local",
		MinIO: conf.AvatarMinIOConfig{
			Enabled:   true,
			Endpoint:  "minio:9000",
			Bucket:    "agenthub-assets",
			AccessKey: "asset-user",
			SecretKey: "asset-secret",
		},
		Local: conf.LocalStorageConfig{Enabled: true, Dir: t.TempDir(), URLPrefix: "/uploads"},
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if runtime.AssetReader != runtime.MinIO || runtime.MinIO == nil {
		t.Fatalf("runtime AssetReader = %T, MinIO = %p; want same reader", runtime.AssetReader, runtime.MinIO)
	}
	if _, ok := runtime.Writer.(*LocalStorage); !ok {
		t.Fatalf("writer = %T, want LocalStorage", runtime.Writer)
	}
}

func TestNewRuntimeDoesNotFallbackOrAcceptUnknownWriter(t *testing.T) {
	if _, err := NewRuntime(&conf.StorageConfig{WriteProvider: "unsupported"}); err == nil {
		t.Fatal("unknown writer was accepted")
	}
	if _, err := NewRuntime(&conf.StorageConfig{
		WriteProvider: "minio",
		MinIO:         conf.AvatarMinIOConfig{Enabled: true, Endpoint: "minio:9000", Bucket: "agenthub-assets"},
		Local:         conf.LocalStorageConfig{Enabled: true, Dir: t.TempDir(), URLPrefix: "/uploads"},
	}); err == nil {
		t.Fatal("MinIO writer without credentials was accepted")
	}
}
