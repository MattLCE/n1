// Command mirord is the daemon process for n1 synchronization.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/n1/n1/internal/log"
	"github.com/n1/n1/internal/miror"
	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"
)

const (
	// DefaultConfigPath is the default path for the mirord configuration file.
	DefaultConfigPath = "~/.config/n1/mirord.yaml"
	// DefaultWALPath is the default path for the mirord WAL directory.
	DefaultWALPath = "~/.local/share/n1/mirord/wal"
	// DefaultPIDFile is the default path for the mirord PID file.
	DefaultPIDFile = "~/.local/share/n1/mirord/mirord.pid"
)

// Config represents the configuration for the mirord daemon.
type Config struct {
	// VaultPath is the path to the vault file.
	VaultPath string
	// WALPath is the path to the WAL directory.
	WALPath string
	// PIDFile is the path to the PID file.
	PIDFile string
	// LogLevel is the logging level.
	LogLevel string
	// ListenAddresses are the addresses to listen on.
	ListenAddresses []string
	// Peers are the known peers.
	Peers []string
	// DiscoveryEnabled indicates whether mDNS discovery is enabled.
	DiscoveryEnabled bool
	// SyncInterval is the interval for automatic synchronization.
	SyncInterval time.Duration
	// TransportConfig is the transport configuration.
	TransportConfig miror.TransportConfig
	// SyncConfig is the synchronization configuration.
	SyncConfig miror.SyncConfig
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		VaultPath:        "",
		WALPath:          expandPath(DefaultWALPath),
		PIDFile:          expandPath(DefaultPIDFile),
		LogLevel:         "info",
		ListenAddresses:  []string{":7000", ":7001"},
		Peers:            []string{},
		DiscoveryEnabled: true,
		SyncInterval:     5 * time.Minute,
		TransportConfig:  miror.DefaultTransportConfig(),
		SyncConfig:       miror.DefaultSyncConfig(),
	}
}

// expandPath expands the ~ in a path to the user's home directory.
func expandPath(path string) string {
	if path == "" || path[0] != '~' {
		return path
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}

	return filepath.Join(home, path[1:])
}

// writePIDFile writes the current process ID to the PID file.
func writePIDFile(path string) error {
	// Ensure the directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory for PID file: %w", err)
	}

	// Write the PID
	pid := os.Getpid()
	if err := os.WriteFile(path, []byte(fmt.Sprintf("%d", pid)), 0644); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}

	return nil
}

// removePIDFile removes the PID file.
func removePIDFile(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove PID file: %w", err)
	}
	return nil
}

// runDaemon runs the mirord daemon with the given configuration.
func runDaemon(config Config) error {
	// Set up logging
	level, err := zerolog.ParseLevel(config.LogLevel)
	if err != nil {
		return fmt.Errorf("invalid log level: %w", err)
	}
	log.SetLevel(level)

	// Write PID file
	if err := writePIDFile(config.PIDFile); err != nil {
		return err
	}
	defer removePIDFile(config.PIDFile)

	// Set up signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-signalCh
		log.Info().Str("signal", sig.String()).Msg("Received signal, shutting down")
		cancel()
	}()

	// TODO: Implement the actual daemon functionality
	// This would include:
	// - Setting up the replicator
	// - Starting the server to listen for incoming connections
	// - Setting up mDNS discovery if enabled
	// - Starting the sync worker
	// - Handling shutdown gracefully

	log.Info().Msg("Mirord daemon started")

	// Wait for context cancellation
	<-ctx.Done()

	log.Info().Msg("Mirord daemon stopped")
	return nil
}

func main() {
	config := DefaultConfig()

	app := &cli.App{
		Name:  "mirord",
		Usage: "n1 synchronization daemon",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "vault",
				Aliases:     []string{"v"},
				Usage:       "Path to the vault file",
				Destination: &config.VaultPath,
			},
			&cli.StringFlag{
				Name:        "wal-path",
				Aliases:     []string{"w"},
				Usage:       "Path to the WAL directory",
				Value:       DefaultWALPath,
				Destination: &config.WALPath,
			},
			&cli.StringFlag{
				Name:        "pid-file",
				Aliases:     []string{"p"},
				Usage:       "Path to the PID file",
				Value:       DefaultPIDFile,
				Destination: &config.PIDFile,
			},
			&cli.StringFlag{
				Name:        "log-level",
				Aliases:     []string{"l"},
				Usage:       "Logging level (debug, info, warn, error)",
				Value:       "info",
				Destination: &config.LogLevel,
			},
			&cli.StringSliceFlag{
				Name:    "listen",
				Aliases: []string{"L"},
				Usage:   "Addresses to listen on",
				Value:   cli.NewStringSlice(":7000", ":7001"),
			},
			&cli.StringSliceFlag{
				Name:    "peer",
				Aliases: []string{"P"},
				Usage:   "Known peers to connect to",
			},
			&cli.BoolFlag{
				Name:        "discovery",
				Aliases:     []string{"d"},
				Usage:       "Enable mDNS discovery",
				Value:       true,
				Destination: &config.DiscoveryEnabled,
			},
			&cli.DurationFlag{
				Name:        "sync-interval",
				Aliases:     []string{"i"},
				Usage:       "Interval for automatic synchronization",
				Value:       5 * time.Minute,
				Destination: &config.SyncInterval,
			},
		},
		Action: func(c *cli.Context) error {
			// Expand paths
			config.WALPath = expandPath(config.WALPath)
			config.PIDFile = expandPath(config.PIDFile)

			// Get values from string slice flags
			config.ListenAddresses = c.StringSlice("listen")
			config.Peers = c.StringSlice("peer")

			// Run the daemon
			return runDaemon(config)
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Error().Err(err).Msg("Mirord failed")
		os.Exit(1)
	}
}
