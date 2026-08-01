package impl

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"agenthub/backend/internal/model"
	"agenthub/backend/internal/service"
	"agenthub/backend/pkg/agentend_client"
)

func TestReviewTaskRequiresAwaitingReviewSession(t *testing.T) {
	agentCalls := make(chan struct{}, 1)
	client := newReviewAgentClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agentCalls <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	sessionDao := &reviewSessionDao{
		session: &model.Session{
			TaskID:    "task-1",
			SessionID: "session-1",
			Status:    sessionStatusCompleted,
		},
	}
	svc := NewTaskService(&reviewTaskDao{}, sessionDao, &reviewMessageDao{}, &reviewDiffSnapshotDao{}, client)

	_, err := svc.ReviewTask("task-1", service.ReviewTaskInput{
		SessionID: "session-1",
		Action:    "approve",
	})
	if err == nil {
		t.Fatal("ReviewTask error = nil, want conflict")
	}
	bizErr, ok := err.(*service.BizError)
	if !ok || bizErr.Code != 409 {
		t.Fatalf("ReviewTask error = %#v, want 409 BizError", err)
	}
	select {
	case <-agentCalls:
		t.Fatal("agent review was called for a non-awaiting session")
	default:
	}
	if len(sessionDao.statuses) != 0 {
		t.Fatalf("session status updates = %#v, want none", sessionDao.statuses)
	}
}

func TestReviewTaskForwardsAndMarksSessionRunning(t *testing.T) {
	requests := make(chan agentend_client.ReviewRequest, 1)
	client := newReviewAgentClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/agent/review" {
			http.Error(w, "unexpected review path", http.StatusNotFound)
			return
		}
		var got agentend_client.ReviewRequest
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			http.Error(w, "invalid review body", http.StatusBadRequest)
			return
		}
		requests <- got
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	sessionDao := &reviewSessionDao{
		session: &model.Session{
			TaskID:    "task-1",
			SessionID: "session-1",
			Status:    sessionStatusAwaitingReview,
		},
	}
	messageDao := &reviewMessageDao{
		latestPlanReview: &model.Message{
			MessageID: "message-1",
			TaskID:    "task-1",
			SessionID: "session-1",
			Content:   `type: plan_review` + "\n" + `json: {"review_key":"review-1","status":"pending"}`,
		},
	}
	svc := NewTaskService(&reviewTaskDao{}, sessionDao, messageDao, &reviewDiffSnapshotDao{}, client)

	result, err := svc.ReviewTask("task-1", service.ReviewTaskInput{
		SessionID: "session-1",
		Action:    "approve",
	})
	if err != nil {
		t.Fatalf("ReviewTask: %v", err)
	}
	if result["status"] != "ok" {
		t.Fatalf("ReviewTask result = %#v, want status ok", result)
	}
	got := <-requests
	if got.SessionID != "session-1" || got.Action != "approve" {
		t.Fatalf("agent review request = %+v", got)
	}
	if len(sessionDao.statuses) != 1 || sessionDao.statuses[0] != sessionStatusRunning {
		t.Fatalf("session status updates = %#v, want running", sessionDao.statuses)
	}
	if !strings.Contains(messageDao.updatedContent, `"status":"approved"`) {
		t.Fatalf("updated review block = %q, want approved status", messageDao.updatedContent)
	}
}

func TestMarkSessionCompletedAfterStreamPreservesAwaitingReview(t *testing.T) {
	sessionDao := &reviewSessionDao{
		session: &model.Session{
			TaskID:    "task-1",
			SessionID: "session-1",
			Status:    sessionStatusAwaitingReview,
		},
	}
	svc := NewTaskService(&reviewTaskDao{}, sessionDao, &reviewMessageDao{}, &reviewDiffSnapshotDao{}, nil)

	svc.markSessionCompletedAfterStream("task-1", "session-1")

	if len(sessionDao.statuses) != 0 {
		t.Fatalf("session status updates = %#v, want none", sessionDao.statuses)
	}
}

func TestMarkSessionCompletedAfterStreamCompletesRunningSession(t *testing.T) {
	sessionDao := &reviewSessionDao{
		session: &model.Session{
			TaskID:    "task-1",
			SessionID: "session-1",
			Status:    sessionStatusRunning,
		},
	}
	svc := NewTaskService(&reviewTaskDao{}, sessionDao, &reviewMessageDao{}, &reviewDiffSnapshotDao{}, nil)

	svc.markSessionCompletedAfterStream("task-1", "session-1")

	if len(sessionDao.statuses) != 1 || sessionDao.statuses[0] != sessionStatusCompleted {
		t.Fatalf("session status updates = %#v, want completed", sessionDao.statuses)
	}
}

func newReviewAgentClient(t *testing.T, handler http.Handler) *agentend_client.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatalf("parse test server port: %v", err)
	}
	host := parsed.Scheme + "://" + parsed.Hostname()
	if strings.Contains(parsed.Hostname(), ":") {
		host = parsed.Scheme + "://[" + parsed.Hostname() + "]"
	}
	return agentend_client.New(host, port)
}

type reviewTaskDao struct{}

func (dao *reviewTaskDao) GetByTaskID(string) (*model.Task, error) { return nil, nil }
func (dao *reviewTaskDao) FindRepoPathByTaskID(string) (string, error) {
	return "", nil
}
func (dao *reviewTaskDao) CreateTaskWithSessions(*model.Task, []model.Session, []model.SessionAgent) error {
	return nil
}
func (dao *reviewTaskDao) ListTasks(int, string) ([]model.Task, error) { return nil, nil }
func (dao *reviewTaskDao) ListSessionAgentsBySessionIDs([]string) ([]model.SessionAgent, error) {
	return nil, nil
}
func (dao *reviewTaskDao) DeleteTaskCascade(string) (bool, error) { return false, nil }
func (dao *reviewTaskDao) GetTaskAndSessionIDs(string) (*model.Task, []string, error) {
	return nil, nil, nil
}
func (dao *reviewTaskDao) PatchTask(string, map[string]interface{}) (bool, error) {
	return false, nil
}

type reviewSessionDao struct {
	session  *model.Session
	statuses []string
}

func (dao *reviewSessionDao) DeactivateSession(string) (bool, error) { return false, nil }
func (dao *reviewSessionDao) GetBySessionID(sessionID string) (*model.Session, error) {
	if dao.session != nil && dao.session.SessionID == sessionID {
		return dao.session, nil
	}
	return nil, nil
}
func (dao *reviewSessionDao) GetByTaskAndSessionID(taskID, sessionID string) (*model.Session, error) {
	if dao.session != nil && dao.session.TaskID == taskID && dao.session.SessionID == sessionID {
		return dao.session, nil
	}
	return nil, nil
}
func (dao *reviewSessionDao) ListByTaskID(string) ([]model.Session, error) { return nil, nil }
func (dao *reviewSessionDao) ListAll() ([]model.Session, error)            { return nil, nil }
func (dao *reviewSessionDao) FindPrimaryGroupSessionID(string) (string, error) {
	return "", nil
}
func (dao *reviewSessionDao) UpdateFields(string, map[string]interface{}) (bool, error) {
	return false, nil
}
func (dao *reviewSessionDao) UpdateSoul(string, string) (bool, error) { return false, nil }
func (dao *reviewSessionDao) UpdateStatusByTask(_, _, status string) error {
	dao.statuses = append(dao.statuses, status)
	if dao.session != nil {
		dao.session.Status = status
	}
	return nil
}

type reviewMessageDao struct {
	latestPlanReview *model.Message
	updatedContent   string
}

func (dao *reviewMessageDao) ListByTask(string, string, string, string, int, *uint64) ([]model.Message, error) {
	return nil, nil
}
func (dao *reviewMessageDao) CountBySessionID(string) (int64, error) { return 0, nil }
func (dao *reviewMessageDao) FindByMessageID(string) (*model.Message, error) {
	return nil, nil
}
func (dao *reviewMessageDao) CreateMessage(model.Message) error { return nil }
func (dao *reviewMessageDao) FindSessionIDByTaskMessage(string, string) (string, error) {
	return "", nil
}
func (dao *reviewMessageDao) FindMessageContent(string) (string, error) { return "", nil }
func (dao *reviewMessageDao) UpdateMessageContentAndSeq(string, string, string) error {
	return nil
}
func (dao *reviewMessageDao) UpdateMessageStatus(string, string) error { return nil }
func (dao *reviewMessageDao) FailStaleStreamingMessages() (int64, error) {
	return 0, nil
}
func (dao *reviewMessageDao) FindLatestCompletedAgentMessage(string, string) (*model.Message, error) {
	return nil, nil
}
func (dao *reviewMessageDao) ListGroupChatWindowMessages(string, string, *model.Message) ([]model.Message, error) {
	return nil, nil
}
func (dao *reviewMessageDao) FindLatestPlanReviewMessage(string, string) (*model.Message, error) {
	return dao.latestPlanReview, nil
}
func (dao *reviewMessageDao) UpdateContent(_ string, content string) error {
	dao.updatedContent = content
	return nil
}

type reviewDiffSnapshotDao struct{}

func (dao *reviewDiffSnapshotDao) GetBySnapshotID(string) (*model.DiffSnapshot, error) {
	return nil, nil
}
func (dao *reviewDiffSnapshotDao) CancelPendingBySession(string, string) error { return nil }
func (dao *reviewDiffSnapshotDao) Upsert(snapshot model.DiffSnapshot) (*model.DiffSnapshot, error) {
	return &snapshot, nil
}
func (dao *reviewDiffSnapshotDao) UpsertPending(string, string, string) error { return nil }
