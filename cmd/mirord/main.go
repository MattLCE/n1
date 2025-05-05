// Command mirord is the daemon process for n1 synchronization.
package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/n1/n1/internal/dao"
	"github.com/n1/n1/internal/log"
	"github.com/n1/n1/internal/miror"
	"github.com/n1/n1/internal/secretstore"
	"github.com/n1/n1/internal/sqlite"
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
		VaultPath:        "", // Must be provided via flag
		WALPath:          expandPath(DefaultWALPath),
		PIDFile:          expandPath(DefaultPIDFile),
		LogLevel:         "info",
		ListenAddresses:  []string{":7001"}, // Default to one standard port
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
		return path // Silently ignore error, return original path
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
	if err := os.WriteFile(path, []byte(fmt.Sprintf("%d", pid)), 0600); err != nil {
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

// --- ObjectStoreAdapter (Copied from cmd/bosr/sync.go for now) ---

// ObjectStoreAdapter adapts the vault DAO to the miror.ObjectStore interface
type ObjectStoreAdapter struct {
	db        *sql.DB
	vaultPath string
	secureDAO *dao.SecureVaultDAO
}

// NewObjectStoreAdapter creates a new adapter for the vault
func NewObjectStoreAdapter(db *sql.DB, vaultPath string, masterKey []byte) *ObjectStoreAdapter {
	return &ObjectStoreAdapter{
		db:        db,
		vaultPath: vaultPath,
		secureDAO: dao.NewSecureVaultDAO(db, masterKey),
	}
}

// GetObject gets an object by its hash
func (a *ObjectStoreAdapter) GetObject(ctx context.Context, hash miror.ObjectHash) ([]byte, error) {
	key := hash.String()
	// TODO: This key conversion is likely incorrect. Need proper hash<->key mapping.
	// For M1, assume hash string IS the key for simplicity in tests.
	return a.secureDAO.Get(key)
}

// PutObject puts an object with the given hash and data
func (a *ObjectStoreAdapter) PutObject(ctx context.Context, hash miror.ObjectHash, data []byte) error {
	key := hash.String()
	// TODO: This key conversion is likely incorrect.
	return a.secureDAO.Put(key, data)
}

// HasObject checks if an object exists
func (a *ObjectStoreAdapter) HasObject(ctx context.Context, hash miror.ObjectHash) (bool, error) {
	key := hash.String()
	// TODO: This key conversion is likely incorrect.
	_, err := a.secureDAO.Get(key)
	if errors.Is(err, dao.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// ListObjects lists all object hashes
func (a *ObjectStoreAdapter) ListObjects(ctx context.Context) ([]miror.ObjectHash, error) {
	keys, err := a.secureDAO.List()
	if err != nil {
		return nil, err
	}

	var hashes []miror.ObjectHash
	for _, key := range keys {
		// Skip the canary record
		if key == "__n1_canary__" {
			continue
		}

		// Convert key to hash
		// TODO: This key conversion is likely incorrect.
		var hash miror.ObjectHash
		copy(hash[:], []byte(key)) // Placeholder conversion
		hashes = append(hashes, hash)
	}

	return hashes, nil
}

// GetObjectReader gets a reader for an object
func (a *ObjectStoreAdapter) GetObjectReader(ctx context.Context, hash miror.ObjectHash) (io.ReadCloser, error) {
	data, err := a.GetObject(ctx, hash)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// GetObjectWriter gets a writer for an object
func (a *ObjectStoreAdapter) GetObjectWriter(ctx context.Context, hash miror.ObjectHash) (io.WriteCloser, error) {
	buf := &bytes.Buffer{}
	return &objectWriter{
		buffer:      buf,
		hash:        hash,
		objectStore: a,
		ctx:         ctx,
	}, nil
}

// objectWriter is a WriteCloser that writes to a buffer and then to the object store when closed
type objectWriter struct {
	buffer      *bytes.Buffer
	hash        miror.ObjectHash
	objectStore *ObjectStoreAdapter
	ctx         context.Context
}

func (w *objectWriter) Write(p []byte) (n int, err error) {
	return w.buffer.Write(p)
}

func (w *objectWriter) Close() error {
	return w.objectStore.PutObject(w.ctx, w.hash, w.buffer.Bytes())
}

// --- End ObjectStoreAdapter ---

// runDaemon runs the mirord daemon with the given configuration.
func runDaemon(config Config) error {
	// Set up logging
	level, err := zerolog.ParseLevel(config.LogLevel)
	if err != nil {
		return fmt.Errorf("invalid log level: %w", err)
	}
	log.SetLevel(level)

	// Validate config
	if config.VaultPath == "" {
		return errors.New("vault path must be provided")
	}
	if len(config.ListenAddresses) == 0 {
		return errors.New("at least one listen address must be provided")
	}

	// Write PID file
	if err := writePIDFile(config.PIDFile); err != nil {
		return err
	}
	defer func() {
		if err := removePIDFile(config.PIDFile); err != nil {
			log.Error().Err(err).Str("path", config.PIDFile).Msg("Failed to remove PID file")
		}
	}()

	// Get master key
	mk, err := secretstore.Default.Get(config.VaultPath)
	if err != nil {
		return fmt.Errorf("failed to get key from secret store for vault %s: %w", config.VaultPath, err)
	}

	// Open DB
	db, err := sqlite.Open(config.VaultPath)
	if err != nil {
		return fmt.Errorf("failed to open database file '%s': %w", config.VaultPath, err)
	}
	defer db.Close()

	// Create ObjectStore and WAL
	objectStore := NewObjectStoreAdapter(db, config.VaultPath, mk)
	wal, err := miror.NewWAL(config.WALPath, 1024*1024) // 1MB sync interval
	if err != nil {
		return fmt.Errorf("failed to create WAL at %s: %w", config.WALPath, err)
	}
	defer wal.Close()

	// Set up signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-signalCh
		log.Info().Str("signal", sig.String()).Msg("Received signal, shutting down")
		cancel()
	}()

	// Start listener(s)
	listeners := make([]net.Listener, 0, len(config.ListenAddresses))
	for _, addr := range config.ListenAddresses {
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			// Clean up already opened listeners before returning
			for _, l := range listeners {
				l.Close()
			}
			return fmt.Errorf("failed to listen on %s: %w", addr, err)
		}
		log.Info().Str("address", addr).Msg("Listening for connections")
		listeners = append(listeners, listener)

		// Start accept loop for this listener
		go func(l net.Listener) {
			for {
				conn, err := l.Accept()
				if err != nil {
					// Check if the error is due to listener being closed
					select {
					case <-ctx.Done():
						log.Debug().Msg("Listener closed due to context cancellation")
						return
					default:
						log.Error().Err(err).Msg("Failed to accept connection")
						// Continue accepting unless it's a fatal error?
						// For now, log and continue.
						continue
					}
				}
				log.Info().Str("remote_addr", conn.RemoteAddr().String()).Msg("Accepted connection")
				// Handle connection in a new goroutine
				go handleConnection(ctx, conn, objectStore, wal, config)
			}
		}(listener)
	}

	log.Info().Msg("Mirord daemon started")

	// Wait for context cancellation (shutdown signal)
	<-ctx.Done()

	// Close listeners
	log.Info().Msg("Closing listeners...")
	for _, l := range listeners {
		l.Close()
	}

	// TODO: Wait for active connections to finish gracefully?

	log.Info().Msg("Mirord daemon stopped")
	return nil
}

// --- START DEBUG LOGGING Variables ---
var connectionCounter int // Simple counter for concurrent connections (not thread-safe, just for debug)
// --- END DEBUG LOGGING Variables ---
// handleConnection handles an incoming synchronization connection.
// Server always sends OFFER first.
func handleConnection(ctx context.Context, conn net.Conn, objectStore miror.ObjectStore, wal miror.WAL, config Config) {
	// Wrap connection in Transport
	transport, err := miror.NewTCPTransport("", config.TransportConfig) // Peer address is empty
	if err != nil {
		log.Error().Err(err).Msg("Failed to create TCP transport for incoming connection")
		conn.Close() // Closes connection on error here
		return
	}
	transport.SetConnection(conn) // Assign the accepted connection
	defer transport.Close()       // Ensure transport (and underlying conn) is closed eventually

	// Create a temporary session ID for logging/WAL
	connectionCounter++ // Increment counter
	var sessionID miror.SessionID
	for i := range sessionID {
		sessionID[i] = byte(i + 100)
	} // Simple placeholder
	// Add connection number to log context
	log := log.Logger.With().Str("remote_addr", conn.RemoteAddr().String()).Str("session_id", sessionID.String()).Int("conn_num", connectionCounter).Logger()

	// --- Server Sends Offer First ---
	log.Info().Msg("Handling new connection.") // Log start

	log.Info().Msg("Listing local objects to offer...") // Changed to Info for visibility
	serverHashes, err := objectStore.ListObjects(ctx)
	if err != nil {
		log.Error().Err(err).Msg("Failed to list objects for initial OFFER")
		// TODO: Send ERROR message to client?
		return // <-- Potential premature exit
	}
	log.Info().Int("count", len(serverHashes)).Msg("Object list retrieved.") // Changed to Info

	log.Info().Msg("Encoding initial OFFER...") // Changed to Info
	offerBody, err := miror.EncodeOfferMessage(serverHashes)
	if err != nil {
		log.Error().Err(err).Msg("Failed to encode initial OFFER")
		return // <-- Potential premature exit
	}
	log.Info().Int("offer_body_size", len(offerBody)).Msg("OFFER encoded.") // Changed to Info

	log.Info().Msg("Attempting to send initial OFFER message...") // Changed to Info
	if err := transport.Send(ctx, miror.MessageTypeOffer, offerBody); err != nil {
		log.Error().Err(err).Msg("Failed to send initial OFFER")
		return // <-- Potential premature exit
	}
	log.Info().Msg("Successfully sent initial OFFER") // Changed to Info

	// --- Wait for Client Response ---
	log.Info().Msg("Waiting for client response (ACCEPT, OFFER, or COMPLETE)") // Changed to Info
	msgType, clientRespBody, err := transport.Receive(ctx)
	if err != nil {
		log.Error().Err(err).Msg("Failed to receive client response")
		return
	}
	log.Info().Uint8("msg_type", msgType).Int("body_size", len(clientRespBody)).Msg("Received client response.") // Changed to Info

	switch msgType {
	case miror.MessageTypeAccept:
		// Client wants objects from server's offer
		log.Info().Msg("Received ACCEPT from client") // Changed to Info
		hashesToSend, err := miror.DecodeAcceptMessage(clientRespBody)
		if err != nil {
			log.Error().Err(err).Msg("Failed to decode client ACCEPT")
			return
		}
		log.Info().Int("count", len(hashesToSend)).Msg("Client accepted objects") // Changed to Info

		if err := sendObjects(ctx, log, transport, objectStore, wal, sessionID, hashesToSend); err != nil {
			log.Error().Err(err).Msg("Failed during server push (sending objects)")
			return
		}
		// After sending objects, server expects COMPLETE from client
		log.Info().Msg("Waiting for final COMPLETE from client after server push") // Changed to Info
		finalMsgType, _, err := transport.Receive(ctx)
		if err != nil {
			log.Error().Err(err).Msg("Failed to receive final COMPLETE from client")
			return
		}
		if finalMsgType != miror.MessageTypeComplete {
			log.Error().Uint8("msg_type", finalMsgType).Msg("Expected final COMPLETE from client, got something else")
			return
		}
		log.Info().Msg("Received final COMPLETE from client") // Changed to Info

	case miror.MessageTypeOffer:
		// Client wants to push its objects (handle client push)
		log.Info().Msg("Received OFFER from client") // Changed to Info
		if err := handleClientPush(ctx, log, transport, objectStore, wal, sessionID, clientRespBody); err != nil {
			log.Error().Err(err).Msg("Failed during client push handling")
			return
		}

	case miror.MessageTypeComplete:
		// Client doesn't need anything and isn't pushing anything. Sync is done.
		log.Info().Msg("Received COMPLETE from client immediately, sync finished") // Changed to Info

	default:
		log.Error().Uint8("msg_type", msgType).Msg("Received unexpected message type from client after initial server OFFER")
		// TODO: Send ERROR message?
		return
	}

	log.Info().Msg("Synchronization handling complete")
}

// handleClientPush handles the logic when the client sends an OFFER message.
func handleClientPush(ctx context.Context, log zerolog.Logger, transport miror.Transport, objectStore miror.ObjectStore, wal miror.WAL, sessionID miror.SessionID, offerBody []byte) error {
	offeredHashes, err := miror.DecodeOfferMessage(offerBody)
	if err != nil {
		return fmt.Errorf("failed to decode client OFFER message: %w", err)
	}
	log.Debug().Int("count", len(offeredHashes)).Msg("Decoded client OFFER")

	// Determine needed hashes
	neededHashes := make([]miror.ObjectHash, 0, len(offeredHashes))
	hashesToReceive := make(map[miror.ObjectHash]struct{})
	for _, hash := range offeredHashes {
		has, err := objectStore.HasObject(ctx, hash)
		if err != nil {
			return fmt.Errorf("failed to check object store for %s: %w", hash, err)
		}
		if !has {
			neededHashes = append(neededHashes, hash)
			hashesToReceive[hash] = struct{}{}
		}
	}
	log.Debug().Int("needed", len(neededHashes)).Int("offered", len(offeredHashes)).Msg("Determined needed objects from client OFFER")

	// Send ACCEPT message
	acceptBody, err := miror.EncodeAcceptMessage(neededHashes)
	if err != nil {
		return fmt.Errorf("failed to encode ACCEPT message: %w", err)
	}
	if err := transport.Send(ctx, miror.MessageTypeAccept, acceptBody); err != nil {
		return fmt.Errorf("failed to send ACCEPT message: %w", err)
	}
	log.Debug().Int("count", len(neededHashes)).Msg("Sent ACCEPT to client")

	if len(neededHashes) == 0 {
		// Client should send COMPLETE now
		log.Debug().Msg("No objects needed from client, waiting for COMPLETE")
	} else {
		log.Debug().Msg("Waiting for DATA messages from client")
	}

	// Receive DATA messages until COMPLETE
	receivedCount := 0
	for len(hashesToReceive) > 0 {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("context cancelled during data transfer: %w", err)
		}

		msgType, dataBody, err := transport.Receive(ctx)
		if err != nil {
			return fmt.Errorf("failed to receive DATA message from client: %w", err)
		}

		// Check for COMPLETE
		if msgType == miror.MessageTypeComplete {
			if len(hashesToReceive) == 0 {
				log.Debug().Msg("Received COMPLETE from client as expected (no objects needed)")
				break // Exit loop normally
			} else {
				return fmt.Errorf("received COMPLETE from client unexpectedly before all accepted objects were received")
			}
		}

		if msgType != miror.MessageTypeData {
			return fmt.Errorf("received unexpected message type %d from client, expected DATA", msgType)
		}

		hash, offset, data, err := miror.DecodeDataMessage(dataBody)
		if err != nil {
			return fmt.Errorf("failed to decode DATA message from client: %w", err)
		}

		if _, ok := hashesToReceive[hash]; !ok {
			return fmt.Errorf("received unexpected object hash %s in DATA message from client", hash)
		}

		// TODO: Handle partial transfers using offset (M2)
		if offset != 0 {
			return fmt.Errorf("received non-zero offset %d for %s, partial transfers not supported in M1", offset, hash)
		}

		log.Debug().Str("hash", hash.String()).Int("size", len(data)).Msg("Received DATA from client")

		// Log receive in WAL
		if err := wal.LogReceive(sessionID, hash); err != nil {
			return fmt.Errorf("failed to log receive to WAL for %s: %w", hash, err)
		}

		// Store object
		if err := objectStore.PutObject(ctx, hash, data); err != nil {
			return fmt.Errorf("failed to put object %s: %w", hash, err)
		}

		// TODO: Send ACK (M2)

		// Complete transfer in WAL
		if err := wal.CompleteTransfer(sessionID, hash); err != nil {
			return fmt.Errorf("failed to complete transfer in WAL for %s: %w", hash, err)
		}

		delete(hashesToReceive, hash)
		receivedCount++
	}

	// If we received objects, we expect one final COMPLETE message now
	if receivedCount > 0 {
		log.Debug().Msg("All expected objects received from client, waiting for final COMPLETE")
		msgType, _, err := transport.Receive(ctx) // Ignore body for now
		if err != nil {
			return fmt.Errorf("failed to receive final COMPLETE message from client: %w", err)
		}
		if msgType != miror.MessageTypeComplete {
			return fmt.Errorf("received unexpected message type %d from client, expected final COMPLETE", msgType)
		}
		log.Debug().Msg("Received final COMPLETE from client")
	}

	log.Info().Int("objects_received", receivedCount).Msg("Client push handling complete")
	return nil
}

// sendObjects sends the specified objects to the peer.
func sendObjects(ctx context.Context, log zerolog.Logger, transport miror.Transport, objectStore miror.ObjectStore, wal miror.WAL, sessionID miror.SessionID, hashesToSend []miror.ObjectHash) error {
	log.Debug().Int("count", len(hashesToSend)).Msg("Starting to send objects")
	for _, hash := range hashesToSend {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("context cancelled during object send: %w", err)
		}

		// Log send
		if err := wal.LogSend(sessionID, hash); err != nil {
			return fmt.Errorf("failed to log send for %s: %w", hash, err)
		}

		// Get object data
		data, err := objectStore.GetObject(ctx, hash)
		if err != nil {
			return fmt.Errorf("failed to get object %s: %w", hash, err)
		}

		// Encode DATA message (offset 0 for M1)
		dataBody, err := miror.EncodeDataMessage(hash, 0, data)
		if err != nil {
			return fmt.Errorf("failed to encode DATA message for %s: %w", hash, err)
		}

		// Send DATA
		log.Debug().Str("hash", hash.String()).Int("size", len(data)).Msg("Sending DATA")
		if err := transport.Send(ctx, miror.MessageTypeData, dataBody); err != nil {
			return fmt.Errorf("failed to send DATA message for %s: %w", hash, err)
		}

		// TODO: Wait for ACK (M2)

		// Complete transfer in WAL
		if err := wal.CompleteTransfer(sessionID, hash); err != nil {
			return fmt.Errorf("failed to complete transfer for %s: %w", hash, err)
		}
	}

	// After sending all data, send COMPLETE
	log.Debug().Msg("Finished sending objects, sending COMPLETE")
	completeBody, err := miror.EncodeCompleteMessage(sessionID)
	if err != nil {
		return fmt.Errorf("failed to encode COMPLETE message: %w", err)
	}
	if err := transport.Send(ctx, miror.MessageTypeComplete, completeBody); err != nil {
		return fmt.Errorf("failed to send COMPLETE message: %w", err)
	}
	log.Info().Int("objects_sent", len(hashesToSend)).Msg("Server push complete")
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
				Usage:       "Path to the vault file (required)",
				Destination: &config.VaultPath,
				Required:    true, // Make vault path mandatory
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
				Usage:   "Addresses to listen on (e.g., :7001)",
				Value:   cli.NewStringSlice(config.ListenAddresses...), // Use default from config
			},
			&cli.StringSliceFlag{
				Name:    "peer",
				Aliases: []string{"P"},
				Usage:   "Known peers to connect to (for client mode, not used by daemon)",
			},
			&cli.BoolFlag{
				Name:        "discovery",
				Aliases:     []string{"d"},
				Usage:       "Enable mDNS discovery (not implemented)",
				Value:       true,
				Destination: &config.DiscoveryEnabled,
			},
			&cli.DurationFlag{
				Name:        "sync-interval",
				Aliases:     []string{"i"},
				Usage:       "Interval for automatic synchronization (not implemented)",
				Value:       5 * time.Minute,
				Destination: &config.SyncInterval,
			},
			&cli.BoolFlag{
				Name:  "verbose", // Add verbose flag for convenience
				Usage: "Enable verbose (debug) logging",
				Value: false,
			},
		},
		Action: func(c *cli.Context) error {
			// Expand paths
			config.WALPath = expandPath(config.WALPath)
			config.PIDFile = expandPath(config.PIDFile)
			config.VaultPath = expandPath(config.VaultPath) // Expand vault path too

			// Get values from string slice flags
			config.ListenAddresses = c.StringSlice("listen")
			config.Peers = c.StringSlice("peer") // Not used by server, but parse anyway

			// Override log level if verbose is set
			if c.Bool("verbose") {
				config.LogLevel = "debug"
			}

			// Run the daemon
			return runDaemon(config)
		},
	}

	if err := app.Run(os.Args); err != nil {
		// Use fmt here as logger might not be initialized or working
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// Need to add SetConnection to TCPTransport in internal/miror/transport.go
// func (t *TCPTransport) SetConnection(conn net.Conn) {
// 	t.conn = conn
// }
// Need to add DecodeOfferMessage to internal/miror/miror.go (if not already present)
// func DecodeOfferMessage(data []byte) ([]ObjectHash, error) { ... }
// Need to add EncodeAcceptMessage to internal/miror/miror.go (if not already present)
// func EncodeAcceptMessage(hashes []ObjectHash) ([]byte, error) { ... }
// Need to add DecodeDataMessage to internal/miror/miror.go (if not already present)
// func DecodeDataMessage(data []byte) (ObjectHash, uint64, []byte, error) { ... }
