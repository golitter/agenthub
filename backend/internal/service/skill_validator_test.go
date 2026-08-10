package service

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"
	"time"
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
		"skill/../SKILL.md",
		"skill//SKILL.md",
		"skill/./SKILL.md",
		"/tmp/SKILL.md",
		"C:/tmp/SKILL.md",
		"skill/\x00name.md",
		" SKILL.md",
		"SKILL.md ",
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

func TestValidateZipRejectsMalformedFrontmatterClosingMarker(t *testing.T) {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	file, err := writer.Create("SKILL.md")
	if err != nil {
		t.Fatalf("create SKILL.md: %v", err)
	}
	if _, err := file.Write([]byte("---\nname: demo\n---not-a-delimiter\n")); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
	archive := buffer.Bytes()
	result, tmpDir, err := ValidateZip(archive)
	if err != nil {
		t.Fatalf("ValidateZip returned error: %v", err)
	}
	if tmpDir != "" {
		defer os.RemoveAll(tmpDir)
	}
	if result == nil || result.Valid {
		t.Fatalf("ValidateZip accepted malformed frontmatter: %+v", result)
	}
}

func TestValidateZipRejectsDuplicateEntries(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, body := range []string{"---\nname: demo\n---\n", "duplicate"} {
		f, err := w.Create("SKILL.md")
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		if _, err := f.Write([]byte(body)); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	result, tmpDir, err := ValidateZip(buf.Bytes())
	if err != nil {
		t.Fatalf("ValidateZip returned error: %v", err)
	}
	if tmpDir != "" {
		defer os.RemoveAll(tmpDir)
	}
	if result.Valid || len(result.Errors) == 0 {
		t.Fatalf("ValidateZip accepted duplicate entries: %+v", result)
	}
}

func TestValidateZipRejectsFileDirectoryPathAlias(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	file, err := w.Create("foo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("file")); err != nil {
		t.Fatal(err)
	}
	directory := &zip.FileHeader{Name: "foo/", Method: zip.Store}
	directory.SetMode(os.ModeDir | 0o755)
	if _, err := w.CreateHeader(directory); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	result, tmpDir, err := ValidateZip(buf.Bytes())
	if err != nil {
		t.Fatalf("ValidateZip returned error: %v", err)
	}
	if tmpDir != "" {
		defer os.RemoveAll(tmpDir)
	}
	if result == nil || result.Valid || len(result.Errors) == 0 {
		t.Fatalf("ValidateZip accepted file/directory alias: %+v", result)
	}
}

func TestValidateZipPreservesOnlyExplicitExecuteBit(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	md, err := w.Create("SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := md.Write([]byte("---\nname: executable\n---\n")); err != nil {
		t.Fatal(err)
	}
	header := &zip.FileHeader{Name: "run.sh", Method: zip.Deflate}
	header.SetMode(0o755)
	script, err := w.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := script.Write([]byte("#!/bin/sh\necho ok\n")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	result, tmpDir, err := ValidateZip(buf.Bytes())
	if err != nil || result == nil || !result.Valid {
		t.Fatalf("ValidateZip result = %+v, err = %v", result, err)
	}
	defer os.RemoveAll(tmpDir)
	info, err := os.Stat(filepath.Join(tmpDir, "run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("executable mode = %o, want 755", info.Mode().Perm())
	}
}

func TestPackValidatedSkillDirIsDeterministic(t *testing.T) {
	first := makeSkillDir(t, "demo", 0o600, time.Unix(100, 0))
	second := makeSkillDir(t, "demo", 0o644, time.Unix(2_000_000, 0))
	defer os.RemoveAll(first)
	defer os.RemoveAll(second)

	firstZip, err := PackValidatedSkillDir("demo", first)
	if err != nil {
		t.Fatalf("pack first skill: %v", err)
	}
	secondZip, err := PackValidatedSkillDir("demo", second)
	if err != nil {
		t.Fatalf("pack second skill: %v", err)
	}
	firstHash := sha256.Sum256(firstZip)
	secondHash := sha256.Sum256(secondZip)
	if firstHash != secondHash {
		t.Fatalf("canonical package hash differs: %x != %x", firstHash, secondHash)
	}
}

func TestPackValidatedSkillDirEnforcesPackageLimitWhileWriting(t *testing.T) {
	dir := makeSkillDir(t, "limited", 0o600, time.Unix(100, 0))
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, "large.txt"), bytes.Repeat([]byte("x"), 4096), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PackValidatedSkillDirWithLimit("limited", dir, 128); err == nil {
		t.Fatal("packer accepted an archive larger than the configured limit")
	}
}

func TestValidateCanonicalSkillPackageRechecksCompressionRatio(t *testing.T) {
	root := t.TempDir()
	dir, err := os.MkdirTemp(root, "skill-upload-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, SkillMDFile), []byte("---\nname: ratio\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "zeros.txt"), bytes.Repeat([]byte{'0'}, 256*1024), 0o600); err != nil {
		t.Fatal(err)
	}
	canonical, err := PackValidatedSkillDirInRoot("ratio", dir, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateCanonicalSkillPackageContext(context.Background(), bytes.NewReader(canonical), int64(len(canonical)), root, "ratio", DefaultZipLimits()); err == nil {
		t.Fatal("canonical package with a >100:1 compression ratio was accepted")
	}
}

func TestPackValidatedSkillDirSupportsConfiguredPrivateTempRoot(t *testing.T) {
	root := t.TempDir()
	dir, err := os.MkdirTemp(root, "skill-upload-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, SkillMDFile), []byte("---\nname: private\n---\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	canonical, err := PackValidatedSkillDirInRoot("private", dir, root)
	if err != nil {
		t.Fatalf("PackValidatedSkillDirInRoot: %v", err)
	}
	if len(canonical) == 0 {
		t.Fatal("canonical package is empty")
	}
}

func TestPackValidatedSkillDirPrefersRootSkillLayout(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	tmpDir := filepath.Join(root, "skill-upload-root")
	if err := os.Mkdir(tmpDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, SkillMDFile), []byte("---\nname: demo\ndescription: root\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "demo", "nested.txt"), []byte("nested"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "root.txt"), []byte("root"), 0o600); err != nil {
		t.Fatal(err)
	}
	archive, err := PackValidatedSkillDirInRoot("demo", tmpDir, root)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	got := make(map[string]bool, len(reader.File))
	for _, entry := range reader.File {
		got[entry.Name] = true
	}
	for _, want := range []string{SkillMDFile, "root.txt", "demo/nested.txt"} {
		if !got[want] {
			t.Fatalf("canonical archive omitted %q; entries=%v", want, got)
		}
	}
}

func TestEnsureSkillTempRootRejectsSymlink(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := EnsureSkillTempRoot(link); err == nil {
		t.Fatal("EnsureSkillTempRoot accepted a symlink root")
	}
}

func TestEnsureSkillTempRootRejectsSymlinkParentBeforeCreatingChild(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	root := filepath.Join(link, "skill-tmp")
	if err := EnsureSkillTempRoot(root); err == nil {
		t.Fatal("EnsureSkillTempRoot accepted a symlink parent")
	}
	if _, err := os.Stat(filepath.Join(target, "skill-tmp")); !os.IsNotExist(err) {
		t.Fatalf("symlink target was modified: stat err = %v", err)
	}
}

func TestPackValidatedSkillDirContextHonorsCancellation(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "skill-upload-cancel")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, SkillMDFile), []byte("---\nname: cancel\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := PackValidatedSkillDirInRootContext(ctx, "cancel", dir, root); err == nil {
		t.Fatal("packing ignored a canceled context")
	}
}

func FuzzValidateZipNeverPanics(f *testing.F) {
	f.Add([]byte("not a zip"))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		result, tmpDir, _ := ValidateZip(data)
		if tmpDir != "" {
			defer os.RemoveAll(tmpDir)
		}
		if result != nil && result.Valid && result.Name == "" {
			t.Fatal("valid result has no Skill name")
		}
	})
}

func makeSkillDir(t *testing.T, name string, mode os.FileMode, modified time.Time) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "skill-upload-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, SkillMDFile), []byte("---\nname: "+name+"\n---\n"), mode); err != nil {
		os.RemoveAll(dir)
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "z.txt"), []byte("z"), 0o644); err != nil {
		os.RemoveAll(dir)
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		os.RemoveAll(dir)
		t.Fatalf("WriteFile: %v", err)
	}
	for _, path := range []string{filepath.Join(dir, SkillMDFile), filepath.Join(dir, "z.txt"), filepath.Join(dir, "a.txt")} {
		if err := os.Chtimes(path, modified, modified); err != nil {
			os.RemoveAll(dir)
			t.Fatalf("Chtimes: %v", err)
		}
	}
	return dir
}
