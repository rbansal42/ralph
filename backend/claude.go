package backend

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

const claudeBin = "claude"

// Claude implements Backend using the Claude Code CLI.
type Claude struct {
	Output io.Writer // subprocess stdout destination (set by backend.New)
}

func (c *Claude) Name() string { return "claude" }

func (c *Claude) CheckAuth(ctx context.Context, model string) error {
	if _, err := exec.LookPath(claudeBin); err != nil {
		return fmt.Errorf("%s not found in PATH: %w", claudeBin, err)
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, claudeBin, "-p", "--model", model)
	cmd.Stdin = strings.NewReader("respond with exactly: RALPH_OK")

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("claude auth check failed: %w\noutput: %s", err, string(out))
	}

	if !strings.Contains(string(out), "RALPH_OK") {
		return fmt.Errorf("claude auth check: expected RALPH_OK in output, got: %s", string(out))
	}

	return nil
}

func (c *Claude) RunPrompt(ctx context.Context, promptFile string, workdir string, model string) (string, int, error) {
	if _, err := exec.LookPath(claudeBin); err != nil {
		return "", -1, fmt.Errorf("%s not found in PATH: %w", claudeBin, err)
	}

	f, err := os.Open(promptFile)
	if err != nil {
		return "", -1, fmt.Errorf("open prompt file: %w", err)
	}
	defer f.Close()

	cmd := exec.CommandContext(ctx, claudeBin,
		"-p",
		"--model", model,
		"--permission-mode", "bypassPermissions",
	)
	cmd.Dir = workdir
	cmd.Stdin = f

	var buf bytes.Buffer
	cmd.Stdout = io.MultiWriter(c.Output, &buf)
	cmd.Stderr = io.MultiWriter(os.Stderr, &buf)

	err = cmd.Run()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return buf.String(), -1, fmt.Errorf("running claude: %w", err)
		}
	}

	return buf.String(), exitCode, nil
}

func (c *Claude) AuthGuide() string {
	return `Claude Code Authentication
==========================

Option 1 — OAuth Token (environment variable):
  Export your OAuth token before running ralph:
    export CLAUDE_CODE_OAUTH_TOKEN="your-token-here"

Option 2 — API Key:
  Export your Anthropic API key:
    export ANTHROPIC_API_KEY="sk-ant-..."

Option 3 — Interactive setup:
  Run the following command and follow the prompts:
    claude setup-token

Once authenticated, verify with:
  echo "hello" | claude -p --model <model>`
}
