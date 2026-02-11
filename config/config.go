package config

import (
	"fmt"
	"os"
	"time"

	"github.com/BurntSushi/toml"
)

// modelAliases maps short model names to their fully qualified identifiers.
var modelAliases = map[string]string{
	"opus":          "anthropic/claude-opus-4-6",
	"sonnet":        "anthropic/claude-sonnet-4-5",
	"gpt5":          "openai/gpt-5",
	"gpt-5.3-codex": "openai/gpt-5.3-codex",
	"o3":            "openai/o3",
	"o4mini":        "openai/o4-mini",
}

// ResolveModelAlias maps friendly model aliases to full model IDs.
// Unknown values are returned unchanged.
func ResolveModelAlias(model string) string {
	if resolved, ok := modelAliases[model]; ok {
		return resolved
	}
	return model
}

// TokenConfig defines an API token entry for the token pool.
type TokenConfig struct {
	Name    string `toml:"name"`
	Key     string `toml:"key"`
	Backend string `toml:"backend"` // "opencode" or "claude"; empty = applies to configured backend
}

// WorkerConfig defines a named worker with a file-matching pattern.
type WorkerConfig struct {
	Name    string `toml:"name"`
	Pattern string `toml:"pattern"`
}

// Config holds the parsed ralph.toml configuration.
type Config struct {
	Backend           string         `toml:"backend"`
	Checklist         string         `toml:"checklist"`
	Prompt            string         `toml:"prompt"`
	Model             string         `toml:"model"`
	CooldownRaw       string         `toml:"cooldown"`
	Cooldown          time.Duration  `toml:"-"`
	MaxIterations     int            `toml:"max_iterations"`
	ItemsPerIteration int            `toml:"items_per_iteration"`
	StateFile         string         `toml:"state_file"`
	Workdir           string         `toml:"workdir"`
	ExternalDirs      []string       `toml:"external_dirs"`
	Tokens            []TokenConfig  `toml:"token"`
	Workers           []WorkerConfig `toml:"worker"`
}

// Load reads a ralph.toml file at path and returns a validated Config.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	cfg := &Config{
		Backend:           "opencode",
		CooldownRaw:       "10s",
		MaxIterations:     80,
		ItemsPerIteration: 5,
		StateFile:         "ralph_state.json",
	}

	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	cfg.Model = ResolveModelAlias(cfg.Model)

	dur, err := time.ParseDuration(cfg.CooldownRaw)
	if err != nil {
		return nil, fmt.Errorf("parsing cooldown %q: %w", cfg.CooldownRaw, err)
	}
	cfg.Cooldown = dur

	if err := validate(cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}

// AppendToken adds a [[token]] entry to the ralph.toml file at path.
// It appends to the end of the file so existing content is untouched.
func AppendToken(path string, name, key string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("opening config file: %w", err)
	}
	defer f.Close()

	entry := fmt.Sprintf("\n[[token]]\nname = %q\nkey = %q\n", name, key)
	if _, err := f.WriteString(entry); err != nil {
		return fmt.Errorf("writing token entry: %w", err)
	}

	return nil
}

// validate checks that cfg satisfies all required constraints.
func validate(cfg *Config) error {
	if cfg.Backend != "opencode" && cfg.Backend != "claude" {
		return fmt.Errorf("backend must be \"opencode\" or \"claude\", got %q", cfg.Backend)
	}

	if cfg.Checklist == "" {
		return fmt.Errorf("checklist must be set")
	}

	if cfg.Prompt == "" {
		return fmt.Errorf("prompt must be set")
	}

	if len(cfg.Workers) == 0 {
		return fmt.Errorf("at least one [[worker]] must be defined")
	}

	for i, w := range cfg.Workers {
		if w.Name == "" {
			return fmt.Errorf("worker[%d]: name must be set", i)
		}
		if w.Pattern == "" {
			return fmt.Errorf("worker[%d] (%s): pattern must be set", i, w.Name)
		}
	}

	return nil
}
