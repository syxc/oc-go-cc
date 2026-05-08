// Package main is the CLI entry point for the oc-go-cc proxy server.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/spf13/cobra"
	opencodeclient "github.com/syxc/oc-go-cc/internal/client"
	"github.com/syxc/oc-go-cc/internal/config"
	"github.com/syxc/oc-go-cc/internal/daemon"
	"github.com/syxc/oc-go-cc/internal/server"
)

const (
	appName     = "oc-go-cc"
	pidFileName = "oc-go-cc.pid"
)

// Version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	rootCmd := &cobra.Command{
		Use:   appName,
		Short: "Proxy Claude Code requests to OpenCode Go API",
		Long: `oc-go-cc is a CLI proxy tool that allows you to use your OpenCode Go
subscription with Claude Code. It intercepts Claude Code's Anthropic API requests,
transforms them to OpenAI format, and forwards them to OpenCode Go.

Configuration is stored at ~/.config/oc-go-cc/config.json`,
		Version: version,
	}

	// Add subcommands.
	rootCmd.AddCommand(serveCmd())
	rootCmd.AddCommand(stopCmd())
	rootCmd.AddCommand(statusCmd())
	rootCmd.AddCommand(initCmd())
	rootCmd.AddCommand(validateCmd())
	rootCmd.AddCommand(modelsCmd())
	rootCmd.AddCommand(autostartCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// serveCmd returns the command to start the proxy server.
func serveCmd() *cobra.Command {
	var configPath string
	var port int
	var background bool
	var daemonize bool // hidden internal flag

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the proxy server",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Handle background mode: fork and exit parent
			if background && !daemonize {
				opts := daemon.BackgroundOpts{
					ConfigPath: configPath,
					Port:       port,
				}
				return daemon.ForkIntoBackground(opts)
			}

			// Override config path if provided.
			if configPath != "" {
				_ = os.Setenv("OC_GO_CC_CONFIG", configPath)
			}

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Override port if provided via flag.
			if port != 0 {
				cfg.Port = port
			}

			pidPath := getPIDPath()

			// Check if already running before writing this process' PID.
			if !daemonize {
				if pid, err := daemon.GetPID(pidPath); err == nil {
					// Check if process is still running.
					if daemon.IsProcessRunning(pid) {
						return fmt.Errorf("server is already running (PID %d)", pid)
					}
					// Stale PID file, clean up.
					_ = os.Remove(pidPath)
				}
			}

			// Daemonize setup (child process after re-exec).
			if daemonize {
				paths, err := daemon.DefaultPaths()
				if err != nil {
					return err
				}
				if err := paths.EnsureConfigDir(); err != nil {
					return err
				}
				if err := daemon.DaemonizeSetup(paths); err != nil {
					return err
				}
			} else {
				// Write PID file for foreground mode.
				if err := daemon.WritePID(pidPath, os.Getpid()); err != nil {
					return fmt.Errorf("failed to write PID file: %w", err)
				}
			}
			defer func() { _ = os.Remove(pidPath) }()

			// Create and start server.
			srv, err := server.NewServer(cfg)
			if err != nil {
				return fmt.Errorf("failed to create server: %w", err)
			}

			fmt.Printf("Starting %s v%s\n", appName, version)
			fmt.Printf("Listening on %s:%d\n", cfg.Host, cfg.Port)
			fmt.Printf("Forwarding to: %s\n", cfg.OpenCodeGo.BaseURL)
			fmt.Println()
			fmt.Println("Configure Claude Code with:")
			fmt.Printf("  export ANTHROPIC_BASE_URL=http://%s:%d\n", cfg.Host, cfg.Port)
			fmt.Println("  export ANTHROPIC_AUTH_TOKEN=unused")
			fmt.Println()

			return srv.Start()
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to config file")
	cmd.Flags().IntVarP(&port, "port", "p", 0, "Override listen port")
	cmd.Flags().BoolVarP(&background, "background", "b", false, "Run as background daemon")
	cmd.Flags().BoolVar(&daemonize, "_daemonize", false, "Internal use only")
	_ = cmd.Flags().MarkHidden("_daemonize")

	return cmd
}

// stopCmd returns the command to stop the proxy server.
func stopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the proxy server",
		RunE: func(cmd *cobra.Command, args []string) error {
			pidPath := getPIDPath()
			pid, err := daemon.GetPID(pidPath)
			if err != nil {
				return fmt.Errorf("server is not running (no PID file)")
			}

			if err := daemon.StopProcess(pid); err != nil {
				return fmt.Errorf("failed to stop server: %w", err)
			}

			fmt.Printf("Sent stop signal to server (PID %d)\n", pid)
			_ = os.Remove(pidPath)
			return nil
		},
	}
}

// statusCmd returns the command to check server status.
func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check server status",
		RunE: func(cmd *cobra.Command, args []string) error {
			pidPath := getPIDPath()
			pid, err := daemon.GetPID(pidPath)
			if err != nil {
				fmt.Println("Server is not running")
				return nil
			}

			if !daemon.IsProcessRunning(pid) {
				fmt.Println("Server is not running (stale PID file)")
				_ = os.Remove(pidPath)
				return nil
			}

			fmt.Printf("Server is running (PID %d)\n", pid)
			return nil
		},
	}
}

// initCmd returns the command to create a default configuration file.
func initCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create default configuration file",
		RunE: func(cmd *cobra.Command, args []string) error {
			configDir := getConfigDir()
			configPath := filepath.Join(configDir, "config.json")

			// Check if config already exists
			if _, err := os.Stat(configPath); err == nil {
				fmt.Printf("Config already exists at %s\n", configPath)
				fmt.Println("Edit the file to update your configuration.")
				return nil
			}

			if err := os.MkdirAll(configDir, 0755); err != nil {
				return fmt.Errorf("failed to create config directory: %w", err)
			}

			if err := os.WriteFile(configPath, []byte(getDefaultConfig()), 0644); err != nil {
				return fmt.Errorf("failed to write config file: %w", err)
			}

			fmt.Printf("Created default config at %s\n", configPath)
			fmt.Println("Edit the file and add your OpenCode Go API key.")
			return nil
		},
	}
}

// validateCmd returns the command to validate the configuration file.
func validateCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate configuration file",
		RunE: func(cmd *cobra.Command, args []string) error {
			if configPath != "" {
				_ = os.Setenv("OC_GO_CC_CONFIG", configPath)
			}

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("invalid config: %w", err)
			}

			fmt.Println("Configuration is valid!")
			fmt.Printf("  Host: %s\n", cfg.Host)
			fmt.Printf("  Port: %d\n", cfg.Port)
			fmt.Printf("  API Key: %s...\n", maskString(cfg.APIKey, 8))
			fmt.Printf("  Base URL: %s\n", cfg.OpenCodeGo.BaseURL)
			fmt.Printf("  Models configured: %d\n", len(cfg.Models))
			fmt.Printf("  Fallback chains: %d\n", len(cfg.Fallbacks))
			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to config file")
	return cmd
}

// modelsCmd returns the command to list and validate models.
// It queries the upstream /v1/models endpoint and cross-references with
// the local config to surface missing or unused models.
func modelsCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "models",
		Short: "List and validate configured models against upstream",
		RunE: func(cmd *cobra.Command, args []string) error {
			if configPath != "" {
				_ = os.Setenv("OC_GO_CC_CONFIG", configPath)
			}

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Collect all model IDs referenced in config (primary + fallbacks).
			configModels := collectConfigModelIDs(cfg)

			// Query upstream.
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			client := opencodeclient.NewOpenCodeClient(cfg.OpenCodeGo, cfg.APIKey)
			upstream, err := client.ListModels(ctx)
			if err != nil {
				fmt.Printf("Warning: could not fetch upstream models (%v)\n", err)
				fmt.Println("Showing config-only view:")
				fmt.Println()
				printConfigModels(configModels)
				return nil
			}

			// Build lookup set.
			upstreamSet := make(map[string]bool, len(upstream))
			for _, m := range upstream {
				upstreamSet[m.ID] = true
			}

			fmt.Printf("Upstream models: %d available\n", len(upstream))
			fmt.Println()

			// Cross-reference.
			fmt.Println("  Config model              Status")
			fmt.Println("  ─────────────────────────────────────────")
			for _, id := range sortedKeys(configModels) {
				if upstreamSet[id] {
					fmt.Printf("  %-26s ✅ available\n", id)
				} else {
					fmt.Printf("  %-26s ❌ not found upstream\n", id)
				}
			}

			// Show unused upstream models.
			configSet := make(map[string]bool, len(configModels))
			for _, id := range configModels {
				configSet[id] = true
			}

			var unused []string
			for _, m := range upstream {
				if !configSet[m.ID] {
					unused = append(unused, m.ID)
				}
			}
			if len(unused) > 0 {
				sort.Strings(unused)
				fmt.Println()
				fmt.Println("  Unused upstream models (not in config):")
				for _, id := range unused {
					fmt.Printf("    • %s\n", id)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to config file")
	return cmd
}

// collectConfigModelIDs returns a deduplicated list of all model IDs referenced in config.
func collectConfigModelIDs(cfg *config.Config) []string {
	seen := make(map[string]bool)
	add := func(id string) {
		if id != "" {
			seen[id] = true
		}
	}
	for _, m := range cfg.Models {
		add(m.ModelID)
	}
	for _, fallbacks := range cfg.Fallbacks {
		for _, m := range fallbacks {
			add(m.ModelID)
		}
	}
	var result []string
	for id := range seen {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

// printConfigModels prints config models when upstream is unreachable.
func printConfigModels(models []string) {
	fmt.Println("  Model ID")
	fmt.Println("  ─────────────────────────────────────────")
	for _, id := range models {
		fmt.Printf("  %s\n", id)
	}
	fmt.Println()
	fmt.Println("(Could not reach upstream to validate availability.)")
}

// sortedKeys returns the keys of a string slice (already sorted).
func sortedKeys(keys []string) []string {
	return keys
}

// getConfigDir returns the default configuration directory path.
func getConfigDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "oc-go-cc")
}

// autostartCmd returns the command to manage autostart on login.
func autostartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "autostart",
		Short: "Manage auto-start on login",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "enable",
		Short: "Enable auto-start on login",
		RunE: func(cmd *cobra.Command, args []string) error {
			var configPath string
			var port int
			if cmd.Flags().Changed("config") {
				configPath, _ = cmd.Flags().GetString("config")
			}
			if cmd.Flags().Changed("port") {
				port, _ = cmd.Flags().GetInt("port")
			}
			return daemon.EnableAutostart(configPath, port)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "disable",
		Short: "Disable auto-start on login",
		RunE: func(cmd *cobra.Command, args []string) error {
			return daemon.DisableAutostart()
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Check auto-start status",
		RunE: func(cmd *cobra.Command, args []string) error {
			return daemon.AutostartStatus()
		},
	})

	cmd.PersistentFlags().StringP("config", "c", "", "Path to config file")
	cmd.PersistentFlags().IntP("port", "p", 0, "Override listen port")

	return cmd
}

// getPIDPath returns the path to the PID file.
func getPIDPath() string {
	paths, err := daemon.DefaultPaths()
	if err != nil {
		// Fallback to temp dir if home dir cannot be determined
		return filepath.Join(os.TempDir(), pidFileName)
	}
	return paths.PIDFile
}

// maskString masks all but the first `visible` characters of a string.
func maskString(s string, visible int) string {
	if len(s) <= visible {
		return s
	}
	return s[:visible] + "..."
}

// getDefaultConfig returns a default configuration JSON template.
// Optimized for cost-efficiency: uses cheaper models by default, expensive ones only when needed.
func getDefaultConfig() string {
	return `{
  "api_key": "${OC_GO_CC_API_KEY}",
  "host": "127.0.0.1",
  "port": 3456,
  "enable_streaming_scenario_routing": false,
  "models": {
    "background": {
      "provider": "opencode-go",
      "model_id": "qwen3.5-plus",
      "temperature": 0.5,
      "max_tokens": 2048
    },
    "default": {
      "provider": "opencode-go",
      "model_id": "kimi-k2.6",
      "temperature": 0.7,
      "max_tokens": 4096
    },
    "long_context": {
      "provider": "opencode-go",
      "model_id": "minimax-m2.5",
      "temperature": 0.7,
      "max_tokens": 16384,
      "context_threshold": 80000
    },
    "deepseek-v4-pro": {
      "provider": "opencode-go",
      "model_id": "deepseek-v4-pro",
      "temperature": 0.7,
      "max_tokens": 8192,
      "reasoning_effort": "max",
      "thinking": {
        "type": "enabled"
      }
    },
    "deepseek-v4-flash": {
      "provider": "opencode-go",
      "model_id": "deepseek-v4-flash",
      "temperature": 0.7,
      "max_tokens": 4096,
      "reasoning_effort": "max",
      "thinking": {
        "type": "enabled"
      }
    },
    "think": {
      "provider": "opencode-go",
      "model_id": "glm-5",
      "temperature": 0.7,
      "max_tokens": 8192
    },
    "complex": {
      "provider": "opencode-go",
      "model_id": "glm-5.1",
      "temperature": 0.7,
      "max_tokens": 4096
    },
    "fast": {
      "provider": "opencode-go",
      "model_id": "qwen3.6-plus",
      "temperature": 0.7,
      "max_tokens": 4096
    }
  },
  "fallbacks": {
    "background": [
      { "provider": "opencode-go", "model_id": "qwen3.6-plus" },
      { "provider": "opencode-go", "model_id": "minimax-m2.5" }
    ],
    "default": [
      { "provider": "opencode-go", "model_id": "mimo-v2-pro" },
      { "provider": "opencode-go", "model_id": "qwen3.6-plus" }
    ],
    "long_context": [
      { "provider": "opencode-go", "model_id": "minimax-m2.7" },
      { "provider": "opencode-go", "model_id": "kimi-k2.6" }
    ],
    "think": [
      { "provider": "opencode-go", "model_id": "kimi-k2.6" },
      { "provider": "opencode-go", "model_id": "mimo-v2-pro" }
    ],
    "complex": [
      { "provider": "opencode-go", "model_id": "glm-5" },
      { "provider": "opencode-go", "model_id": "kimi-k2.6" }
    ],
    "fast": [
      { "provider": "opencode-go", "model_id": "qwen3.5-plus" },
      { "provider": "opencode-go", "model_id": "minimax-m2.5" }
    ]
  },
  "opencode_go": {
    "base_url": "https://opencode.ai/zen/go/v1/chat/completions",
    "anthropic_base_url": "https://opencode.ai/zen/go/v1/messages",
    "timeout_ms": 300000
  },
  "logging": {
    "level": "info",
    "requests": true
  }
}
`
}
