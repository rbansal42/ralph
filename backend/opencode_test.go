package backend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildOpenCodeEnvMirrorsDataHomeWithoutAuth(t *testing.T) {
	// Create a fake opencode data home with auth.json and other files
	realDataHome := filepath.Join(t.TempDir(), "opencode")
	if err := os.MkdirAll(realDataHome, 0755); err != nil {
		t.Fatal(err)
	}
	// Write a fake auth.json
	if err := os.WriteFile(filepath.Join(realDataHome, "auth.json"), []byte(`{"anthropic":{"type":"oauth"}}`), 0644); err != nil {
		t.Fatal(err)
	}
	// Write another file that should be preserved
	binDir := filepath.Join(realDataHome, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "tool"), []byte("binary"), 0755); err != nil {
		t.Fatal(err)
	}

	workdir := t.TempDir()

	env, err := buildOpenCodeEnv(workdir, realDataHome)
	if err != nil {
		t.Fatalf("buildOpenCodeEnv returned error: %v", err)
	}

	// Should set XDG_DATA_HOME to the mirror directory
	mirrorBase := filepath.Join(workdir, ".ralph", "opencode-data")
	mirrorOpencode := filepath.Join(mirrorBase, "opencode")

	found := ""
	for _, entry := range env {
		if strings.HasPrefix(entry, "XDG_DATA_HOME=") {
			found = strings.TrimPrefix(entry, "XDG_DATA_HOME=")
			break
		}
	}
	if found != mirrorBase {
		t.Fatalf("XDG_DATA_HOME=%q, want %q", found, mirrorBase)
	}

	// auth.json should exist but be empty
	authPath := filepath.Join(mirrorOpencode, "auth.json")
	data, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatalf("auth.json should exist in mirror: %v", err)
	}
	if string(data) != "{}" {
		t.Fatalf("auth.json should be empty JSON, got %q", string(data))
	}

	// bin directory should be symlinked
	binLink := filepath.Join(mirrorOpencode, "bin")
	target, err := os.Readlink(binLink)
	if err != nil {
		t.Fatalf("bin should be a symlink: %v", err)
	}
	if target != binDir {
		t.Fatalf("bin symlink target = %q, want %q", target, binDir)
	}
}
