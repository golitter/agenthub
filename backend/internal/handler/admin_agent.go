package handler

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"agenthub/backend/internal/vo"

	"github.com/gin-gonic/gin"
)

type AgentInfo struct {
	Type          string `json:"type"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	ConfigDir     string `json:"configDir"`
	ConfigFile    string `json:"configFile"`
	ConfigContent string `json:"configContent,omitempty"`
}

var sensitivePattern = regexp.MustCompile(`(?i)(api[_-]?key|token|secret|password|credential|auth[_-]?token)\s*[:=]\s*["']?[^"'\s,}]+["']?`)

func (h *AdminHandler) GetAgents(c *gin.Context) {
	home, _ := os.UserHomeDir()
	agents := []AgentInfo{
		{
			Type: "claude_code", Name: "Claude Code",
			Description: "Anthropic Claude Code CLI", ConfigDir: filepath.Join(home, ".claude"),
			ConfigFile: "settings.json",
		},
		{
			Type: "opencode", Name: "OpenCode",
			Description: "OpenCode CLI", ConfigDir: filepath.Join(home, ".opencode"),
			ConfigFile: "config.json",
		},
		{
			Type: "codex", Name: "Codex",
			Description: "OpenAI Codex CLI", ConfigDir: filepath.Join(home, ".codex"),
			ConfigFile: "config.toml",
		},
		{
			Type: "orchestrator", Name: "Orchestrator",
			Description: "Task Orchestrator", ConfigDir: filepath.Join(home, ".orchestrator"),
			ConfigFile: "config.yaml",
		},
	}

	for i, agent := range agents {
		path := filepath.Join(agent.ConfigDir, agent.ConfigFile)
		data, err := os.ReadFile(path)
		if err != nil {
			agents[i].ConfigContent = "配置文件不存在或无法读取"
			continue
		}
		agents[i].ConfigContent = sanitizeConfig(string(data))
	}

	vo.OK(c, agents)
}

func sanitizeConfig(content string) string {
	return sensitivePattern.ReplaceAllStringFunc(content, func(match string) string {
		parts := strings.SplitN(match, ":", 2)
		if len(parts) == 2 {
			return parts[0] + ": ***"
		}
		parts = strings.SplitN(match, "=", 2)
		if len(parts) == 2 {
			return parts[0] + "=***"
		}
		return "***"
	})
}
