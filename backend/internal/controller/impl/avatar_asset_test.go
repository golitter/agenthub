package impl

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"agenthub/backend/pkg/storage"

	"github.com/gin-gonic/gin"
)

func TestAssetControllerServesGetHeadAndNotModified(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := storage.NewMemoryStore()
	key := "avatars/550e8400-e29b-41d4-a716-446655440000.png"
	data := testPNG(t)
	if _, err := store.UploadBytes(t.Context(), key, data); err != nil {
		t.Fatalf("UploadBytes: %v", err)
	}

	router := gin.New()
	assets := router.Group("/api/assets")
	NewAssetController(store).RegisterRoutes(assets)
	path := "/api/assets/avatars/550e8400-e29b-41d4-a716-446655440000.png"

	get := httptest.NewRecorder()
	router.ServeHTTP(get, httptest.NewRequest(http.MethodGet, path, nil))
	if get.Code != http.StatusOK || !bytes.Equal(get.Body.Bytes(), data) {
		t.Fatalf("GET status/body = %d/%d, want 200/%d", get.Code, get.Body.Len(), len(data))
	}
	if get.Header().Get("Content-Type") != "image/png" || get.Header().Get("Content-Length") != strconv.Itoa(len(data)) {
		t.Fatalf("GET headers = %#v", get.Header())
	}
	if get.Header().Get("Cache-Control") != avatarCacheControl || get.Header().Get("X-Content-Type-Options") != avatarNoSniff {
		t.Fatalf("cache/security headers = %#v", get.Header())
	}
	etag := get.Header().Get("ETag")
	if etag == "" {
		t.Fatal("GET did not return ETag")
	}

	head := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodHead, path, nil)
	r.Header.Set("If-None-Match", etag)
	router.ServeHTTP(head, r)
	if head.Code != http.StatusNotModified || head.Body.Len() != 0 {
		t.Fatalf("conditional HEAD = %d/%d, want 304/0", head.Code, head.Body.Len())
	}

	missing := httptest.NewRecorder()
	router.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/api/assets/avatars/550e8400-e29b-41d4-a716-446655440001.png", nil))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d, want 404", missing.Code)
	}
	if missing.Header().Get("Cache-Control") != "" {
		t.Fatalf("missing object unexpectedly received immutable cache header: %q", missing.Header().Get("Cache-Control"))
	}
}

func TestAssetControllerRejectsUnsafePathsAndMethods(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := storage.NewMemoryStore()
	router := gin.New()
	NewAssetController(store).RegisterRoutes(router.Group("/api/assets"))

	for _, path := range []string{
		"/api/assets/avatars/../secret.png",
		"/api/assets/avatars/550e8400-e29b-41d4-a716-446655440000%2f.png",
		"/api/assets/avatars/550e8400-e29b-41d4-a716-446655440000.PNG",
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusNotFound {
			t.Errorf("unsafe path %q status = %d, want 404", path, recorder.Code)
		}
	}
	post := httptest.NewRecorder()
	router.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/api/assets/avatars/550e8400-e29b-41d4-a716-446655440000.png", nil))
	if post.Code != http.StatusNotFound {
		t.Fatalf("POST status = %d, want 404", post.Code)
	}
}

func TestAssetControllerMapsReadTimeoutBeforeResponseCommit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewAssetController(timeoutAssetReader{}).RegisterRoutes(router.Group("/api/assets"))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/assets/avatars/550e8400-e29b-41d4-a716-446655440000.png", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("read timeout status = %d, want 503", recorder.Code)
	}
}

func TestStrongAvatarETagRejectsHeaderInjection(t *testing.T) {
	if got := strongAvatarETag(storage.ObjectInfo{SHA256: "bad\r\nX-Injected: yes"}); got != `"unknown"` {
		t.Fatalf("unsafe ETag = %q, want unknown", got)
	}
}

type timeoutAssetReader struct{}

func (timeoutAssetReader) Open(_ context.Context, _ string) (io.ReadCloser, storage.ObjectInfo, error) {
	return timeoutReadCloser{}, storage.ObjectInfo{Size: 1, ContentType: "image/png"}, nil
}

func (timeoutAssetReader) Stat(context.Context, string) (storage.ObjectInfo, error) {
	return storage.ObjectInfo{}, storage.ErrNotFound
}

func (timeoutAssetReader) Health(context.Context) error { return nil }

type timeoutReadCloser struct{}

func (timeoutReadCloser) Read([]byte) (int, error) { return 0, storage.ErrTimeout }

func (timeoutReadCloser) Close() error { return nil }

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}
