package service

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"agenthub/backend/internal/model"
	"agenthub/backend/pkg/db"

	"gopkg.in/yaml.v3"
)

const (
	MaxUnzipSize int64 = 10 * 1024 * 1024 // 10MB
	MaxFileCount int   = 100
	SkillMDFile        = "SKILL.md"
	HubBasePath        = "../data/skills/hub"
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
		// 路径安全检查
		if strings.Contains(f.Name, "..") {
			errors = append(errors, "path traversal detected")
			continue
		}
		if filepath.IsAbs(f.Name) {
			errors = append(errors, "absolute path not allowed")
			continue
		}

		// 符号链接检查
		if f.Mode()&os.ModeSymlink != 0 {
			errors = append(errors, "symbolic links not allowed")
			continue
		}

		destPath := filepath.Join(tmpDir, f.Name)

		if f.FileInfo().IsDir() {
			os.MkdirAll(destPath, 0755)
			continue
		}

		// 确保父目录存在
		os.MkdirAll(filepath.Dir(destPath), 0755)

		rc, err := f.Open()
		if err != nil {
			errors = append(errors, fmt.Sprintf("cannot open %s: %v", f.Name, err))
			continue
		}

		var buf bytes.Buffer
		n, err := io.Copy(&buf, rc)
		rc.Close()
		if err != nil {
			errors = append(errors, fmt.Sprintf("cannot read %s: %v", f.Name, err))
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
			errors = append(errors, fmt.Sprintf("cannot write %s: %v", f.Name, err))
			continue
		}

		// 检查 SKILL.md（根目录或一级子目录，如 skill-name/SKILL.md）
		if filepath.Base(f.Name) == SkillMDFile && !strings.Contains(filepath.Dir(f.Name), "/") {
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

	// 校验 name 不与 builtin 冲突
	var count int64
	db.GetDB().Model(&model.SkillHub{}).Where("name = ? AND builtin = ?", fmName, true).Count(&count)
	if count > 0 {
		os.RemoveAll(tmpDir)
		return &ValidationResult{Valid: false, Errors: []string{"name conflicts with builtin skill"}}, "", nil
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

// ConfirmSkill 先写 DB 再移文件到 hub
func ConfirmSkill(name string, description string, fileCount int, totalSize int64, tmpDir string) error {
	// 先写 DB
	skill := model.SkillHub{
		Name:        name,
		Builtin:     false,
		StoragePath: filepath.Join(HubBasePath, name),
		Description: description,
		FileCount:   fileCount,
		TotalSize:   totalSize,
	}

	if err := db.GetDB().Create(&skill).Error; err != nil {
		// DB 写入失败，临时文件由清理 job 回收
		return fmt.Errorf("db write failed: %w", err)
	}

	// 移文件：tmpDir 结构可能是 course/SKILL.md（zip 有外层目录）或 SKILL.md（无外层目录）
	hubPath := filepath.Join(HubBasePath, name)
	if err := os.MkdirAll(hubPath, 0755); err != nil {
		return fmt.Errorf("create hub dir: %w", err)
	}

	srcDir := filepath.Join(tmpDir, name)
	if info, err := os.Stat(srcDir); err == nil && info.IsDir() {
		// zip 有一层外层目录，取内部内容
		if err := copyDir(srcDir, hubPath); err != nil {
			os.RemoveAll(hubPath)
			db.GetDB().Delete(&skill)
			return fmt.Errorf("copy to hub: %w", err)
		}
	} else {
		// zip 没有外层目录，直接复制
		if err := copyDir(tmpDir, hubPath); err != nil {
			os.RemoveAll(hubPath)
			db.GetDB().Delete(&skill)
			return fmt.Errorf("copy to hub: %w", err)
		}
	}
	os.RemoveAll(tmpDir)
	return nil
}

// DeleteSkillFromHub 删除 hub 中的 external skill
func DeleteSkillFromHub(name string) error {
	var skill model.SkillHub
	if err := db.GetDB().Where("name = ?", name).First(&skill).Error; err != nil {
		return fmt.Errorf("skill not found")
	}
	if skill.Builtin {
		return fmt.Errorf("cannot delete builtin skill")
	}

	// 删除文件
	if skill.StoragePath != "" {
		os.RemoveAll(skill.StoragePath)
	}

	// 删除 DB 记录
	return db.GetDB().Delete(&skill).Error
}

// PackSkillDir 将目录打包为 zip 字节流
func PackSkillDir(src string) ([]byte, error) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
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

// copyDir 递归复制目录
func copyDir(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, info.Mode()); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
