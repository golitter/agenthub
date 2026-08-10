package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeScanner(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "scanner.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCommandSkillScannerRejectsNonZeroExit(t *testing.T) {
	command := writeScanner(t, "echo rejected >&2; exit 7")
	scanner, err := NewCommandSkillScanner(command, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	err = scanner.Scan(context.Background(), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("scan error = %v, want fail-closed rejection", err)
	}
}

func TestCommandSkillScannerTimesOut(t *testing.T) {
	command := writeScanner(t, "sleep 1")
	scanner, err := NewCommandSkillScanner(command, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	err = scanner.Scan(context.Background(), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("scan error = %v, want timeout", err)
	}
}

func TestCommandSkillScannerRejectsInvalidCommand(t *testing.T) {
	if _, err := NewCommandSkillScanner("scanner\n--unsafe", time.Second); err == nil {
		t.Fatal("newline command was accepted")
	}
}
