# n1 System Design

This document outlines the high-level architecture and key features of the n1 system as of Milestone 0 (M0) and looks ahead to planned capabilities.

## Architecture Overview

n1 is designed as a local-first, privacy-preserving personal knowledge and security workbench built primarily in Go.

### Core Principles

*   **Local-First:** Data resides primarily on the user's device. Cloud interaction is optional and opt-in (e.g., for future sync or model access).
*   **Privacy & Security:** User data is encrypted at rest using strong, standard cryptography, with keys managed securely.
*   **Robustness:** Operations, especially those modifying core data or keys, are designed to be resilient against failure and prevent data loss.
*   **Minimal Dependencies:** Core functionality relies on Go standard libraries, SQLite, and minimal, well-vetted third-party libraries.

### Major Components (M0)

1.  **CLI (`bosr`):** The primary user interface in M0, built using Go (`cmd/bosr`) and the `urfave/cli` library. It orchestrates all core operations.
2.  **Core Logic (Internal Packages):** Encapsulated within the `internal/` directory:
    *   `crypto`: Handles key generation (AES-256), application-level encryption/decryption (AES-GCM), and secure key derivation (HKDF, though potentially unused currently).
    *   `secretstore`: Provides a platform-agnostic interface for storing the master key securely using OS keychains (macOS), DPAPI (Windows), or a fallback file (Linux).
    *   `dao`: Data Access Objects (`VaultDAO`, `SecureVaultDAO`) provide an abstraction layer for interacting with the vault storage, handling encryption/decryption before database writes/reads.
    *   `sqlite`: Manages opening and interacting with the underlying SQLite database file.
    *   `migrations`: Handles database schema creation and evolution in a versioned manner.
    *   `log`: Provides structured logging using `zerolog`.
    *   `holdr`: Placeholder for the core domain model (`Hold`).
3.  **Storage:** A standard SQLite database file stores the user's vault data.

*(Flow Example: `bosr put vault.db mykey myvalue` -> CLI parses -> gets master key via `secretstore` -> calls `SecureVaultDAO.Put` -> `crypto.EncryptBlob` -> `VaultDAO.Put` -> `sqlite` writes encrypted blob to DB file)*

### Data Model

*   **Hold (Conceptual):** The atomic unit of information (note, credential, task, etc.). Intended to be an immutable JSON record. *(Note: `internal/holdr` is currently a placeholder; M0 focuses on the underlying storage mechanism).*
*   **Blob (Conceptual):** Binary attachments associated with Holds (future).
*   **Vault Table (M0 Implementation):** The primary storage in M0 is a single SQLite table named `vault`:
    *   `id` (INTEGER PRIMARY KEY): Unique row identifier.
    *   `key` (TEXT UNIQUE NOT NULL): User-defined unique key for the record.
    *   `value` (BLOB NOT NULL): The **encrypted** payload (using AES-GCM with the master key) representing the Hold's content.
    *   `created_at`, `updated_at` (TIMESTAMP): Standard metadata columns.
*   **Event Log (Future):** The long-term vision includes an append-only event log as the source of truth, enabling robust synchronization and history, aligning with M1 goals.

### Encryption

*   **Strategy:** Application-level encryption. Data is encrypted/decrypted by the Go application *before* being written to / *after* being read from the SQLite database. See [ADR-001](4_DECISIONS_CONVENTIONS.md#adr-001-encryption-strategy) for rationale.
*   **Algorithm:** AES-256-GCM used via `crypto/aes` and `crypto/cipher`. Each `value` blob in the `vault` table is encrypted independently.
*   **Master Key:** A single 256-bit (32-byte) master key is generated (`crypto.Generate`) for each vault file.
*   **Key Storage:** The master key is stored securely using the `internal/secretstore` package, keyed by the absolute path of the vault file.
*   **Key Rotation:** The `bosr key rotate` command generates a new master key, creates a backup (`.bak`), re-encrypts all data into a temporary file (`.tmp`), updates the key in the secret store, and atomically replaces the original file. See [ADR-002](4_DECISIONS_CONVENTIONS.md#adr-002-key-rotation) for details.

### Storage

*   **Database:** Standard SQLite. The database file itself is **plaintext** (unencrypted), containing encrypted `value` blobs.
*   **Access:** Managed via the `internal/sqlite` package using the `mattn/go-sqlite3` driver (without SQLCipher extensions).
*   **Schema:** Defined and managed by the `internal/migrations` package, ensuring consistent database structure across versions. The initial migration creates the `vault` table, index, and update trigger.
*   **Future:** Potential support for WASM/IndexedDB for web-based versions.

---

## Features

### CLI (`bosr`)

The reference command-line interface (`bᴏx ‑ ᴏᴘᴇɴ ‑ sᴇᴀʟ ‑ ʀᴏᴛᴀᴛᴇ`) provides the core functionality available in M0.

*   **`bosr init <vault.db>`:**
    *   Generates a new master key.
    *   Stores the key in the OS secret store.
    *   Creates a new, empty SQLite database file at the specified path.
    *   Runs initial database migrations (`BootstrapVault`).
    *   Adds a canary record (`__n1_canary__`) to allow verifying key validity on open.
*   **`bosr open <vault.db>`:**
    *   Retrieves the master key from the secret store.
    *   Opens the SQLite database file.
    *   **Verifies key validity** by attempting to decrypt the canary record. Reports success only if decryption succeeds and the content matches.
*   **`bosr put <vault.db> <key> <value>`:**
    *   Retrieves the master key.
    *   Encrypts the provided `value` using AES-GCM.
    *   Inserts or updates the record associated with the `key` in the `vault` table with the encrypted blob.
*   **`bosr get <vault.db> <key>`:**
    *   Retrieves the master key.
    *   Reads the encrypted blob associated with the `key` from the `vault` table.
    *   Decrypts the blob using AES-GCM.
    *   Prints the resulting plaintext value to standard output.
*   **`bosr key rotate <vault.db>`:**
    *   Performs an atomic, backup-driven key rotation process (see Encryption section and [ADR-002](4_DECISIONS_CONVENTIONS.md#adr-002-key-rotation)).
    *   Includes pre-flight checks for disk space and warnings for large vaults.
    *   Provides progress reporting during data migration.
    *   Supports a `--dry-run` flag.

### Synchronization (M1 - Mirror) - Planned

*   **Goal:** Provide seamless, encrypted, eventually consistent synchronization between n1 vault replicas on different devices.
*   **Approach:** Likely involves an append-only event log, logical clocks/cursors, and a dedicated sync protocol (`miror` library).
*   **Interface:** Planned `bosr sync --peer <address> --follow` command.

### Web UI (M2 - Visor) - Planned

*   **Goal:** Provide a local graphical interface for interacting with the vault.
*   **Technology:** Planned use of Wails (Go + WebView), HTMX, and Tailwind CSS.

### Other Planned Features

*   **Export/Import (M3):** Securely exporting and importing vault data.
*   **Multiple Vaults (M4):** Ability to work with more than one vault concurrently.
*   **Scopes (M5):** Implementing user-defined contexts like Inbox, Sandbox.
*   **Vector Search:** Enabling semantic search within Holds using `go-vec`.
*   **Model Adapters:** Interfacing with local (Ollama) or remote (GPT-4) AI models.
*   **Integrations (M8):** Ingesting data from external sources like email or calendars.

---

This document provides a snapshot of the system design. Refer to specific ADRs and code for implementation details. It will be updated as the system evolves.