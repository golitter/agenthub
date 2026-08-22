package impl

import (
	"strings"
	"testing"

	"agenthub/backend/internal/service"
)

func TestNormalizeCreateTaskInputTrimsAndRequiresAgent(t *testing.T) {
	input, err := normalizeCreateTaskInput(service.CreateTaskInput{
		Title:    "  Build it  ",
		RepoPath: "  /repo  ",
		Agents:   []service.AgentConfig{{Type: " codex ", Name: " Codex "}},
	})
	if err != nil {
		t.Fatalf("normalizeCreateTaskInput: %v", err)
	}
	if input.Title != "Build it" || input.RepoPath != "/repo" {
		t.Fatalf("unexpected normalized task input: %+v", input)
	}
	if input.Agents[0].Type != "codex" || input.Agents[0].Name != "Codex" {
		t.Fatalf("unexpected normalized agent: %+v", input.Agents[0])
	}

	if _, err := normalizeCreateTaskInput(service.CreateTaskInput{Title: "   "}); err == nil {
		t.Fatal("blank title accepted")
	}
	if _, err := normalizeCreateTaskInput(service.CreateTaskInput{Title: "Task"}); err == nil {
		t.Fatal("task without agents accepted")
	}
	if _, err := normalizeCreateTaskInput(service.CreateTaskInput{
		Title:    "Task",
		RepoPath: strings.Repeat("x", maxRepoPathLen+1),
		Agents:   []service.AgentConfig{{Type: "codex"}},
	}); err == nil {
		t.Fatal("too long repo_path accepted")
	}
	if _, err := normalizeCreateTaskInput(service.CreateTaskInput{
		Title:  "Task",
		Agents: []service.AgentConfig{{Type: strings.Repeat("x", maxAgentTypeLen+1)}},
	}); err == nil {
		t.Fatal("too long agent type accepted")
	}
	if _, err := normalizeCreateTaskInput(service.CreateTaskInput{
		Title:  "Task",
		Agents: []service.AgentConfig{{Type: "unknown-agent"}},
	}); err == nil {
		t.Fatal("invalid agent type accepted")
	}
	if _, err := normalizeCreateTaskInput(service.CreateTaskInput{
		Title:  "Task",
		Agents: []service.AgentConfig{{Type: "codex", Name: strings.Repeat("x", maxAgentNameLen+1)}},
	}); err == nil {
		t.Fatal("too long agent name accepted")
	}
}

func TestNormalizeTaskListOptions(t *testing.T) {
	options, err := normalizeTaskListOptions(service.TaskListOptions{Before: " task-1 "})
	if err != nil {
		t.Fatalf("normalizeTaskListOptions: %v", err)
	}
	if options.Limit != defaultTaskListLimit || options.Before != "task-1" {
		t.Fatalf("options = %+v, want default limit and trimmed cursor", options)
	}

	if _, err := normalizeTaskListOptions(service.TaskListOptions{Limit: -1}); err == nil {
		t.Fatal("negative limit accepted")
	}
	if _, err := normalizeTaskListOptions(service.TaskListOptions{Limit: maxTaskListLimit + 1}); err == nil {
		t.Fatal("oversized limit accepted")
	}
}

func TestNormalizeTaskID(t *testing.T) {
	taskID, err := normalizeTaskID(" task-1 ")
	if err != nil {
		t.Fatalf("normalizeTaskID: %v", err)
	}
	if taskID != "task-1" {
		t.Fatalf("taskID = %q, want task-1", taskID)
	}

	if _, err := normalizeTaskID("   "); err == nil {
		t.Fatal("blank task_id accepted")
	}
	if _, err := normalizeTaskID(strings.Repeat("t", maxTaskIDLen+1)); err == nil {
		t.Fatal("too long task_id accepted")
	}
}

func TestNormalizeRunTaskInput(t *testing.T) {
	input, err := normalizeRunTaskInput(service.RunTaskInput{
		Message:   " hello ",
		SessionID: " session-1 ",
		AgentType: " codex ",
		Cwd:       " /repo ",
	})
	if err != nil {
		t.Fatalf("normalizeRunTaskInput: %v", err)
	}
	if input.Message != "hello" || input.SessionID != "session-1" || input.AgentType != "codex" || input.Cwd != "/repo" {
		t.Fatalf("unexpected normalized input: %+v", input)
	}

	if _, err := normalizeRunTaskInput(service.RunTaskInput{Message: "   ", SessionID: "s"}); err == nil {
		t.Fatal("blank message accepted")
	}
	if _, err := normalizeRunTaskInput(service.RunTaskInput{Message: "hello", SessionID: "   "}); err == nil {
		t.Fatal("blank session_id accepted")
	}
	if _, err := normalizeRunTaskInput(service.RunTaskInput{Message: "hello", SessionID: "s", AgentType: "invalid"}); err == nil {
		t.Fatal("invalid agent type accepted")
	}
	if _, err := normalizeRunTaskInput(service.RunTaskInput{Message: strings.Repeat("x", maxRunMessageLen+1), SessionID: "s"}); err == nil {
		t.Fatal("too long message accepted")
	}
	if _, err := normalizeRunTaskInput(service.RunTaskInput{Message: "hello", SessionID: "s", Cwd: "bad\x00cwd"}); err == nil {
		t.Fatal("cwd containing NUL accepted")
	}
}

func TestNormalizeRunTaskInputValidatesChildRunIdentity(t *testing.T) {
	runID := "33333333-3333-4333-8333-333333333333"
	root := "11111111-1111-4111-8111-111111111111"
	parent := "22222222-2222-4222-8222-222222222222"
	input, err := normalizeRunTaskInput(service.RunTaskInput{
		Message:     "child",
		SessionID:   "session-1",
		RootRunID:   root,
		ParentRunID: parent,
		RunID:       runID,
	})
	if err != nil {
		t.Fatalf("normalize child run: %v", err)
	}
	if input.RootRunID != root || input.ParentRunID != parent || input.RunID != runID {
		t.Fatalf("run identity changed: %#v", input)
	}
	if _, err := normalizeRunTaskInput(service.RunTaskInput{
		Message: "child", SessionID: "session-1", ParentRunID: parent,
	}); err == nil {
		t.Fatal("parent_run_id without root_run_id was accepted")
	}
	if _, err := normalizeRunTaskInput(service.RunTaskInput{
		Message: "child", SessionID: "session-1", RunID: "not-a-uuid",
	}); err == nil {
		t.Fatal("invalid run_id was accepted")
	}
}

func TestNormalizeRunTaskInputRequiresCompleteIntegrationIdentity(t *testing.T) {
	base := service.RunTaskInput{
		Message:                "child",
		SessionID:              "session-1",
		RunID:                  "33333333-3333-4333-8333-333333333333",
		RootRunID:              "11111111-1111-4111-8111-111111111111",
		ParentRunID:            "22222222-2222-4222-8222-222222222222",
		CurrentRunID:           "22222222-2222-4222-8222-222222222222",
		PlanTaskID:             "plan-task-1",
		IntegrationOperationID: "44444444-4444-4444-8444-444444444444",
		WorkspaceID:            "workspace-identity",
		WorkspaceHandle:        "opaque-workspace-handle",
	}
	if _, err := normalizeRunTaskInput(base); err != nil {
		t.Fatalf("complete integration identity rejected: %v", err)
	}

	for name, mutate := range map[string]func(*service.RunTaskInput){
		"run_id":           func(input *service.RunTaskInput) { input.RunID = "" },
		"root_run_id":      func(input *service.RunTaskInput) { input.RootRunID = "" },
		"parent_run_id":    func(input *service.RunTaskInput) { input.ParentRunID = "" },
		"current_run_id":   func(input *service.RunTaskInput) { input.CurrentRunID = "" },
		"workspace_id":     func(input *service.RunTaskInput) { input.WorkspaceID = "" },
		"workspace_handle": func(input *service.RunTaskInput) { input.WorkspaceHandle = "" },
	} {
		input := base
		mutate(&input)
		if _, err := normalizeRunTaskInput(input); err == nil {
			t.Fatalf("missing %s was accepted", name)
		}
	}

	mismatched := base
	mismatched.CurrentRunID = "11111111-1111-4111-8111-111111111111"
	if _, err := normalizeRunTaskInput(mismatched); err == nil {
		t.Fatal("current_run_id unrelated to parent_run_id was accepted")
	}
}

func TestRunTaskRequestHashIsStableAndContentSensitive(t *testing.T) {
	input := service.RunTaskInput{
		Message: "child", SessionID: "session-1", RunID: "33333333-3333-4333-8333-333333333333",
		Budget: map[string]interface{}{"max_children": 2, "wall_time_seconds": 30},
	}
	if hashRunTaskInput(input) != hashRunTaskInput(input) {
		t.Fatal("same run request produced different hashes")
	}
	changed := input
	changed.Message = "different"
	if hashRunTaskInput(input) == hashRunTaskInput(changed) {
		t.Fatal("different run requests produced the same hash")
	}
	withCapability := input
	withCapability.IntegrationCapability = "fresh-capability"
	if hashRunTaskInput(input) != hashRunTaskInput(withCapability) {
		t.Fatal("single-use capability should not change the immutable run request hash")
	}
}

func TestNormalizeReviewTaskInput(t *testing.T) {
	input, err := normalizeReviewTaskInput(service.ReviewTaskInput{
		SessionID: " session-1 ",
		Action:    " approve ",
		Content:   " looks good ",
	})
	if err != nil {
		t.Fatalf("normalizeReviewTaskInput: %v", err)
	}
	if input.SessionID != "session-1" || input.Action != "approve" || input.Content != "looks good" {
		t.Fatalf("unexpected normalized input: %+v", input)
	}

	if _, err := normalizeReviewTaskInput(service.ReviewTaskInput{SessionID: "   "}); err == nil {
		t.Fatal("blank session_id accepted")
	}
	if _, err := normalizeReviewTaskInput(service.ReviewTaskInput{SessionID: "s", Content: strings.Repeat("x", maxReviewContentLen+1)}); err == nil {
		t.Fatal("too long review content accepted")
	}
}

func TestValidateAvatarURL(t *testing.T) {
	for _, avatarURL := range []string{"/uploads/avatars/550e8400-e29b-41d4-a716-446655440000.png", "https://example.com/a.png", "http://example.com/a.png"} {
		if err := validateAvatarURL(avatarURL); err != nil {
			t.Fatalf("validateAvatarURL(%q): %v", avatarURL, err)
		}
	}
	if err := validateAvatarURL("/media/avatars/550e8400-e29b-41d4-a716-446655440000.png", "/media"); err != nil {
		t.Fatalf("custom local prefix was rejected: %v", err)
	}
	if err := validateAvatarURL("/other/avatars/550e8400-e29b-41d4-a716-446655440000.png", "/media"); err == nil {
		t.Fatal("unconfigured local prefix was accepted")
	}

	for _, avatarURL := range []string{"javascript:alert(1)", "ftp://example.com/a.png", "//example.com/a.png", "/uploads\\bad.png", "https://example.com/a b.png", strings.Repeat("x", maxAvatarURLLen+1)} {
		if err := validateAvatarURL(avatarURL); err == nil {
			t.Fatalf("invalid avatar_url accepted: %q", avatarURL)
		}
	}
}

func TestNormalizeProfileSessionID(t *testing.T) {
	sessionID, err := normalizeProfileSessionID(" session-1 ")
	if err != nil {
		t.Fatalf("normalizeProfileSessionID: %v", err)
	}
	if sessionID != "session-1" {
		t.Fatalf("sessionID = %q, want session-1", sessionID)
	}

	if _, err := normalizeProfileSessionID("   "); err == nil {
		t.Fatal("blank session_id accepted")
	}
	if _, err := normalizeProfileSessionID(strings.Repeat("s", maxSessionIDLen+1)); err == nil {
		t.Fatal("too long session_id accepted")
	}
}

func TestNormalizeAdminSessionIDs(t *testing.T) {
	sessionIDs, err := normalizeAdminSessionIDs([]string{" s1 ", "s2", "s1"})
	if err != nil {
		t.Fatalf("normalizeAdminSessionIDs: %v", err)
	}
	if len(sessionIDs) != 2 || sessionIDs[0] != "s1" || sessionIDs[1] != "s2" {
		t.Fatalf("sessionIDs = %#v, want deduped trimmed ids", sessionIDs)
	}

	if _, err := normalizeAdminSessionIDs([]string{"s1", "   "}); err == nil {
		t.Fatal("blank session_id accepted")
	}
}

func TestNormalizeSkillName(t *testing.T) {
	name, err := normalizeSkillName(" reviewer ")
	if err != nil {
		t.Fatalf("normalizeSkillName: %v", err)
	}
	if name != "reviewer" {
		t.Fatalf("name = %q, want reviewer", name)
	}

	for _, name := range []string{"   ", "bad/name", "bad\\name", strings.Repeat("s", maxSkillNameLen+1)} {
		if _, err := normalizeSkillName(name); err == nil {
			t.Fatalf("invalid skill name accepted: %q", name)
		}
	}
}

func TestNormalizeMessageListInput(t *testing.T) {
	sessionID, mode, primarySessionID, err := normalizeMessageListInput(" s1 ", " group ", " primary ")
	if err != nil {
		t.Fatalf("normalizeMessageListInput: %v", err)
	}
	if sessionID != "s1" || mode != "group" || primarySessionID != "primary" {
		t.Fatalf("unexpected normalized message list input: %q %q %q", sessionID, mode, primarySessionID)
	}

	if _, _, _, err := normalizeMessageListInput("s1", "direct", ""); err == nil {
		t.Fatal("invalid mode accepted")
	}
	if _, _, _, err := normalizeMessageListInput(strings.Repeat("s", maxSessionIDLen+1), "", ""); err == nil {
		t.Fatal("too long session_id accepted")
	}
	if _, _, _, err := normalizeMessageListInput("", "group", strings.Repeat("s", maxSessionIDLen+1)); err == nil {
		t.Fatal("too long primary_session_id accepted")
	}
}

func TestNormalizeContactGroupName(t *testing.T) {
	name, err := normalizeContactGroupName("  Inbox  ")
	if err != nil {
		t.Fatalf("normalizeContactGroupName: %v", err)
	}
	if name != "Inbox" {
		t.Fatalf("name = %q, want Inbox", name)
	}

	if _, err := normalizeContactGroupName("   "); err == nil {
		t.Fatal("blank group name accepted")
	}
	if _, err := normalizeContactGroupName(strings.Repeat("长", maxContactGroupNameLen+1)); err == nil {
		t.Fatal("too long group name accepted")
	}
}

func TestNormalizeContactGroupID(t *testing.T) {
	groupID, err := normalizeContactGroupID(" group-1 ")
	if err != nil {
		t.Fatalf("normalizeContactGroupID: %v", err)
	}
	if groupID != "group-1" {
		t.Fatalf("groupID = %q, want group-1", groupID)
	}

	if _, err := normalizeContactGroupID("   "); err == nil {
		t.Fatal("blank group_id accepted")
	}
	if _, err := normalizeContactGroupID(strings.Repeat("g", maxContactGroupIDLen+1)); err == nil {
		t.Fatal("too long group_id accepted")
	}
}

func TestNormalizeAnnouncementInput(t *testing.T) {
	input, err := normalizeAnnouncementInput(service.CreateAnnouncementInput{
		SenderID:   " user-1 ",
		SenderName: " Alice ",
		Content:    " hello ",
		Pinned:     true,
	})
	if err != nil {
		t.Fatalf("normalizeAnnouncementInput: %v", err)
	}
	if input.SenderID != "user-1" || input.SenderName != "Alice" || input.Content != "hello" || !input.Pinned {
		t.Fatalf("unexpected normalized announcement: %+v", input)
	}

	if _, err := normalizeAnnouncementInput(service.CreateAnnouncementInput{SenderID: "u", SenderName: "n", Content: "   "}); err == nil {
		t.Fatal("blank content accepted")
	}
	if _, err := normalizeAnnouncementInput(service.CreateAnnouncementInput{SenderID: "u", SenderName: "n", Content: strings.Repeat("x", maxAnnouncementContentLen+1)}); err == nil {
		t.Fatal("too long content accepted")
	}
	if _, err := normalizeAnnouncementInput(service.CreateAnnouncementInput{SenderID: strings.Repeat("u", maxAnnouncementSenderLen+1), SenderName: "n", Content: "content"}); err == nil {
		t.Fatal("too long sender_id accepted")
	}
}

func TestNormalizeAnnouncementID(t *testing.T) {
	id, err := normalizeAnnouncementID(" 12 ")
	if err != nil {
		t.Fatalf("normalizeAnnouncementID: %v", err)
	}
	if id != 12 {
		t.Fatalf("id = %d, want 12", id)
	}

	for _, input := range []string{"", "   ", "0", "-1", "abc", "1abc"} {
		if _, err := normalizeAnnouncementID(input); err == nil {
			t.Fatalf("invalid announcement id %q accepted", input)
		}
	}
}
