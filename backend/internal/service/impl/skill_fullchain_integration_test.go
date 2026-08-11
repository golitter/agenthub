package impl

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"agenthub/backend/internal/conf"
	"agenthub/backend/internal/dao"
	gormdao "agenthub/backend/internal/dao/gorm"
	"agenthub/backend/internal/model"
	"agenthub/backend/internal/service"
	"agenthub/backend/pkg/db"
	"agenthub/backend/pkg/package_store"
	"agenthub/backend/pkg/skill_upload_session"

	goredis "github.com/redis/go-redis/v9"
)

// ============================================================================
// Full-chain Skill storage integration tests against REAL storage services.
//
// These cover the two remaining acceptance items in design doc
// 10-skills-minio-storage-migration.md: the real external service full-chain
// (upload → confirm → import → remove → delete) plus the migration-period
// resilience guarantees (receipt idempotency, concurrent confirmation
// fencing, multi-instance restart, fault injection and observation-period
// shadow BLOB dual-read).
// MinIO, Redis and MySQL are real external dependencies when enabled; AgentEnd
// is a deterministic fake client and the multi-instance restart is simulated by
// constructing a fresh in-process SkillService.
//
// Gated behind SKILL_E2E=1 so the default `go test ./...` never touches a
// developer's MySQL/Redis/MinIO.  When enabled, connection details come from
// the SKILL_E2E_* environment variables (sensible non-secret defaults are
// provided for host/port/bucket):
//
//	SKILL_E2E=1
//	SKILL_E2E_MYSQL_HOST / _PORT / _USER / _PASSWORD / _DBNAME
//	SKILL_E2E_REDIS_HOST / _PORT / _PASSWORD / _DB
//	SKILL_E2E_MINIO_ENDPOINT / _BUCKET / _ACCESS_KEY / _SECRET_KEY / _USE_SSL
//
// Every test uses a unique skill name and session id and cleans up its own DB
// rows, MinIO objects and Redis keys via t.Cleanup, so the shared dev database
// is left untouched.
// ============================================================================

const e2eGate = "SKILL_E2E"

func e2eSkip(t *testing.T) {
	t.Helper()
	if os.Getenv(e2eGate) != "1" {
		t.Skipf("set %s=1 with real MySQL/Redis/MinIO env to run the full-chain integration tests", e2eGate)
	}
}

func e2eEnvOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func e2eEnvIntOr(key string, def int) int {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

var (
	e2eInitOnce sync.Once
	e2eInitErr  error
)

// e2eEnsureDB initialises the shared MySQL connection and runs AutoMigrate
// exactly once for the whole test binary.  The production GORM DAOs read
// through the db.GetDB() singleton, so a single init wires every DAO.
func e2eEnsureDB(t *testing.T) {
	t.Helper()
	e2eInitOnce.Do(func() {
		cfg := conf.MySQLConfig{
			Host:     e2eEnvOr("SKILL_E2E_MYSQL_HOST", "127.0.0.1"),
			Port:     e2eEnvIntOr("SKILL_E2E_MYSQL_PORT", 3307),
			User:     e2eEnvOr("SKILL_E2E_MYSQL_USER", "root"),
			Password: os.Getenv("SKILL_E2E_MYSQL_PASSWORD"),
			DBName:   e2eEnvOr("SKILL_E2E_MYSQL_DBNAME", "agenthub"),
			Charset:  "utf8mb4",
		}
		if err := db.Init(&cfg); err != nil {
			e2eInitErr = fmt.Errorf("mysql init: %w", err)
			return
		}
		if err := db.GetDB().AutoMigrate(
			&model.SkillHub{}, &model.AgentSkill{}, &model.SkillUploadReceipt{},
			&model.SkillOperationJob{}, &model.SkillAuditEvent{}, &model.Session{},
		); err != nil {
			e2eInitErr = fmt.Errorf("automigrate: %w", err)
		}
	})
	if e2eInitErr != nil {
		t.Fatalf("e2e database init failed: %v", e2eInitErr)
	}
}

var e2eCounter int32

// e2eToken returns a short, monotonic, lowercase token used to derive unique
// skill names and session ids per test invocation.
func e2eToken() string {
	n := atomic.AddInt32(&e2eCounter, 1)
	return strconv.FormatInt(time.Now().UnixNano(), 36) + strconv.FormatInt(int64(n), 36)
}

// e2eAgentClient is a deterministic AgentEnd stand-in.  It records every
// install/remove call by skill name so tests can assert exactly which side
// effects reached the agent, and supports injected failures for fault paths.
type e2eAgentClient struct {
	mu            sync.Mutex
	installs      map[string]int
	removes       map[string]int
	installErr    error
	installFailOn int32 // atomic; fail the Nth install call overall
	installCount  int32
}

func newE2EAgentClient() *e2eAgentClient {
	return &e2eAgentClient{installs: map[string]int{}, removes: map[string]int{}}
}

func (c *e2eAgentClient) InstallSkill(agentType, sessionID, skillName string, zipData []byte) error {
	n := atomic.AddInt32(&c.installCount, 1)
	if fail := atomic.LoadInt32(&c.installFailOn); fail > 0 && n == fail && c.installErr != nil {
		return c.installErr
	}
	c.mu.Lock()
	c.installs[skillName]++
	c.mu.Unlock()
	return nil
}

func (c *e2eAgentClient) InstallSkillWithContext(ctx context.Context, agentType, sessionID, skillName string, zipData []byte) error {
	return c.InstallSkill(agentType, sessionID, skillName, zipData)
}

func (c *e2eAgentClient) RemoveSkill(agentType, sessionID, skillName string) error {
	c.mu.Lock()
	c.removes[skillName]++
	c.mu.Unlock()
	return nil
}

func (c *e2eAgentClient) RemoveSkillWithContext(ctx context.Context, agentType, sessionID, skillName string) error {
	return c.RemoveSkill(agentType, sessionID, skillName)
}

func (c *e2eAgentClient) snapshot(skillName string) (installs, removes int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.installs[skillName], c.removes[skillName]
}

type e2eOptions struct {
	readPreference  string
	shadowWriteBlob bool
}

// e2eEnv bundles the real external service clients and the DAOs shared by one
// test.  Each test builds its own e2eEnv and registers per-resource cleanup.
type e2eEnv struct {
	dao        dao.SkillDao
	opDao      dao.SkillOperationDao
	store      *package_store.MinIOStore
	upload     *skill_upload_session.Store
	redis      *goredis.Client
	agent      *e2eAgentClient
	svc        *SkillService
	owner      string
	bucket     string
	tempRoot   string
}

func e2eMinIOStore(t *testing.T) *package_store.MinIOStore {
	t.Helper()
	store, err := package_store.NewMinIOStore(package_store.MinIOConfig{
		Endpoint:  e2eEnvOr("SKILL_E2E_MINIO_ENDPOINT", "127.0.0.1:19000"),
		Bucket:    e2eEnvOr("SKILL_E2E_MINIO_BUCKET", "e2e-skill-packages"),
		AccessKey: e2eEnvOr("SKILL_E2E_MINIO_ACCESS_KEY", "minioadmin"),
		SecretKey: e2eEnvOr("SKILL_E2E_MINIO_SECRET_KEY", "minioadmin"),
		UseSSL:    os.Getenv("SKILL_E2E_MINIO_USE_SSL") == "1",
	})
	if err != nil {
		t.Fatalf("minio store: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// Create the private bucket if missing, mirroring the app's startup path
	// (EnsureBucket is idempotent and the production minio-init task does this).
	if err := store.EnsureBucket(ctx); err != nil {
		t.Fatalf("minio ensure bucket: %v", err)
	}
	if err := store.Health(ctx); err != nil {
		t.Fatalf("minio health: %v", err)
	}
	return store
}

func e2eRedis(t *testing.T) *goredis.Client {
	t.Helper()
	addr := e2eEnvOr("SKILL_E2E_REDIS_HOST", "127.0.0.1") + ":" + strconv.Itoa(e2eEnvIntOr("SKILL_E2E_REDIS_PORT", 6380))
	cli := goredis.NewClient(&goredis.Options{
		Addr:         addr,
		Password:     os.Getenv("SKILL_E2E_REDIS_PASSWORD"),
		DB:           e2eEnvIntOr("SKILL_E2E_REDIS_DB", 0),
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := cli.Ping(ctx).Err(); err != nil {
		_ = cli.Close()
		t.Fatalf("redis ping: %v", err)
	}
	return cli
}

func setupE2E(t *testing.T) *e2eEnv {
	t.Helper()
	e2eSkip(t)
	e2eEnsureDB(t)
	store := e2eMinIOStore(t)
	redis := e2eRedis(t)
	upload := skill_upload_session.New(redis, skill_upload_session.Options{
		TTL:             5 * time.Minute,
		Lease:           30 * time.Second,
		ResultRetention: time.Hour,
	})
	env := &e2eEnv{
		dao:      gormdao.NewSkillDao(),
		opDao:    gormdao.NewSkillOperationDao(),
		store:    store,
		upload:   upload,
		redis:    redis,
		agent:    newE2EAgentClient(),
		owner:    "e2e-tester",
		bucket:   e2eEnvOr("SKILL_E2E_MINIO_BUCKET", "e2e-skill-packages"),
		tempRoot: t.TempDir(),
	}
	t.Cleanup(func() { _ = redis.Close() })
	env.svc = env.newService(t, e2eOptions{readPreference: "minio", shadowWriteBlob: true})
	return env
}

// newService builds a fresh SkillService against the shared real stores.  A
// second instance built this way mimics a different Backend process: it shares
// no in-memory state with env.svc, so any result it produces is proof that the
// durable state in MySQL/Redis/MinIO is sufficient.
func (e *e2eEnv) newService(t *testing.T, opts e2eOptions) *SkillService {
	svc := NewSkillService(e.dao, gormdao.NewSessionDao(), e.agent, e.store)
	svc.SetUploadSessionStore(e.upload)
	svc.SetOperationDao(e.opDao)
	svc.SetSkillTempDir(t.TempDir())
	svc.SetStorageReadOptions(opts.readPreference, opts.shadowWriteBlob)
	// MinIO is authoritative in these tests; legacy tmp_dir confirmation must
	// not provide a back door onto arbitrary incoming object keys.
	svc.SetLegacyTmpConfirmAllowed(false)
	return svc
}

func (e *e2eEnv) ownerCtx() context.Context {
	return service.WithSkillOwner(service.WithSkillAdmin(context.Background(), true), e.owner)
}

func (e *e2eEnv) uniqueName(base string) string {
	return "e2e-" + base + "-" + e2eToken()
}

func (e *e2eEnv) seedSession(t *testing.T, agentType string) string {
	t.Helper()
	sid := "e2e-session-" + e2eToken()
	sess := model.Session{
		SessionID: sid, TaskID: "e2e-task", AgentType: agentType,
		AgentName: "e2e-" + agentType, Status: "idle",
	}
	if err := db.GetDB().Create(&sess).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	return sid
}

// e2eSkillZip builds a minimal valid skill archive whose SKILL.md name matches
// the supplied name; the upload validator requires the ZIP base name to equal
// the SKILL.md name.
func e2eSkillZip(t *testing.T, name string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	entry, err := zw.Create("SKILL.md")
	if err != nil {
		t.Fatalf("create SKILL.md: %v", err)
	}
	if _, err := entry.Write([]byte("---\nname: " + name + "\ndescription: e2e skill\n---\n")); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func (e *e2eEnv) uploadSkill(t *testing.T, svc *SkillService, name string) string {
	t.Helper()
	res, err := svc.UploadSkill(e.ownerCtx(), name+".zip", e2eSkillZip(t, name))
	if err != nil {
		t.Fatalf("upload %s: %v", name, err)
	}
	if !res.Valid || res.UploadID == "" || res.SHA256 == "" {
		t.Fatalf("upload result for %s = %+v", name, res)
	}
	return res.UploadID
}

func e2eFormalKey(name, sha string) string { return "skills/" + name + "/" + sha + ".zip" }
func e2eStagingKey(uploadID string) string  { return "incoming/" + uploadID + ".zip" }

func e2eSHA256(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// register wires best-effort cleanup for a test's skill, session, upload
// receipts and MinIO objects.  Cleanup never follows links and only deletes
// rows/objects owned by this test's unique names.
func (e *e2eEnv) register(t *testing.T, skillName, sessionID string, uploadIDs ...string) {
	t.Cleanup(func() {
		ctx := context.Background()
		g := db.GetDB()
		// Operation jobs and agent-skill reservations reference the skill name.
		g.Where("skill_name = ?", skillName).Delete(&model.SkillOperationJob{})
		g.Where("skill_name = ?", skillName).Delete(&model.AgentSkill{})
		// Receipts and Redis sessions are keyed by upload id.
		for _, uid := range uploadIDs {
			g.Where("upload_id = ?", uid).Delete(&model.SkillUploadReceipt{})
			_ = e.redis.Del(ctx, skill_upload_session.Key(uid)).Err()
			_ = e.store.Delete(ctx, e2eStagingKey(uid))
		}
		// Formal object + hub row + audit events.
		if sk, _ := e.dao.GetSkillByName(skillName); sk != nil {
			if sk.ObjectKey != "" {
				_ = e.store.Delete(ctx, sk.ObjectKey)
			}
			g.Where("skill_id = ?", sk.ID).Delete(&model.SkillUploadReceipt{})
		}
		g.Where("name = ?", skillName).Delete(&model.SkillHub{})
		g.Where("skill_name = ?", skillName).Delete(&model.SkillAuditEvent{})
		if sessionID != "" {
			g.Where("session_id = ?", sessionID).Delete(&model.Session{})
		}
	})
}

// ----------------------------------------------------------------------------
// Happy path: upload → confirm → import → remove → delete against real MinIO.
// ----------------------------------------------------------------------------

func TestE2E_FullChainMinIO(t *testing.T) {
	env := setupE2E(t)
	name := env.uniqueName("fullchain")
	sessionID := env.seedSession(t, "claude-code")
	uploadID := env.uploadSkill(t, env.svc, name)
	env.register(t, name, sessionID, uploadID)

	ownerCtx := env.ownerCtx()
	staging := e2eStagingKey(uploadID)

	// Confirm: MinIO authority + shadow BLOB during the observation window.
	if _, err := env.svc.ConfirmSkill(ownerCtx, name, "client text must be ignored", 999, 999, "minio:"+staging); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	sk, err := env.dao.GetSkillByName(name)
	if err != nil || sk == nil {
		t.Fatalf("skill missing after confirm: %v", err)
	}
	if sk.StorageType != model.SkillStorageMinIO || sk.ObjectKey == "" || sk.SHA256 == "" {
		t.Fatalf("skill metadata after confirm = %+v", sk)
	}
	if sk.Status != model.SkillStatusReady {
		t.Fatalf("skill status = %q, want ready", sk.Status)
	}
	if sk.UploadedBy != env.owner {
		t.Fatalf("UploadedBy = %q, want %q", sk.UploadedBy, env.owner)
	}
	if _, err := env.dao.GetSkillUploadReceipt(uploadID); err != nil {
		t.Fatalf("receipt missing after confirm: %v", err)
	}
	if _, err := env.store.Stat(context.Background(), sk.ObjectKey); err != nil {
		t.Fatalf("formal object missing: %v", err)
	}
	if _, err := env.store.Stat(context.Background(), staging); err == nil {
		t.Fatal("staging object was not cleaned up after confirm")
	}

	// Import into a session: package is read back from MinIO and re-verified.
	if _, err := env.svc.ImportSkill(ownerCtx, name, sessionID); err != nil {
		t.Fatalf("import: %v", err)
	}
	if installs, removes := env.agent.snapshot(name); installs != 1 || removes != 0 {
		t.Fatalf("agent calls after import = install:%d remove:%d", installs, removes)
	}
	if has, _ := env.dao.HasAgentSkill(sessionID, name); !has {
		t.Fatal("AgentSkill missing after import")
	}

	// Remove from the session.
	if _, err := env.svc.RemoveSkill(ownerCtx, name, sessionID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if installs, removes := env.agent.snapshot(name); installs != 1 || removes != 1 {
		t.Fatalf("agent calls after remove = install:%d remove:%d", installs, removes)
	}
	if has, _ := env.dao.HasAgentSkill(sessionID, name); has {
		t.Fatal("AgentSkill still present after remove")
	}

	// Delete the skill: object is removed synchronously, hub row cascaded.
	objectKey := sk.ObjectKey
	if err := env.svc.DeleteSkill(ownerCtx, name); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got, _ := env.dao.GetSkillByName(name); got != nil {
		t.Fatal("skill row still present after delete")
	}
	if _, err := env.store.Stat(context.Background(), objectKey); err == nil {
		t.Fatal("formal object still present after delete")
	}
}

// ----------------------------------------------------------------------------
// Confirm is idempotent on upload_id, and the durable receipt lets a retry
// succeed even after the Redis upload session is lost (process crash after
// the MySQL commit).  Design doc §7.2 and §13.2.
// ----------------------------------------------------------------------------

func TestE2E_ConfirmIdempotent_ReceiptSurvivesRedisLoss(t *testing.T) {
	env := setupE2E(t)
	name := env.uniqueName("idem")
	sessionID := env.seedSession(t, "claude-code")
	uploadID := env.uploadSkill(t, env.svc, name)
	env.register(t, name, sessionID, uploadID)

	ownerCtx := env.ownerCtx()
	staging := e2eStagingKey(uploadID)

	if _, err := env.svc.ConfirmSkill(ownerCtx, name, "", 0, 0, "minio:"+staging); err != nil {
		t.Fatalf("first confirm: %v", err)
	}
	first, _ := env.dao.GetSkillByName(name)
	if first == nil {
		t.Fatal("skill missing after first confirm")
	}

	// Re-confirming the same upload_id must be a no-op: still exactly one row.
	if _, err := env.svc.ConfirmSkill(ownerCtx, name, "", 0, 0, "minio:"+staging); err != nil {
		t.Fatalf("idempotent re-confirm: %v", err)
	}
	if count, _ := env.dao.CountByName(name); count != 1 {
		t.Fatalf("after re-confirm, skill count = %d, want 1", count)
	}

	// Simulate Redis session loss after the commit.  The retry must still
	// resolve through the durable MySQL receipt, not fail as "expired".
	ctx := context.Background()
	if err := env.redis.Del(ctx, skill_upload_session.Key(uploadID)).Err(); err != nil {
		t.Fatalf("delete redis session: %v", err)
	}
	// Staging object is already gone after the first confirm; the receipt path
	// does not require it.
	if _, err := env.svc.ConfirmSkill(ownerCtx, name, "", 0, 0, "minio:"+staging); err != nil {
		t.Fatalf("confirm after redis loss: %v", err)
	}
	if count, _ := env.dao.CountByName(name); count != 1 {
		t.Fatalf("after redis-loss confirm, skill count = %d, want 1", count)
	}
	if receipt, _ := env.dao.GetSkillUploadReceipt(uploadID); receipt == nil {
		t.Fatal("receipt missing after redis-loss confirm")
	}
}

// ----------------------------------------------------------------------------
// Two concurrent confirmations of the same upload_id produce exactly one skill
// row and never delete the content-addressed formal object.  Design doc §7.2
// and §13.2.
// ----------------------------------------------------------------------------

func TestE2E_ConcurrentConfirmSameUploadID(t *testing.T) {
	env := setupE2E(t)
	name := env.uniqueName("conc")
	sessionID := env.seedSession(t, "claude-code")
	uploadID := env.uploadSkill(t, env.svc, name)
	env.register(t, name, sessionID, uploadID)

	ownerCtx := env.ownerCtx()
	staging := e2eStagingKey(uploadID)

	var wg sync.WaitGroup
	var errs [2]error
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = env.svc.ConfirmSkill(ownerCtx, name, "", 0, 0, "minio:"+staging)
		}(i)
	}
	wg.Wait()

	// One call wins and creates the skill; the other either returns 202
	// (lease held) or resolves idempotently to the same row.  Either way there
	// must be exactly one row and no error that deleted the formal object.
	for i, err := range errs {
		if err == nil {
			continue
		}
		var biz *service.BizError
		if !errors.As(err, &biz) {
			t.Fatalf("concurrent confirm[%d] returned non-biz error: %v", i, err)
		}
		// 202 (already in progress) is the only acceptable non-success outcome.
		if biz.Code != 202 {
			t.Fatalf("concurrent confirm[%d] code = %d, want 200 or 202: %v", i, biz.Code, err)
		}
	}
	if count, _ := env.dao.CountByName(name); count != 1 {
		t.Fatalf("concurrent confirm produced %d skill rows, want 1", count)
	}
	sk, _ := env.dao.GetSkillByName(name)
	if sk == nil || sk.SHA256 == "" {
		t.Fatalf("skill missing or has no sha after concurrent confirm: %+v", sk)
	}
	if _, err := env.store.Stat(context.Background(), e2eFormalKey(name, sk.SHA256)); err != nil {
		t.Fatalf("formal object missing after concurrent confirm: %v", err)
	}
}

// ----------------------------------------------------------------------------
// Confirmation on one instance, import/remove on a freshly-built instance that
// shares only the durable stores — proving no in-memory state is required and
// a restarted Backend can complete the lifecycle.  Design doc §13.2.
// ----------------------------------------------------------------------------

func TestE2E_RestartAcrossInstances(t *testing.T) {
	env := setupE2E(t)
	name := env.uniqueName("restart")
	sessionID := env.seedSession(t, "opencode")
	uploadID := env.uploadSkill(t, env.svc, name)
	env.register(t, name, sessionID, uploadID)

	// Instance A confirms.
	if _, err := env.svc.ConfirmSkill(env.ownerCtx(), name, "", 0, 0, "minio:"+e2eStagingKey(uploadID)); err != nil {
		t.Fatalf("confirm on A: %v", err)
	}

	// Instance B is a brand-new SkillService with no shared in-memory state.
	instanceB := env.newService(t, e2eOptions{readPreference: "minio", shadowWriteBlob: true})

	if _, err := instanceB.ImportSkill(env.ownerCtx(), name, sessionID); err != nil {
		t.Fatalf("import on restarted instance B: %v", err)
	}
	if installs, _ := env.agent.snapshot(name); installs != 1 {
		t.Fatalf("agent installs after restart import = %d, want 1", installs)
	}
	if has, _ := env.dao.HasAgentSkill(sessionID, name); !has {
		t.Fatal("AgentSkill missing after restart import")
	}

	// Instance B can also remove — the AgentSkill reference is durable.
	if _, err := instanceB.RemoveSkill(env.ownerCtx(), name, sessionID); err != nil {
		t.Fatalf("remove on restarted instance B: %v", err)
	}
	if has, _ := env.dao.HasAgentSkill(sessionID, name); has {
		t.Fatal("AgentSkill still present after restart remove")
	}
}

// ----------------------------------------------------------------------------
// Fault injection: if the authoritative MinIO object disappears between
// confirm and import, import must fail closed (no AgentEnd install) rather
// than silently fall back to a stale copy.  Design doc §9.1 and §13.2.
// ----------------------------------------------------------------------------

func TestE2E_FaultImportMissingObject(t *testing.T) {
	env := setupE2E(t)
	name := env.uniqueName("fault")
	sessionID := env.seedSession(t, "codex")
	uploadID := env.uploadSkill(t, env.svc, name)
	env.register(t, name, sessionID, uploadID)

	if _, err := env.svc.ConfirmSkill(env.ownerCtx(), name, "", 0, 0, "minio:"+e2eStagingKey(uploadID)); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	sk, _ := env.dao.GetSkillByName(name)
	if sk == nil {
		t.Fatal("skill missing after confirm")
	}

	// Tamper: delete the authoritative object out from under the Hub row.
	if err := env.store.Delete(context.Background(), sk.ObjectKey); err != nil {
		t.Fatalf("delete formal object for fault injection: %v", err)
	}

	_, err := env.svc.ImportSkill(env.ownerCtx(), name, sessionID)
	if err == nil {
		t.Fatal("import succeeded against a missing authoritative object; expected failure")
	}
	if installs, _ := env.agent.snapshot(name); installs != 0 {
		t.Fatalf("agent was called %d time(s) for a skill whose package read failed", installs)
	}
	// The Hub row must not remain in a usable ready state after the integrity
	// failure; either it is marked storage_error or the import left an
	// installing/sync_error reservation that blocks re-import.
	after, _ := env.dao.GetSkillByName(name)
	if after != nil && after.Status == model.SkillStatusReady {
		t.Fatalf("skill remained ready after authoritative object went missing")
	}
}

// ----------------------------------------------------------------------------
// Observation-period dual read: with shadow_write_blob enabled, confirm stores
// both the MinIO object and a MySQL shadow BLOB with identical SHA-256.  Both
// read_preference=minio and read_preference=db can then import the package —
// the rollback-dual-read guarantee.  Design doc §9.1, §10.1, §10.2.
// ----------------------------------------------------------------------------

func TestE2E_ObservationShadowBlob_DualReadRollback(t *testing.T) {
	env := setupE2E(t)
	name := env.uniqueName("shadow")
	uploadID := env.uploadSkill(t, env.svc, name)
	env.register(t, name, "", uploadID)

	if _, err := env.svc.ConfirmSkill(env.ownerCtx(), name, "", 0, 0, "minio:"+e2eStagingKey(uploadID)); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	sk, _ := env.dao.GetSkillByName(name)
	if sk == nil {
		t.Fatal("skill missing after confirm")
	}

	// Shadow BLOB was written alongside the MinIO object.
	blob, err := env.dao.GetSkillContent(name)
	if err != nil {
		t.Fatalf("read shadow blob: %v", err)
	}
	if len(blob) == 0 {
		t.Fatal("observation-period shadow BLOB is empty")
	}
	if e2eSHA256(blob) != sk.SHA256 {
		t.Fatalf("shadow blob sha256 = %s, want %s", e2eSHA256(blob), sk.SHA256)
	}

	// read_preference=minio imports the authoritative object.
	sessionMinIO := env.seedSession(t, "claude-code")
	minIOSvc := env.newService(t, e2eOptions{readPreference: "minio", shadowWriteBlob: true})
	if _, err := minIOSvc.ImportSkill(env.ownerCtx(), name, sessionMinIO); err != nil {
		t.Fatalf("import via minio: %v", err)
	}
	if _, err := minIOSvc.RemoveSkill(env.ownerCtx(), name, sessionMinIO); err != nil {
		t.Fatalf("remove minio import: %v", err)
	}

	// read_preference=db imports the shadow BLOB — proving the rollback path.
	sessionDB := env.seedSession(t, "opencode")
	dbSvc := env.newService(t, e2eOptions{readPreference: "db", shadowWriteBlob: true})
	if _, err := dbSvc.ImportSkill(env.ownerCtx(), name, sessionDB); err != nil {
		t.Fatalf("import via db shadow blob: %v", err)
	}
	if installs, _ := env.agent.snapshot(name); installs != 2 {
		t.Fatalf("agent installs after dual read = %d, want 2", installs)
	}

	// register() cleans the two extra sessions created here.
	env.register(t, name, sessionMinIO, uploadID)
	env.register(t, name, sessionDB)
}
