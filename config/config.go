package config

import (
	"fmt"
	"os"
	"strconv"
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

// NotifyConfig defines notification settings.
type NotifyConfig struct {
	TelegramBotToken string   `toml:"telegram_bot_token"`
	TelegramChatID   string   `toml:"telegram_chat_id"`
	NotifyOn         []string `toml:"notify_on"` // "start", "complete", "error", "stall"
}

// WorkerConfig defines a named worker with a file-matching pattern.
type WorkerConfig struct {
	Name    string `toml:"name"`
	Pattern string `toml:"pattern"`
}

// Config holds the parsed ralph.toml configuration.
type Config struct {
	Backend                string         `toml:"backend"`
	Checklist              string         `toml:"checklist"`
	Prompt                 string         `toml:"prompt"`
	Model                  string         `toml:"model"`
	CooldownRaw            string         `toml:"cooldown"`
	Cooldown               time.Duration  `toml:"-"`
	MaxRetries             int            `toml:"max_retries"`
	RetryDelayRaw          string         `toml:"retry_delay"`
	RetryDelay             time.Duration  `toml:"-"`
	MaxIterations          int            `toml:"max_iterations"`
	ItemsPerIteration      int            `toml:"items_per_iteration"`
	Concurrency            int            `toml:"concurrency"`
	MaxStaleIterations     int            `toml:"max_stale_iterations"`
	AutoApprovePermissions bool           `toml:"auto_approve_permissions"`
	StateFile              string         `toml:"state_file"`
	Workdir                string         `toml:"workdir"`
	ExternalDirs           []string       `toml:"external_dirs"`
	Tokens                 []TokenConfig  `toml:"token"`
	Workers                []WorkerConfig `toml:"worker"`
	BudgetLimitRaw         string         `toml:"budget_limit"`       // optional, e.g. "50.00" USD
	BudgetLimit            float64        `toml:"-"`                  // parsed budget limit in USD (0 = no limit)
	BatchMode              string         `toml:"batch_mode"`         // "fixed" or "smart"
	ComplexityBudget       int            `toml:"complexity_budget"`  // target complexity per iteration (smart mode)
	ParallelSubagents      bool           `toml:"parallel_subagents"` // hint agent to use subagents for parallel item processing
	WorkerParallelism      int            `toml:"worker_parallelism"`
	ParentResetAfterRuns   int            `toml:"parent_reset_after_runs"`
	CommitBatchSize        int            `toml:"commit_batch_size"`
	Notify                 NotifyConfig   `toml:"notify"`
}

// Load reads a ralph.toml file at path and returns a validated Config.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	cfg := &Config{
		Backend:                "opencode",
		CooldownRaw:            "10s",
		MaxRetries:             2,
		RetryDelayRaw:          "5s",
		MaxIterations:          80,
		ItemsPerIteration:      5,
		Concurrency:            0,    // 0 = all workers in parallel (default)
		MaxStaleIterations:     3,    // stop worker after 3 consecutive iterations with 0 completions (0 = disabled)
		AutoApprovePermissions: true, // auto-approve permission blocks by default
		StateFile:              "ralph_state.json",
		BatchMode:              "fixed",
		ComplexityBudget:       500,
		WorkerParallelism:      2,
		ParentResetAfterRuns:   25,
		CommitBatchSize:        3,
	}

	md, err := toml.Decode(string(data), cfg)
	if err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	cfg.Model = ResolveModelAlias(cfg.Model)

	// Default parallel_subagents to true when using opencode backend,
	// unless explicitly set in the config file.
	if !md.IsDefined("parallel_subagents") {
		cfg.ParallelSubagents = (cfg.Backend == "opencode")
	}

	dur, err := time.ParseDuration(cfg.CooldownRaw)
	if err != nil {
		return nil, fmt.Errorf("parsing cooldown %q: %w", cfg.CooldownRaw, err)
	}
	cfg.Cooldown = dur

	retryDur, err := time.ParseDuration(cfg.RetryDelayRaw)
	if err != nil {
		return nil, fmt.Errorf("parsing retry_delay %q: %w", cfg.RetryDelayRaw, err)
	}
	cfg.RetryDelay = retryDur

	if cfg.BudgetLimitRaw != "" {
		budget, err := strconv.ParseFloat(cfg.BudgetLimitRaw, 64)
		if err != nil {
			return nil, fmt.Errorf("parsing budget_limit %q: must be a number (e.g. \"50.00\"): %w", cfg.BudgetLimitRaw, err)
		}
		if budget < 0 {
			return nil, fmt.Errorf("budget_limit must be >= 0, got %q", cfg.BudgetLimitRaw)
		}
		cfg.BudgetLimit = budget
	}

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

	if cfg.MaxRetries < 0 {
		return fmt.Errorf("max_retries must be >= 0, got %d", cfg.MaxRetries)
	}

	if cfg.Concurrency < 0 {
		return fmt.Errorf("concurrency must be >= 0 (0 = all workers in parallel), got %d", cfg.Concurrency)
	}

	if cfg.MaxStaleIterations < 0 {
		return fmt.Errorf("max_stale_iterations must be >= 0 (0 = disabled), got %d", cfg.MaxStaleIterations)
	}

	if cfg.BatchMode != "fixed" && cfg.BatchMode != "smart" {
		return fmt.Errorf("batch_mode must be \"fixed\" or \"smart\", got %q", cfg.BatchMode)
	}

	if cfg.ComplexityBudget <= 0 {
		return fmt.Errorf("complexity_budget must be > 0, got %d", cfg.ComplexityBudget)
	}

	if cfg.WorkerParallelism <= 0 {
		return fmt.Errorf("worker_parallelism must be > 0, got %d", cfg.WorkerParallelism)
	}

	if cfg.ParentResetAfterRuns <= 0 {
		return fmt.Errorf("parent_reset_after_runs must be > 0, got %d", cfg.ParentResetAfterRuns)
	}

	if cfg.CommitBatchSize <= 0 {
		return fmt.Errorf("commit_batch_size must be > 0, got %d", cfg.CommitBatchSize)
	}

	return nil
}
