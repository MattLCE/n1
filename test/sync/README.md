# n1 Sync Tests

This directory contains tests for the n1 synchronization functionality (Milestone 1 - Mirror). The tests verify that the sync functionality works correctly under various network conditions and scenarios.

## Test Types

1. **Basic Sync Tests** (`sync_test.go`): These tests verify the basic functionality of the sync feature, including:
   - Syncing between two empty vaults
   - Syncing from a populated vault to an empty vault
   - Handling conflicts when both vaults have different values for the same key

2. **Network Simulation Tests** (`network_test.go`): These tests use Toxiproxy to simulate different network conditions:
   - Normal LAN: 1ms latency, no packet loss
   - Bad WiFi: 200ms latency, 5% packet loss, 2Mbps bandwidth limit
   - Mobile Edge: 1000ms latency, 30% packet loss, 56kbps bandwidth limit

3. **Resumable Transfer Tests**: These tests verify that transfers can be resumed after interruption:
   - Transferring a large file (5MB)
   - Interrupting the transfer midway
   - Resuming the transfer and verifying completion

4. **Continuous Sync Tests**: These tests verify the "follow" mode that keeps vaults in sync:
   - Starting continuous sync between two vaults
   - Adding data to one vault and verifying it appears in the other
   - Changing network conditions and verifying sync still works
   - Disconnecting and reconnecting the vaults

## Running the Tests

### Prerequisites

- Docker and Docker Compose
- Go 1.23 or later
- Make

### Running All Tests

To run all the sync tests in Docker containers with network simulation:

```bash
make test-net
```

This will:
1. Build the Docker images
2. Start the containers (toxiproxy, vault1, vault2, test-runner)
3. Run the tests
4. Shut down the containers

### Running Specific Tests

To run a specific test or test suite:

```bash
make test-net-TestSyncBasic
make test-net-TestSyncWithNetworkProfiles
make test-net-TestSyncResumableWithNetworkInterruption
make test-net-TestSyncContinuousWithNetworkChanges
```

### Cleaning Up

To clean up the Docker containers and test data:

```bash
make test-net-clean
```

## Test Environment

The test environment consists of:

1. **Toxiproxy**: A TCP proxy that simulates network conditions like latency, packet loss, and bandwidth limitations.
2. **Vault1**: A container running the n1 application with a vault.
3. **Vault2**: Another container running the n1 application with a different vault.
4. **Test Runner**: A container that runs the tests, connecting to vault1 and vault2 through toxiproxy.

## Manual Testing on Physical Devices

For testing on physical devices (Windows laptops, Android phone), follow these steps:

1. **Build for the target platforms**:
   ```bash
   # For Windows
   GOOS=windows GOARCH=amd64 go build -o bin/bosr.exe ./cmd/bosr
   
   # For Android (via Termux)
   GOOS=linux GOARCH=arm64 go build -o bin/bosr-android ./cmd/bosr
   ```

2. **Copy the binaries to the target devices**.

3. **On Laptop A**:
   ```bash
   # Initialize a vault
   bosr.exe init vault.db
   
   # Add some data
   bosr.exe put vault.db key1 value1
   bosr.exe put vault.db key2 value2
   
   # For large file testing
   fsutil file createnew big.bin 1048576000
   bosr.exe put vault.db big_file @big.bin
   ```

4. **On Laptop B**:
   ```bash
   # Sync from Laptop A
   bosr.exe sync \\laptopA\vault.db
   
   # Verify the data
   bosr.exe get vault.db key1
   bosr.exe get vault.db key2
   
   # Start continuous sync
   bosr.exe sync --follow \\laptopA\vault.db
   ```

5. **Test network interruptions**:
   - Disconnect the network while syncing
   - Reconnect and verify sync resumes
   - Add data to both vaults while disconnected
   - Reconnect and verify conflicts are resolved

## Chaos Testing

For manual "pull-the-plug" chaos testing:

1. Start a sync of a large file
2. Kill the process or shut down the computer
3. Restart and resume the sync
4. Verify the sync completes successfully

For WAL corruption testing:

1. Start a sync
2. Locate the WAL file
3. Truncate it halfway through
4. Restart the sync
5. Verify recovery works correctly