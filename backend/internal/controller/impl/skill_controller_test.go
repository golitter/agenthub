package impl

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"agenthub/backend/internal/service"

	"github.com/gin-gonic/gin"
)

type controllerSkillService struct {
	limit        int64
	path         string
	content      []byte
	mode         os.FileMode
	uploadCalls  int
	confirmCalls int
	confirmTmp   string
}

func (f *controllerSkillService) UploadSkill(context.Context, string, []byte) (*service.ValidationResult, error) {
	return nil, nil
}

func (f *controllerSkillService) UploadSkillFile(_ context.Context, filename, path string, size int64) (*service.ValidationResult, error) {
	f.uploadCalls++
	f.path = path
	if info, statErr := os.Stat(path); statErr == nil {
		f.mode = info.Mode().Perm()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if filename != "demo.zip" || int64(len(data)) != size {
		return nil, os.ErrInvalid
	}
	f.content = data
	return &service.ValidationResult{Valid: true, Name: "demo"}, nil
}

func (f *controllerSkillService) SkillUploadLimit() int64 { return f.limit }

func (f *controllerSkillService) ConfirmSkill(_ context.Context, _ string, _ string, _ int, _ int64, tmpDir string) (*service.SkillImportResult, error) {
	f.confirmCalls++
	f.confirmTmp = tmpDir
	return nil, nil
}

func (f *controllerSkillService) ListSkills() ([]service.SkillHubItem, error) { return nil, nil }

func (f *controllerSkillService) DeleteSkill(context.Context, string) error { return nil }

func (f *controllerSkillService) ImportSkill(context.Context, string, string) (*service.SkillImportResult, error) {
	return nil, nil
}

func (f *controllerSkillService) RemoveSkill(context.Context, string, string) (*service.SkillImportResult, error) {
	return nil, nil
}

func (f *controllerSkillService) ReportBuiltinSkills([]service.BuiltinSkillItem) error { return nil }

func makeSkillMultipart(t *testing.T, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "demo.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return body, writer.FormDataContentType()
}

func TestSkillUploadUsesPrivateStagingAndCleansInput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	privateRoot := t.TempDir()
	serviceStub := &controllerSkillService{limit: 1024}
	controller := NewSkillController(serviceStub, privateRoot)
	router := gin.New()
	router.POST("/skills/upload", controller.Upload)

	body, contentType := makeSkillMultipart(t, []byte("zip bytes"))
	req := httptest.NewRequest("POST", "/skills/upload", body)
	req.Header.Set("Content-Type", contentType)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != 200 {
		t.Fatalf("unexpected status: %d body=%s", resp.Code, resp.Body.String())
	}
	if serviceStub.uploadCalls != 1 || !bytes.Equal(serviceStub.content, []byte("zip bytes")) {
		t.Fatalf("UploadSkillFile did not receive the streamed file: calls=%d content=%q", serviceStub.uploadCalls, serviceStub.content)
	}
	if serviceStub.path == "" || filepath.Dir(serviceStub.path) != privateRoot {
		t.Fatalf("upload was not staged in the configured private root: %q", serviceStub.path)
	}
	if serviceStub.mode == 0 {
		t.Fatal("service could not inspect the staging file")
	}
	if serviceStub.mode&0o077 != 0 {
		t.Fatalf("staging file is accessible by group/other: mode=%o", serviceStub.mode)
	}
	if _, err := os.Stat(serviceStub.path); !os.IsNotExist(err) {
		t.Fatalf("staging file was not removed after request: err=%v", err)
	}
}

func TestSkillUploadRejectsOversizedFileBeforeService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	serviceStub := &controllerSkillService{limit: 4}
	controller := NewSkillController(serviceStub, t.TempDir())
	router := gin.New()
	router.POST("/skills/upload", controller.Upload)

	body, contentType := makeSkillMultipart(t, []byte("too large"))
	req := httptest.NewRequest("POST", "/skills/upload", body)
	req.Header.Set("Content-Type", contentType)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != 400 {
		t.Fatalf("unexpected status: %d body=%s", resp.Code, resp.Body.String())
	}
	if serviceStub.uploadCalls != 0 {
		t.Fatal("oversized upload reached the service")
	}
}

func TestSkillConfirmDoesNotAcceptInternalMinIOPathFromTmpDir(t *testing.T) {
	gin.SetMode(gin.TestMode)
	serviceStub := &controllerSkillService{}
	controller := NewSkillController(serviceStub, t.TempDir())
	router := gin.New()
	router.POST("/skills/confirm", controller.Confirm)

	req := httptest.NewRequest("POST", "/skills/confirm", bytes.NewBufferString(`{"name":"demo","tmp_dir":"minio:incoming/abc.zip"}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != 400 {
		t.Fatalf("unexpected status: %d body=%s", resp.Code, resp.Body.String())
	}
	if serviceStub.confirmCalls != 0 {
		t.Fatal("internal MinIO path reached the confirmation service")
	}
}
