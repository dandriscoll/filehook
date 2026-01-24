package cli

import (
	"fmt"
	"os"

	"github.com/dandriscoll/filehook/internal/config"
	"github.com/spf13/cobra"
)

var (
	// Global flags
	cfgFile      string
	directory    string
	jsonOutput   bool
	dryRun       bool
	debugMode    bool

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
	rootCmd.PersistentFlags().StringVarP(&directory, "directory", "d", "", "target directory (finds config upward, processes only this dir)")
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "output in JSON format")
	rootCmd.PersistentFlags().BoolVarP(&dryRun, "dry-run", "n", false, "print what would be done without executing")
	rootCmd.PersistentFlags().BoolVar(&debugMode, "debug", false, "enable verbose debug logging to .filehook/debug.log")

	// Make -? work as an alias for -h (help)
	rootCmd.PersistentFlags().BoolP("help", "?", false, "help for filehook")
	rootCmd.PersistentFlags().Lookup("help").Hidden = true
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
		cfg, err = loader.DiscoverAndLoad(directory)
	}

	if err != nil {
		return nil, err
	}

	// Apply --debug flag (overrides config file setting)
	if debugMode {
		cfg.Debug = true
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

// isDryRun returns whether dry-run mode is enabled
func isDryRun() bool {
	return dryRun
}

// getTargetDirectory returns the target directory if specified, empty string otherwise
func getTargetDirectory() string {
	return directory
}

// getEffectiveWatchPaths returns the watch paths to use.
// If -d is specified, returns just that directory; otherwise uses config's watch paths.
func getEffectiveWatchPaths(cfg *config.Config) []string {
	if directory != "" {
		return []string{cfg.ResolvePath(directory)}
	}
	return cfg.WatchPaths()
}

// validatePatternFilter checks that the given pattern name exists in the config.
// Returns an error if the pattern filter is specified but doesn't match any pattern.
func validatePatternFilter(cfg *config.Config, patternFilter string) error {
	if patternFilter == "" {
		return nil
	}

	for _, p := range cfg.Inputs.Patterns {
		if p.Name == patternFilter {
			return nil
		}
	}

	// Build a list of available pattern names for the error message
	var names []string
	for _, p := range cfg.Inputs.Patterns {
		if p.Name != "" {
			names = append(names, p.Name)
		}
	}

	if len(names) == 0 {
		return fmt.Errorf("pattern %q not found: no named patterns defined in config", patternFilter)
	}
	return fmt.Errorf("pattern %q not found, available patterns: %v", patternFilter, names)
}
