package storage

import (
	"fmt"

	"agenthub/backend/internal/conf"
	"agenthub/backend/pkg/qiniu"
)

// NewProvider 根据配置返回合适的存储实现。
//
// 当 type 为空时的自动判定逻辑：
//   - 七牛 AccessKey 与 SecretKey 均非空 → QiniuStorage
//   - 否则 → LocalStorage
func NewProvider(qiniuCfg *conf.QiniuConfig, storageCfg *conf.StorageConfig) (Provider, error) {
	typ := storageCfg.Type
	if typ == "" {
		if qiniuCfg.AccessKey != "" && qiniuCfg.SecretKey != "" {
			typ = "qiniu"
		} else {
			typ = "local"
		}
	}

	switch typ {
	case "qiniu":
		if qiniuCfg.AccessKey == "" || qiniuCfg.SecretKey == "" {
			return nil, fmt.Errorf("qiniu storage requires QINIU_ACCESS_KEY and QINIU_SECRET_KEY")
		}
		return NewQiniuStorage(qiniu.NewUploader(qiniuCfg)), nil
	case "local":
		dir := storageCfg.Local.Dir
		if dir == "" {
			dir = "./uploads"
		}
		prefix := storageCfg.Local.URLPrefix
		if prefix == "" {
			prefix = "http://localhost:8080/uploads"
		}
		return NewLocalStorage(dir, prefix)
	default:
		return nil, fmt.Errorf("unknown storage type: %s", typ)
	}
}
