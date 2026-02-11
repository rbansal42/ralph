package backend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildOpenCodeEnvSetsIsolatedDataHome(t *testing.T) {
	workdir := t.TempDir()

	env, err := buildOpenCodeEnv(workdir)
	if err != nil {
		t.Fatalf("buildOpenCodeEnv returned error: %v", err)
	}

	want := filepath.Join(workdir, ".ralph", "opencode-data")
	found := ""
	for _, entry := range env {
		if strings.HasPrefix(entry, "XDG_DATA_HOME=") {
			found = strings.TrimPrefix(entry, "XDG_DATA_HOME=")
			break
		}
	}

	if found != want {
		t.Fatalf("XDG_DATA_HOME=%q, want %q", found, want)
	}

	if info, err := os.Stat(want); err != nil {
		t.Fatalf("expected data dir to exist: %v", err)
	} else if !info.IsDir() {
		t.Fatalf("expected %q to be a directory", want)
	}
}
