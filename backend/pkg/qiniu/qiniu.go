package qiniu

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"agenthub/backend/internal/conf"

	"github.com/qiniu/go-sdk/v7/auth/qbox"
	"github.com/qiniu/go-sdk/v7/storage"
)

type Uploader struct {
	mac    *qbox.Mac
	bucket string
	domain string
	cfg    storage.Config
}

func NewUploader(cfg *conf.QiniuConfig) *Uploader {
	zone := zoneFromID(cfg.Region)
	return &Uploader{
		mac:    qbox.NewMac(cfg.AccessKey, cfg.SecretKey),
		bucket: cfg.Bucket,
		domain: cfg.Domain,
		cfg: storage.Config{
			Zone:          zone,
			UseHTTPS:      true,
			UseCdnDomains: true,
		},
	}
}

func (u *Uploader) token() string {
	putPolicy := storage.PutPolicy{Scope: u.bucket}
	return putPolicy.UploadToken(u.mac)
}

// UploadBytes 上传内存中的数据，并返回公开访问 URL。
func (u *Uploader) UploadBytes(ctx context.Context, key string, data []byte) (string, error) {
	uploader := storage.NewFormUploader(&u.cfg)
	ret := storage.PutRet{}
	reader := bytes.NewReader(data)
	if err := uploader.Put(ctx, &ret, u.token(), key, reader, int64(len(data)), nil); err != nil {
		return "", fmt.Errorf("qiniu upload: %w", err)
	}
	return u.publicURL(ret.Key), nil
}

// UploadReader 从 io.Reader 读取数据上传，并返回公开访问 URL。
func (u *Uploader) UploadReader(ctx context.Context, key string, reader io.Reader, size int64) (string, error) {
	uploader := storage.NewFormUploader(&u.cfg)
	ret := storage.PutRet{}
	if err := uploader.Put(ctx, &ret, u.token(), key, reader, size, nil); err != nil {
		return "", fmt.Errorf("qiniu upload: %w", err)
	}
	return u.publicURL(ret.Key), nil
}

func (u *Uploader) publicURL(key string) string {
	return storage.MakePublicURL(u.domain, key)
}

// PrivateURL 返回在 ttl 时长内有效的签名 URL。
func (u *Uploader) PrivateURL(key string, ttl time.Duration) string {
	deadline := time.Now().Add(ttl).Unix()
	return storage.MakePrivateURL(u.mac, u.domain, key, deadline)
}

func zoneFromID(id string) *storage.Zone {
	switch id {
	case "z1":
		return &storage.ZoneHuabei
	case "z2":
		return &storage.ZoneHuanan
	case "na0":
		return &storage.ZoneBeimei
	default:
		return &storage.ZoneHuadong
	}
}
