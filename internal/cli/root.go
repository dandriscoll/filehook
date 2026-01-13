package cli

import (
	"fmt"
	"os"

	"github.com/anthropics/filehook/internal/config"
	"github.com/spf13/cobra"
)

var (
	// Global flags
	cfgFile    string
	jsonOutput bool

	// Loaded config (set during PreRunE)
	loadedConfig *config.Config
)

// rootCmd represents the base command
var rootCmd = &cobra.Command{
	Use:   "filehook",
	Short: "A CLI file transformation watcher",
	Long: `filehook watches for input files and runs configured commands
to transform them into output files.

It supports plugins for filename generation and processing decisions,
parallel or sequential job execution, and durable on-disk queuing.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "config file (default: search for filehook.yaml)")
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "output in JSON format")
}

// loadConfig loads and validates the config, used by subcommands that need it
func loadConfig() (*config.Config, error) {
	if loadedConfig != nil {
		return loadedConfig, nil
	}

	loader := config.NewLoader("")

	var cfg *config.Config
	var err error

	if cfgFile != "" {
		cfg, err = loader.Load(cfgFile)
	} else {
		cfg, err = loader.DiscoverAndLoad("")
	}

	if err != nil {
		return nil, err
	}

	loadedConfig = cfg
	return cfg, nil
}

// mustLoadConfig loads config or exits with error
func mustLoadConfig() *config.Config {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	return cfg
}

// validateConfig validates the loaded config
func validateConfig(cfg *config.Config) error {
	errs := config.Validate(cfg)
	if len(errs) > 0 {
		for _, err := range errs {
			fmt.Fprintf(os.Stderr, "validation error: %v\n", err)
		}
		return fmt.Errorf("config validation failed with %d errors", len(errs))
	}
	return nil
}

// isJSONOutput returns whether JSON output is requested
func isJSONOutput() bool {
	return jsonOutput
}
