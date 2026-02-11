package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	cfgFile string
	verbose bool
)

var rootCmd = &cobra.Command{
	Use:           "ralph",
	Short:         "Parallel Coding Agent Runner",
	Long:          "Ralph orchestrates multiple AI coding agents in parallel, each working on a subset of checklist items.",
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "ralph.toml", "config file path")
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "enable verbose output")

	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(authCmd)
	rootCmd.AddCommand(generateCmd)
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
