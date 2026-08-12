package artifact_store

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type MinIOConfig struct {
	Endpoint       string
	Bucket         string
	AccessKey      string
	SecretKey      string
	UseSSL         bool
	CAFile         string
	RequestTimeout time.Duration
}

type MinIOStore struct {
	client         *minio.Client
	bucket         string
	requestTimeout time.Duration
}

func NewMinIOStore(cfg MinIOConfig) (*MinIOStore, error) {
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" || strings.TrimSpace(cfg.Bucket) == "" || cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, fmt.Errorf("artifact minio endpoint, bucket and credentials are required")
	}
	if strings.HasPrefix(endpoint, "http://") {
		endpoint = strings.TrimPrefix(endpoint, "http://")
		cfg.UseSSL = false
	} else if strings.HasPrefix(endpoint, "https://") {
		endpoint = strings.TrimPrefix(endpoint, "https://")
		cfg.UseSSL = true
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 15 * time.Second
	}
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("default HTTP transport is not cloneable")
	}
	transport := base.Clone()
	// Artifact objects and credentials stay on the private MinIO connection;
	// never route this client through HTTP_PROXY/HTTPS_PROXY inherited by the
	// Backend process.
	transport.Proxy = nil
	originalDial := transport.DialContext
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		dialCtx, cancel := context.WithTimeout(ctx, cfg.RequestTimeout)
		defer cancel()
		if originalDial != nil {
			return originalDial(dialCtx, network, address)
		}
		return (&net.Dialer{Timeout: cfg.RequestTimeout}).DialContext(dialCtx, network, address)
	}
	transport.TLSHandshakeTimeout = cfg.RequestTimeout
	transport.ResponseHeaderTimeout = cfg.RequestTimeout
	if cfg.CAFile != "" {
		pemData, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read artifact minio CA certificate: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pemData) {
			return nil, fmt.Errorf("artifact minio CA certificate contains no certificates")
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	}
	client, err := minio.New(strings.TrimRight(endpoint, "/"), &minio.Options{
		Creds: credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""), Secure: cfg.UseSSL, Transport: transport,
	})
	if err != nil {
		return nil, fmt.Errorf("create artifact minio client: %w", err)
	}
	return &MinIOStore{client: client, bucket: cfg.Bucket, requestTimeout: cfg.RequestTimeout}, nil
}

func (s *MinIOStore) Put(ctx context.Context, key string, body io.Reader, size int64, options PutOptions) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("artifact minio store is not initialized")
	}
	if err := CheckContext(ctx); err != nil {
		return err
	}
	if err := ValidateObjectKey(key); err != nil {
		return err
	}
	if body == nil || size < 0 || size == 1<<63-1 {
		return fmt.Errorf("invalid artifact upload")
	}
	ctx, cancel := s.operationContext(ctx)
	defer cancel()
	putOptions := minio.PutObjectOptions{ContentType: options.ContentType, UserMetadata: minio.StringMap{"x-amz-meta-sha256": options.SHA256}}
	putOptions.SetMatchETagExcept("*")
	_, err := s.client.PutObject(ctx, s.bucket, key, body, size, putOptions)
	if err != nil {
		if isPreconditionFailure(err) {
			return ErrExists
		}
		return mapMinIOError(err)
	}
	return nil
}

func (s *MinIOStore) Open(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	if s == nil || s.client == nil {
		return nil, ObjectInfo{}, fmt.Errorf("artifact minio store is not initialized")
	}
	if err := ValidateObjectKey(key); err != nil {
		return nil, ObjectInfo{}, err
	}
	ctx, cancel := s.operationContext(ctx)
	object, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
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
	return &cancelReadCloser{ReadCloser: object, cancel: cancel}, objectInfo(info), nil
}

func (s *MinIOStore) Stat(ctx context.Context, key string) (ObjectInfo, error) {
	if s == nil || s.client == nil {
		return ObjectInfo{}, fmt.Errorf("artifact minio store is not initialized")
	}
	if err := ValidateObjectKey(key); err != nil {
		return ObjectInfo{}, err
	}
	ctx, cancel := s.operationContext(ctx)
	defer cancel()
	info, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return ObjectInfo{}, mapMinIOError(err)
	}
	return objectInfo(info), nil
}

func (s *MinIOStore) Delete(ctx context.Context, key string) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("artifact minio store is not initialized")
	}
	if err := ValidateObjectKey(key); err != nil {
		return err
	}
	ctx, cancel := s.operationContext(ctx)
	defer cancel()
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return mapMinIOError(err)
	}
	return nil
}

func (s *MinIOStore) Health(ctx context.Context) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("artifact minio store is not initialized")
	}
	ctx, cancel := s.operationContext(ctx)
	defer cancel()
	// Use the bucket-location probe rather than BucketExists/HeadBucket so the
	// runtime account does not need ListBucket permission just to report ready.
	if _, err := s.client.GetBucketLocation(ctx, s.bucket); err != nil {
		return mapMinIOError(err)
	}
	return nil
}

func (s *MinIOStore) operationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := s.requestTimeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return context.WithTimeout(ctx, timeout)
}

func objectInfo(info minio.ObjectInfo) ObjectInfo {
	return ObjectInfo{Key: info.Key, Size: info.Size, ContentType: info.ContentType, SHA256: objectSHA(info.UserMetadata), ETag: info.ETag}
}

func objectSHA(metadata minio.StringMap) string {
	for key, value := range metadata {
		if strings.EqualFold(key, "sha256") || strings.EqualFold(key, "x-amz-meta-sha256") {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func mapMinIOError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: %v", ErrTimeout, err)
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("%w: %v", ErrTimeout, err)
	}
	resp := minio.ToErrorResponse(err)
	if resp.StatusCode == http.StatusNotFound || resp.Code == "NoSuchKey" || resp.Code == "NoSuchObject" {
		return fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized || resp.Code == "AccessDenied" {
		return fmt.Errorf("%w: %v", ErrPermission, err)
	}
	return err
}

func isPreconditionFailure(err error) bool {
	resp := minio.ToErrorResponse(err)
	return resp.StatusCode == http.StatusPreconditionFailed || resp.Code == "PreconditionFailed"
}

type cancelReadCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (r *cancelReadCloser) Close() error {
	err := r.ReadCloser.Close()
	r.cancel()
	return err
}
