package impl

import (
	"context"
	"errors"
	"testing"
	"time"

	"agenthub/backend/internal/model"
)

type fakeTaskCleanupDao struct {
	job       *model.TaskCleanupJob
	completed bool
	retried   bool
}

func (dao *fakeTaskCleanupDao) ClaimDueTaskCleanup(context.Context, time.Time, time.Duration) (*model.TaskCleanupJob, error) {
	job := dao.job
	dao.job = nil
	return job, nil
}
func (dao *fakeTaskCleanupDao) CompleteTaskCleanup(context.Context, uint64, string) error {
	dao.completed = true
	return nil
}
func (dao *fakeTaskCleanupDao) RetryTaskCleanup(context.Context, uint64, string, string, time.Time) error {
	dao.retried = true
	return nil
}

type fakeTaskCleanupClient struct {
	destroyed []string
	taskID    string
	repoPath  string
	err       error
}

type durableDeleteTaskDao struct {
	reviewTaskDao
	ctx    context.Context
	taskID string
	action string
}

func (dao *durableDeleteTaskDao) DeleteTaskCascadeWithCleanup(ctx context.Context, taskID, action string) (bool, error) {
	dao.ctx, dao.taskID, dao.action = ctx, taskID, action
	return true, nil
}

func (client *fakeTaskCleanupClient) DestroySessionContext(_ context.Context, sessionID string) error {
	client.destroyed = append(client.destroyed, sessionID)
	return client.err
}
func (client *fakeTaskCleanupClient) CleanupByTaskContext(_ context.Context, taskID string) error {
	client.taskID = taskID
	return client.err
}
func (client *fakeTaskCleanupClient) CleanupTaskBranchesContext(_ context.Context, taskID, repoPath string) error {
	client.taskID, client.repoPath = taskID, repoPath
	return client.err
}

func TestTaskCleanupWorkerCompletesAllCleanupSteps(t *testing.T) {
	dao := &fakeTaskCleanupDao{job: &model.TaskCleanupJob{
		ID: 1, TaskID: "task-1", RepoPath: "/repo", SessionIDsJSON: `["session-1","session-2"]`,
		LeaseToken: "lease", Attempts: 1,
	}}
	client := &fakeTaskCleanupClient{}
	worker := NewTaskCleanupWorker(dao, client)
	worker.processOne(context.Background())

	if !dao.completed || dao.retried {
		t.Fatalf("completed/retried = %v/%v, want true/false", dao.completed, dao.retried)
	}
	if len(client.destroyed) != 2 || client.taskID != "task-1" || client.repoPath != "/repo" {
		t.Fatalf("cleanup calls = %#v, task=%q repo=%q", client.destroyed, client.taskID, client.repoPath)
	}
}

func TestTaskCleanupWorkerRetriesTransientFailure(t *testing.T) {
	dao := &fakeTaskCleanupDao{job: &model.TaskCleanupJob{
		ID: 1, TaskID: "task-1", SessionIDsJSON: `[]`, LeaseToken: "lease", Attempts: 1,
	}}
	worker := NewTaskCleanupWorker(dao, &fakeTaskCleanupClient{err: errors.New("agentend unavailable")})
	worker.processOne(context.Background())

	if dao.completed || !dao.retried {
		t.Fatalf("completed/retried = %v/%v, want false/true", dao.completed, dao.retried)
	}
}

func TestTaskCleanupBackoffIsCapped(t *testing.T) {
	if got := taskCleanupBackoff(100); got != time.Hour {
		t.Fatalf("backoff = %s, want 1h", got)
	}
}

func TestDeleteTaskUsesDurableDAOAndPropagatesContext(t *testing.T) {
	taskDao := &durableDeleteTaskDao{}
	service := NewTaskService(taskDao, &reviewSessionDao{}, &reviewMessageDao{}, &reviewDiffSnapshotDao{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := service.DeleteTask(ctx, "task-1"); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}
	if taskDao.ctx != ctx || taskDao.taskID != "task-1" || taskDao.action != "delete" {
		t.Fatalf("durable delete received ctx/task/action = %v/%q/%q", taskDao.ctx, taskDao.taskID, taskDao.action)
	}
}
