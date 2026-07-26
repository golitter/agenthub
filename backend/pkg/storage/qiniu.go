package storage

import (
	"context"
	"io"

	"agenthub/backend/pkg/qiniu"
)

// QiniuStorage 将已有的 qiniu.Uploader 适配为 Provider 接口。
type QiniuStorage struct {
	uploader *qiniu.Uploader
}

// NewQiniuStorage 把一个 qiniu.Uploader 包装成 Provider。
func NewQiniuStorage(uploader *qiniu.Uploader) *QiniuStorage {
	return &QiniuStorage{uploader: uploader}
}

func (s *QiniuStorage) UploadBytes(ctx context.Context, key string, data []byte) (string, error) {
	return s.uploader.UploadBytes(ctx, key, data)
}

func (s *QiniuStorage) UploadReader(ctx context.Context, key string, reader io.Reader, size int64) (string, error) {
	return s.uploader.UploadReader(ctx, key, reader, size)
}
