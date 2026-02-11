package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/rahulvramesh/ralph/config"
	"github.com/rahulvramesh/ralph/permission"
	"github.com/spf13/cobra"
)

var tokenCmd = &cobra.Command{
	Use:   "token",
	Short: "Manage API tokens",
	Long:  "List, switch, and manage API tokens in the token pool.",
}

var tokenListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all configured tokens",
	RunE:  runTokenList,
}

var tokenSwitchCmd = &cobra.Command{
	Use:   "switch [name]",
	Short: "Switch to a specific token by name",
	Args:  cobra.ExactArgs(1),
	RunE:  runTokenSwitch,
}

var tokenAddCmd = &cobra.Command{
	Use:   "add [name] [key]",
	Short: "Add a token to the current session (not persisted to config)",
	Long: `Add a token to the current session. The token is not persisted to ralph.toml.

  ralph token add                  Interactive — prompts for key and name
  ralph token add <key>            Auto-names the token (token-1, token-2, ...)
  ralph token add <name> <key>     Explicit name and key`,
	Args: cobra.MaximumNArgs(2),
	RunE: runTokenAdd,
}

func init() {
	tokenCmd.AddCommand(tokenListCmd)
	tokenCmd.AddCommand(tokenSwitchCmd)
	tokenCmd.AddCommand(tokenAddCmd)
}

func buildTokenManager(cfg *config.Config) *permission.TokenManager {
	var entries []permission.TokenEntry
	for _, tc := range cfg.Tokens {
		b := tc.Backend
		if b == "" {
			b = cfg.Backend
		}
		if b == cfg.Backend {
			entries = append(entries, permission.TokenEntry{
				Name:   tc.Name,
				Key:    tc.Key,
				EnvVar: permission.ResolveEnvVar(cfg.Backend, tc.Key),
			})
		}
	}
	return permission.NewTokenManager(cfg.Backend, entries)
}

func runTokenList(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	tm := buildTokenManager(cfg)

	if tm.Count() == 0 {
		fmt.Println("No tokens configured.")
		fmt.Println()
		fmt.Println("Add tokens to ralph.toml:")
		fmt.Println()
		fmt.Println("  [[token]]")
		fmt.Println("  name = \"personal\"")
		fmt.Println("  key = \"sk-ant-api03-...\"")
		fmt.Println()
		fmt.Println("  [[token]]")
		fmt.Println("  name = \"work\"")
		fmt.Println("  key = \"sk-ant-api03-...\"")
		return nil
	}

	fmt.Printf("Token pool (%d tokens, backend: %s):\n", tm.Count(), cfg.Backend)
	for _, line := range tm.List() {
		fmt.Printf("  %s\n", line)
	}

	return nil
}

func runTokenSwitch(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	tm := buildTokenManager(cfg)
	name := args[0]

	if err := tm.SetByName(name); err != nil {
		return err
	}

	cur := tm.Current()
	fmt.Printf("Switched to token %q (%s)\n", cur.Name, cur.EnvVar)
	fmt.Printf("Set %s=%s\n", cur.EnvVar, permission.MaskKey(cur.Key))

	return nil
}

func runTokenAdd(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	var name, key string
	reader := bufio.NewReader(os.Stdin)

	switch len(args) {
	case 0:
		// Interactive mode
		fmt.Print("API key: ")
		// Read key — we read with hidden input if terminal, otherwise plain
		fd := int(os.Stdin.Fd())
		oldState, rawErr := makeRaw(fd)
		if rawErr == nil {
			// Read hidden input (no echo)
			var keyBytes []byte
			for {
				buf := make([]byte, 1)
				n, err := os.Stdin.Read(buf)
				if err != nil || n == 0 {
					break
				}
				if buf[0] == '\r' || buf[0] == '\n' {
					break
				}
				if buf[0] == 127 || buf[0] == 8 { // backspace
					if len(keyBytes) > 0 {
						keyBytes = keyBytes[:len(keyBytes)-1]
					}
					continue
				}
				if buf[0] == 3 { // Ctrl+C
					restoreTerminal(fd, oldState)
					fmt.Println()
					return fmt.Errorf("cancelled")
				}
				keyBytes = append(keyBytes, buf[0])
			}
			restoreTerminal(fd, oldState)
			fmt.Println() // newline after hidden input
			key = string(keyBytes)
		} else {
			// Not a terminal — read line normally
			line, readErr := reader.ReadString('\n')
			if readErr != nil {
				return fmt.Errorf("reading key: %w", readErr)
			}
			key = strings.TrimSpace(line)
		}

		if key == "" {
			return fmt.Errorf("no key provided")
		}

		// Ask for name (optional)
		defaultName := fmt.Sprintf("token-%d", len(cfg.Tokens)+1)
		fmt.Printf("Name [%s]: ", defaultName)
		line, _ := reader.ReadString('\n')
		name = strings.TrimSpace(line)
		if name == "" {
			name = defaultName
		}

	case 1:
		// Just key — auto-name
		key = args[0]
		name = fmt.Sprintf("token-%d", len(cfg.Tokens)+1)

	case 2:
		// name + key (original behavior)
		name = args[0]
		key = args[1]
	}

	envVar := permission.ResolveEnvVar(cfg.Backend, key)
	if err := os.Setenv(envVar, key); err != nil {
		return fmt.Errorf("setting env var: %w", err)
	}

	fmt.Printf("Added token %q for this session\n", name)
	fmt.Printf("Set %s=%s\n", envVar, permission.MaskKey(key))
	fmt.Println()
	fmt.Println("To persist, add to ralph.toml:")
	fmt.Println()
	fmt.Printf("  [[token]]\n")
	fmt.Printf("  name = %q\n", name)
	fmt.Printf("  key = %q\n", key)

	return nil
}
