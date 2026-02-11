package permission

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rahulvramesh/ralph/config"
)

// opencodeConfig represents the opencode.json configuration structure.
type opencodeConfig struct {
	Schema     string      `json:"$schema"`
	Permission interface{} `json:"permission"`
}

// opencodePermission is the detailed permission object.
type opencodePermission struct {
	Wildcard    string            `json:"*"`
	ExternalDir map[string]string `json:"external_directory"`
	Read        interface{}       `json:"read,omitempty"`
}

// EnsureOpenCodePermissions creates or updates opencode.json in the workdir
// to allow all tool operations and external directory access.
// It preserves any existing config and only adds/updates what's needed.
func EnsureOpenCodePermissions(cfg *config.Config) error {
	workdir := cfg.Workdir
	if workdir == "" {
		workdir = "."
	}
	configPath := filepath.Join(workdir, "opencode.json")

	// Read existing config if present
	existing := make(map[string]interface{})
	if data, err := os.ReadFile(configPath); err == nil {
		if jsonErr := json.Unmarshal(data, &existing); jsonErr != nil {
			return fmt.Errorf("parsing existing opencode.json: %w", jsonErr)
		}
	}

	// Build the permission object
	perm := buildPermissions(cfg, existing)

	// Set schema and permission
	existing["$schema"] = "https://opencode.ai/config.json"
	existing["permission"] = perm

	// Write back
	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling opencode.json: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("writing opencode.json: %w", err)
	}

	return nil
}

// buildPermissions constructs the permission object, merging with existing config.
func buildPermissions(cfg *config.Config, existing map[string]interface{}) map[string]interface{} {
	perm := make(map[string]interface{})

	// Start from existing permissions if present
	if existingPerm, ok := existing["permission"]; ok {
		if permMap, ok := existingPerm.(map[string]interface{}); ok {
			for k, v := range permMap {
				perm[k] = v
			}
		}
	}

	// Ensure wildcard allow for all tools
	perm["*"] = "allow"

	// Build external_directory rules
	extDir := make(map[string]string)

	// Preserve existing external_directory rules
	if existingExt, ok := perm["external_directory"]; ok {
		if extMap, ok := existingExt.(map[string]interface{}); ok {
			for k, v := range extMap {
				if vs, ok := v.(string); ok {
					extDir[k] = vs
				}
			}
		}
	}

	// Add configured external dirs
	for _, dir := range cfg.ExternalDirs {
		// Expand ~ to home dir
		if strings.HasPrefix(dir, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				dir = filepath.Join(home, dir[2:])
			}
		}
		// Ensure trailing /** for glob matching
		pattern := dir
		if !strings.HasSuffix(pattern, "/**") && !strings.HasSuffix(pattern, "/*") {
			pattern = strings.TrimRight(pattern, "/") + "/**"
		}
		extDir[pattern] = "allow"
	}

	// Always allow wildcard as fallback (last rule wins in opencode)
	extDir["*"] = "allow"

	perm["external_directory"] = extDir

	return perm
}

// AddExternalDir adds a directory to the opencode.json external_directory
// allow list. This is called at runtime when the agent requests access to
// a directory the user approves.
func AddExternalDir(workdir string, dir string) error {
	configPath := filepath.Join(workdir, "opencode.json")

	existing := make(map[string]interface{})
	if data, err := os.ReadFile(configPath); err == nil {
		if jsonErr := json.Unmarshal(data, &existing); jsonErr != nil {
			return fmt.Errorf("parsing opencode.json: %w", jsonErr)
		}
	}

	// Get or create permission map
	perm := make(map[string]interface{})
	if existingPerm, ok := existing["permission"]; ok {
		if permMap, ok := existingPerm.(map[string]interface{}); ok {
			perm = permMap
		}
	}

	// Get or create external_directory map
	extDir := make(map[string]string)
	if existingExt, ok := perm["external_directory"]; ok {
		if extMap, ok := existingExt.(map[string]interface{}); ok {
			for k, v := range extMap {
				if vs, ok := v.(string); ok {
					extDir[k] = vs
				}
			}
		}
	}

	// Add the new directory
	pattern := strings.TrimRight(dir, "/") + "/**"
	extDir[pattern] = "allow"

	perm["external_directory"] = extDir
	existing["$schema"] = "https://opencode.ai/config.json"
	existing["permission"] = perm

	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling opencode.json: %w", err)
	}
	data = append(data, '\n')

	return os.WriteFile(configPath, data, 0644)
}

// DetectPermissionBlock scans agent output for permission denial/ask patterns.
// Returns the blocked path if found, or empty string if no permission issue detected.
//
// opencode permission blocks look like:
//   - "Permission denied" or "permission required" in output
//   - "external_directory" mentions
//   - "approve" or "allow" prompts that timed out
func DetectPermissionBlock(output string) string {
	lower := strings.ToLower(output)

	// Patterns that indicate a permission block
	blockPatterns := []string{
		"permission denied",
		"permission required",
		"external_directory",
		"access denied",
		"not allowed to read",
		"not allowed to access",
		"outside the project",
		"outside of the project",
		"cannot access",
		"approve access",
	}

	for _, pattern := range blockPatterns {
		if strings.Contains(lower, pattern) {
			// Try to extract the blocked path
			return extractBlockedPath(output)
		}
	}

	return ""
}

// extractBlockedPath attempts to pull a directory path from a permission error message.
func extractBlockedPath(output string) string {
	// Look for common path patterns in the output
	// Paths typically start with / and contain no spaces before the next separator
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		lower := strings.ToLower(line)
		if !strings.Contains(lower, "permission") &&
			!strings.Contains(lower, "denied") &&
			!strings.Contains(lower, "external") &&
			!strings.Contains(lower, "access") &&
			!strings.Contains(lower, "outside") {
			continue
		}

		// Find path-like strings in this line
		words := strings.Fields(line)
		for _, word := range words {
			// Clean up quotes and punctuation
			word = strings.Trim(word, "\"'`(),;:")
			if strings.HasPrefix(word, "/") && len(word) > 1 {
				// Looks like an absolute path — return the directory portion
				if info, err := os.Stat(word); err == nil && info.IsDir() {
					return word
				}
				// Return parent dir if it exists
				dir := filepath.Dir(word)
				if _, err := os.Stat(dir); err == nil {
					return dir
				}
				// Return the path anyway — user can confirm
				return word
			}
		}
	}

	// Couldn't extract a specific path
	return ""
}

// EnsureClaudePermissions is a no-op for claude backend since
// --permission-mode bypassPermissions handles everything.
func EnsureClaudePermissions(_ *config.Config) error {
	return nil
}

// EnsurePermissions dispatches to the right handler based on backend name.
func EnsurePermissions(cfg *config.Config) error {
	switch cfg.Backend {
	case "opencode":
		return EnsureOpenCodePermissions(cfg)
	case "claude":
		return EnsureClaudePermissions(cfg)
	default:
		return nil
	}
}
