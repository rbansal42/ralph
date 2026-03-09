package config

import (
	"os"
	"path/filepath"
	"testing"
)

func mustLoadConfigFromTOML(t *testing.T, raw string) *Config {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "ralph.toml")
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	return cfg
}

func TestLoadParentWorkerDefaults(t *testing.T) {
	cfg := mustLoadConfigFromTOML(t, `
backend = "opencode"
checklist = "CHECKLIST.md"
prompt = "prompt.md"

[[worker]]
name = "tasks"
pattern = "app/Tasks"
`)

	if cfg.WorkerParallelism != 2 {
		t.Fatalf("WorkerParallelism = %d, want 2", cfg.WorkerParallelism)
	}

	if cfg.ParentResetAfterRuns != 25 {
		t.Fatalf("ParentResetAfterRuns = %d, want 25", cfg.ParentResetAfterRuns)
	}
}

func TestResolveModelAlias(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "opus alias", in: "opus", want: "anthropic/claude-opus-4-6"},
		{name: "sonnet alias", in: "sonnet", want: "anthropic/claude-sonnet-4-5"},
		{name: "gpt5 alias", in: "gpt5", want: "openai/gpt-5"},
		{name: "o3 alias", in: "o3", want: "openai/o3"},
		{name: "gpt-5.3-codex alias", in: "gpt-5.3-codex", want: "openai/gpt-5.3-codex"},
		{name: "passthrough", in: "openai/gpt-5-mini", want: "openai/gpt-5-mini"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveModelAlias(tt.in)
			if got != tt.want {
				t.Fatalf("ResolveModelAlias(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
