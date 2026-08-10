package service

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// SkillContentScanner is the extension point for malware/signature scanners.
// The scanner receives the private extracted directory and must return an
// error to reject the package.  Implementations must fail closed.
type SkillContentScanner interface {
	Scan(ctx context.Context, extractedDir string) error
}

// CommandSkillScanner adapts a configured scanner executable without invoking
// a shell. Optional fixed arguments are split without a shell; the extracted
// directory is appended as the final argument, so shell metacharacters in
// Skill paths cannot become commands.
type CommandSkillScanner struct {
	command string
	timeout time.Duration
}

func NewCommandSkillScanner(command string, timeout time.Duration) (*CommandSkillScanner, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, fmt.Errorf("content scanner command is empty")
	}
	if strings.ContainsAny(command, "\r\n") {
		return nil, fmt.Errorf("content scanner command contains a newline")
	}
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	return &CommandSkillScanner{command: command, timeout: timeout}, nil
}

func (s *CommandSkillScanner) Scan(ctx context.Context, extractedDir string) error {
	if s == nil || strings.TrimSpace(s.command) == "" {
		return fmt.Errorf("content scanner is not configured")
	}
	if strings.TrimSpace(extractedDir) == "" {
		return fmt.Errorf("content scanner directory is empty")
	}
	scanCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	parts := strings.Fields(s.command)
	if len(parts) == 0 {
		return fmt.Errorf("content scanner command is empty")
	}
	args := append(append([]string(nil), parts[1:]...), extractedDir)
	cmd := exec.CommandContext(scanCtx, parts[0], args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		if scanCtx.Err() != nil {
			return fmt.Errorf("content scanner timed out: %w", scanCtx.Err())
		}
		trimmed := strings.TrimSpace(string(output))
		if trimmed != "" {
			return fmt.Errorf("content scanner rejected package: %s", trimmed)
		}
		return fmt.Errorf("content scanner failed: %w", err)
	}
	return nil
}
