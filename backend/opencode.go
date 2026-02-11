package backend

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const opencodeBin = "opencode"

// OpenCode implements Backend using the opencode CLI.
type OpenCode struct{}

func (o *OpenCode) Name() string { return "opencode" }

// openCodeDataHome returns the real opencode data home directory.
// It checks XDG_DATA_HOME first, then falls back to ~/.local/share.
func openCodeDataHome() string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "opencode")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "opencode")
}

// buildOpenCodeEnv returns process environment with an isolated XDG_DATA_HOME
// that mirrors the real opencode data dir but replaces auth.json with an empty
// file. This forces opencode to use env-var API keys instead of its stored
// OAuth credentials, making ralph token switching work.
//
// If realDataHome is empty, it auto-detects from XDG_DATA_HOME / ~/.local/share.
func buildOpenCodeEnv(workdir string, realDataHome string) ([]string, error) {
	if workdir == "" {
		workdir = "."
	}

	if realDataHome == "" {
		realDataHome = openCodeDataHome()
	}

	mirrorBase := filepath.Join(workdir, ".ralph", "opencode-data")
	mirrorOpencode := filepath.Join(mirrorBase, "opencode")
	if err := os.MkdirAll(mirrorOpencode, 0755); err != nil {
		return nil, fmt.Errorf("creating opencode mirror directory: %w", err)
	}

	// Symlink all entries from real data home except auth.json
	entries, err := os.ReadDir(realDataHome)
	if err != nil {
		// If real data home doesn't exist, just use the empty mirror
		if os.IsNotExist(err) {
			entries = nil
		} else {
			return nil, fmt.Errorf("reading opencode data home: %w", err)
		}
	}

	for _, entry := range entries {
		name := entry.Name()
		mirrorPath := filepath.Join(mirrorOpencode, name)
		realPath := filepath.Join(realDataHome, name)

		if name == "auth.json" {
			// Write empty JSON so opencode doesn't error on missing file
			if err := os.WriteFile(mirrorPath, []byte("{}"), 0644); err != nil {
				return nil, fmt.Errorf("writing empty auth.json: %w", err)
			}
			continue
		}

		// Skip if already exists (idempotent)
		if _, err := os.Lstat(mirrorPath); err == nil {
			continue
		}

		// Symlink everything else
		if err := os.Symlink(realPath, mirrorPath); err != nil {
			return nil, fmt.Errorf("symlinking %s: %w", name, err)
		}
	}

	env := os.Environ()
	prefix := "XDG_DATA_HOME="
	for i, e := range env {
		if strings.HasPrefix(e, prefix) {
			env[i] = prefix + mirrorBase
			return env, nil
		}
	}

	env = append(env, prefix+mirrorBase)
	return env, nil
}

// hasAPIKeyInEnv returns true if a real API key (not an OAuth token) is set
// in the environment. OAuth access tokens start with "sk-ant-oat" and should
// not trigger auth isolation.
func hasAPIKeyInEnv() bool {
	for _, envVar := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY"} {
		key := os.Getenv(envVar)
		if key == "" {
			continue
		}
		// OAuth access tokens are not real API keys
		if strings.HasPrefix(key, "sk-ant-oat") {
			continue
		}
		return true
	}
	return false
}

func (o *OpenCode) CheckAuth(ctx context.Context, model string) error {
	if _, err := exec.LookPath(opencodeBin); err != nil {
		return fmt.Errorf("%s not found in PATH: %w", opencodeBin, err)
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, opencodeBin, "run", "-m", model)
	cmd.Stdin = strings.NewReader("respond with exactly: RALPH_OK")

	// Only isolate auth when a real API key is in the environment.
	// Otherwise let opencode use its own OAuth credentials.
	if hasAPIKeyInEnv() {
		env, envErr := buildOpenCodeEnv(".", "")
		if envErr != nil {
			return envErr
		}
		cmd.Env = env
	}

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

	if hasAPIKeyInEnv() {
		env, envErr := buildOpenCodeEnv(workdir, "")
		if envErr != nil {
			return "", -1, envErr
		}
		cmd.Env = env
	}

	var buf bytes.Buffer
	cmd.Stdout = io.MultiWriter(os.Stdout, &buf)
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
