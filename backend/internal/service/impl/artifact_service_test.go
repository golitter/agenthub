package impl

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"agenthub/backend/internal/dao"
	"agenthub/backend/internal/generated"
	"agenthub/backend/internal/model"
	"agenthub/backend/pkg/artifact_store"

	"github.com/golang-jwt/jwt/v5"
)

type fakeArtifactDao struct {
	dao.ArtifactDao
	items map[string]*model.Artifact
}

func (f *fakeArtifactDao) CreatePending(artifact *model.Artifact, maxObjects int64) error {
	if f.items == nil {
		f.items = map[string]*model.Artifact{}
	}
	copy := *artifact
	f.items[artifact.ResourceID] = &copy
	return nil
}

func (f *fakeArtifactDao) MarkReady(resourceID string, size int64, sha256 string) error {
	item := f.items[resourceID]
	item.Size, item.SHA256, item.Status = size, sha256, model.ArtifactStatusReady
	return nil
}

func (f *fakeArtifactDao) MarkFailed(resourceID, message string) error {
	f.items[resourceID].Status = model.ArtifactStatusFailed
	return nil
}

func (f *fakeArtifactDao) FindReadyByResourceID(resourceID string) (*model.Artifact, error) {
	item := f.items[resourceID]
	if item == nil || item.Status != model.ArtifactStatusReady {
		return nil, nil
	}
	return item, nil
}

func (f *fakeArtifactDao) FindByResourceID(resourceID string) (*model.Artifact, error) {
	return f.items[resourceID], nil
}

func (f *fakeArtifactDao) FindByMessageAndIdempotency(messageID, key string) (*model.Artifact, error) {
	for _, item := range f.items {
		if item.MessageID == messageID && item.IdempotencyKey != nil && *item.IdempotencyKey == key {
			return item, nil
		}
	}
	return nil, nil
}

func (f *fakeArtifactDao) CountByMessageID(messageID string) (int64, error) {
	var count int64
	for _, item := range f.items {
		if item.MessageID == messageID && item.Status != model.ArtifactStatusDeleted {
			count++
		}
	}
	return count, nil
}

type fakeMessageDao struct {
	dao.MessageDao
	message *model.Message
}

func (f *fakeMessageDao) FindByMessageID(string) (*model.Message, error) { return f.message, nil }

type uncertainPutStore struct{ inner *artifact_store.MemoryStore }

func (s *uncertainPutStore) Put(ctx context.Context, key string, body io.Reader, size int64, options artifact_store.PutOptions) error {
	if err := s.inner.Put(ctx, key, body, size, options); err != nil {
		return err
	}
	return errors.New("connection lost after object commit")
}

func (s *uncertainPutStore) Open(ctx context.Context, key string) (io.ReadCloser, artifact_store.ObjectInfo, error) {
	return s.inner.Open(ctx, key)
}

func (s *uncertainPutStore) Stat(ctx context.Context, key string) (artifact_store.ObjectInfo, error) {
	return s.inner.Stat(ctx, key)
}

func (s *uncertainPutStore) Delete(ctx context.Context, key string) error {
	return s.inner.Delete(ctx, key)
}

func (s *uncertainPutStore) Health(ctx context.Context) error { return s.inner.Health(ctx) }

func TestArtifactCapabilityBindsMessageContext(t *testing.T) {
	service := NewArtifactService(nil, nil, artifact_store.NewMemoryStore(), ArtifactServiceConfig{
		CapabilitySecret:   "01234567890123456789012345678901",
		UploadTokenTTL:     time.Minute,
		MaxObjectSize:      1024,
		MaxArtifactsPerMsg: 2,
	})
	token, err := service.IssueUploadToken(
		"11111111-1111-4111-8111-111111111111",
		"22222222-2222-4222-8222-222222222222",
		"33333333-3333-4333-8333-333333333333",
	)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	capability, err := service.validateToken(token)
	if err != nil {
		t.Fatalf("validate token: %v", err)
	}
	if capability.MessageID != "33333333-3333-4333-8333-333333333333" || capability.MaxBytes != 1024 || capability.MaxObjects != 2 {
		t.Fatalf("unexpected capability: %+v", capability)
	}
}

func TestArtifactCapabilityRejectsWrongAudience(t *testing.T) {
	service := NewArtifactService(nil, nil, artifact_store.NewMemoryStore(), ArtifactServiceConfig{
		CapabilitySecret: "01234567890123456789012345678901",
	})
	if _, err := service.validateToken("not-a-token"); err == nil {
		t.Fatal("invalid capability unexpectedly accepted")
	}
	claims := jwt.MapClaims{
		"aud":         []string{"artifact-upload", "other"},
		"jti":         "test",
		"task_id":     "11111111-1111-4111-8111-111111111111",
		"session_id":  "22222222-2222-4222-8222-222222222222",
		"message_id":  "33333333-3333-4333-8333-333333333333",
		"max_bytes":   1024,
		"max_objects": 1,
		"iat":         time.Now().Unix(),
		"exp":         time.Now().Add(time.Minute).Unix(),
	}
	wrongAudience, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(service.secret)
	if err != nil {
		t.Fatalf("sign wrong audience token: %v", err)
	}
	if _, err := service.validateToken(wrongAudience); err == nil {
		t.Fatal("multi-audience capability unexpectedly accepted")
	}
}

func TestArtifactUploadStoresHtmlAndIsIdempotent(t *testing.T) {
	artifactDao := &fakeArtifactDao{items: map[string]*model.Artifact{}}
	messageDao := &fakeMessageDao{message: &model.Message{
		MessageID: "33333333-3333-4333-8333-333333333333",
		TaskID:    "11111111-1111-4111-8111-111111111111",
		SessionID: "22222222-2222-4222-8222-222222222222",
		Role:      string(generated.MessageRoleAgent),
	}}
	store := artifact_store.NewMemoryStore()
	service := NewArtifactService(artifactDao, messageDao, store, ArtifactServiceConfig{
		CapabilitySecret: "01234567890123456789012345678901", MaxObjectSize: 1024, MaxArtifactsPerMsg: 2,
	})
	token, err := service.IssueUploadToken(messageDao.message.TaskID, messageDao.message.SessionID, messageDao.message.MessageID)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	body := []byte("<html><body>ok</body></html>")
	first, err := service.UploadWithIdempotency(context.Background(), token, model.ArtifactKindHTML, "preview.html", "retry-1", bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	second, err := service.UploadWithIdempotency(context.Background(), token, model.ArtifactKindHTML, "preview.html", "retry-1", bytes.NewReader(body), int64(len(body)))
	if err != nil || second.ResourceID != first.ResourceID {
		t.Fatalf("idempotent upload = %+v, err=%v", second, err)
	}
	reader, artifact, err := service.Open(context.Background(), first.ResourceID)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer reader.Close()
	if artifact.ResourceID != first.ResourceID {
		t.Fatalf("opened artifact mismatch: %+v", artifact)
	}
}

func TestArtifactUploadCompensatesUnknownPutOutcome(t *testing.T) {
	artifactDao := &fakeArtifactDao{items: map[string]*model.Artifact{}}
	messageDao := &fakeMessageDao{message: &model.Message{
		MessageID: "33333333-3333-4333-8333-333333333333",
		TaskID:    "11111111-1111-4111-8111-111111111111",
		SessionID: "22222222-2222-4222-8222-222222222222",
		Role:      string(generated.MessageRoleAgent),
	}}
	store := &uncertainPutStore{inner: artifact_store.NewMemoryStore()}
	svc := NewArtifactService(artifactDao, messageDao, store, ArtifactServiceConfig{
		CapabilitySecret: "01234567890123456789012345678901", MaxObjectSize: 1024, MaxArtifactsPerMsg: 2,
	})
	token, err := svc.IssueUploadToken(messageDao.message.TaskID, messageDao.message.SessionID, messageDao.message.MessageID)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	if _, err := svc.Upload(context.Background(), token, model.ArtifactKindHTML, "preview.html", bytes.NewReader([]byte("<p>ok</p>")), 9); err == nil {
		t.Fatal("upload unexpectedly succeeded")
	}
	if len(artifactDao.items) != 1 {
		t.Fatalf("artifact rows = %d, want one failed row for cleanup", len(artifactDao.items))
	}
	for _, artifact := range artifactDao.items {
		if _, statErr := store.inner.Stat(context.Background(), artifact.ObjectKey); !errors.Is(statErr, artifact_store.ErrNotFound) {
			t.Fatalf("object cleanup error = %v, want ErrNotFound", statErr)
		}
	}
}

func TestValidateUploadCapabilityRechecksAgentMessage(t *testing.T) {
	messageDao := &fakeMessageDao{message: &model.Message{
		MessageID: "33333333-3333-4333-8333-333333333333",
		TaskID:    "11111111-1111-4111-8111-111111111111",
		SessionID: "22222222-2222-4222-8222-222222222222",
		Role:      string(generated.MessageRoleAgent),
	}}
	service := NewArtifactService(nil, messageDao, artifact_store.NewMemoryStore(), ArtifactServiceConfig{
		CapabilitySecret: "01234567890123456789012345678901",
	})
	token, err := service.IssueUploadToken(messageDao.message.TaskID, messageDao.message.SessionID, messageDao.message.MessageID)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	if err := service.ValidateUploadCapability(context.Background(), token); err != nil {
		t.Fatalf("validate upload capability: %v", err)
	}

	messageDao.message.Role = string(generated.MessageRoleUser)
	if err := service.ValidateUploadCapability(context.Background(), token); err == nil {
		t.Fatal("user message unexpectedly authorized for artifact upload")
	}
}
