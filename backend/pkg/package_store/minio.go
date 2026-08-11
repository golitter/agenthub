package package_store

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// MinIOConfig contains the connection settings for a private package bucket.
// Endpoint must be a host:port value; an http(s) scheme is accepted and
// overrides UseSSL for convenience when values come from environment files.
type MinIOConfig struct {
	Endpoint  string
	Bucket    string
	AccessKey string
	SecretKey string
	UseSSL    bool
	CAFile    string
}

// MinIOStore implements PackageStore using the MinIO Go SDK. Objects are
// immutable: Put and Promote are idempotent for the same content and reject
// attempts to replace an existing key with different content.
type MinIOStore struct {
	client *minio.Client
	bucket string
}

func NewMinIOStore(cfg MinIOConfig) (*MinIOStore, error) {
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" || strings.TrimSpace(cfg.Bucket) == "" {
		return nil, fmt.Errorf("minio endpoint and bucket are required")
	}
	if strings.HasPrefix(endpoint, "http://") {
		endpoint = strings.TrimPrefix(endpoint, "http://")
		cfg.UseSSL = false
	} else if strings.HasPrefix(endpoint, "https://") {
		endpoint = strings.TrimPrefix(endpoint, "https://")
		cfg.UseSSL = true
	}
	endpoint = strings.TrimRight(endpoint, "/")
	options := &minio.Options{Creds: credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""), Secure: cfg.UseSSL}
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("default HTTP transport is not cloneable")
	}
	transport := base.Clone()
	// Keep per-operation context deadlines as the outer bound while ensuring a
	// dead endpoint cannot hold a connection attempt or response header forever.
	originalDial := transport.DialContext
	if originalDial == nil {
		dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
		transport.DialContext = dialer.DialContext
	} else {
		transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			return originalDial(dialCtx, network, address)
		}
	}
	transport.TLSHandshakeTimeout = 10 * time.Second
	transport.ResponseHeaderTimeout = 30 * time.Second
	transport.ExpectContinueTimeout = time.Second
	transport.IdleConnTimeout = 90 * time.Second
	options.Transport = transport
	if caFile := strings.TrimSpace(cfg.CAFile); caFile != "" {
		pemData, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read minio CA certificate: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pemData) {
			return nil, fmt.Errorf("minio CA certificate contains no certificates")
		}
		if transport.TLSClientConfig != nil {
			transport.TLSClientConfig = transport.TLSClientConfig.Clone()
		} else {
			transport.TLSClientConfig = &tls.Config{}
		}
		transport.TLSClientConfig.RootCAs = pool
	}
	client, err := minio.New(endpoint, options)
	if err != nil {
		return nil, fmt.Errorf("create minio client: %w", err)
	}
	return &MinIOStore{client: client, bucket: cfg.Bucket}, nil
}

// EnsureBucket creates the configured bucket when it does not exist. It is
// intentionally separate from NewMinIOStore so constructing the application
// does not perform a network write unless storage has been enabled.
func (s *MinIOStore) EnsureBucket(ctx context.Context) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("minio store is not initialized")
	}
	if err := checkContext(ctx); err != nil {
		return err
	}
	ctx = contextOrBackground(ctx)
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return mapMinIOError(err)
	}
	if exists {
		return nil
	}
	if err := s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{Region: "us-east-1"}); err != nil {
		resp := minio.ToErrorResponse(err)
		if resp.Code == "BucketAlreadyExists" || resp.Code == "BucketAlreadyOwnedByYou" {
			return nil
		}
		return mapMinIOError(err)
	}
	return nil
}

// Health checks bucket reachability without creating or mutating storage.
func (s *MinIOStore) Health(ctx context.Context) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("minio store is not initialized")
	}
	if err := checkContext(ctx); err != nil {
		return err
	}
	exists, err := s.client.BucketExists(contextOrBackground(ctx), s.bucket)
	if err != nil {
		return mapMinIOError(err)
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

func (s *MinIOStore) Put(ctx context.Context, key string, body io.Reader, size int64, expectedSHA string) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	ctx = contextOrBackground(ctx)
	if s == nil || s.client == nil || body == nil || size < 0 {
		return fmt.Errorf("invalid minio package object: key=%q size=%d", key, size)
	}
	// The bounded preflight below reads size+1 bytes. Reject the only int64
	// value for which that arithmetic would overflow before touching storage.
	if size == 1<<63-1 {
		return fmt.Errorf("invalid minio package size: %d", size)
	}
	if err := validateObjectKey(key); err != nil {
		return err
	}
	// minio-go retries idempotent requests only when the request body is
	// seekable.  The upload path normally supplies an *os.File, but wrapping it
	// in io.TeeReader would hide that capability and silently reduce Put to a
	// single attempt.  Preflight seekable bodies in bounded streaming fashion,
	// reset them before checking the target.  This is important even when the
	// target already exists: immutable-object idempotency must compare the
	// caller's bytes, not merely trust a declared hash or the target's size.
	// Non-seekable callers retain the streaming fallback; an existing target is
	// hashed directly (the body is not needed again), while a missing target is
	// streamed once into MinIO.
	var expectedBodySHA string
	var seekableBody io.ReadSeeker
	if seeker, ok := body.(io.ReadSeeker); ok {
		start, seekErr := seeker.Seek(0, io.SeekCurrent)
		if seekErr != nil {
			return seekErr
		}
		var hashErr error
		expectedBodySHA, hashErr = hashReaderExact(seeker, size)
		if hashErr != nil {
			return hashErr
		}
		if expectedSHA != "" && !strings.EqualFold(expectedBodySHA, expectedSHA) {
			return fmt.Errorf("%w: got %s want %s", ErrIntegrity, expectedBodySHA, expectedSHA)
		}
		if _, seekErr = seeker.Seek(start, io.SeekStart); seekErr != nil {
			return seekErr
		}
		seekableBody = seeker
	}

	if current, err := s.Stat(ctx, key); err == nil {
		compareSHA := expectedBodySHA
		if seekableBody == nil {
			compareSHA, err = hashReaderExact(body, size)
			if err != nil {
				return err
			}
			if expectedSHA != "" && !strings.EqualFold(compareSHA, expectedSHA) {
				return fmt.Errorf("%w: got %s want %s", ErrIntegrity, compareSHA, expectedSHA)
			}
		}
		if err := compareObject(current, size, compareSHA, key); err != nil {
			return err
		}
		return s.verifyStoredContent(ctx, key, size, compareSHA)
	} else if !isNotFound(err) {
		return err
	}

	var putBody io.Reader = body
	var counting *countingReader
	if seekableBody == nil {
		hasher := sha256.New()
		counting = &countingReader{Reader: io.TeeReader(io.LimitReader(body, size), hasher), hasher: hasher}
		putBody = counting
	}
	shaForObject := strings.TrimSpace(expectedSHA)
	if shaForObject == "" && expectedBodySHA != "" {
		// Seekable upload bodies can be preflighted without buffering. Preserve
		// the computed digest as object metadata even when the caller did not
		// provide an expected value, matching MemoryStore's behavior.
		shaForObject = expectedBodySHA
	}
	putOptions := minio.PutObjectOptions{
		ContentType:  "application/zip",
		UserMetadata: metadataForSHA(shaForObject),
	}
	// The preliminary Stat is only a fast path.  Keep the actual write
	// conditional as well so two Backend instances racing on an immutable key
	// cannot overwrite one another between Stat and PutObject.
	putOptions.SetMatchETagExcept("*")
	_, err := s.client.PutObject(ctx, s.bucket, key, putBody, size, putOptions)
	if err != nil {
		if target, statErr := s.Stat(ctx, key); statErr == nil {
			if seekableBody == nil {
				// A non-seekable source may have been partially consumed by the
				// failed request.  Even when the caller supplied an expected SHA,
				// accepting the target here would trust metadata without proving
				// that this source carried the same bytes.  The caller can retry
				// with a fresh reader; never turn this race into an unsafe success.
				return fmt.Errorf("%w: %s cannot compare non-seekable source after race", ErrTargetConflict, key)
			}
			compareSHA := strings.TrimSpace(expectedSHA)
			if compareSHA == "" {
				compareSHA = expectedBodySHA
			}
			if compareSHA == "" {
				return fmt.Errorf("%w: %s cannot compare non-seekable source", ErrTargetConflict, key)
			}
			if compareErr := compareObject(target, size, compareSHA, key); compareErr == nil {
				return s.verifyStoredContent(ctx, key, size, compareSHA)
			} else {
				return compareErr
			}
		}
		if isPreconditionFailure(err) {
			return fmt.Errorf("%w: %s", ErrTargetConflict, key)
		}
		return mapMinIOError(err)
	}
	if seekableBody != nil {
		// The preflight above read size+1 bytes from the same source and reset it
		// before PutObject, so an oversized or short seekable source cannot be
		// silently truncated by the declared object size.
		return s.verifyStoredContent(ctx, key, size, shaForObject)
	}
	if counting == nil {
		// The preflight above read size+1 bytes from the same source and reset it
		// before PutObject, so an oversized or short seekable source cannot be
		// silently truncated by the declared object size.
		return s.verifyStoredContent(ctx, key, size, shaForObject)
	}
	if counting.n != size {
		_ = s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
		return fmt.Errorf("package size mismatch: got %d want %d", counting.n, size)
	}
	// PutObject is given the declared size and therefore stops reading at that
	// boundary. Probe the caller's reader once more so an oversized source is
	// not silently accepted merely because its first `size` bytes were valid.
	var extra [1]byte
	if n, readErr := body.Read(extra[:]); n > 0 {
		_ = s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
		return fmt.Errorf("package size mismatch: source contains bytes beyond %d", size)
	} else if readErr != nil && !errors.Is(readErr, io.EOF) {
		_ = s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
		return fmt.Errorf("read package after declared size: %w", readErr)
	}
	actual := hex.EncodeToString(counting.hasher.Sum(nil))
	if expectedSHA != "" && !strings.EqualFold(actual, expectedSHA) {
		_ = s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
		return fmt.Errorf("%w: got %s want %s", ErrIntegrity, actual, expectedSHA)
	}
	// Non-seekable bodies cannot carry the computed digest in the initial
	// PutObject metadata, but the bytes are still verified before success is
	// reported. Callers that need future metadata-only comparisons should pass
	// an expected SHA-256 (the normal Backend path uses seekable files).
	return s.verifyStoredContent(ctx, key, size, actual)
}

// hashReaderExact computes the identity of exactly size bytes and rejects both
// short and oversized readers.  It intentionally does not buffer the payload;
// callers that need to send a seekable source again reset it after this pass.
func hashReaderExact(body io.Reader, size int64) (string, error) {
	if body == nil || size < 0 || size == 1<<63-1 {
		return "", fmt.Errorf("invalid package size or reader")
	}
	hasher := sha256.New()
	readSize, err := io.Copy(hasher, io.LimitReader(body, size+1))
	if err != nil {
		return "", fmt.Errorf("read package for integrity check: %w", err)
	}
	if readSize != size {
		return "", fmt.Errorf("package size mismatch: got %d want %d", readSize, size)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func (s *MinIOStore) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	ctx = contextOrBackground(ctx)
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("invalid minio object key")
	}
	if err := validateObjectKey(key); err != nil {
		return nil, err
	}
	object, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, mapMinIOError(err)
	}
	if _, err := object.Stat(); err != nil {
		_ = object.Close()
		return nil, mapMinIOError(err)
	}
	return object, nil
}

func (s *MinIOStore) Stat(ctx context.Context, key string) (*ObjectInfo, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	ctx = contextOrBackground(ctx)
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("invalid minio object key")
	}
	if err := validateObjectKey(key); err != nil {
		return nil, err
	}
	info, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return nil, mapMinIOError(err)
	}
	return &ObjectInfo{
		Key:          info.Key,
		Size:         info.Size,
		SHA256:       objectSHA(info.UserMetadata),
		LastModified: info.LastModified,
	}, nil
}

func (s *MinIOStore) Promote(ctx context.Context, sourceKey, targetKey string, expected ObjectInfo) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	ctx = contextOrBackground(ctx)
	if err := validateObjectKey(sourceKey); err != nil {
		return err
	}
	if err := validateObjectKey(targetKey); err != nil {
		return err
	}
	source, err := s.Stat(ctx, sourceKey)
	if err != nil {
		return err
	}
	if err := verifyObject(source, expected.Size, expected.SHA256, sourceKey); err != nil {
		return err
	}
	sourceSHA := expected.SHA256
	if sourceSHA == "" {
		sourceSHA = source.SHA256
	}
	if sourceSHA == "" {
		// Legacy objects may not carry the user-metadata digest.  Derive the
		// source identity from its bytes before comparing/reusing a target; a
		// size-only comparison would allow two different packages to converge
		// under one immutable key.
		sourceSHA, err = s.hashStoredContent(ctx, sourceKey, source.Size)
		if err != nil {
			return err
		}
	}
	if err := s.verifyStoredContent(ctx, sourceKey, source.Size, sourceSHA); err != nil {
		return err
	}
	if target, err := s.Stat(ctx, targetKey); err == nil {
		if err := compareObject(target, source.Size, sourceSHA, targetKey); err != nil {
			return err
		}
		return s.verifyStoredContent(ctx, targetKey, source.Size, sourceSHA)
	} else if !isNotFound(err) {
		return err
	}
	// S3 CopyObject has no portable destination If-None-Match condition.  Read
	// the already-verified source through the same private API and perform a
	// conditional PutObject instead; the 12 MiB package limit keeps this
	// bounded while preserving the no-overwrite invariant across instances.
	sourceReader, err := s.Open(ctx, sourceKey)
	if err != nil {
		return err
	}
	defer sourceReader.Close()
	copyOptions := minio.PutObjectOptions{
		ContentType:  "application/zip",
		UserMetadata: metadataForSHA(sourceSHA),
	}
	copyOptions.SetMatchETagExcept("*")
	_, err = s.client.PutObject(ctx, s.bucket, targetKey, sourceReader, source.Size, copyOptions)
	if err != nil {
		if target, statErr := s.Stat(ctx, targetKey); statErr == nil {
			if compareErr := compareObject(target, source.Size, sourceSHA, targetKey); compareErr == nil {
				return s.verifyStoredContent(ctx, targetKey, source.Size, sourceSHA)
			} else {
				return compareErr
			}
		}
		if isPreconditionFailure(err) {
			return fmt.Errorf("%w: %s", ErrTargetConflict, targetKey)
		}
		return mapMinIOError(err)
	}
	created, err := s.Stat(ctx, targetKey)
	if err != nil {
		return err
	}
	if err := compareObject(created, source.Size, sourceSHA, targetKey); err != nil {
		return err
	}
	return s.verifyStoredContent(ctx, targetKey, source.Size, sourceSHA)
}

func (s *MinIOStore) List(ctx context.Context, prefix, cursor string, limit int) ([]ObjectInfo, string, error) {
	if err := checkContext(ctx); err != nil {
		return nil, "", err
	}
	ctx = contextOrBackground(ctx)
	if s == nil || s.client == nil {
		return nil, "", fmt.Errorf("minio store is not initialized")
	}
	if prefix != "" {
		if err := validateObjectKey(strings.TrimSuffix(prefix, "/")); err != nil {
			return nil, "", err
		}
	}
	if limit <= 0 {
		limit = 100
	} else if limit > 1000 {
		// Keep a caller-controlled page size from turning List into an
		// unbounded allocation or an oversized S3 response.
		limit = 1000
	}
	items := make([]ObjectInfo, 0, limit)
	last := ""
	hasMore := false
	listCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	for object := range s.client.ListObjects(listCtx, s.bucket, minio.ListObjectsOptions{
		Prefix:       prefix,
		StartAfter:   cursor,
		Recursive:    true,
		WithMetadata: true,
		MaxKeys:      limit + 1,
	}) {
		if object.Err != nil {
			return nil, "", mapMinIOError(object.Err)
		}
		if object.Key <= cursor {
			continue
		}
		if len(items) >= limit {
			hasMore = true
			cancel()
			break
		}
		items = append(items, ObjectInfo{
			Key:          object.Key,
			Size:         object.Size,
			SHA256:       objectSHA(object.UserMetadata),
			LastModified: object.LastModified,
		})
		last = object.Key
	}
	if hasMore && last != "" {
		return items, last, nil
	}
	return items, "", nil
}

func (s *MinIOStore) Delete(ctx context.Context, key string) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	ctx = contextOrBackground(ctx)
	if s == nil || s.client == nil {
		return fmt.Errorf("minio store is not initialized")
	}
	if err := validateObjectKey(key); err != nil {
		return err
	}
	if _, err := s.Stat(ctx, key); err != nil {
		return err
	}
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return mapMinIOError(err)
	}
	return nil
}

type countingReader struct {
	io.Reader
	n      int64
	hasher hash.Hash
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.n += int64(n)
	return n, err
}

func metadataForSHA(value string) map[string]string {
	if value == "" {
		return nil
	}
	return map[string]string{"sha256": value}
}

func objectSHA(metadata minio.StringMap) string {
	for key, value := range metadata {
		if strings.EqualFold(key, "sha256") || strings.EqualFold(key, "x-amz-meta-sha256") {
			return value
		}
	}
	return ""
}

func compareObject(object *ObjectInfo, size int64, sha, key string) error {
	if object == nil {
		return fmt.Errorf("%w: %s", ErrNotFound, key)
	}
	if size >= 0 && object.Size != size {
		return fmt.Errorf("%w: %s size", ErrTargetConflict, key)
	}
	if sha != "" && object.SHA256 != "" {
		if !strings.EqualFold(object.SHA256, sha) {
			return fmt.Errorf("%w: %s sha256", ErrTargetConflict, key)
		}
	}
	return nil
}

func verifyObject(object *ObjectInfo, size int64, sha, key string) error {
	if object == nil {
		return fmt.Errorf("%w: %s", ErrNotFound, key)
	}
	if size >= 0 && object.Size != size {
		return fmt.Errorf("%w: %s size", ErrIntegrity, key)
	}
	if sha != "" && object.SHA256 != "" && !strings.EqualFold(object.SHA256, sha) {
		return fmt.Errorf("%w: %s sha256", ErrIntegrity, key)
	}
	return nil
}

// verifyStoredContent closes the metadata-only integrity gap: MinIO user
// metadata is useful for a fast filter, but immutable-object decisions must
// ultimately be based on the bytes returned by the object service.
func (s *MinIOStore) verifyStoredContent(ctx context.Context, key string, size int64, expectedSHA string) error {
	actual, err := s.hashStoredContent(ctx, key, size)
	if err != nil {
		return err
	}
	if expectedSHA != "" && !strings.EqualFold(actual, expectedSHA) {
		return fmt.Errorf("%w: %s sha256", ErrIntegrity, key)
	}
	return nil
}

func (s *MinIOStore) hashStoredContent(ctx context.Context, key string, size int64) (string, error) {
	if size < 0 || size == 1<<63-1 {
		return "", fmt.Errorf("%w: %s size", ErrIntegrity, key)
	}
	rc, err := s.Open(ctx, key)
	if err != nil {
		return "", err
	}
	defer rc.Close()
	hasher := sha256.New()
	count, err := io.Copy(hasher, io.LimitReader(rc, size+1))
	if err != nil {
		return "", mapMinIOError(err)
	}
	if count != size {
		return "", fmt.Errorf("%w: %s size", ErrIntegrity, key)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func isNotFound(err error) bool {
	return err != nil && (strings.Contains(err.Error(), ErrNotFound.Error()) || minio.ToErrorResponse(err).StatusCode == 404)
}

func isPreconditionFailure(err error) bool {
	if err == nil {
		return false
	}
	resp := minio.ToErrorResponse(err)
	return resp.StatusCode == 412 || resp.Code == "PreconditionFailed"
}

func mapMinIOError(err error) error {
	if err == nil {
		return nil
	}
	resp := minio.ToErrorResponse(err)
	if resp.StatusCode == 404 || resp.Code == "NoSuchKey" || resp.Code == "NoSuchObject" || resp.Code == "NotFound" {
		return fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	return err
}
