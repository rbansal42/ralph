package worker

import (
	"regexp"
	"strconv"
	"sync"
)

// TokenUsage tracks cumulative token consumption for a single entity (worker or global).
type TokenUsage struct {
	mu           sync.RWMutex
	InputTokens  int64
	OutputTokens int64
	TotalTokens  int64
	CostUSD      float64
}

// Add accumulates token counts from a single iteration.
func (t *TokenUsage) Add(input, output int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.InputTokens += input
	t.OutputTokens += output
	t.TotalTokens += input + output
	// Rough cost estimate: $3/M input, $15/M output (Claude Opus ballpark)
	t.CostUSD += float64(input) * 3.0 / 1_000_000
	t.CostUSD += float64(output) * 15.0 / 1_000_000
}

// Snapshot returns a copy of current usage values.
func (t *TokenUsage) Snapshot() (input, output, total int64, cost float64) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.InputTokens, t.OutputTokens, t.TotalTokens, t.CostUSD
}

// Various patterns for extracting token usage from agent output.
// Claude Code outputs: "Total tokens: 12345" or "Input tokens: X, Output tokens: Y"
// OpenCode may output similar patterns.
var tokenPatterns = struct {
	input  *regexp.Regexp
	output *regexp.Regexp
	total  *regexp.Regexp
}{
	// "input_tokens: 1234" / "output_tokens: 5678"
	input:  regexp.MustCompile(`(?i)input[_\s]tokens?:\s*(\d+)`),
	output: regexp.MustCompile(`(?i)output[_\s]tokens?:\s*(\d+)`),
	// "Total tokens: 12345"
	total: regexp.MustCompile(`(?i)total[_\s]tokens?:\s*(\d+)`),
}

// ParseTokenUsage extracts token counts from agent output text.
// Returns input tokens, output tokens parsed from the output.
func ParseTokenUsage(output string) (inputTokens, outputTokens int64) {
	// Try to find input tokens
	if m := tokenPatterns.input.FindStringSubmatch(output); m != nil {
		if n, err := strconv.ParseInt(m[1], 10, 64); err == nil {
			inputTokens = n
		}
	}
	// Try to find output tokens
	if m := tokenPatterns.output.FindStringSubmatch(output); m != nil {
		if n, err := strconv.ParseInt(m[1], 10, 64); err == nil {
			outputTokens = n
		}
	}
	// If we only got total, split roughly 3:1 input:output
	if inputTokens == 0 && outputTokens == 0 {
		if m := tokenPatterns.total.FindStringSubmatch(output); m != nil {
			if n, err := strconv.ParseInt(m[1], 10, 64); err == nil {
				inputTokens = n * 3 / 4
				outputTokens = n / 4
			}
		}
	}
	return
}
