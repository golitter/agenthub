package impl

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"agenthub/backend/internal/model"
	"agenthub/backend/internal/service"

	"github.com/gin-gonic/gin"
)

type controllerArtifactService struct {
	validateErr  error
	validateCall int
	uploadCall   int
}

func (s *controllerArtifactService) ValidateUploadCapability(context.Context, string) error {
	s.validateCall++
	return s.validateErr
}

func (s *controllerArtifactService) Upload(context.Context, string, string, string, io.Reader, int64) (*service.ArtifactInfo, error) {
	s.uploadCall++
	return &service.ArtifactInfo{ResourceID: "11111111-1111-4111-8111-111111111111", Kind: model.ArtifactKindHTML}, nil
}

func (s *controllerArtifactService) Get(string) (*model.Artifact, error) { return nil, nil }

func (s *controllerArtifactService) Open(context.Context, string) (io.ReadCloser, *model.Artifact, error) {
	return nil, nil, nil
}

type trackingReadCloser struct {
	*bytes.Reader
	reads int
}

func (r *trackingReadCloser) Read(p []byte) (int, error) {
	r.reads++
	return r.Reader.Read(p)
}

func (r *trackingReadCloser) Close() error { return nil }

func newArtifactUploadRouter(svc service.ArtifactService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	ctrl := NewArtifactController(svc, 128)
	ctrl.RegisterUploadRoutes(router.Group("/api/internal"))
	return router
}

func TestArtifactUploadValidatesCapabilityBeforeReadingMultipartBody(t *testing.T) {
	svc := &controllerArtifactService{validateErr: service.ErrUnauthorized("invalid artifact capability")}
	router := newArtifactUploadRouter(svc)
	body := &trackingReadCloser{Reader: bytes.NewReader([]byte("not multipart"))}
	req := httptest.NewRequest(http.MethodPost, "/api/internal/artifacts", body)
	req.Header.Set("Authorization", "Bearer scoped-token")
	req.ContentLength = -1
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusUnauthorized)
	}
	if svc.validateCall != 1 || svc.uploadCall != 0 || body.reads != 0 {
		t.Fatalf("validation/read order = calls=%d upload=%d reads=%d", svc.validateCall, svc.uploadCall, body.reads)
	}
}

func TestArtifactUploadPassesValidatedMultipartToService(t *testing.T) {
	svc := &controllerArtifactService{}
	router := newArtifactUploadRouter(svc)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("kind", model.ArtifactKindHTML); err != nil {
		t.Fatalf("write kind: %v", err)
	}
	part, err := writer.CreateFormFile("file", "preview.html")
	if err != nil {
		t.Fatalf("create file part: %v", err)
	}
	if _, err := part.Write([]byte("<p>ok</p>")); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/internal/artifacts", &body)
	req.Header.Set("Authorization", "Bearer scoped-token")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d body=%s", resp.Code, http.StatusCreated, resp.Body.String())
	}
	if svc.validateCall != 1 || svc.uploadCall != 1 {
		t.Fatalf("service calls = validate %d upload %d", svc.validateCall, svc.uploadCall)
	}
}
