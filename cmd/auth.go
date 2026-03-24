package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rahulvramesh/ralph/backend"
	"github.com/rahulvramesh/ralph/config"
	"github.com/rahulvramesh/ralph/permission"
	"github.com/spf13/cobra"
)

var (
	authBackend string
	authModel   string
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Test authentication with the configured backend",
	RunE:  runAuth,
}

func init() {
	authCmd.Flags().StringVar(&authBackend, "backend", "", "override backend (default from config)")
	authCmd.Flags().StringVar(&authModel, "model", "", "override model (default from config)")
}

func runAuth(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Apply flag overrides.
	backendName := cfg.Backend
	if authBackend != "" {
		backendName = authBackend
	}
	model := cfg.Model
	if authModel != "" {
		model = config.ResolveModelAlias(authModel)
	}

	b, err := backend.New(backendName, nil)
	if err != nil {
		return fmt.Errorf("creating backend: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Printf("Testing authentication with %s (model: %s)...\n", backendName, model)

	if err := b.CheckAuth(ctx, model); err == nil {
		fmt.Printf("\033[32mAuthentication successful!\033[0m\n")
		return nil
	}

	fmt.Printf("\033[31mAuthentication failed.\033[0m\n\n")
	fmt.Println(b.AuthGuide())

	// Prompt for token.
	fmt.Print("\nPaste your token (or press Enter to skip): ")
	reader := bufio.NewReader(os.Stdin)
	token, _ := reader.ReadString('\n')
	token = strings.TrimSpace(token)

	if token == "" {
		return fmt.Errorf("no token provided, authentication not configured")
	}

	// Determine env var from backend/model.
	envVar := permission.ResolveEnvVarForModel(backendName, model, token)

	// Set env var and retry.
	os.Setenv(envVar, token)

	ctx2, cancel2 := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel2()

	if err := b.CheckAuth(ctx2, model); err != nil {
		fmt.Printf("\033[31mToken verification failed: %v\033[0m\n", err)
		return fmt.Errorf("authentication failed with provided token")
	}

	fmt.Printf("\033[32mToken works!\033[0m Set this in your shell:\n\n")
	fmt.Printf("  export %s=%s\n\n", envVar, token)

	return nil
}
