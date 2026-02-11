package cmd

import (
	"fmt"
	"os"

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
	Args:  cobra.ExactArgs(2),
	RunE:  runTokenAdd,
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

	name := args[0]
	key := args[1]

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
