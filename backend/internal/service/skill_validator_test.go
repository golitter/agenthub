package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInspectValidatedSkillDirRejectsNonUploadTmpDir(t *testing.T) {
	dir := t.TempDir()
	if _, err := InspectValidatedSkillDir("demo", dir); err == nil {
		t.Fatal("InspectValidatedSkillDir accepted a non skill-upload temp dir")
	}
}

func TestInspectValidatedSkillDirVerifiesMetadata(t *testing.T) {
	dir, err := os.MkdirTemp("", "skill-upload-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	skillDir := filepath.Join(dir, "demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, SkillMDFile), []byte("---\nname: demo\ndescription: test skill\n---\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	result, err := InspectValidatedSkillDir("demo", dir)
	if err != nil {
		t.Fatalf("InspectValidatedSkillDir: %v", err)
	}
	if result.Name != "demo" || result.Description != "test skill" || result.FileCount != 1 {
		t.Fatalf("unexpected metadata: %+v", result)
	}
}

func TestInspectValidatedSkillDirRejectsNameMismatch(t *testing.T) {
	dir, err := os.MkdirTemp("", "skill-upload-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	if err := os.WriteFile(filepath.Join(dir, SkillMDFile), []byte("---\nname: actual\n---\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := InspectValidatedSkillDir("requested", dir); err == nil {
		t.Fatal("InspectValidatedSkillDir accepted mismatched skill name")
	}
}

func TestNormalizeZipEntryNameRejectsTraversal(t *testing.T) {
	for _, name := range []string{
		"../SKILL.md",
		"skill/../../SKILL.md",
		"/tmp/SKILL.md",
		"",
	} {
		if _, err := normalizeZipEntryName(name); err == nil {
			t.Fatalf("normalizeZipEntryName(%q) error = nil, want error", name)
		}
	}
}

func TestNormalizeZipEntryNameAllowsDotsInsideFilename(t *testing.T) {
	name, err := normalizeZipEntryName("skill/docs/notes..draft.md")
	if err != nil {
		t.Fatalf("normalizeZipEntryName: %v", err)
	}
	if name != "skill/docs/notes..draft.md" {
		t.Fatalf("name = %q, want normalized name", name)
	}
}
