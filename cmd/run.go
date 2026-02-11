package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/rahulvramesh/ralph/backend"
	"github.com/rahulvramesh/ralph/config"
	"github.com/rahulvramesh/ralph/permission"
	"github.com/rahulvramesh/ralph/state"
	"github.com/rahulvramesh/ralph/ui"
	"github.com/rahulvramesh/ralph/worker"
	"github.com/spf13/cobra"
)

var (
	flagModel   string
	flagWorker  int
	flagDryRun  bool
	flagBackend string
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run workers to process checklist items",
	Long:  "Launches AI coding agent workers in parallel, each processing checklist items matching their pattern.",
	RunE:  runRun,
}

func init() {
	runCmd.Flags().StringVar(&flagModel, "model", "", "override model from config")
	runCmd.Flags().IntVar(&flagWorker, "worker", 0, "run only worker N (1-based)")
	runCmd.Flags().BoolVar(&flagDryRun, "dry-run", false, "show what each worker would do without executing")
	runCmd.Flags().StringVar(&flagBackend, "backend", "", "override backend from config")
}

func runRun(cmd *cobra.Command, args []string) error {
	// 1. Load config
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// 2. Apply flag overrides
	if flagModel != "" {
		cfg.Model = config.ResolveModelAlias(flagModel)
	}
	if flagBackend != "" {
		cfg.Backend = flagBackend
	}

	// 3. Create backend
	b, err := backend.New(cfg.Backend)
	if err != nil {
		return fmt.Errorf("creating backend: %w", err)
	}

	// 4. Print banner
	ui.PrintBanner()

	// 5. Print initial status
	workerInfos := buildWorkerInfos(cfg)
	ui.PrintStatus(cfg.Checklist, workerInfos)

	// 6. Dry-run mode
	if flagDryRun {
		fmt.Println()
		fmt.Println("--- DRY RUN ---")
		for i, wc := range cfg.Workers {
			pending, err := worker.GetPending(cfg.Checklist, wc.Pattern)
			if err != nil {
				fmt.Fprintf(os.Stderr, "W%d %s: error reading checklist: %v\n", i+1, wc.Name, err)
				continue
			}
			fmt.Printf("\nW%d %s (%d items):\n", i+1, wc.Name, len(pending))
			limit := 3
			if len(pending) < limit {
				limit = len(pending)
			}
			for j := 0; j < limit; j++ {
				fmt.Printf("  - %s\n", pending[j].Path)
			}
			if len(pending) > 3 {
				fmt.Printf("  ... and %d more\n", len(pending)-3)
			}
		}
		return nil
	}

	// 7. Set up permissions (auto-create opencode.json if needed)
	fmt.Print("Setting up permissions... ")
	if err := permission.EnsurePermissions(cfg); err != nil {
		fmt.Println("WARNING:", err)
	} else {
		fmt.Println("OK")
		if cfg.Backend == "opencode" {
			if len(cfg.ExternalDirs) > 0 {
				fmt.Printf("  External dirs allowed: %s\n", strings.Join(cfg.ExternalDirs, ", "))
			}
		}
	}

	// 8. Set up token manager
	var tokenEntries []permission.TokenEntry
	for _, tc := range cfg.Tokens {
		b := tc.Backend
		if b == "" {
			b = cfg.Backend
		}
		if b == cfg.Backend {
			tokenEntries = append(tokenEntries, permission.TokenEntry{
				Name:   tc.Name,
				Key:    tc.Key,
				EnvVar: permission.ResolveEnvVarForModel(cfg.Backend, cfg.Model, tc.Key),
			})
		}
	}
	tokenMgr := permission.NewTokenManager(cfg.Backend, tokenEntries)

	// Activate preferred token (from .ralph-token) or first available
	if tokenMgr.Count() > 0 {
		if preferred := loadActiveToken(); preferred != "" {
			if err := tokenMgr.SetByName(preferred); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: preferred token %q not found, using first: %v\n", preferred, err)
				tokenMgr.Activate()
			}
		} else {
			tokenMgr.Activate()
		}
		fmt.Printf("Token pool: %d token(s) available\n", tokenMgr.Count())
		for _, line := range tokenMgr.List() {
			fmt.Printf("  %s\n", line)
		}
	}

	// 9. Check auth
	ctx := context.Background()
	if err := b.CheckAuth(ctx, cfg.Model); err != nil {
		fmt.Fprintf(os.Stderr, "\nAuth check failed: %v\n\n", err)
		fmt.Println(b.AuthGuide())

		fmt.Print("Paste your token: ")
		reader := bufio.NewReader(os.Stdin)
		token, readErr := reader.ReadString('\n')
		if readErr != nil {
			return fmt.Errorf("reading token: %w", readErr)
		}
		token = strings.TrimSpace(token)

		envVar := permission.ResolveEnvVarForModel(cfg.Backend, cfg.Model, token)
		os.Setenv(envVar, token)

		if err := b.CheckAuth(ctx, cfg.Model); err != nil {
			return fmt.Errorf("auth still failing after token: %w", err)
		}
		fmt.Println("Auth successful!")
	}

	// 10. Load state
	st, err := state.Load(cfg.StateFile)
	if err != nil {
		return fmt.Errorf("loading state: %w", err)
	}

	// 9. Create log dir
	if err := os.MkdirAll("logs/ralph", 0755); err != nil {
		return fmt.Errorf("creating log directory: %w", err)
	}

	// 10. Shared mutexes
	var gitMutex sync.Mutex
	var stateMutex sync.Mutex

	// 11. Shutdown flag
	var shutdown atomic.Bool

	// 12. Signal handler
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nShutdown requested, finishing current iterations...")
		shutdown.Store(true)
		<-sigCh
		fmt.Println("\nForced exit.")
		os.Exit(1)
	}()

	// 12b. Hotkey listener for live token switching
	if tokenMgr.Count() > 1 {
		fmt.Printf("\nHotkeys: [t] rotate token | [s] status\n\n")
		go listenHotkeys(tokenMgr, cfg, &shutdown)
	}

	// Permission handler — called when a worker detects a permission block.
	// Prompts the user, adds the directory to opencode.json, and returns true if resolved.
	permHandler := func(output string) bool {
		blockedPath := permission.DetectPermissionBlock(output)
		if blockedPath == "" {
			return false
		}

		fmt.Printf("\n  Permission block detected for: %s\n", blockedPath)
		fmt.Print("  Allow access to this directory? [Y/n]: ")
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))

		if answer == "" || answer == "y" || answer == "yes" {
			workdir := cfg.Workdir
			if workdir == "" {
				workdir = "."
			}
			if err := permission.AddExternalDir(workdir, blockedPath); err != nil {
				fmt.Fprintf(os.Stderr, "  Failed to update permissions: %v\n", err)
				return false
			}
			fmt.Printf("  Added %s to opencode.json — will retry\n", blockedPath)
			return true
		}

		fmt.Println("  Denied — worker will continue without access")
		return false
	}

	// Build workers
	workers := make([]*worker.Worker, 0, len(cfg.Workers))
	for i, wc := range cfg.Workers {
		w := &worker.Worker{
			Num:          i + 1,
			Name:         wc.Name,
			Pattern:      wc.Pattern,
			TotalWorkers: len(cfg.Workers),
			Backend:      b,
			Config:       cfg,
			State:        st,
			GitMutex:     &gitMutex,
			StateMutex:   &stateMutex,
			Shutdown:     &shutdown,
			TokenManager: tokenMgr,
			PermHandler:  permHandler,
		}
		workers = append(workers, w)
	}

	// 13. Run workers
	if flagWorker > 0 {
		if flagWorker > len(workers) {
			return fmt.Errorf("worker %d does not exist (have %d workers)", flagWorker, len(workers))
		}
		w := workers[flagWorker-1]
		fmt.Printf("\nRunning single worker: W%d %s\n\n", w.Num, w.Name)
		if err := w.Run(ctx); err != nil {
			return fmt.Errorf("worker %d failed: %w", flagWorker, err)
		}
	} else {
		var wg sync.WaitGroup

		for _, w := range workers {
			wg.Add(1)
			go func(w *worker.Worker) {
				defer wg.Done()
				if err := w.Run(ctx); err != nil {
					fmt.Fprintf(os.Stderr, "[W%d %s] Error: %v\n", w.Num, w.Name, err)
				}
			}(w)
		}

		// 14. Progress ticker
		ticker := time.NewTicker(60 * time.Second)
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		go func() {
			for {
				select {
				case <-ticker.C:
					infos := buildLiveWorkerInfos(cfg, workers)
					fmt.Println()
					ui.PrintStatus(cfg.Checklist, infos)
				case <-done:
					ticker.Stop()
					return
				}
			}
		}()

		// 15. Wait for all workers
		<-done
	}

	// 16. Final status
	fmt.Println()
	finalInfos := buildLiveWorkerInfos(cfg, workers)
	ui.PrintStatus(cfg.Checklist, finalInfos)
	fmt.Println("\nAll workers finished.")

	return nil
}

// buildWorkerInfos creates WorkerInfo slices from config for initial display.
func buildWorkerInfos(cfg *config.Config) []ui.WorkerInfo {
	infos := make([]ui.WorkerInfo, 0, len(cfg.Workers))
	for i, wc := range cfg.Workers {
		remaining, _ := worker.CountPending(cfg.Checklist, wc.Pattern)
		infos = append(infos, ui.WorkerInfo{
			Num:       i + 1,
			Name:      wc.Name,
			Pattern:   wc.Pattern,
			Remaining: remaining,
			Status:    "waiting",
		})
	}
	return infos
}

// buildLiveWorkerInfos creates WorkerInfo slices from live worker state.
func buildLiveWorkerInfos(cfg *config.Config, workers []*worker.Worker) []ui.WorkerInfo {
	infos := make([]ui.WorkerInfo, 0, len(workers))
	for _, w := range workers {
		remaining, _ := worker.CountPending(cfg.Checklist, w.Pattern)
		infos = append(infos, ui.WorkerInfo{
			Num:       w.Num,
			Name:      w.Name,
			Pattern:   w.Pattern,
			Remaining: remaining,
			Status:    w.GetStatus(),
		})
	}
	return infos
}

// listenHotkeys reads single keypresses from stdin for live token switching.
// Runs as a goroutine during ralph run.
func listenHotkeys(tokenMgr *permission.TokenManager, cfg *config.Config, shutdown *atomic.Bool) {
	// Set terminal to raw mode to read single characters
	// Save old state and restore on exit
	fd := int(os.Stdin.Fd())
	oldState, err := makeRaw(fd)
	if err != nil {
		// Can't set raw mode (piped stdin, not a terminal) — skip hotkeys silently
		return
	}
	defer restoreTerminal(fd, oldState)

	buf := make([]byte, 1)
	for {
		if shutdown.Load() {
			return
		}

		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			return
		}

		switch buf[0] {
		case 't':
			// Rotate to next token
			entry, err := tokenMgr.Rotate()
			if err != nil {
				fmt.Printf("\n[ralph] Token rotate failed: %v\n", err)
			} else {
				fmt.Printf("\n[ralph] Switched to token: %s (%s)\n", entry.Name, permission.MaskKey(entry.Key))
				fmt.Println("[ralph] New iterations will use this token")
			}

		case 's':
			// Print status
			fmt.Println()
			fmt.Println("[ralph] Token pool:")
			for _, line := range tokenMgr.List() {
				fmt.Printf("  %s\n", line)
			}

		case 3: // Ctrl+C
			// Let the signal handler deal with it
			return
		}
	}
}
