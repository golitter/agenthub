package storage

import (
	"context"
	"io"
)

// Provider 是供 Service 层上传文件使用的存储抽象。
type Provider interface {
	// UploadBytes 上传内存中的数据，并返回公开访问 URL。
	UploadBytes(ctx context.Context, key string, data []byte) (string, error)
	// UploadReader 从 reader 读取数据上传，并返回公开访问 URL。
	UploadReader(ctx context.Context, key string, reader io.Reader, size int64) (string, error)
}
