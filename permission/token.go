package permission

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

// TokenEntry represents a single API token with its metadata.
type TokenEntry struct {
	Name   string
	Key    string
	EnvVar string // which env var this sets (ANTHROPIC_API_KEY, CLAUDE_CODE_OAUTH_TOKEN, etc.)
}

// TokenManager manages a pool of API tokens with rotation support.
type TokenManager struct {
	mu      sync.Mutex
	tokens  []TokenEntry
	current int    // index of currently active token
	backend string // "opencode" or "claude"
}

// NewTokenManager creates a manager from config tokens, filtered for the given backend.
// If no tokens are provided, it creates a single entry from the current environment.
func NewTokenManager(backend string, tokens []TokenEntry) *TokenManager {
	tm := &TokenManager{
		backend: backend,
	}

	if len(tokens) > 0 {
		tm.tokens = tokens
	} else {
		// Fall back to environment variables
		entry := tokenFromEnv(backend)
		if entry.Key != "" {
			tm.tokens = []TokenEntry{entry}
		}
	}

	return tm
}

// tokenFromEnv creates a default TokenEntry by reading environment variables
// for the given backend.
func tokenFromEnv(backend string) TokenEntry {
	switch backend {
	case "opencode":
		if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
			return TokenEntry{
				Name:   "default",
				Key:    key,
				EnvVar: "ANTHROPIC_API_KEY",
			}
		}
		if key := os.Getenv("OPENAI_API_KEY"); key != "" {
			return TokenEntry{
				Name:   "default",
				Key:    key,
				EnvVar: "OPENAI_API_KEY",
			}
		}
	case "claude":
		if key := os.Getenv("CLAUDE_CODE_OAUTH_TOKEN"); key != "" {
			return TokenEntry{
				Name:   "default",
				Key:    key,
				EnvVar: "CLAUDE_CODE_OAUTH_TOKEN",
			}
		}
		if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
			return TokenEntry{
				Name:   "default",
				Key:    key,
				EnvVar: "ANTHROPIC_API_KEY",
			}
		}
	}
	return TokenEntry{}
}

// ResolveEnvVar determines which environment variable a key should be set as
// based on the backend and the key prefix.
func ResolveEnvVar(backend, key string) string {
	switch backend {
	case "opencode":
		// Keys starting with "sk-" but NOT "sk-ant" are OpenAI keys
		if strings.HasPrefix(key, "sk-") && !strings.HasPrefix(key, "sk-ant") {
			return "OPENAI_API_KEY"
		}
		return "ANTHROPIC_API_KEY"
	case "claude":
		return "CLAUDE_CODE_OAUTH_TOKEN"
	default:
		return "ANTHROPIC_API_KEY"
	}
}

// ResolveEnvVarForModel determines the env var for a backend/model combination.
// For opencode, model provider takes precedence; if unknown, it falls back to key-based inference.
func ResolveEnvVarForModel(backend, model, key string) string {
	switch backend {
	case "claude":
		return "CLAUDE_CODE_OAUTH_TOKEN"
	case "opencode":
		m := strings.ToLower(strings.TrimSpace(model))
		if strings.HasPrefix(m, "openai/") || strings.HasPrefix(m, "gpt") || strings.HasPrefix(m, "o1") || strings.HasPrefix(m, "o3") || strings.HasPrefix(m, "o4") {
			return "OPENAI_API_KEY"
		}
		if strings.HasPrefix(m, "anthropic/") || strings.Contains(m, "claude") {
			return "ANTHROPIC_API_KEY"
		}
		return ResolveEnvVar(backend, key)
	default:
		return ResolveEnvVar(backend, key)
	}
}

// Current returns the currently active token entry.
// Returns a zero-value TokenEntry if no tokens are available.
func (tm *TokenManager) Current() TokenEntry {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if len(tm.tokens) == 0 {
		return TokenEntry{}
	}
	return tm.tokens[tm.current]
}

// Activate sets the current token's env var so child processes inherit it.
// Returns an error if no tokens are available.
func (tm *TokenManager) Activate() error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if len(tm.tokens) == 0 {
		return fmt.Errorf("no tokens available for backend %q", tm.backend)
	}

	entry := tm.tokens[tm.current]
	return os.Setenv(entry.EnvVar, entry.Key)
}

// Rotate switches to the next token in the pool and activates it.
// Returns the new token entry, or an error if only one token (or none) is available.
func (tm *TokenManager) Rotate() (TokenEntry, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if len(tm.tokens) <= 1 {
		return TokenEntry{}, fmt.Errorf("cannot rotate: only %d token(s) available", len(tm.tokens))
	}

	tm.current = (tm.current + 1) % len(tm.tokens)
	entry := tm.tokens[tm.current]

	if err := os.Setenv(entry.EnvVar, entry.Key); err != nil {
		return TokenEntry{}, fmt.Errorf("activating token %q: %w", entry.Name, err)
	}

	return entry, nil
}

// rateLimitPatterns are substrings checked (case-insensitive) against agent
// output to detect rate limiting. Some patterns require two terms to both
// appear in the same output to avoid false positives.
var rateLimitPatterns = [][]string{
	{"rate limit"},
	{"rate_limit"},
	{"429"},
	{"too many requests"},
	{"quota exceeded"},
	{"capacity", "exceeded"},
	{"retry after"},
	{"retry-after"},
	{"billing", "limit"},
}

// RotateIfRateLimited checks agent output for rate limit signals and
// auto-rotates if detected. Returns true if rotation happened.
func (tm *TokenManager) RotateIfRateLimited(output string) bool {
	if !isRateLimited(output) {
		return false
	}

	// Only rotate if there are multiple tokens
	tm.mu.Lock()
	count := len(tm.tokens)
	tm.mu.Unlock()

	if count <= 1 {
		return false
	}

	_, err := tm.Rotate()
	return err == nil
}

// isRateLimited scans output (case-insensitive) for rate limiting indicators.
func isRateLimited(output string) bool {
	lower := strings.ToLower(output)

	for _, parts := range rateLimitPatterns {
		allFound := true
		for _, part := range parts {
			if !strings.Contains(lower, part) {
				allFound = false
				break
			}
		}
		if allFound {
			return true
		}
	}

	return false
}

// SetByName switches to a specific token by name and activates it.
func (tm *TokenManager) SetByName(name string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	for i, entry := range tm.tokens {
		if entry.Name == name {
			tm.current = i
			return os.Setenv(entry.EnvVar, entry.Key)
		}
	}

	available := make([]string, len(tm.tokens))
	for i, entry := range tm.tokens {
		available[i] = entry.Name
	}
	return fmt.Errorf("token %q not found (available: %s)", name, strings.Join(available, ", "))
}

// List returns all available tokens with keys masked for display.
// Each entry is formatted as: "name (env_var): sk-ant-ap...xYz1"
func (tm *TokenManager) List() []string {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	result := make([]string, len(tm.tokens))
	for i, entry := range tm.tokens {
		marker := "  "
		if i == tm.current {
			marker = "* "
		}
		result[i] = fmt.Sprintf("%s%s (%s): %s", marker, entry.Name, entry.EnvVar, MaskKey(entry.Key))
	}
	return result
}

// Count returns the total number of tokens in the pool.
func (tm *TokenManager) Count() int {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return len(tm.tokens)
}

// MaskKey returns a masked version of a key, showing the first 8 and last 4
// characters with "..." in between. Short keys are returned as-is.
func MaskKey(key string) string {
	if len(key) <= 12 {
		return key
	}
	return key[:8] + "..." + key[len(key)-4:]
}
