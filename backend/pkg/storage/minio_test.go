package storage

import (
	"context"
	"errors"
	"net/http"
	"testing"

	minio "github.com/minio/minio-go/v7"
)

func TestMapMinIOErrorDistinguishesObjectAndBucketNotFound(t *testing.T) {
	objectErr := mapMinIOError(minio.ErrorResponse{Code: "NoSuchKey", StatusCode: http.StatusNotFound})
	if !errors.Is(objectErr, ErrNotFound) {
		t.Fatalf("object not-found error = %v, want ErrNotFound", objectErr)
	}

	bucketErr := mapMinIOError(minio.ErrorResponse{Code: "NoSuchBucket", StatusCode: http.StatusNotFound})
	if errors.Is(bucketErr, ErrNotFound) {
		t.Fatalf("bucket not-found error = %v, must not be treated as a missing avatar", bucketErr)
	}
}

func TestMapMinIOErrorMapsPermissionTimeoutAndPrecondition(t *testing.T) {
	permissionErr := mapMinIOError(minio.ErrorResponse{Code: "AccessDenied", StatusCode: http.StatusForbidden})
	if !errors.Is(permissionErr, ErrPermission) {
		t.Fatalf("permission error = %v, want ErrPermission", permissionErr)
	}

	timeoutErr := mapMinIOError(context.DeadlineExceeded)
	if !errors.Is(timeoutErr, ErrTimeout) {
		t.Fatalf("timeout error = %v, want ErrTimeout", timeoutErr)
	}

	if !isPreconditionFailure(minio.ErrorResponse{Code: "PreconditionFailed", StatusCode: http.StatusPreconditionFailed}) {
		t.Fatal("PreconditionFailed was not recognized as an immutable-key collision")
	}
}
