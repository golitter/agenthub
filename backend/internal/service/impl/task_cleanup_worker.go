package impl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"agenthub/backend/internal/dao"
	"agenthub/backend/internal/model"
	"agenthub/backend/pkg/agentend_client"
)

type taskCleanupClient interface {
	DestroySessionContext(context.Context, string) error
	CleanupByTaskContext(context.Context, string) error
	CleanupTaskBranchesContext(context.Context, string, string) error
}

type TaskCleanupWorker struct {
	dao          dao.TaskCleanupDao
	client       taskCleanupClient
	pollInterval time.Duration
	lease        time.Duration
	callTimeout  time.Duration
}

func NewTaskCleanupWorker(cleanupDao dao.TaskCleanupDao, client taskCleanupClient) *TaskCleanupWorker {
	return &TaskCleanupWorker{
		dao: cleanupDao, client: client, pollInterval: 2 * time.Second,
		lease: 2 * time.Minute, callTimeout: time.Minute,
	}
}

func (w *TaskCleanupWorker) Run(ctx context.Context) {
	if w == nil || w.dao == nil || w.client == nil {
		return
	}
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	for {
		for w.processOne(ctx) {
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *TaskCleanupWorker) processOne(ctx context.Context) bool {
	job, err := w.dao.ClaimDueTaskCleanup(ctx, time.Now(), w.lease)
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("claim task cleanup job failed", "error", err)
		}
		return false
	}
	if job == nil {
		return false
	}
	callCtx, cancel := context.WithTimeout(ctx, w.callTimeout)
	err = w.execute(callCtx, job)
	cancel()
	if err == nil {
		if completeErr := w.dao.CompleteTaskCleanup(ctx, job.ID, job.LeaseToken); completeErr != nil && ctx.Err() == nil {
			slog.Warn("complete task cleanup job failed", "job_id", job.ID, "task_id", job.TaskID, "error", completeErr)
		}
		return true
	}
	if ctx.Err() != nil {
		return false // lease expiry makes the interrupted job reclaimable after restart
	}
	nextRetry := time.Now().Add(taskCleanupBackoff(job.Attempts))
	if retryErr := w.dao.RetryTaskCleanup(ctx, job.ID, job.LeaseToken, err.Error(), nextRetry); retryErr != nil {
		slog.Warn("schedule task cleanup retry failed", "job_id", job.ID, "task_id", job.TaskID, "error", retryErr)
		return true
	}
	slog.Warn("task cleanup deferred", "job_id", job.ID, "task_id", job.TaskID, "attempt", job.Attempts, "next_retry_at", nextRetry, "error", err)
	return true
}

func (w *TaskCleanupWorker) execute(ctx context.Context, job *model.TaskCleanupJob) error {
	var sessionIDs []string
	if err := json.Unmarshal([]byte(job.SessionIDsJSON), &sessionIDs); err != nil {
		return fmt.Errorf("decode session snapshot: %w", err)
	}
	var cleanupErrors []error
	for _, sessionID := range sessionIDs {
		if err := w.client.DestroySessionContext(ctx, sessionID); !cleanupAlreadyGone(err) {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("destroy session %s: %w", sessionID, err))
		}
	}
	if err := w.client.CleanupByTaskContext(ctx, job.TaskID); !cleanupAlreadyGone(err) {
		cleanupErrors = append(cleanupErrors, fmt.Errorf("cleanup workspaces: %w", err))
	}
	if job.RepoPath != "" {
		if err := w.client.CleanupTaskBranchesContext(ctx, job.TaskID, job.RepoPath); !cleanupAlreadyGone(err) {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("cleanup branches: %w", err))
		}
	}
	return errors.Join(cleanupErrors...)
}

func cleanupAlreadyGone(err error) bool {
	if err == nil {
		return true
	}
	var statusErr *agentend_client.HTTPStatusError
	return errors.As(err, &statusErr) && statusErr.StatusCode == http.StatusNotFound
}

func taskCleanupBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 13 {
		attempt = 13
	}
	delay := time.Second * time.Duration(1<<uint(attempt-1))
	if delay > time.Hour {
		return time.Hour
	}
	return delay
}
