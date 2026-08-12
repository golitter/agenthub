package impl

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"testing"

	"agenthub/backend/pkg/storage"
)

type avatarTestProvider struct {
	ctx context.Context
}

func (p *avatarTestProvider) UploadBytes(ctx context.Context, _ string, _ []byte) (string, error) {
	p.ctx = ctx
	return "/api/assets/avatars/550e8400-e29b-41d4-a716-446655440000.png", ctx.Err()
}

func (p *avatarTestProvider) UploadReader(context.Context, string, io.Reader, int64) (string, error) {
	return "", nil
}

func TestAvatarServiceValidatesRealImageAndReturnsStableURL(t *testing.T) {
	provider := storage.NewMemoryStore()
	service := NewAvatarService(nil, provider)
	data := testAvatarPNG(t)

	if _, err := service.UploadAvatar(t.Context(), "avatar.jpg", data); err == nil {
		t.Fatal("extension/content mismatch accepted")
	}
	url, err := service.UploadAvatar(t.Context(), "avatar.png", data)
	if err != nil {
		t.Fatalf("UploadAvatar: %v", err)
	}
	if len(url) != len("/api/assets/avatars/")+len("550e8400-e29b-41d4-a716-446655440000.png") || url[:len("/api/assets/avatars/")] != "/api/assets/avatars/" {
		t.Fatalf("avatar URL = %q, want stable MinIO URL", url)
	}
}

func TestAvatarServicePassesCancellationToProvider(t *testing.T) {
	provider := &avatarTestProvider{}
	service := NewAvatarService(nil, provider)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := service.UploadAvatar(ctx, "avatar.png", testAvatarPNG(t)); err == nil {
		t.Fatal("cancelled upload unexpectedly succeeded")
	}
	if provider.ctx == nil || provider.ctx.Err() == nil {
		t.Fatal("provider did not receive cancelled context")
	}
}

func testAvatarPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{G: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}
