package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func resolveRegularWorkspaceFile(root string, rawPath string) (string, error) {
	if rawPath == "" || rawPath != strings.TrimSpace(rawPath) || filepath.IsAbs(rawPath) {
		return "", fmt.Errorf("路径必须是工作区内的相对文件路径")
	}
	for _, char := range rawPath {
		if char < 0x20 || char == 0x7f {
			return "", fmt.Errorf("路径包含控制字符")
		}
	}

	cleanRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("解析工作区失败: %w", err)
	}
	cleanPath := filepath.Clean(rawPath)
	if cleanPath == "." || cleanPath == ".." {
		return "", fmt.Errorf("路径不能为空或根目录")
	}
	fullPath, err := filepath.EvalSymlinks(filepath.Join(cleanRoot, cleanPath))
	if err != nil {
		return "", fmt.Errorf("文件不存在或无法解析: %w", err)
	}
	relative, err := filepath.Rel(cleanRoot, fullPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("路径不允许: %s", rawPath)
	}
	info, err := os.Stat(fullPath)
	if err != nil {
		return "", fmt.Errorf("文件不存在: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("路径不是普通文件")
	}
	return filepath.ToSlash(cleanPath), nil
}
