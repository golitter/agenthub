package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	minio "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const defaultMinIORequestTimeout = 10 * time.Second

// MinIOConfig contains the private avatar bucket connection settings.
// Endpoint may include an http(s) scheme for local/operator convenience; the
// configuration validator still rejects http endpoints in production mode.
type MinIOConfig struct {
	Endpoint       string
	Bucket         string
	AccessKey      string
	SecretKey      string
	UseSSL         bool
	CAFile         string
	RequestTimeout time.Duration
}

// MinIOStorage is the private, avatar-only MinIO implementation of both
// Provider and ObjectReader.
type MinIOStorage struct {
	client         *minio.Client
	bucket         string
	requestTimeout time.Duration
}

func NewMinIOStorage(cfg MinIOConfig) (*MinIOStorage, error) {
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" || strings.TrimSpace(cfg.Bucket) == "" {
		return nil, fmt.Errorf("avatar minio endpoint and bucket are required")
	}
	if strings.TrimSpace(cfg.AccessKey) == "" || strings.TrimSpace(cfg.SecretKey) == "" {
		return nil, fmt.Errorf("avatar minio credentials are required")
	}
	if len([]byte(cfg.SecretKey)) < 8 {
		return nil, fmt.Errorf("avatar minio secret key must be at least 8 characters")
	}
	scheme := strings.ToLower(endpoint)
	if strings.HasPrefix(scheme, "http://") {
		endpoint = endpoint[len("http://"):]
		cfg.UseSSL = false
	} else if strings.HasPrefix(scheme, "https://") {
		endpoint = endpoint[len("https://"):]
		cfg.UseSSL = true
	}
	endpoint = strings.TrimRight(endpoint, "/")
	if endpoint == "" {
		return nil, fmt.Errorf("avatar minio endpoint is required")
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = defaultMinIORequestTimeout
	}

	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("default HTTP transport is not cloneable")
	}
	transport := base.Clone()
	originalDial := transport.DialContext
	if originalDial == nil {
		dialer := &net.Dialer{Timeout: cfg.RequestTimeout, KeepAlive: 30 * time.Second}
		transport.DialContext = dialer.DialContext
	} else {
		transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			dialCtx, cancel := context.WithTimeout(ctx, cfg.RequestTimeout)
			defer cancel()
			return originalDial(dialCtx, network, address)
		}
	}
	transport.TLSHandshakeTimeout = cfg.RequestTimeout
	transport.ResponseHeaderTimeout = cfg.RequestTimeout
	transport.ExpectContinueTimeout = time.Second
	transport.IdleConnTimeout = 90 * time.Second
	if caFile := strings.TrimSpace(cfg.CAFile); caFile != "" {
		pemData, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read avatar minio CA certificate: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pemData) {
			return nil, fmt.Errorf("avatar minio CA certificate contains no certificates")
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	}

	client, err := minio.New(endpoint, &minio.Options{
		Creds:     credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure:    cfg.UseSSL,
		Transport: transport,
	})
	if err != nil {
		return nil, fmt.Errorf("create avatar minio client: %w", err)
	}
	return &MinIOStorage{client: client, bucket: cfg.Bucket, requestTimeout: cfg.RequestTimeout}, nil
}

func (s *MinIOStorage) UploadBytes(ctx context.Context, key string, data []byte) (string, error) {
	if err := checkContext(ctx); err != nil {
		return "", err
	}
	if err := validateObjectKey(key); err != nil {
		return "", err
	}
	if s == nil || s.client == nil {
		return "", fmt.Errorf("avatar minio storage is not initialized")
	}

	digest := sha256.Sum256(data)
	sha := hex.EncodeToString(digest[:])
	operationCtx, cancel := s.operationContext(ctx)
	defer cancel()

	options := minio.PutObjectOptions{
		ContentType:  contentTypeForKey(key),
		UserMetadata: map[string]string{"sha256": sha},
	}
	// Use the conditional write as the sole collision check. A preflight
	// StatObject would require the caller to distinguish a missing object from
	// a permission-denied response when ListBucket is intentionally absent.
	// MinIO/S3 rejects an existing key with PreconditionFailed.
	options.SetMatchETagExcept("*")
	_, err := s.client.PutObject(operationCtx, s.bucket, key, bytes.NewReader(data), int64(len(data)), options)
	if err != nil {
		if isPreconditionFailure(err) {
			return "", fmt.Errorf("%w: %s", ErrObjectExists, key)
		}
		return "", mapMinIOError(err)
	}
	return s.publicURL(key), nil
}

func (s *MinIOStorage) UploadReader(ctx context.Context, key string, reader io.Reader, size int64) (string, error) {
	if reader == nil || size < 0 {
		return "", fmt.Errorf("invalid avatar upload reader")
	}
	if size == 1<<63-1 {
		return "", fmt.Errorf("invalid avatar upload size")
	}
	if err := validateObjectKey(key); err != nil {
		return "", err
	}
	data, err := io.ReadAll(io.LimitReader(contextReader{ctx: ctx, reader: reader}, size+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) != size {
		return "", fmt.Errorf("avatar upload size mismatch: got %d want %d", len(data), size)
	}
	return s.UploadBytes(ctx, key, data)
}

func (s *MinIOStorage) Open(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	if err := checkContext(ctx); err != nil {
		return nil, ObjectInfo{}, err
	}
	if err := validateObjectKey(key); err != nil {
		return nil, ObjectInfo{}, err
	}
	if s == nil || s.client == nil {
		return nil, ObjectInfo{}, fmt.Errorf("avatar minio storage is not initialized")
	}
	operationCtx, cancel := s.operationContext(ctx)
	object, err := s.client.GetObject(operationCtx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		cancel()
		return nil, ObjectInfo{}, mapMinIOError(err)
	}
	info, err := object.Stat()
	if err != nil {
		_ = object.Close()
		cancel()
		return nil, ObjectInfo{}, mapMinIOError(err)
	}
	objectInfo := objectInfoFromMinIO(key, info)
	return &cancelReadCloser{ReadCloser: object, cancel: cancel}, objectInfo, nil
}

func (s *MinIOStorage) Stat(ctx context.Context, key string) (ObjectInfo, error) {
	if err := checkContext(ctx); err != nil {
		return ObjectInfo{}, err
	}
	if err := validateObjectKey(key); err != nil {
		return ObjectInfo{}, err
	}
	if s == nil || s.client == nil {
		return ObjectInfo{}, fmt.Errorf("avatar minio storage is not initialized")
	}
	operationCtx, cancel := s.operationContext(ctx)
	defer cancel()
	return s.stat(operationCtx, key)
}

func (s *MinIOStorage) stat(ctx context.Context, key string) (ObjectInfo, error) {
	info, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return ObjectInfo{}, mapMinIOError(err)
	}
	return objectInfoFromMinIO(key, info), nil
}

func (s *MinIOStorage) Health(ctx context.Context) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("avatar minio storage is not initialized")
	}
	if err := checkContext(ctx); err != nil {
		return mapContextError(err)
	}
	operationCtx, cancel := s.operationContext(ctx)
	defer cancel()
	// GetBucketLocation is intentionally used instead of BucketExists: the
	// Asset policy grants this read-only probe but deliberately does not grant
	// ListBucket/HeadBucket permissions.
	if _, err := s.client.GetBucketLocation(operationCtx, s.bucket); err != nil {
		return mapMinIOError(err)
	}
	return nil
}

func (s *MinIOStorage) operationContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, s.requestTimeout)
}

func (s *MinIOStorage) publicURL(key string) string {
	return "/api/assets/" + strings.TrimPrefix(key, "/")
}

func objectInfoFromMinIO(key string, info minio.ObjectInfo) ObjectInfo {
	sha := objectSHA(info.UserMetadata)
	etag := sha
	if etag == "" {
		etag = strings.Trim(info.ETag, "\"")
	}
	return ObjectInfo{
		Key:         key,
		Size:        info.Size,
		SHA256:      sha,
		ContentType: info.ContentType,
		ETag:        etag,
	}
}

func contentTypeForKey(key string) string {
	dot := strings.LastIndex(key, ".")
	if dot < 0 {
		return "application/octet-stream"
	}
	switch strings.ToLower(key[dot:]) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}

func objectSHA(metadata minio.StringMap) string {
	for key, value := range metadata {
		if strings.EqualFold(key, "sha256") || strings.EqualFold(key, "x-amz-meta-sha256") {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func mapContextError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: %v", ErrTimeout, err)
	}
	return err
}

func mapMinIOError(err error) error {
	if err == nil {
		return nil
	}
	if mapped := mapContextError(err); !errors.Is(mapped, err) || errors.Is(err, context.DeadlineExceeded) {
		return mapped
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("%w: %v", ErrTimeout, err)
	}
	resp := minio.ToErrorResponse(err)
	switch {
	case resp.Code == "NoSuchBucket":
		// A missing bucket is an upstream storage failure, not a missing avatar;
		// keep it as an internal error so the proxy does not cache a misleading
		// 404 for every object.
		return err
	case resp.StatusCode == http.StatusNotFound || resp.Code == "NoSuchKey" || resp.Code == "NoSuchObject" || resp.Code == "NotFound":
		return fmt.Errorf("%w: %v", ErrNotFound, err)
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || resp.Code == "AccessDenied":
		return fmt.Errorf("%w: %v", ErrPermission, err)
	default:
		return err
	}
}

func isPreconditionFailure(err error) bool {
	resp := minio.ToErrorResponse(err)
	return resp.StatusCode == http.StatusPreconditionFailed || resp.Code == "PreconditionFailed"
}

type cancelReadCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (r *cancelReadCloser) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if err != nil {
		return n, mapMinIOError(err)
	}
	return n, nil
}

func (r *cancelReadCloser) Close() error {
	err := r.ReadCloser.Close()
	r.cancel()
	return err
}
