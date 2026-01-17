package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

const defaultConfigFilename = "filehook.yaml"

const configTemplate = `version: 1

watch:
  paths:
    - ./input
  ignore:
    - "*.tmp"
    - "*.partial"
    - ".git"
  debounce_ms: 200

inputs:
  patterns:
    - "*.txt"

outputs:
  root: ./output

# Command to run for each input file
# Available variables: {{input}}, {{output}}, {{outputs}}, {{outputs_json}}, {{output_dir}}
command: ["sh", "-c", "cp {{input}} {{output}}"]

# Optional plugins for custom behavior
# plugins:
#   # Naming plugin returns JSON: {"outputs": ["out.txt"], "ready": true}
#   # - outputs: generated output filenames
#   # - ready: if false, file is skipped (defaults to true)
#   naming:
#     path: ./plugins/naming.sh
#     args: []
#   # should_process checks modification policy (NOT readiness - use naming for that)
#   should_process:
#     path: ./plugins/should_process.sh
#     args: []

concurrency:
  mode: parallel    # parallel | sequential_switch
  max_workers: 2

# Policy when input files are modified after initial processing
# Options: ignore, reprocess, if-newer, versioned
on_modified: if-newer

# Where to store queue database and job logs
state_dir: .filehook
`

var (
	forceOverwrite bool
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new filehook.yaml config file",
	Long: `Creates a new filehook.yaml configuration file in the current directory
with sensible defaults that you can customize for your use case.`,
	RunE: runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().BoolVarP(&forceOverwrite, "force", "f", false, "overwrite existing config file")
}

func runInit(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	configPath := filepath.Join(cwd, defaultConfigFilename)

	// Check if file already exists
	if _, err := os.Stat(configPath); err == nil {
		if !forceOverwrite {
			return fmt.Errorf("%s already exists (use --force to overwrite)", defaultConfigFilename)
		}
	}

	// Write the template
	if err := os.WriteFile(configPath, []byte(configTemplate), 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	fmt.Printf("Created %s\n", configPath)
	fmt.Println("\nNext steps:")
	fmt.Println("  1. Edit the config to match your input patterns and command")
	fmt.Println("  2. Create the input directory: mkdir -p ./input")
	fmt.Println("  3. Run: filehook watch")

	return nil
}
