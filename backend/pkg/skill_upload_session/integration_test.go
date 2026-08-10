package skill_upload_session

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// Run explicitly with REDIS_INTEGRATION=1.  This exercises the Lua fencing
// scripts against Redis itself, including concurrent confirmation attempts.
func TestRedisUploadSessionIntegration(t *testing.T) {
	if os.Getenv("REDIS_INTEGRATION") != "1" {
		t.Skip("set REDIS_INTEGRATION=1 to run against a real Redis service")
	}
	addr := os.Getenv("REDIS_INTEGRATION_ADDR")
	if addr == "" {
		addr = "127.0.0.1:6379"
	}
	db, _ := strconv.Atoi(os.Getenv("REDIS_INTEGRATION_DB"))
	client := goredis.NewClient(&goredis.Options{Addr: addr, Password: os.Getenv("REDIS_INTEGRATION_PASSWORD"), DB: db})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	store := New(client, Options{TTL: 10 * time.Minute, Lease: time.Minute, ResultRetention: time.Hour})
	id := "integration-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	defer client.Del(context.Background(), Key(id))
	if err := store.Create(ctx, Session{UploadID: id, OwnerID: "owner-a", ObjectKey: "incoming/" + id + ".zip", Name: "demo"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BeginConfirm(ctx, id, "owner-b", "bad-token", time.Now()); err != ErrOwnerMismatch {
		t.Fatalf("wrong owner error = %v", err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := store.BeginConfirm(ctx, id, "owner-a", "token-"+strconv.Itoa(i), time.Now())
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	acquired, running := 0, 0
	for err := range results {
		if err == nil {
			acquired++
		} else if err == ErrConfirmRunning {
			running++
		} else if !strings.Contains(err.Error(), "already running") {
			t.Fatalf("concurrent begin error = %v", err)
		}
	}
	if acquired != 1 || running != 1 {
		t.Fatalf("concurrent fencing results acquired=%d running=%d", acquired, running)
	}
}
