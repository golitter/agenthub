package gormdao

import (
	"bytes"
	"os"
	"strconv"
	"testing"
	"time"

	"agenthub/backend/internal/conf"
	"agenthub/backend/internal/model"
	"agenthub/backend/pkg/db"
)

// Run explicitly with MYSQL_INTEGRATION=1 and a disposable MySQL database.
// The test verifies the expand-only tables and, importantly, that ordinary
// metadata projections do not load the Content BLOB.
func TestSkillStorageMySQLIntegration(t *testing.T) {
	if os.Getenv("MYSQL_INTEGRATION") != "1" {
		t.Skip("set MYSQL_INTEGRATION=1 to run against a disposable MySQL service")
	}
	port, _ := strconv.Atoi(os.Getenv("MYSQL_INTEGRATION_PORT"))
	if port == 0 {
		port = 3306
	}
	cfg := conf.MySQLConfig{
		Host: os.Getenv("MYSQL_INTEGRATION_HOST"), User: os.Getenv("MYSQL_INTEGRATION_USER"),
		Password: os.Getenv("MYSQL_INTEGRATION_PASSWORD"), DBName: os.Getenv("MYSQL_INTEGRATION_DBNAME"),
		Charset: "utf8mb4", Port: port,
	}
	if cfg.Host == "" || cfg.User == "" || cfg.DBName == "" {
		t.Fatal("MYSQL_INTEGRATION_HOST, _USER and _DBNAME are required")
	}
	if err := db.Init(&cfg); err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.GetDB().AutoMigrate(&model.SkillHub{}, &model.AgentSkill{}, &model.SkillUploadReceipt{}, &model.SkillOperationJob{}, &model.SkillAuditEvent{}); err != nil {
		t.Fatal(err)
	}
	dao := NewSkillDao()
	name := "integration-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	content := []byte("blob-content")
	if err := db.GetDB().Create(&model.SkillHub{Name: name, Builtin: false, Content: content, SHA256: "hash", PackageSize: int64(len(content)), StorageType: model.SkillStorageDB, Status: model.SkillStatusReady}).Error; err != nil {
		t.Fatal(err)
	}
	defer db.GetDB().Where("name = ?", name).Delete(&model.SkillHub{})
	listed, err := dao.GetSkillByName(name)
	if err != nil || listed == nil {
		t.Fatalf("metadata lookup = %+v, err = %v", listed, err)
	}
	if len(listed.Content) != 0 {
		t.Fatal("metadata projection unexpectedly loaded Content")
	}
	got, err := dao.GetSkillContent(name)
	if err != nil || string(got) != string(content) {
		t.Fatalf("content lookup = %q, err = %v", got, err)
	}
	// Bounded BLOB reads must scan the LEFT() prefix into []byte, not a single
	// uint8 element.  Guards against the regression where the raw prefix
	// projection made database/sql report "converting []uint8 to uint8".
	if limited, err := dao.GetSkillContentLimited(name, int64(len(content)+1)); err != nil || !bytes.Equal(limited, content) {
		t.Fatalf("GetSkillContentLimited = %d bytes, err = %v", len(limited), err)
	}
	if limited, err := dao.GetSkillContentByIDLimited(listed.ID, int64(len(content)+1)); err != nil || !bytes.Equal(limited, content) {
		t.Fatalf("GetSkillContentByIDLimited = %d bytes, err = %v", len(limited), err)
	}
	// A limit below the payload size must fail closed: the bounded read fetches
	// maxBytes+1 bytes precisely so it can detect overflow rather than return a
	// truncated prefix that looks valid.
	if _, err := dao.GetSkillContentLimited(name, 1); err == nil {
		t.Fatal("GetSkillContentLimited(1) unexpectedly succeeded below payload size")
	}
	receipt := model.SkillUploadReceipt{UploadID: "integration-" + strconv.FormatInt(time.Now().UnixNano(), 10), SkillID: listed.ID, SHA256: "hash", CreatedAt: time.Now()}
	if err := dao.CreateSkillUploadReceipt(receipt); err != nil {
		t.Fatal(err)
	}
	defer db.GetDB().Where("upload_id = ?", receipt.UploadID).Delete(&model.SkillUploadReceipt{})
	if found, err := dao.GetSkillUploadReceipt(receipt.UploadID); err != nil || found == nil {
		t.Fatalf("receipt lookup = %+v, err = %v", found, err)
	}
}
