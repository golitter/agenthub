package impl

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"agenthub/backend/internal/dao"
	"agenthub/backend/internal/generated"
	"agenthub/backend/internal/model"
	"agenthub/backend/internal/service"
	"agenthub/backend/pkg/artifact_store"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type ArtifactServiceConfig struct {
	CapabilitySecret   string
	UploadTokenTTL     time.Duration
	MaxObjectSize      int64
	MaxArtifactsPerMsg int
}

type ArtifactService struct {
	dao           dao.ArtifactDao
	messageDao    dao.MessageDao
	store         artifact_store.Store
	secret        []byte
	tokenTTL      time.Duration
	maxBytes      int64
	maxPerMessage int
}

func NewArtifactService(artifactDao dao.ArtifactDao, messageDao dao.MessageDao, store artifact_store.Store, cfg ArtifactServiceConfig) *ArtifactService {
	if cfg.UploadTokenTTL <= 0 {
		cfg.UploadTokenTTL = 30 * time.Minute
	}
	if cfg.MaxObjectSize <= 0 {
		cfg.MaxObjectSize = 25 * 1024 * 1024
	}
	if cfg.MaxArtifactsPerMsg <= 0 {
		cfg.MaxArtifactsPerMsg = 20
	}
	return &ArtifactService{dao: artifactDao, messageDao: messageDao, store: store, secret: []byte(cfg.CapabilitySecret), tokenTTL: cfg.UploadTokenTTL, maxBytes: cfg.MaxObjectSize, maxPerMessage: cfg.MaxArtifactsPerMsg}
}

func (svc *ArtifactService) IssueUploadToken(taskID, sessionID, messageID string) (string, error) {
	if svc == nil || len(svc.secret) == 0 {
		return "", fmt.Errorf("artifact capability is not configured")
	}
	now := time.Now()
	jtiValue, err := uuid.NewRandom()
	if err != nil {
		return "", err
	}
	jti := jtiValue.String()
	claims := jwt.MapClaims{
		"aud":         "artifact-upload",
		"jti":         jti,
		"task_id":     taskID,
		"session_id":  sessionID,
		"message_id":  messageID,
		"max_bytes":   svc.maxBytes,
		"max_objects": svc.maxPerMessage,
		"iat":         now.Unix(),
		"exp":         now.Add(svc.tokenTTL).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(svc.secret)
}

type artifactCapability struct {
	TaskID     string
	SessionID  string
	MessageID  string
	MaxBytes   int64
	MaxObjects int
}

func (svc *ArtifactService) validateToken(raw string) (*artifactCapability, error) {
	if svc == nil || len(svc.secret) == 0 || strings.TrimSpace(raw) == "" {
		return nil, service.ErrUnauthorized("invalid artifact capability")
	}
	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return svc.secret, nil
	}, jwt.WithAudience("artifact-upload"), jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || token == nil || !token.Valid {
		return nil, service.ErrUnauthorized("invalid artifact capability")
	}
	if audience, ok := claims["aud"].(string); !ok || audience != "artifact-upload" {
		return nil, service.ErrUnauthorized("invalid artifact capability")
	}
	getString := func(name string) string {
		value, _ := claims[name].(string)
		return strings.TrimSpace(value)
	}
	if _, uuidOK := normalizeUUID(getString("jti")); !uuidOK {
		return nil, service.ErrUnauthorized("invalid artifact capability")
	}
	capability := &artifactCapability{TaskID: getString("task_id"), SessionID: getString("session_id"), MessageID: getString("message_id")}
	var uuidOK bool
	if capability.TaskID, uuidOK = normalizeUUID(capability.TaskID); !uuidOK {
		return nil, service.ErrUnauthorized("invalid artifact capability")
	}
	if capability.SessionID, uuidOK = normalizeUUID(capability.SessionID); !uuidOK {
		return nil, service.ErrUnauthorized("invalid artifact capability")
	}
	if capability.MessageID, uuidOK = normalizeUUID(capability.MessageID); !uuidOK {
		return nil, service.ErrUnauthorized("invalid artifact capability")
	}
	issuedAt, ok := numericClaimInt64(claims["iat"])
	if !ok || issuedAt <= 0 {
		return nil, service.ErrUnauthorized("invalid artifact capability")
	}
	maxBytes, ok := numericClaimInt64(claims["max_bytes"])
	if !ok || maxBytes <= 0 || maxBytes > svc.maxBytes {
		return nil, service.ErrUnauthorized("invalid artifact capability")
	}
	maxObjects, ok := numericClaimInt64(claims["max_objects"])
	if !ok || maxObjects <= 0 || maxObjects > int64(svc.maxPerMessage) {
		return nil, service.ErrUnauthorized("invalid artifact capability")
	}
	capability.MaxBytes = maxBytes
	capability.MaxObjects = int(maxObjects)
	return capability, nil
}

func numericClaimInt64(value interface{}) (int64, bool) {
	switch typed := value.(type) {
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) || typed < math.MinInt64 || typed > math.MaxInt64 || typed != math.Trunc(typed) {
			return 0, false
		}
		return int64(typed), true
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	default:
		return 0, false
	}
}

func (svc *ArtifactService) Upload(ctx context.Context, rawToken, kind, filename string, body io.Reader, size int64) (*service.ArtifactInfo, error) {
	return svc.UploadWithIdempotency(ctx, rawToken, kind, filename, "", body, size)
}

func (svc *ArtifactService) ValidateUploadCapability(ctx context.Context, rawToken string) error {
	capability, err := svc.validateToken(rawToken)
	if err != nil {
		return err
	}
	if svc.messageDao == nil {
		return service.ErrServiceUnavailable("artifact storage is not configured")
	}
	if _, err := svc.loadCapabilityMessage(capability); err != nil {
		return err
	}
	return nil
}

func (svc *ArtifactService) UploadWithIdempotency(ctx context.Context, rawToken, kind, filename, idempotencyKey string, body io.Reader, size int64) (*service.ArtifactInfo, error) {
	capability, err := svc.validateToken(rawToken)
	if err != nil {
		return nil, err
	}
	if svc.store == nil || svc.dao == nil || svc.messageDao == nil {
		return nil, service.ErrServiceUnavailable("artifact storage is not configured")
	}
	if size < 0 || size > svc.maxBytes || size > capability.MaxBytes {
		return nil, service.ErrBadRequest("artifact exceeds the configured size limit")
	}
	if kind != model.ArtifactKindHTML {
		return nil, service.ErrBadRequest("only html artifacts are supported")
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if len(idempotencyKey) > 128 || strings.ContainsAny(idempotencyKey, "\r\n") {
		return nil, service.ErrBadRequest("invalid idempotency key")
	}
	if _, err := svc.loadCapabilityMessage(capability); err != nil {
		return nil, err
	}
	filename = normalizeArtifactFilename(filename)
	data, sha256Hex, err := readArtifactBody(ctx, body, size, minInt64(svc.maxBytes, capability.MaxBytes))
	if err != nil {
		return nil, service.ErrBadRequest(err.Error())
	}
	if idempotencyKey != "" {
		existing, lookupErr := svc.dao.FindByMessageAndIdempotency(capability.MessageID, idempotencyKey)
		if lookupErr != nil {
			return nil, service.ErrInternal("failed to check artifact idempotency")
		}
		if existing != nil {
			if existing.Status == model.ArtifactStatusReady && equalDigest(existing.SHA256, sha256Hex) {
				return artifactInfo(existing), nil
			}
			return nil, service.ErrConflict("artifact upload with this idempotency key conflicts with an existing upload")
		}
	}
	count, err := svc.dao.CountByMessageID(capability.MessageID)
	if err != nil {
		return nil, service.ErrInternal("failed to check artifact quota")
	}
	if count >= int64(capability.MaxObjects) || count >= int64(svc.maxPerMessage) {
		return nil, service.ErrConflict("artifact quota exceeded")
	}
	resourceUUID, err := uuid.NewRandom()
	if err != nil {
		return nil, service.ErrInternal("failed to allocate artifact resource id")
	}
	resourceID := resourceUUID.String()
	objectKey := "artifacts/" + capability.TaskID + "/" + capability.MessageID + "/" + resourceID + ".html"
	artifact := &model.Artifact{
		ResourceID: resourceID, TaskID: capability.TaskID, SessionID: capability.SessionID, MessageID: capability.MessageID,
		Kind: kind, ObjectKey: objectKey, Filename: filename, ContentType: "text/html; charset=utf-8", Size: size, SHA256: sha256Hex, Status: model.ArtifactStatusPending,
	}
	if idempotencyKey != "" {
		artifact.IdempotencyKey = &idempotencyKey
	}
	if err := svc.dao.CreatePending(artifact, int64(capability.MaxObjects)); err != nil {
		if errors.Is(err, dao.ErrArtifactQuota) {
			return nil, service.ErrConflict("artifact quota exceeded")
		}
		// The composite message/idempotency unique index closes the concurrent
		// retry race. Re-read the winner so a duplicate request still receives
		// the original resource instead of a spurious 500.
		if idempotencyKey != "" {
			existing, lookupErr := svc.dao.FindByMessageAndIdempotency(capability.MessageID, idempotencyKey)
			if lookupErr == nil && existing != nil {
				if existing.Status == model.ArtifactStatusReady && equalDigest(existing.SHA256, sha256Hex) {
					return artifactInfo(existing), nil
				}
				return nil, service.ErrConflict("artifact upload with this idempotency key conflicts with an existing upload")
			}
		}
		return nil, service.ErrInternal("failed to create artifact metadata")
	}
	if err := svc.store.Put(ctx, objectKey, bytes.NewReader(data), int64(len(data)), artifact_store.PutOptions{ContentType: artifact.ContentType, SHA256: sha256Hex}); err != nil {
		_ = svc.dao.MarkFailed(resourceID, "artifact object upload failed")
		// An immutable-key collision means this request never owned the object;
		// do not let the stale-row cleanup delete another artifact's object.
		if errors.Is(err, artifact_store.ErrExists) {
			_ = svc.dao.DeleteRow(resourceID)
		} else {
			// Put has an unknown outcome on network errors: MinIO may have
			// committed the object before the response was lost. The key is
			// unique to this pending row, so a best-effort compensating delete is
			// safe even when the request context has already been cancelled.
			svc.cleanupObject(objectKey, resourceID)
		}
		return nil, service.ErrServiceUnavailable("artifact storage upload failed")
	}
	info, err := svc.store.Stat(ctx, objectKey)
	if err != nil || info.Size != int64(len(data)) || !equalDigest(info.SHA256, sha256Hex) || !strings.HasPrefix(strings.ToLower(info.ContentType), "text/html") {
		_ = svc.dao.MarkFailed(resourceID, "artifact object verification failed")
		svc.cleanupObject(objectKey, resourceID)
		return nil, service.ErrServiceUnavailable("artifact storage verification failed")
	}
	if err := svc.dao.MarkReady(resourceID, int64(len(data)), sha256Hex); err != nil {
		// A database error is not proof that the conditional update rolled back.
		// Re-read before deciding whether the object can be removed; otherwise a
		// committed ready row could be left pointing at a deleted MinIO object.
		if existing, lookupErr := svc.dao.FindReadyByResourceID(resourceID); lookupErr == nil && existing != nil &&
			existing.Size == int64(len(data)) && equalDigest(existing.SHA256, sha256Hex) {
			return artifactInfo(existing), nil
		}
		// MarkFailed is conditional on pending status. If task deletion already
		// removed the row, or moved it to a terminal cleanup state, remove the
		// object now; otherwise leave it attached to the failed row for stale
		// cleanup. This closes the row-deleted-after-Put orphan window without
		// deleting an object whose ready metadata may have committed.
		_ = svc.dao.MarkFailed(resourceID, "artifact metadata finalization failed")
		if current, lookupErr := svc.dao.FindByResourceID(resourceID); lookupErr == nil &&
			(current == nil || current.Status == model.ArtifactStatusFailed || current.Status == model.ArtifactStatusDeleting || current.Status == model.ArtifactStatusDeleted) {
			svc.cleanupObject(objectKey, resourceID)
		}
		return nil, service.ErrInternal("failed to finalize artifact metadata")
	}
	artifact.Status = model.ArtifactStatusReady
	artifact.Size = int64(len(data))
	artifact.UpdatedAt = time.Now()
	return artifactInfo(artifact), nil
}

func (svc *ArtifactService) cleanupObject(objectKey, resourceID string) {
	if svc == nil || svc.store == nil {
		return
	}
	// Do not reuse an upload request context here: callers may cancel it after
	// the object has already been committed, which would make compensation
	// impossible. Store implementations still enforce their own request limit.
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := svc.store.Delete(cleanupCtx, objectKey); err != nil && !errors.Is(err, artifact_store.ErrNotFound) {
		slog.Warn("cleanup artifact object failed", "resource_id", resourceID, "error", err)
	}
}

func (svc *ArtifactService) loadCapabilityMessage(capability *artifactCapability) (*model.Message, error) {
	if svc == nil || svc.messageDao == nil || capability == nil {
		return nil, service.ErrServiceUnavailable("artifact storage is not configured")
	}
	message, err := svc.messageDao.FindByMessageID(capability.MessageID)
	if err != nil {
		return nil, service.ErrInternal("failed to verify artifact message")
	}
	if message == nil || message.Role != string(generated.MessageRoleAgent) || message.TaskID != capability.TaskID || message.SessionID != capability.SessionID {
		return nil, service.ErrForbidden("artifact message does not match capability")
	}
	return message, nil
}

func (svc *ArtifactService) Get(resourceID string) (*model.Artifact, error) {
	if svc == nil || svc.dao == nil {
		return nil, service.ErrServiceUnavailable("artifact storage is not configured")
	}
	canonicalID, ok := normalizeUUID(resourceID)
	if !ok {
		return nil, service.ErrNotFound("artifact not found")
	}
	artifact, err := svc.dao.FindReadyByResourceID(canonicalID)
	if err != nil {
		return nil, service.ErrInternal("failed to load artifact")
	}
	if artifact == nil {
		return nil, service.ErrNotFound("artifact not found")
	}
	if artifact.Kind != model.ArtifactKindHTML || !validSHA256(artifact.SHA256) || artifact.Size < 0 || artifact.ContentType != "text/html; charset=utf-8" {
		return nil, service.ErrServiceUnavailable("artifact metadata integrity check failed")
	}
	return artifact, nil
}

func (svc *ArtifactService) Open(ctx context.Context, resourceID string) (io.ReadCloser, *model.Artifact, error) {
	if svc == nil || svc.store == nil {
		return nil, nil, service.ErrServiceUnavailable("artifact storage is not configured")
	}
	artifact, err := svc.Get(resourceID)
	if err != nil {
		return nil, nil, err
	}
	reader, info, err := svc.store.Open(ctx, artifact.ObjectKey)
	if err != nil {
		if errors.Is(err, artifact_store.ErrNotFound) {
			return nil, nil, service.ErrNotFound("artifact not found")
		}
		return nil, nil, service.ErrServiceUnavailable("artifact storage is unavailable")
	}
	if info.Size != artifact.Size || !equalDigest(info.SHA256, artifact.SHA256) || !strings.HasPrefix(strings.ToLower(info.ContentType), "text/html") {
		_ = reader.Close()
		return nil, nil, service.ErrServiceUnavailable("artifact storage integrity check failed")
	}
	return reader, artifact, nil
}

func artifactInfo(artifact *model.Artifact) *service.ArtifactInfo {
	return &service.ArtifactInfo{ResourceID: artifact.ResourceID, Kind: artifact.Kind, Filename: artifact.Filename, ContentType: artifact.ContentType, Size: artifact.Size, SHA256: artifact.SHA256, CreatedAt: artifact.CreatedAt}
}

func readArtifactBody(ctx context.Context, body io.Reader, size, limit int64) ([]byte, string, error) {
	if err := artifact_store.CheckContext(ctx); err != nil {
		return nil, "", err
	}
	if body == nil || size < 0 || size > limit || size == 1<<63-1 {
		return nil, "", fmt.Errorf("artifact exceeds the configured size limit")
	}
	// The multipart header supplies the declared object size. Read at most one
	// byte beyond that declaration so a mismatched/overlong part fails without
	// buffering the entire configured limit first.
	reader := io.LimitReader(body, size+1)
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, "", fmt.Errorf("read artifact: %w", err)
	}
	if int64(len(data)) != size {
		return nil, "", fmt.Errorf("artifact size mismatch")
	}
	if !utf8HTML(data) {
		return nil, "", fmt.Errorf("artifact must be valid UTF-8 html")
	}
	digest := make([]byte, 0, 32)
	// Keep the hash calculation local and allocation-free for normal HTML.
	hashValue := sha256Bytes(data)
	digest = append(digest, hashValue...)
	return data, hex.EncodeToString(digest), nil
}

func sha256Bytes(data []byte) []byte {
	result := sha256.Sum256(data)
	return result[:]
}

func utf8HTML(data []byte) bool {
	return utf8.Valid(data)
}

func normalizeArtifactFilename(filename string) string {
	// Multipart filenames can come from a Windows client even when Backend
	// runs on Linux. Normalize both separator styles before keeping only the
	// final component; the name is metadata/header-only and never an object key.
	filename = filepath.Base(strings.ReplaceAll(strings.TrimSpace(filename), "\\", "/"))
	if filename == "." || filename == ".." || filename == "/" || filename == "" {
		return "preview.html"
	}
	if len([]rune(filename)) > 255 || len([]byte(filename)) > 255 || strings.ContainsAny(filename, "\r\n") || strings.ContainsFunc(filename, unicode.IsControl) {
		return "preview.html"
	}
	return filename
}

func equalDigest(left, right string) bool {
	left = strings.ToLower(strings.TrimSpace(left))
	right = strings.ToLower(strings.TrimSpace(right))
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func minInt64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}

func normalizeUUID(value string) (string, bool) {
	value = strings.TrimSpace(value)
	parsed, err := uuid.Parse(value)
	if err != nil || !strings.EqualFold(parsed.String(), value) {
		return "", false
	}
	return parsed.String(), true
}
