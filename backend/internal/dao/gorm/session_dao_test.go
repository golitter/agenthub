package gormdao

import "testing"

func TestIsAllowedSessionStatus(t *testing.T) {
	for _, status := range []string{"idle", "running", "awaiting_review", "completed", "interrupted", "error", "inactive"} {
		if !isAllowedSessionStatus(status) {
			t.Fatalf("status %q rejected", status)
		}
	}

	for _, status := range []string{"active", "failed", "cleaned", ""} {
		if isAllowedSessionStatus(status) {
			t.Fatalf("status %q accepted", status)
		}
	}
}

func TestUpdateStatusByTaskRejectsInvalidStatus(t *testing.T) {
	dao := NewSessionDao()
	if err := dao.UpdateStatusByTask("session-1", "task-1", "failed"); err == nil {
		t.Fatal("UpdateStatusByTask accepted invalid session status")
	}
}
