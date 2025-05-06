# Vault Key Storage Refactoring Design

## 1. Problem Analysis

### Current Implementation
- Master keys are stored using the absolute vault file path as the identifier
- This approach works in simple scenarios but fails when:
  - The same vault is accessed from different paths
  - The vault is accessed from different environments (e.g., Docker containers vs. local CLI)
  - The vault file is moved or renamed
  - The vault is synchronized across different machines

### Test Environment Workaround
- In the test environment, a workaround is already implemented:
  - A shared volume for the secretstore
  - Consistent names like "vault_vault1" instead of absolute paths
  - This demonstrates the viability of using stable identifiers

## 2. Vault Identification Strategy

We recommend using a **content-derived identifier** stored within the vault itself:

### UUID-Based Approach
1. **Generate a UUID** during vault creation
2. **Store the UUID in the vault** in a special metadata table
3. **Use the UUID as the key identifier** in the secretstore

This approach has several advantages:
- **Globally unique**: UUIDs are designed to be globally unique
- **Stable**: The UUID remains the same regardless of the vault's path
- **Self-contained**: The identifier is stored within the vault itself
- **Simple**: UUIDs are easy to generate and use
- **Standardized**: UUIDs are well-understood and widely used

## 3. Implementation Plan

### 3.1 Database Schema Changes

Create a new `metadata` table in the vault:

```sql
CREATE TABLE IF NOT EXISTS metadata (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

This table will store the vault UUID and other metadata:
- `vault_uuid`: The unique identifier for the vault
- `version`: The vault schema version
- `created_at`: When the vault was created
- `name`: Optional user-defined name for the vault

### 3.2 SecretStore Interface Changes

The current interface is simple and doesn't need to change:

```go
type Store interface {
    Put(name string, data []byte) error
    Get(name string) ([]byte, error)
    Delete(name string) error
}
```

However, we need to change how we use it:

1. Create a new package `internal/vaultid` with functions:
   ```go
   // GetVaultID retrieves the UUID from a vault file
   func GetVaultID(vaultPath string) (string, error)
   
   // EnsureVaultID ensures a vault has a UUID, generating one if needed
   func EnsureVaultID(vaultPath string) (string, error)
   
   // GenerateVaultID generates a new UUID for a vault
   func GenerateVaultID() string
   
   // FormatSecretName formats a secret name using the vault ID
   func FormatSecretName(vaultID string) string
   ```

2. Use a consistent naming scheme for secrets:
   ```go
   // Format: "n1_vault_{uuid}"
   secretName := fmt.Sprintf("n1_vault_%s", vaultID)
   ```

### 3.3 Migration Strategy

We need a strategy to migrate existing vaults:

1. **Automatic Migration**:
   - When opening an existing vault, check if it has a UUID
   - If not, generate one and store it in the metadata table
   - Retrieve the master key using the old path-based method
   - Store the master key using the new UUID-based method
   - Keep the old key for a grace period, then remove it

2. **Command-Line Migration**:
   - Add a new command: `bosr migrate <vault.db>`
   - This command performs the migration explicitly
   - Useful for batch migration of multiple vaults

3. **Backward Compatibility**:
   - Always try the UUID-based method first
   - Fall back to the path-based method if the UUID-based method fails
   - Log a deprecation warning when using the path-based method

## 4. Component Updates

### 4.1 CLI Integration

Update the CLI commands to use the new approach:

#### `bosr init`
```go
// 1. Create the vault file and initialize schema
db, err := sqlite.Open(path)
// ...

// 2. Generate a UUID and store it in the metadata table
vaultID, err := vaultid.EnsureVaultID(path)
// ...

// 3. Generate the master key
mk, err := crypto.Generate(32)
// ...

// 4. Store the master key using the UUID-based name
secretName := vaultid.FormatSecretName(vaultID)
err = secretstore.Default.Put(secretName, mk)
// ...
```

#### `bosr open`
```go
// 1. Get the vault UUID from the file
vaultID, err := vaultid.GetVaultID(path)
// ...

// 2. Retrieve the master key using the UUID-based name
secretName := vaultid.FormatSecretName(vaultID)
mk, err := secretstore.Default.Get(secretName)
if err != nil {
    // Fall back to the path-based method for backward compatibility
    mk, fallbackErr := secretstore.Default.Get(path)
    if fallbackErr != nil {
        return fmt.Errorf("failed to get key: %w", err)
    }
    
    // Log a deprecation warning
    log.Warn().Str("path", path).Msg("Using deprecated path-based key storage. Run 'bosr migrate' to update.")
    
    // Migrate the key to the UUID-based method
    err = secretstore.Default.Put(secretName, mk)
    if err != nil {
        log.Warn().Err(err).Msg("Failed to migrate key to UUID-based storage")
    }
    
    return mk, nil
}
// ...
```

#### `bosr key rotate`
```go
// Similar to the open command, but needs to update the key
// ...
```

#### `bosr migrate` (new command)
```go
// 1. Get the vault UUID from the file or generate one
vaultID, err := vaultid.EnsureVaultID(path)
// ...

// 2. Retrieve the master key using the path-based method
mk, err := secretstore.Default.Get(path)
// ...

// 3. Store the master key using the UUID-based name
secretName := vaultid.FormatSecretName(vaultID)
err = secretstore.Default.Put(secretName, mk)
// ...

// 4. Optionally delete the old key
if !keepOld {
    err = secretstore.Default.Delete(path)
    // ...
}
```

### 4.2 Daemon Integration

Update the mirord daemon to use the new approach:

```go
// In the ObjectStoreAdapter constructor
func NewObjectStoreAdapter(db *sql.DB, vaultPath string) (*ObjectStoreAdapter, error) {
    // Get the vault UUID
    vaultID, err := vaultid.GetVaultID(vaultPath)
    if err != nil {
        return nil, fmt.Errorf("failed to get vault ID: %w", err)
    }
    
    // Get the master key using the UUID-based name
    secretName := vaultid.FormatSecretName(vaultID)
    masterKey, err := secretstore.Default.Get(secretName)
    if err != nil {
        // Fall back to the path-based method
        masterKey, fallbackErr := secretstore.Default.Get(vaultPath)
        if fallbackErr != nil {
            return nil, fmt.Errorf("failed to get master key: %w", err)
        }
        
        // Log a deprecation warning
        log.Warn().Str("path", vaultPath).Msg("Using deprecated path-based key storage")
    }
    
    // Create the adapter with the master key
    adapter := &ObjectStoreAdapter{
        db:        db,
        vaultPath: vaultPath,
        vaultID:   vaultID,
        secureDAO: dao.NewSecureVaultDAO(db, masterKey),
        // ...
    }
    
    return adapter, nil
}
```

## 5. Testing Strategy

### 5.1 Unit Tests

1. **VaultID Package Tests**:
   - Test UUID generation and retrieval
   - Test migration from path-based to UUID-based storage

2. **SecretStore Tests**:
   - Test storing and retrieving keys using UUID-based names
   - Test backward compatibility with path-based names

3. **CLI Tests**:
   - Test the new `migrate` command
   - Test backward compatibility with existing vaults

### 5.2 Integration Tests

1. **Path Independence Tests**:
   - Test accessing the same vault from different paths
   - Test moving and renaming vaults

2. **Sync Tests**:
   - Test synchronization between vaults with different paths
   - Test synchronization across different environments

### 5.3 Docker Tests

Update the existing Docker tests to use the new approach:

1. Modify `test/sync/network_test.go` to use UUID-based identifiers
2. Update the Docker environment to test path independence

## 6. Component Decoupling

To reduce interdependencies between components:

1. **Create a VaultID Package**:
   - Encapsulate all UUID-related functionality
   - Provide a clean API for other components

2. **Update the SecretStore Interface**:
   - Keep the interface simple
   - Add helper functions for common operations

3. **Use Dependency Injection**:
   - Pass the secretstore as a parameter to components that need it
   - Avoid global variables like `secretstore.Default`

4. **Create a VaultManager**:
   - Encapsulate vault operations (open, close, etc.)
   - Handle key retrieval and storage
   - Provide a clean API for other components

## 7. Implementation Roadmap

### Phase 1: Core Changes
1. Create the `metadata` table schema
2. Implement the `vaultid` package
3. Update the `bosr init` command to store UUIDs
4. Add backward compatibility to `bosr open`

### Phase 2: Migration
1. Implement the `bosr migrate` command
2. Add automatic migration to `bosr open`
3. Update `bosr key rotate` to use UUIDs

### Phase 3: Daemon Updates
1. Update the mirord daemon to use UUIDs
2. Update the ObjectStoreAdapter to use UUIDs

### Phase 4: Testing
1. Update unit tests
2. Update integration tests
3. Update Docker tests

### Phase 5: Cleanup
1. Add deprecation warnings for path-based methods
2. Plan for eventual removal of path-based fallbacks
3. Document the new approach

## 8. Conclusion

This design provides a comprehensive solution for making key storage independent of absolute vault file paths. By using UUIDs stored within the vault itself, we achieve:

1. **Path Independence**: Keys are stored using stable identifiers
2. **Backward Compatibility**: Existing vaults continue to work
3. **Cross-Platform Support**: The solution works on all platforms
4. **Sync Support**: Vaults can be synchronized across different paths
5. **Security Preservation**: The security model remains unchanged

The implementation can be done incrementally, with backward compatibility maintained throughout the process.