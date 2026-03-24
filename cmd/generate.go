package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rahulvramesh/ralph/backend"
	"github.com/spf13/cobra"
)

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Interactive setup wizard to create ralph.toml, checklist, and prompt files",
	RunE:  runGenerate,
}

// promptString asks for a string value with a default.
func promptString(reader *bufio.Reader, prompt string, defaultVal string) string {
	if defaultVal != "" {
		fmt.Printf("%s [%s]: ", prompt, defaultVal)
	} else {
		fmt.Printf("%s: ", prompt)
	}
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultVal
	}
	return line
}

// promptChoice asks the user to pick from a list of choices.
func promptChoice(reader *bufio.Reader, prompt string, choices []string, defaultVal string) string {
	fmt.Printf("%s (%s) [%s]: ", prompt, strings.Join(choices, ", "), defaultVal)
	for {
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			return defaultVal
		}
		for _, c := range choices {
			if strings.EqualFold(line, c) {
				return c
			}
		}
		fmt.Printf("  Please choose one of: %s [%s]: ", strings.Join(choices, ", "), defaultVal)
	}
}

// promptInt asks for an integer value with a default.
func promptInt(reader *bufio.Reader, prompt string, defaultVal int) int {
	for {
		fmt.Printf("%s [%d]: ", prompt, defaultVal)
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			return defaultVal
		}
		n, err := strconv.Atoi(line)
		if err != nil {
			fmt.Println("  Please enter a valid number.")
			continue
		}
		return n
	}
}

// promptYesNo asks a yes/no question with a default.
func promptYesNo(reader *bufio.Reader, prompt string, defaultVal bool) bool {
	defStr := "y/N"
	if defaultVal {
		defStr = "Y/n"
	}
	for {
		fmt.Printf("%s [%s]: ", prompt, defStr)
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(strings.ToLower(line))
		if line == "" {
			return defaultVal
		}
		switch line {
		case "y", "yes":
			return true
		case "n", "no":
			return false
		default:
			fmt.Println("  Please answer y or n.")
		}
	}
}

func runGenerate(cmd *cobra.Command, args []string) error {
	reader := bufio.NewReader(os.Stdin)

	// 1. Header
	fmt.Println("Ralph — Setup Wizard")
	fmt.Println()
	fmt.Println("Let's configure your project.")
	fmt.Println()

	// 2. Backend
	backendName := promptChoice(reader, "Which coding agent?", []string{"opencode", "claude"}, "opencode")

	// 3. Model
	model := promptString(reader, "Model to use?", "anthropic/claude-opus-4-6")

	// 4. Checklist
	var checklistPath string
	hasChecklist := promptYesNo(reader, "Do you have an existing checklist file?", false)
	if hasChecklist {
		checklistPath = promptString(reader, "Path to checklist file?", "CHECKLIST.md")
	} else {
		checklistPath = promptString(reader, "Name for new checklist file?", "CHECKLIST.md")
		checklistContent := `# Checklist
# Add items with [~] prefix:
#   - [~] path/to/file — description
# Status: [~] = pending, [x] = done, [s] = skip
`
		if err := os.WriteFile(checklistPath, []byte(checklistContent), 0644); err != nil {
			return fmt.Errorf("creating checklist file: %w", err)
		}
		fmt.Printf("  Created %s\n", checklistPath)
	}

	// 5. Prompt
	var promptPath string
	hasPrompt := promptYesNo(reader, "Do you have an existing prompt file?", false)
	if hasPrompt {
		promptPath = promptString(reader, "Path to prompt file?", "prompt.md")
	} else {
		taskDesc := promptString(reader, "Describe the task in one sentence:", "")
		promptPath = "prompt.md"

		promptContent, err := generatePromptContent(backendName, model, taskDesc)
		if err != nil {
			return err
		}
		if err := os.WriteFile(promptPath, []byte(promptContent), 0644); err != nil {
			return fmt.Errorf("creating prompt file: %w", err)
		}
		fmt.Printf("  Created %s\n", promptPath)
	}

	// 6. Workers
	numWorkers := promptInt(reader, "How many parallel workers?", 1)
	type workerEntry struct {
		name    string
		pattern string
	}
	workers := make([]workerEntry, 0, numWorkers)
	for i := 0; i < numWorkers; i++ {
		name := promptString(reader, fmt.Sprintf("Worker %d name:", i+1), "")
		pattern := promptString(reader, fmt.Sprintf("Worker %d pattern (grep to match checklist lines):", i+1), "")
		workers = append(workers, workerEntry{name: name, pattern: pattern})
	}

	// Warn about overlapping patterns
	for i := 0; i < len(workers); i++ {
		for j := i + 1; j < len(workers); j++ {
			if workers[i].pattern == workers[j].pattern {
				fmt.Printf("  Warning: workers %q and %q have the same pattern %q — they may process the same items\n",
					workers[i].name, workers[j].name, workers[i].pattern)
			} else if strings.Contains(workers[i].pattern, workers[j].pattern) || strings.Contains(workers[j].pattern, workers[i].pattern) {
				fmt.Printf("  Warning: patterns for %q (%s) and %q (%s) may overlap\n",
					workers[i].name, workers[i].pattern, workers[j].name, workers[j].pattern)
			}
		}
	}

	// 7. Items per iteration
	itemsPerIteration := promptInt(reader, "Items per iteration?", 5)

	// 7b. Concurrency
	concurrency := promptInt(reader, "Max workers to run in parallel? (0 = all)", 0)

	// 8. Write ralph.toml
	var toml strings.Builder
	fmt.Fprintf(&toml, "backend = %q\n", backendName)
	fmt.Fprintf(&toml, "checklist = %q\n", checklistPath)
	fmt.Fprintf(&toml, "prompt = %q\n", promptPath)
	fmt.Fprintf(&toml, "model = %q\n", model)
	fmt.Fprintf(&toml, "items_per_iteration = %d\n", itemsPerIteration)
	fmt.Fprintf(&toml, "concurrency = %d\n", concurrency)
	fmt.Fprintf(&toml, "auto_approve_permissions = true\n")
	fmt.Fprintf(&toml, "\n")

	for _, w := range workers {
		fmt.Fprintf(&toml, "[[worker]]\n")
		fmt.Fprintf(&toml, "name = %q\n", w.name)
		fmt.Fprintf(&toml, "pattern = %q\n", w.pattern)
		fmt.Fprintf(&toml, "\n")
	}

	if err := os.WriteFile("ralph.toml", []byte(toml.String()), 0644); err != nil {
		return fmt.Errorf("writing ralph.toml: %w", err)
	}

	// 9. Summary
	fmt.Println()
	fmt.Println("Created:")
	fmt.Printf("  %-20s (config)\n", "ralph.toml")
	fmt.Printf("  %-20s (checklist)\n", checklistPath)
	fmt.Printf("  %-20s (prompt template)\n", promptPath)
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Add items to your checklist")
	fmt.Println("  2. Review/edit the prompt template")
	fmt.Println("  3. Run: ralph run")

	return nil
}

// generatePromptContent attempts to use the AI backend to generate a prompt template.
// Falls back to a basic template if the backend is unavailable or fails.
func generatePromptContent(backendName string, model string, taskDesc string) (string, error) {
	if taskDesc == "" {
		return basicPromptTemplate(""), nil
	}

	be, err := backend.New(backendName, nil)
	if err != nil {
		fmt.Printf("  Could not create backend: %v — using basic template\n", err)
		return basicPromptTemplate(taskDesc), nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := be.CheckAuth(ctx, model); err != nil {
		fmt.Printf("  Backend auth check failed: %v — using basic template\n", err)
		return basicPromptTemplate(taskDesc), nil
	}

	// Write the meta-prompt to a temp file for RunPrompt
	metaPrompt := fmt.Sprintf(`Generate a detailed prompt template for a coding agent that will: %s

The prompt should:
1. Clearly explain the task
2. List step-by-step instructions 
3. Include quality guardrails
4. End with "Output RALPH_SUMMARY at the end."

Output ONLY the prompt content, nothing else.`, taskDesc)

	tmpFile, err := os.CreateTemp("", "ralph-meta-prompt-*.md")
	if err != nil {
		fmt.Printf("  Could not create temp file: %v — using basic template\n", err)
		return basicPromptTemplate(taskDesc), nil
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.WriteString(metaPrompt); err != nil {
		tmpFile.Close()
		fmt.Printf("  Could not write meta-prompt: %v — using basic template\n", err)
		return basicPromptTemplate(taskDesc), nil
	}
	tmpFile.Close()

	fmt.Println("  Generating prompt via AI backend...")
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}

	output, exitCode, err := be.RunPrompt(ctx, tmpPath, cwd, model)
	if err != nil || exitCode != 0 {
		if err != nil {
			fmt.Printf("  AI generation failed: %v — using basic template\n", err)
		} else {
			fmt.Printf("  AI generation failed (exit code %d) — using basic template\n", exitCode)
		}
		return basicPromptTemplate(taskDesc), nil
	}

	output = strings.TrimSpace(output)
	if output == "" {
		fmt.Println("  AI returned empty output — using basic template")
		return basicPromptTemplate(taskDesc), nil
	}

	return output + "\n", nil
}

// basicPromptTemplate returns a simple prompt template with the given task description.
func basicPromptTemplate(taskDesc string) string {
	if taskDesc == "" {
		taskDesc = "TODO: describe your task here"
	}
	return fmt.Sprintf(`# Task: %s

## Instructions

You are an autonomous coding agent. Your task: %s

For each item in your batch:
1. Read the source
2. Implement the equivalent
3. Update the checklist marking completed items as [x]

**DO NOT run git add or git commit.** The runner handles commits.

Output RALPH_SUMMARY at the end.
`, taskDesc, taskDesc)
}
