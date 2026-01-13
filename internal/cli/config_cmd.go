package cli

import (
	"encoding/json"
	"fmt"

	"github.com/anthropics/filehook/internal/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Config management commands",
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show resolved config and discovered path",
	RunE:  runConfigShow,
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configShowCmd)
}

func runConfigShow(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	if isJSONOutput() {
		return printConfigJSON(cfg)
	}

	return printConfigHuman(cfg)
}

func printConfigJSON(cfg *config.Config) error {
	// Create a struct that includes the resolved paths
	output := struct {
		ConfigPath   string                  `json:"config_path"`
		ConfigDir    string                  `json:"config_dir"`
		Version      int                     `json:"version"`
		Watch        config.WatchConfig      `json:"watch"`
		WatchPaths   []string                `json:"watch_paths_resolved"`
		Inputs       config.InputsConfig     `json:"inputs"`
		Outputs      config.OutputsConfig    `json:"outputs"`
		OutputRoot   string                  `json:"output_root_resolved"`
		Command      string                  `json:"command"`
		Plugins      pluginsOutput           `json:"plugins"`
		Concurrency  config.ConcurrencyConfig `json:"concurrency"`
		OnModified   config.ModifiedPolicy   `json:"on_modified"`
		StateDir     string                  `json:"state_dir"`
		StateDirPath string                  `json:"state_dir_resolved"`
	}{
		ConfigPath:   cfg.ConfigPath,
		ConfigDir:    cfg.ConfigDir,
		Version:      cfg.Version,
		Watch:        cfg.Watch,
		WatchPaths:   cfg.WatchPaths(),
		Inputs:       cfg.Inputs,
		Outputs:      cfg.Outputs,
		OutputRoot:   cfg.OutputRoot(),
		Command:      cfg.Command.AsString(),
		Plugins:      formatPlugins(cfg),
		Concurrency:  cfg.Concurrency,
		OnModified:   cfg.OnModified,
		StateDir:     cfg.StateDir,
		StateDirPath: cfg.StateDirectory(),
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

type pluginsOutput struct {
	FilenameGenerator *pluginOutput `json:"filename_generator,omitempty"`
	ShouldProcess     *pluginOutput `json:"should_process,omitempty"`
	GroupKey          *pluginOutput `json:"group_key,omitempty"`
}

type pluginOutput struct {
	Path         string   `json:"path"`
	ResolvedPath string   `json:"resolved_path"`
	Args         []string `json:"args,omitempty"`
}

func formatPlugins(cfg *config.Config) pluginsOutput {
	out := pluginsOutput{}
	if cfg.Plugins.FilenameGenerator != nil {
		out.FilenameGenerator = &pluginOutput{
			Path:         cfg.Plugins.FilenameGenerator.Path,
			ResolvedPath: cfg.ResolvePath(cfg.Plugins.FilenameGenerator.Path),
			Args:         cfg.Plugins.FilenameGenerator.Args,
		}
	}
	if cfg.Plugins.ShouldProcess != nil {
		out.ShouldProcess = &pluginOutput{
			Path:         cfg.Plugins.ShouldProcess.Path,
			ResolvedPath: cfg.ResolvePath(cfg.Plugins.ShouldProcess.Path),
			Args:         cfg.Plugins.ShouldProcess.Args,
		}
	}
	if cfg.Plugins.GroupKey != nil {
		out.GroupKey = &pluginOutput{
			Path:         cfg.Plugins.GroupKey.Path,
			ResolvedPath: cfg.ResolvePath(cfg.Plugins.GroupKey.Path),
			Args:         cfg.Plugins.GroupKey.Args,
		}
	}
	return out
}

func printConfigHuman(cfg *config.Config) error {
	fmt.Printf("Config file: %s\n", cfg.ConfigPath)
	fmt.Println()

	// Print YAML representation
	data, err := yaml.Marshal(struct {
		Version     int                     `yaml:"version"`
		Watch       config.WatchConfig      `yaml:"watch"`
		Inputs      config.InputsConfig     `yaml:"inputs"`
		Outputs     config.OutputsConfig    `yaml:"outputs"`
		Command     string                  `yaml:"command"`
		Concurrency config.ConcurrencyConfig `yaml:"concurrency"`
		OnModified  config.ModifiedPolicy   `yaml:"on_modified"`
		StateDir    string                  `yaml:"state_dir"`
	}{
		Version:     cfg.Version,
		Watch:       cfg.Watch,
		Inputs:      cfg.Inputs,
		Outputs:     cfg.Outputs,
		Command:     cfg.Command.AsString(),
		Concurrency: cfg.Concurrency,
		OnModified:  cfg.OnModified,
		StateDir:    cfg.StateDir,
	})
	if err != nil {
		return err
	}
	fmt.Println(string(data))

	fmt.Println("Resolved paths:")
	fmt.Printf("  Watch paths:\n")
	for _, p := range cfg.WatchPaths() {
		fmt.Printf("    - %s\n", p)
	}
	fmt.Printf("  Output root: %s\n", cfg.OutputRoot())
	fmt.Printf("  State dir:   %s\n", cfg.StateDirectory())

	if cfg.Plugins.FilenameGenerator != nil {
		fmt.Printf("  Filename generator: %s\n", cfg.ResolvePath(cfg.Plugins.FilenameGenerator.Path))
	}
	if cfg.Plugins.ShouldProcess != nil {
		fmt.Printf("  Should process: %s\n", cfg.ResolvePath(cfg.Plugins.ShouldProcess.Path))
	}
	if cfg.Plugins.GroupKey != nil {
		fmt.Printf("  Group key: %s\n", cfg.ResolvePath(cfg.Plugins.GroupKey.Path))
	}

	return nil
}
