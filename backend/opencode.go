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

const opencodeBin = "opencode"

// OpenCode implements Backend using the opencode CLI.
type OpenCode struct {
	Output io.Writer // subprocess stdout destination (set by backend.New)
}

func (o *OpenCode) Name() string { return "opencode" }

func (o *OpenCode) CheckAuth(ctx context.Context, model string) error {
	if _, err := exec.LookPath(opencodeBin); err != nil {
		return fmt.Errorf("%s not found in PATH: %w", opencodeBin, err)
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, opencodeBin, "run", "-m", model)
	cmd.Stdin = strings.NewReader("respond with exactly: RALPH_OK")

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("opencode auth check failed: %w\noutput: %s", err, string(out))
	}

	if !strings.Contains(string(out), "RALPH_OK") {
		return fmt.Errorf("opencode auth check: expected RALPH_OK in output, got: %s", string(out))
	}

	return nil
}

func (o *OpenCode) RunPrompt(ctx context.Context, promptFile string, workdir string, model string) (string, int, error) {
	if _, err := exec.LookPath(opencodeBin); err != nil {
		return "", -1, fmt.Errorf("%s not found in PATH: %w", opencodeBin, err)
	}

	f, err := os.Open(promptFile)
	if err != nil {
		return "", -1, fmt.Errorf("open prompt file: %w", err)
	}
	defer f.Close()

	cmd := exec.CommandContext(ctx, opencodeBin, "run", "-m", model)
	cmd.Dir = workdir
	cmd.Stdin = f

	var buf bytes.Buffer
	cmd.Stdout = io.MultiWriter(o.Output, &buf)
	cmd.Stderr = io.MultiWriter(os.Stderr, &buf)

	err = cmd.Run()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return buf.String(), -1, fmt.Errorf("running opencode: %w", err)
		}
	}

	return buf.String(), exitCode, nil
}

func (o *OpenCode) AuthGuide() string {
	return `OpenCode Authentication
=======================

Option 1 — API Key (environment variable):
  Export the appropriate API key for your provider before running ralph.
  For example, for Anthropic:
    export ANTHROPIC_API_KEY="sk-ant-..."
  For OpenAI:
    export OPENAI_API_KEY="sk-..."

Option 2 — Interactive login:
  Run the following command and follow the prompts:
    opencode auth

Once authenticated, verify with:
  echo "hello" | opencode run -m <model>`
}
