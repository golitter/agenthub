package impl

import (
	"strings"
	"testing"

	mockdao "agenthub/backend/internal/dao/mock"
	"agenthub/backend/internal/model"
	"agenthub/backend/internal/service"
)

func TestNormalizeDiffSnapshotInput(t *testing.T) {
	snapshotID, input, err := normalizeDiffSnapshotInput(" snap-1 ", service.SaveDiffSnapshotInput{
		SessionID: " session-1 ",
		Diff:      "diff",
		Status:    " pending ",
	})
	if err != nil {
		t.Fatalf("normalizeDiffSnapshotInput: %v", err)
	}
	if snapshotID != "snap-1" || input.SessionID != "session-1" || input.Status != "pending" {
		t.Fatalf("unexpected normalized values: %q %+v", snapshotID, input)
	}

	if _, _, err := normalizeDiffSnapshotInput(" ", service.SaveDiffSnapshotInput{SessionID: "s", Status: "pending"}); err == nil {
		t.Fatal("blank snapshot_id accepted")
	}
	if _, _, err := normalizeDiffSnapshotInput("snap", service.SaveDiffSnapshotInput{SessionID: " ", Status: "pending"}); err == nil {
		t.Fatal("blank session_id accepted")
	}
	if _, _, err := normalizeDiffSnapshotInput("snap", service.SaveDiffSnapshotInput{SessionID: "s", Status: "unknown"}); err == nil {
		t.Fatal("invalid status accepted")
	}
	if _, _, err := normalizeDiffSnapshotInput("snap", service.SaveDiffSnapshotInput{SessionID: "s", Status: "pending", Diff: strings.Repeat("x", maxDiffContentLen+1)}); err == nil {
		t.Fatal("oversized diff accepted")
	}
}

func TestSaveDiffSnapshotCancelsOtherPendingSnapshots(t *testing.T) {
	cancelled := false
	upserted := model.DiffSnapshot{}
	svc := NewDiffSnapshotService(&mockdao.DiffSnapshotDao{
		CancelPendingBySessionFunc: func(sessionID, excludedSnapshotID string) error {
			if sessionID != "session-1" || excludedSnapshotID != "snap-1" {
				t.Fatalf("CancelPendingBySession(%q, %q), want session-1/snap-1", sessionID, excludedSnapshotID)
			}
			cancelled = true
			return nil
		},
		UpsertFunc: func(snapshot model.DiffSnapshot) (*model.DiffSnapshot, error) {
			upserted = snapshot
			return &snapshot, nil
		},
	})

	if _, err := svc.SaveDiffSnapshot(" snap-1 ", service.SaveDiffSnapshotInput{
		SessionID: " session-1 ",
		Diff:      "diff",
		Status:    " pending ",
	}); err != nil {
		t.Fatalf("SaveDiffSnapshot: %v", err)
	}
	if !cancelled {
		t.Fatal("pending snapshot did not cancel other pending snapshots")
	}
	if upserted.SnapshotID != "snap-1" || upserted.SessionID != "session-1" || upserted.Status != "pending" {
		t.Fatalf("unexpected upserted snapshot: %+v", upserted)
	}
}

func TestSaveDiffSnapshotRejectsTerminalOverwrite(t *testing.T) {
	svc := NewDiffSnapshotService(&mockdao.DiffSnapshotDao{
		GetBySnapshotIDFunc: func(snapshotID string) (*model.DiffSnapshot, error) {
			return &model.DiffSnapshot{SnapshotID: snapshotID, Status: "committed"}, nil
		},
	})

	_, err := svc.SaveDiffSnapshot("snap-1", service.SaveDiffSnapshotInput{
		SessionID: "session-1",
		Status:    "reverted",
	})
	bizErr, ok := err.(*service.BizError)
	if !ok || bizErr.Code != 409 {
		t.Fatalf("err = %#v, want 409 BizError", err)
	}
}
