package permission

import "testing"

func TestResolveEnvVarForModel(t *testing.T) {
	tests := []struct {
		name    string
		backend string
		model   string
		key     string
		want    string
	}{
		{
			name:    "claude backend uses oauth token",
			backend: "claude",
			model:   "anthropic/claude-sonnet-4-5",
			key:     "any",
			want:    "CLAUDE_CODE_OAUTH_TOKEN",
		},
		{
			name:    "opencode openai model",
			backend: "opencode",
			model:   "openai/gpt-5",
			key:     "any",
			want:    "OPENAI_API_KEY",
		},
		{
			name:    "opencode anthropic model",
			backend: "opencode",
			model:   "anthropic/claude-opus-4-6",
			key:     "any",
			want:    "ANTHROPIC_API_KEY",
		},
		{
			name:    "opencode unknown model falls back to key",
			backend: "opencode",
			model:   "custom/provider-model",
			key:     "sk-test-123",
			want:    "OPENAI_API_KEY",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveEnvVarForModel(tt.backend, tt.model, tt.key)
			if got != tt.want {
				t.Fatalf("ResolveEnvVarForModel(%q, %q, %q) = %q, want %q", tt.backend, tt.model, tt.key, got, tt.want)
			}
		})
	}
}
