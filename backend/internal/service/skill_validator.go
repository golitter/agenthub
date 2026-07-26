package service

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	MaxUnzipSize int64 = 10 * 1024 * 1024 // 10MB
	MaxFileCount int   = 100
	SkillMDFile        = "SKILL.md"
	// Deprecated: 仅用于迁移过渡，迁移完成后移除
	HubBasePath = "../data/skills/hub"
)

type ValidationResult struct {
	Valid       bool     `json:"valid"`
	Name        string   `json:"name,omitempty"`
	Description string   `json:"description,omitempty"`
	FileCount   int      `json:"file_count,omitempty"`
	TotalSize   int64    `json:"total_size,omitempty"`
	TmpDir      string   `json:"tmp_dir,omitempty"`
	Errors      []string `json:"errors,omitempty"`
}

type frontmatterData struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// ValidateZip 解压到临时目录并校验
func ValidateZip(zipData []byte) (*ValidationResult, string, error) {
	reader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return &ValidationResult{Valid: false, Errors: []string{"invalid zip file"}}, "", err
	}

	// 创建临时目录
	tmpDir, err := os.MkdirTemp("", "skill-upload-*")
	if err != nil {
		return nil, "", fmt.Errorf("create temp dir: %w", err)
	}

	var (
		totalSize  int64
		fileCount  int
		errors     []string
		hasSkillMD bool
		fmName     string
		fmDesc     string
	)

	for _, f := range reader.File {
		cleanName, err := normalizeZipEntryName(f.Name)
		if err != nil {
			errors = append(errors, err.Error())
			continue
		}

		// 符号链接检查
		if f.Mode()&os.ModeSymlink != 0 {
			errors = append(errors, "symbolic links not allowed")
			continue
		}

		destPath := filepath.Join(tmpDir, filepath.FromSlash(cleanName))
		if !isPathInside(tmpDir, destPath) {
			errors = append(errors, "path traversal detected")
			continue
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(destPath, 0o755); err != nil {
				errors = append(errors, fmt.Sprintf("cannot create directory %s: %v", cleanName, err))
			}
			continue
		}

		// 确保父目录存在
		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			errors = append(errors, fmt.Sprintf("cannot create directory for %s: %v", cleanName, err))
			continue
		}

		rc, err := f.Open()
		if err != nil {
			errors = append(errors, fmt.Sprintf("cannot open %s: %v", cleanName, err))
			continue
		}

		var buf bytes.Buffer
		n, err := io.Copy(&buf, rc)
		rc.Close()
		if err != nil {
			errors = append(errors, fmt.Sprintf("cannot read %s: %v", cleanName, err))
			continue
		}

		totalSize += n
		fileCount++

		// Zip bomb 检查
		if totalSize > MaxUnzipSize {
			errors = append(errors, fmt.Sprintf("zip bomb: total size exceeds %dMB", MaxUnzipSize/1024/1024))
			os.RemoveAll(tmpDir)
			return &ValidationResult{Valid: false, Errors: errors}, tmpDir, nil
		}
		if fileCount > MaxFileCount {
			errors = append(errors, fmt.Sprintf("too many files: exceeds %d", MaxFileCount))
			os.RemoveAll(tmpDir)
			return &ValidationResult{Valid: false, Errors: errors}, tmpDir, nil
		}

		// 写入文件
		if err := os.WriteFile(destPath, buf.Bytes(), 0644); err != nil {
			errors = append(errors, fmt.Sprintf("cannot write %s: %v", cleanName, err))
			continue
		}

		// 检查 SKILL.md（根目录或一级子目录，如 skill-name/SKILL.md）
		if filepath.Base(cleanName) == SkillMDFile && !strings.Contains(filepath.Dir(cleanName), "/") {
			hasSkillMD = true
			name, desc, parseErr := parseFrontmatter(buf.Bytes())
			if parseErr != nil {
				errors = append(errors, parseErr.Error())
			} else {
				fmName = name
				fmDesc = desc
			}
		}
	}

	if !hasSkillMD {
		errors = append(errors, "missing SKILL.md")
	}

	if len(errors) > 0 {
		return &ValidationResult{Valid: false, Errors: errors}, tmpDir, nil
	}

	return &ValidationResult{
		Valid:       true,
		Name:        fmName,
		Description: fmDesc,
		FileCount:   fileCount,
		TotalSize:   totalSize,
		TmpDir:      tmpDir,
	}, tmpDir, nil
}

func normalizeZipEntryName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("empty zip entry name")
	}
	if filepath.IsAbs(name) || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("absolute path not allowed")
	}
	cleanName := filepath.ToSlash(filepath.Clean(name))
	if cleanName == "." || cleanName == "" {
		return "", fmt.Errorf("empty zip entry name")
	}
	for _, segment := range strings.Split(cleanName, "/") {
		if segment == ".." {
			return "", fmt.Errorf("path traversal detected")
		}
	}
	return cleanName, nil
}

func isPathInside(baseDir, targetPath string) bool {
	rel, err := filepath.Rel(baseDir, targetPath)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// parseFrontmatter 解析 SKILL.md 的 YAML frontmatter
func parseFrontmatter(data []byte) (name, description string, err error) {
	content := string(data)
	if !strings.HasPrefix(content, "---") {
		return "", "", fmt.Errorf("missing frontmatter")
	}

	end := strings.Index(content[3:], "---")
	if end == -1 {
		return "", "", fmt.Errorf("missing frontmatter")
	}

	fmContent := strings.TrimSpace(content[3 : end+3])
	var fm frontmatterData
	if err := yaml.Unmarshal([]byte(fmContent), &fm); err != nil {
		return "", "", fmt.Errorf("invalid frontmatter: %v", err)
	}

	if fm.Name == "" {
		return "", "", fmt.Errorf("missing name field")
	}

	return fm.Name, fm.Description, nil
}

// PackValidatedSkillDir repacks the validated skill directory into a zip blob.
func PackValidatedSkillDir(name string, tmpDir string) ([]byte, error) {
	srcDir, err := validatedSkillSourceDir(name, tmpDir)
	if err != nil {
		return nil, err
	}

	// 将已校验的文件重新打包为 zip
	zipData, err := zipDir(srcDir)
	if err != nil {
		return nil, fmt.Errorf("pack skill files: %w", err)
	}

	return zipData, nil
}

func InspectValidatedSkillDir(name string, tmpDir string) (*ValidationResult, error) {
	srcDir, err := validatedSkillSourceDir(name, tmpDir)
	if err != nil {
		return nil, err
	}

	result := &ValidationResult{Valid: true}
	err = filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("walk error at %s: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic links not allowed")
		}
		if info.IsDir() {
			return nil
		}

		result.FileCount++
		if result.FileCount > MaxFileCount {
			return fmt.Errorf("too many files: exceeds %d", MaxFileCount)
		}
		result.TotalSize += info.Size()
		if result.TotalSize > MaxUnzipSize {
			return fmt.Errorf("total size exceeds %dMB", MaxUnzipSize/1024/1024)
		}

		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return fmt.Errorf("relative path error: %w", err)
		}
		rel = filepath.ToSlash(rel)
		if filepath.Base(rel) == SkillMDFile && !strings.Contains(pathpkgDir(rel), "/") {
			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read %s: %w", SkillMDFile, err)
			}
			skillName, desc, err := parseFrontmatter(data)
			if err != nil {
				return err
			}
			result.Name = skillName
			result.Description = desc
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if result.Name == "" {
		return nil, fmt.Errorf("missing %s", SkillMDFile)
	}
	if result.Name != name {
		return nil, fmt.Errorf("skill name mismatch: confirm name (%s) does not match SKILL.md name (%s)", name, result.Name)
	}
	return result, nil
}

func validatedSkillSourceDir(name string, tmpDir string) (string, error) {
	safeTmpDir, err := ensureSkillUploadTmpDir(tmpDir)
	if err != nil {
		return "", err
	}

	srcDir := filepath.Join(safeTmpDir, name)
	if info, err := os.Stat(srcDir); err != nil || !info.IsDir() {
		srcDir = safeTmpDir
	}
	return srcDir, nil
}

func ensureSkillUploadTmpDir(tmpDir string) (string, error) {
	if tmpDir == "" {
		return "", fmt.Errorf("tmp_dir is required")
	}

	absTmpDir, err := filepath.Abs(tmpDir)
	if err != nil {
		return "", fmt.Errorf("invalid tmp_dir: %w", err)
	}
	absSystemTmp, err := filepath.Abs(os.TempDir())
	if err != nil {
		return "", fmt.Errorf("invalid system tmp dir: %w", err)
	}

	if filepath.Dir(absTmpDir) != absSystemTmp || !strings.HasPrefix(filepath.Base(absTmpDir), "skill-upload-") {
		return "", fmt.Errorf("invalid skill upload tmp_dir")
	}

	info, err := os.Lstat(absTmpDir)
	if err != nil {
		return "", fmt.Errorf("invalid skill upload tmp_dir: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", fmt.Errorf("invalid skill upload tmp_dir")
	}
	return absTmpDir, nil
}

func pathpkgDir(p string) string {
	dir := filepath.ToSlash(filepath.Dir(p))
	if dir == "." {
		return ""
	}
	return dir
}

// zipDir 将目录打包为 zip 字节流
func zipDir(src string) ([]byte, error) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	var totalSize int64
	var fileCount int

	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("walk error at %s: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic links not allowed")
		}
		if info.IsDir() {
			return nil
		}
		fileCount++
		if fileCount > MaxFileCount {
			return fmt.Errorf("too many files: exceeds %d", MaxFileCount)
		}
		totalSize += info.Size()
		if totalSize > MaxUnzipSize {
			return fmt.Errorf("total size exceeds %dMB", MaxUnzipSize/1024/1024)
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return fmt.Errorf("relative path error: %w", err)
		}
		rel = filepath.ToSlash(rel) // 统一正斜杠，跨平台一致
		f, err := w.Create(rel)
		if err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		_, err = io.Copy(f, in)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
