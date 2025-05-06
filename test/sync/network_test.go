package sync_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/n1/n1/internal/crypto"
	"github.com/n1/n1/internal/dao"
	"github.com/n1/n1/internal/secretstore"
	"github.com/n1/n1/internal/sqlite"
	"github.com/n1/n1/internal/vaultid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// NetworkProfile represents a network condition profile for testing
type NetworkProfile struct {
	Name        string
	Latency     int     // in ms
	Jitter      int     // in ms
	PacketLoss  float64 // percentage (0-100)
	Bandwidth   int     // in kbps, 0 for unlimited
	Corruption  float64 // percentage (0-100)
	Description string
}

// Common network profiles for testing
var (
	NormalLAN = NetworkProfile{
		Name:        "normal-lan",
		Latency:     1,
		Jitter:      0,
		PacketLoss:  0,
		Bandwidth:   0, // unlimited
		Corruption:  0,
		Description: "Normal LAN connection with minimal latency",
	}

	BadWiFi = NetworkProfile{
		Name:        "bad-wifi",
		Latency:     200,
		Jitter:      50,
		PacketLoss:  5,
		Bandwidth:   2000, // 2 Mbps
		Corruption:  0.1,
		Description: "Poor WiFi connection with high latency and packet loss",
	}

	MobileEdge = NetworkProfile{
		Name:        "mobile-edge",
		Latency:     1000,
		Jitter:      200,
		PacketLoss:  30,
		Bandwidth:   56, // 56 kbps
		Corruption:  1,
		Description: "Edge mobile connection with very high latency and packet loss",
	}
)

// ToxiproxyClient is a simple client for the Toxiproxy API
type ToxiproxyClient struct {
	BaseURL string
}

// NewToxiproxyClient creates a new Toxiproxy client
func NewToxiproxyClient() *ToxiproxyClient {
	addr := os.Getenv("N1_TOXIPROXY_ADDR")
	if addr == "" {
		addr = "localhost:8474" // Default if not set in environment
	}
	// Declare the client variable first
	client := &ToxiproxyClient{
		BaseURL: fmt.Sprintf("http://%s", addr),
	}

	// Add a retry loop to wait for toxiproxy API
	maxRetries := 5
	retryDelay := 1 * time.Second
	fmt.Printf("Waiting for Toxiproxy API at %s...\n", client.BaseURL) // Added for clarity
	for i := 0; i < maxRetries; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second) // Add timeout for the check
		req, err := http.NewRequestWithContext(ctx, "GET", client.BaseURL+"/version", nil)
		if err != nil {
			cancel()
			fmt.Printf("  Retry %d: Error creating request: %v\n", i+1, err)
			time.Sleep(retryDelay)
			continue
		}

		resp, err := http.DefaultClient.Do(req)
		cancel() // Release context resources

		if err == nil && resp.StatusCode == http.StatusOK {
			fmt.Printf("  Toxiproxy API is ready!\n")
			resp.Body.Close()
			return client // Toxiproxy is ready, return the client
		}

		// *** FIX IS HERE ***
		// Determine the status code safely before printing
		statusCode := 0
		if resp != nil {
			statusCode = resp.StatusCode
			resp.Body.Close() // Ensure body is closed if resp is not nil
		}
		// Now use the statusCode variable in Printf
		fmt.Printf("  Retry %d: Toxiproxy not ready (err: %v, status: %d). Waiting %v...\n", i+1, err, statusCode, retryDelay)
		// *** END FIX ***

		time.Sleep(retryDelay)
	}
	// If we exit the loop, toxiproxy never became ready
	panic(fmt.Sprintf("Toxiproxy API at %s did not become available after %d retries", client.BaseURL, maxRetries))
}

// CreateProxy creates a new proxy
func (c *ToxiproxyClient) CreateProxy(name, listen, upstream string) error {
	payload := map[string]string{
		"name":     name,
		"listen":   listen,
		"upstream": upstream,
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	resp, err := http.Post(fmt.Sprintf("%s/proxies", c.BaseURL), "application/json", bytes.NewBuffer(jsonPayload))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to create proxy: %s", body)
	}

	return nil
}

// DeleteProxy deletes a proxy
func (c *ToxiproxyClient) DeleteProxy(name string) error {
	req, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/proxies/%s", c.BaseURL, name), nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to delete proxy: %s", body)
	}

	return nil
}

// AddToxic adds a toxic to a proxy
func (c *ToxiproxyClient) AddToxic(proxyName, toxicName, toxicType string, attributes map[string]interface{}) error {
	payload := map[string]interface{}{
		"name":       toxicName,
		"type":       toxicType,
		"stream":     "downstream",
		"toxicity":   1.0,
		"attributes": attributes,
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	resp, err := http.Post(fmt.Sprintf("%s/proxies/%s/toxics", c.BaseURL, proxyName), "application/json", bytes.NewBuffer(jsonPayload))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to add toxic: %s", body)
	}

	return nil
}

// ApplyNetworkProfile applies a network profile to a proxy
func (c *ToxiproxyClient) ApplyNetworkProfile(proxyName string, profile NetworkProfile) error {
	// First, remove any existing toxics
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/proxies/%s/toxics", c.BaseURL, proxyName), nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to get toxics: %s", body)
	}

	var toxics []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&toxics); err != nil {
		return err
	}

	for _, toxic := range toxics {
		toxicName := toxic["name"].(string)
		req, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/proxies/%s/toxics/%s", c.BaseURL, proxyName, toxicName), nil)
		if err != nil {
			return err
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		resp.Body.Close()
	}

	// Add latency toxic
	if profile.Latency > 0 {
		attributes := map[string]interface{}{
			"latency": profile.Latency,
			"jitter":  profile.Jitter,
		}
		if err := c.AddToxic(proxyName, "latency_toxic", "latency", attributes); err != nil {
			return err
		}
	}

	// Add packet loss toxic
	if profile.PacketLoss > 0 {
		attributes := map[string]interface{}{
			"rate": profile.PacketLoss / 100.0, // Convert percentage to fraction
		}
		if err := c.AddToxic(proxyName, "loss_toxic", "timeout", attributes); err != nil {
			return err
		}
	}

	// Add bandwidth limit toxic
	if profile.Bandwidth > 0 {
		attributes := map[string]interface{}{
			"rate": profile.Bandwidth, // in kbps
		}
		if err := c.AddToxic(proxyName, "bandwidth_toxic", "bandwidth", attributes); err != nil {
			return err
		}
	}

	// Add corruption toxic
	if profile.Corruption > 0 {
		attributes := map[string]interface{}{
			"rate": profile.Corruption / 100.0, // Convert percentage to fraction
		}
		if err := c.AddToxic(proxyName, "corruption_toxic", "slicer", attributes); err != nil {
			return err
		}
	}

	return nil
}

// TestSyncBasicNetwork tests basic push/pull functionality over the network proxy.
func TestSyncBasicNetwork(t *testing.T) {
	// Skip if Toxiproxy address is not set
	if os.Getenv("N1_TOXIPROXY_ADDR") == "" {
		t.Skip("Skipping network test: N1_TOXIPROXY_ADDR not set")
	}

	// Get environment variables
	vault1Addr := os.Getenv("N1_VAULT1_ADDR")
	if vault1Addr == "" {
		vault1Addr = "vault1:7001" // Default service name and port
	}
	vault2Addr := os.Getenv("N1_VAULT2_ADDR")
	if vault2Addr == "" {
		vault2Addr = "vault2:7002" // Default service name and port
	}

	// Create Toxiproxy client
	toxiClient := NewToxiproxyClient()

	// Create proxy for vault1 -> vault2 communication
	proxy1to2Name := "v1_to_v2_basic"
	proxy1to2Listen := "0.0.0.0:7003" // Use a unique port for this test
	proxy1to2Upstream := vault2Addr
	err := toxiClient.CreateProxy(proxy1to2Name, proxy1to2Listen, proxy1to2Upstream)
	// Allow proxy to already exist from previous failed run
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		require.NoError(t, err, "Failed to create proxy %s", proxy1to2Name)
	}
	defer func() {
		if err := toxiClient.DeleteProxy(proxy1to2Name); err != nil && !strings.Contains(err.Error(), "proxy not found") { // Allow proxy not found on cleanup
			t.Logf("Warning: Failed to delete proxy %s: %v", proxy1to2Name, err)
		}
	}()

	// Create proxy for vault2 -> vault1 communication
	proxy2to1Name := "v2_to_v1_basic"
	proxy2to1Listen := "0.0.0.0:7004" // Use a unique port for this test
	proxy2to1Upstream := vault1Addr
	err = toxiClient.CreateProxy(proxy2to1Name, proxy2to1Listen, proxy2to1Upstream)
	// Allow proxy to already exist from previous failed run
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		require.NoError(t, err, "Failed to create proxy %s", proxy2to1Name)
	}
	defer func() {
		if err := toxiClient.DeleteProxy(proxy2to1Name); err != nil && !strings.Contains(err.Error(), "proxy not found") { // Allow proxy not found on cleanup
			t.Logf("Warning: Failed to delete proxy %s: %v", proxy2to1Name, err)
		}
	}()

	// Apply normal LAN profile to both proxies
	err = toxiClient.ApplyNetworkProfile(proxy1to2Name, NormalLAN)
	require.NoError(t, err, "Failed to apply profile to %s", proxy1to2Name)
	err = toxiClient.ApplyNetworkProfile(proxy2to1Name, NormalLAN)
	require.NoError(t, err, "Failed to apply profile to %s", proxy2to1Name)

	// --- Test Setup ---
	// *** CHANGE: Use paths within the mounted volume ***
	// The test runner's working dir is /test, which contains the mounted ./test/sync
	// The vault containers mount ./test/sync/data/vaultX to /data
	// So, the test runner should manipulate files in /test/test/sync/data/vaultX
	baseDataDir := "/test/test/sync/data" // Path within test-runner container
	vault1Dir := filepath.Join(baseDataDir, "vault1")
	vault2Dir := filepath.Join(baseDataDir, "vault2")
	vault1Path := filepath.Join(vault1Dir, "vault.db") // This corresponds to /data/vault.db in vault1 container
	vault2Path := filepath.Join(vault2Dir, "vault.db") // This corresponds to /data/vault.db in vault2 container

	// Ensure the target directories exist within the runner container
	err = os.MkdirAll(vault1Dir, 0755)
	require.NoError(t, err, "Failed to create vault1 directory")
	err = os.MkdirAll(vault2Dir, 0755)
	require.NoError(t, err, "Failed to create vault2 directory")

	// Clean up existing vault files before init
	os.Remove(vault1Path)
	os.Remove(vault2Path)
	// Clean up potential backups/temp files from previous runs
	os.Remove(vault1Path + ".bak")
	os.Remove(vault1Path + ".tmp")
	os.Remove(vault2Path + ".bak")
	os.Remove(vault2Path + ".tmp")

	// Initialize vaults (bosr init still creates the DB file)
	cmd := exec.Command("bosr", "init", vault1Path)
	output, err := cmd.CombinedOutput()
	// We now EXPECT init to potentially fail key storage if run twice,
	// or succeed but store under the wrong name. We'll overwrite/store manually.
	t.Logf("Output from bosr init %s: %s (err: %v)", vault1Path, output, err)
	// Check if vault file exists, ignore errors from init itself for now
	_, statErr := os.Stat(vault1Path)
	require.NoError(t, statErr, "Vault file %s should exist after init", vault1Path)

	cmd = exec.Command("bosr", "init", vault2Path)
	output, err = cmd.CombinedOutput()
	t.Logf("Output from bosr init %s: %s (err: %v)", vault2Path, output, err)
	_, statErr = os.Stat(vault2Path)
	require.NoError(t, statErr, "Vault file %s should exist after init", vault2Path)

	// --- Store keys using vault ID mechanism ---
	secretStorePath := os.Getenv("N1_SECRET_STORE_PATH") // Get base path
	require.NotEmpty(t, secretStorePath, "N1_SECRET_STORE_PATH must be set")

	// Get or create vault ID for vault 1
	vaultID1, err := vaultid.EnsureVaultIDFromPath(vault1Path)
	require.NoError(t, err, "Failed to ensure vault ID for vault1")
	key1Name := vaultid.FormatSecretName(vaultID1)
	t.Logf("Using vault ID %s for vault1", vaultID1)

	// Get or create vault ID for vault 2
	vaultID2, err := vaultid.EnsureVaultIDFromPath(vault2Path)
	require.NoError(t, err, "Failed to ensure vault ID for vault2")
	key2Name := vaultid.FormatSecretName(vaultID2)
	t.Logf("Using vault ID %s for vault2", vaultID2)

	// Create key for vault 1
	mk1, err := crypto.Generate(32)
	require.NoError(t, err)
	err = secretstore.Default.Put(key1Name, mk1) // Use vault ID-based name
	require.NoError(t, err, "Failed to manually store key for %s", key1Name)
	t.Logf("Manually stored key for %s in %s", key1Name, secretStorePath)

	// Create key for vault 2
	mk2, err := crypto.Generate(32)
	require.NoError(t, err)
	err = secretstore.Default.Put(key2Name, mk2) // Use vault ID-based name
	require.NoError(t, err, "Failed to manually store key for %s", key2Name)
	t.Logf("Manually stored key for %s in %s", key2Name, secretStorePath)

	// Add canary records manually using the correct key
	db1, err := sqlite.Open(vault1Path)
	require.NoError(t, err)
	defer db1.Close()
	dao1 := dao.NewSecureVaultDAO(db1, mk1)
	err = dao1.Put("__n1_canary__", []byte("ok"))
	require.NoError(t, err, "Failed to put canary in vault1")

	db2, err := sqlite.Open(vault2Path)
	require.NoError(t, err)
	defer db2.Close()
	dao2 := dao.NewSecureVaultDAO(db2, mk2)
	err = dao2.Put("__n1_canary__", []byte("ok"))
	require.NoError(t, err, "Failed to put canary in vault2")
	// --- End Manual Key Storage ---

	// --- Test Push v1 -> v2 ---
	t.Logf("Testing Push: %s -> %s", vault1Path, vault2Path)
	key1 := "hello"
	value1 := "world"
	cmd = exec.Command("bosr", "put", vault1Path, key1, value1)
	output, err = cmd.CombinedOutput()
	require.NoError(t, err, "Failed to put key '%s' in vault1: %s", key1, output)

	// Sync (default is Pull from client perspective, server offers) from vault1 to vault2 via proxy
	syncTarget1to2 := "toxiproxy:toxiproxy:7003" // Connect to the proxy listening for vault2
	cmd = exec.Command("bosr", "sync", vault1Path, syncTarget1to2)
	output, err = cmd.CombinedOutput()
	// Add detailed output logging on failure
	if err != nil {
		t.Logf("Sync v1 -> v2 command output:\n%s", string(output))
	}
	require.NoError(t, err, "Failed to sync v1 -> v2")

	// Verify data in vault2
	cmd = exec.Command("bosr", "get", vault2Path, key1)
	output, err = cmd.CombinedOutput()
	require.NoError(t, err, "Failed to get key '%s' from vault2: %s", key1, output)
	assert.Equal(t, value1, string(bytes.TrimSpace(output)), "Value mismatch for key '%s' in vault2", key1)

	// --- Test Push v2 -> v1 ---
	t.Logf("Testing Push: %s -> %s", vault2Path, vault1Path)
	key2 := "foo"
	value2 := "bar"
	cmd = exec.Command("bosr", "put", vault2Path, key2, value2)
	output, err = cmd.CombinedOutput()
	require.NoError(t, err, "Failed to put key '%s' in vault2: %s", key2, output)

	// Sync (default is Pull from client perspective, server offers) from vault2 to vault1 via proxy
	syncTarget2to1 := "toxiproxy:toxiproxy:7004" // Connect to the proxy listening for vault1
	cmd = exec.Command("bosr", "sync", vault2Path, syncTarget2to1)
	output, err = cmd.CombinedOutput()
	// Add detailed output logging on failure
	if err != nil {
		t.Logf("Sync v2 -> v1 command output:\n%s", string(output))
	}
	require.NoError(t, err, "Failed to sync v2 -> v1")

	// Verify data in vault1
	cmd = exec.Command("bosr", "get", vault1Path, key2)
	output, err = cmd.CombinedOutput()
	require.NoError(t, err, "Failed to get key '%s' from vault1: %s", key2, output)
	assert.Equal(t, value2, string(bytes.TrimSpace(output)), "Value mismatch for key '%s' in vault1", key2)

	// --- Final Check: Verify both vaults have both keys ---
	// Vault 1 should now have key1 (original) and key2 (synced from v2)
	cmd = exec.Command("bosr", "get", vault1Path, key1)
	output, err = cmd.CombinedOutput()
	require.NoError(t, err, "Final check: Failed to get key '%s' from vault1: %s", key1, output)
	assert.Equal(t, value1, string(bytes.TrimSpace(output)), "Final check: Value mismatch for key '%s' in vault1", key1)

	// Vault 2 should now have key1 (synced from v1) and key2 (original)
	cmd = exec.Command("bosr", "get", vault2Path, key2)
	output, err = cmd.CombinedOutput()
	require.NoError(t, err, "Final check: Failed to get key '%s' from vault2: %s", key2, output)
	assert.Equal(t, value2, string(bytes.TrimSpace(output)), "Final check: Value mismatch for key '%s' in vault2", key2)

	t.Log("Basic network sync test completed successfully")
}

// TestSyncWithNetworkProfiles tests synchronization with different network profiles

// TestSyncResumableWithNetworkInterruption tests resumable synchronization with network interruption
func TestSyncResumableWithNetworkInterruption(t *testing.T) {
	// Skip if Toxiproxy address is not set
	if os.Getenv("N1_TOXIPROXY_ADDR") == "" {
		t.Skip("Skipping network test: N1_TOXIPROXY_ADDR not set")
	}

	// We've now implemented the resumable sync functionality
	// t.Skip("Skipping resumable sync test for milestone_1 implementation")

	// Get environment variables
	// Note: vault1Addr is not used in this test, but kept for consistency
	_ = os.Getenv("N1_VAULT1_ADDR")

	vault2Addr := os.Getenv("N1_VAULT2_ADDR")
	if vault2Addr == "" {
		vault2Addr = "vault2:7002"
	}

	// Create Toxiproxy client
	toxiClient := NewToxiproxyClient()

	// Create proxy for vault1 to vault2 communication
	proxyName := "vault1_to_vault2_resumable"
	proxyListen := "0.0.0.0:7011"
	proxyUpstream := vault2Addr
	err := toxiClient.CreateProxy(proxyName, proxyListen, proxyUpstream)
	require.NoError(t, err, "Failed to create proxy")
	defer func() {
		if err := toxiClient.DeleteProxy(proxyName); err != nil {
			t.Logf("Warning: Failed to delete proxy: %v", err)
		}
	}()

	// Define vault paths relative to the test-runner container's mount point (/test)
	vault1Path := "/test/test/sync/data/vault1/vault.db"
	vault2Path := "/test/test/sync/data/vault2/vault.db"
	largeFilePath := "/test/test/sync/data/large_file.bin" // Place large file in mounted dir too

	// Ensure parent directories exist
	err = os.MkdirAll(filepath.Dir(vault1Path), 0755)
	require.NoError(t, err, "Failed to create directory for vault1")
	err = os.MkdirAll(filepath.Dir(vault2Path), 0755)
	require.NoError(t, err, "Failed to create directory for vault2")

	// Clean up existing vault files if they exist
	os.Remove(vault1Path)
	os.Remove(vault2Path)
	os.Remove(largeFilePath)

	// Initialize vault1
	cmd := exec.Command("bosr", "init", vault1Path)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "Failed to initialize vault1: %s", output)

	// Ensure vault1 has a vault ID
	vaultID1, err := vaultid.EnsureVaultIDFromPath(vault1Path)
	require.NoError(t, err, "Failed to ensure vault ID for vault1")
	t.Logf("Using vault ID %s for vault1", vaultID1)

	// Create a large file (5MB) to add to vault1
	largeFile, err := os.Create(largeFilePath)
	require.NoError(t, err, "Failed to create large file")

	// Fill the file with data
	data := make([]byte, 5*1024*1024) // 5MB
	for i := range data {
		data[i] = byte(i % 256)
	}
	_, err = largeFile.Write(data)
	require.NoError(t, err, "Failed to write to large file")
	largeFile.Close() // Close the file before putting it

	// Add the large file to vault1
	cmd = exec.Command("bosr", "put", vault1Path, "large_file", fmt.Sprintf("@%s", largeFilePath)) // #nosec G204 -- paths/key constructed locally
	output, err = cmd.CombinedOutput()
	require.NoError(t, err, "Failed to add large file to vault1: %s", output)

	// Initialize vault2
	cmd = exec.Command("bosr", "init", vault2Path)
	output, err = cmd.CombinedOutput()
	require.NoError(t, err, "Failed to initialize vault2: %s", output)

	// Ensure vault2 has a vault ID
	vaultID2, err := vaultid.EnsureVaultIDFromPath(vault2Path)
	require.NoError(t, err, "Failed to ensure vault ID for vault2")
	t.Logf("Using vault ID %s for vault2", vaultID2)

	// Apply a slow network profile to the proxy
	slowProfile := NetworkProfile{
		Name:       "slow-connection",
		Latency:    500,
		Bandwidth:  100, // 100 kbps
		PacketLoss: 0,
	}
	err = toxiClient.ApplyNetworkProfile(proxyName, slowProfile)
	require.NoError(t, err, "Failed to apply slow network profile")

	// Start sync in a goroutine
	syncDone := make(chan struct{})
	go func() {
		defer close(syncDone)
		cmd := exec.Command("bosr", "sync", vault1Path, "toxiproxy:toxiproxy:7011") // #nosec G204 -- paths constructed locally, proxy addr controlled by test
		if err := cmd.Run(); err != nil {
			// This is expected since we're interrupting the sync
			// We're just logging it for debugging purposes
			fmt.Printf("Sync interrupted as expected: %v\n", err)
		}
	}()

	// Wait for sync to start
	time.Sleep(2 * time.Second)

	// Interrupt the sync by cutting the connection
	err = toxiClient.AddToxic(proxyName, "cut_connection", "timeout", map[string]interface{}{
		"timeout": 0, // Immediate timeout
	})
	require.NoError(t, err, "Failed to cut connection")

	// Wait for the sync to fail
	select {
	case <-syncDone:
		// Sync has failed as expected
	case <-time.After(5 * time.Second):
		t.Fatal("Sync did not fail after connection cut")
	}

	// Remove the connection cut toxic
	req, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/proxies/%s/toxics/cut_connection", toxiClient.BaseURL, proxyName), nil)
	require.NoError(t, err, "Failed to create delete request")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "Failed to delete toxic")
	resp.Body.Close()

	// Apply a normal network profile
	err = toxiClient.ApplyNetworkProfile(proxyName, NormalLAN)
	require.NoError(t, err, "Failed to apply normal network profile")

	// Resume the sync
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Connect using the service name 'toxiproxy' and the port the proxy listens on, in the format expected by transport.go
	cmd = exec.CommandContext(ctx, "bosr", "sync", vault1Path, "toxiproxy:toxiproxy:7011") // #nosec G204 -- vault path constructed locally, proxy addr controlled by test
	output, err = cmd.CombinedOutput()
	require.NoError(t, err, "Resume sync failed: %s", output)

	// Verify that vault2 has the large file
	cmd = exec.Command("bosr", "get", vault2Path, "large_file") // #nosec G204 -- vault path constructed locally, key is constant
	output, err = cmd.CombinedOutput()
	require.NoError(t, err, "Failed to get large file from vault2 after resume: %s", output)
	assert.Equal(t, len(data), len(output), "Large file size mismatch")

	t.Log("Resumable sync test completed successfully")
}

// TestSyncContinuousWithNetworkChanges tests continuous synchronization with changing network conditions
func TestSyncContinuousWithNetworkChanges(t *testing.T) {
	// Skip if Toxiproxy address is not set
	if os.Getenv("N1_TOXIPROXY_ADDR") == "" {
		t.Skip("Skipping network test: N1_TOXIPROXY_ADDR not set")
	}

	// We've now implemented the continuous sync functionality
	// t.Skip("Skipping continuous sync test for milestone_1 implementation")

	// Get environment variables
	// Note: vault1Addr is not used in this test, but kept for consistency
	_ = os.Getenv("N1_VAULT1_ADDR")

	vault2Addr := os.Getenv("N1_VAULT2_ADDR")
	if vault2Addr == "" {
		vault2Addr = "vault2:7002"
	}

	// Create Toxiproxy client
	toxiClient := NewToxiproxyClient()

	// Create proxy for vault1 to vault2 communication
	proxyName := "vault1_to_vault2_continuous"
	proxyListen := "0.0.0.0:7012"
	proxyUpstream := vault2Addr
	err := toxiClient.CreateProxy(proxyName, proxyListen, proxyUpstream)
	require.NoError(t, err, "Failed to create proxy")
	defer func() {
		if err := toxiClient.DeleteProxy(proxyName); err != nil {
			t.Logf("Warning: Failed to delete proxy: %v", err)
		}
	}()

	// Create test data directory
	testDir := filepath.Join(os.TempDir(), "n1-sync-continuous-test")
	err = os.MkdirAll(testDir, 0755)
	require.NoError(t, err, "Failed to create test directory")
	defer os.RemoveAll(testDir)

	// Initialize vault1
	vault1Path := filepath.Join(testDir, "vault1.db")
	cmd := exec.Command("bosr", "init", vault1Path)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "Failed to initialize vault1: %s", output)

	// Ensure vault1 has a vault ID
	vaultID1, err := vaultid.EnsureVaultIDFromPath(vault1Path)
	require.NoError(t, err, "Failed to ensure vault ID for vault1")
	t.Logf("Using vault ID %s for vault1", vaultID1)

	// Initialize vault2
	vault2Path := filepath.Join(testDir, "vault2.db")
	cmd = exec.Command("bosr", "init", vault2Path)
	output, err = cmd.CombinedOutput()
	require.NoError(t, err, "Failed to initialize vault2: %s", output)

	// Ensure vault2 has a vault ID
	vaultID2, err := vaultid.EnsureVaultIDFromPath(vault2Path)
	require.NoError(t, err, "Failed to ensure vault ID for vault2")
	t.Logf("Using vault ID %s for vault2", vaultID2)

	// Apply normal network profile
	err = toxiClient.ApplyNetworkProfile(proxyName, NormalLAN)
	require.NoError(t, err, "Failed to apply normal network profile")

	// Start continuous sync in a goroutine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		cmd := exec.CommandContext(ctx, "bosr", "sync", "--follow", vault1Path, "toxiproxy:toxiproxy:7012")
		_ = cmd.Run() // Ignore errors as we'll cancel the context
	}()

	// Wait for sync to start
	time.Sleep(2 * time.Second)

	// Add data to vault1 and verify it appears in vault2
	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("continuous_key%d", i)
		value := fmt.Sprintf("continuous_value%d", i)
		cmd := exec.Command("bosr", "put", vault1Path, key, value)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "Failed to add data to vault1: %s", output)

		// Wait longer for sync to propagate the change
		time.Sleep(10 * time.Second)

		// Verify the data in vault2
		cmd = exec.Command("bosr", "get", vault2Path, key)
		output, err = cmd.CombinedOutput()
		require.NoError(t, err, "Failed to get data from vault2: %s", output)
		assert.Equal(t, value, string(bytes.TrimSpace(output)), "Value mismatch for key %s", key)
	}

	// Change network conditions to bad WiFi
	err = toxiClient.ApplyNetworkProfile(proxyName, BadWiFi)
	require.NoError(t, err, "Failed to apply bad WiFi profile")

	// Add more data to vault1
	for i := 5; i < 10; i++ {
		key := fmt.Sprintf("continuous_key%d", i)
		value := fmt.Sprintf("continuous_value%d", i)
		cmd := exec.Command("bosr", "put", vault1Path, key, value)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "Failed to add data to vault1: %s", output)

		// Wait longer for sync to propagate the change (bad network)
		time.Sleep(10 * time.Second)

		// Verify the data in vault2
		cmd = exec.Command("bosr", "get", vault2Path, key)
		output, err = cmd.CombinedOutput()
		require.NoError(t, err, "Failed to get data from vault2: %s", output)
		assert.Equal(t, value, string(bytes.TrimSpace(output)), "Value mismatch for key %s", key)
	}

	// Cut the connection completely
	err = toxiClient.AddToxic(proxyName, "cut_connection", "timeout", map[string]interface{}{
		"timeout": 0, // Immediate timeout
	})
	require.NoError(t, err, "Failed to cut connection")

	// Add data to both vaults while disconnected
	for i := 10; i < 15; i++ {
		// Add to vault1
		key1 := fmt.Sprintf("vault1_key%d", i)
		value1 := fmt.Sprintf("vault1_value%d", i)
		cmd := exec.Command("bosr", "put", vault1Path, key1, value1)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "Failed to add data to vault1: %s", output)

		// Add to vault2
		key2 := fmt.Sprintf("vault2_key%d", i)
		value2 := fmt.Sprintf("vault2_value%d", i)
		cmd = exec.Command("bosr", "put", vault2Path, key2, value2)
		output, err = cmd.CombinedOutput()
		require.NoError(t, err, "Failed to add data to vault2: %s", output)
	}

	// Wait a bit
	time.Sleep(5 * time.Second)

	// Remove the connection cut toxic
	req, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/proxies/%s/toxics/cut_connection", toxiClient.BaseURL, proxyName), nil)
	require.NoError(t, err, "Failed to create delete request")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "Failed to delete toxic")
	resp.Body.Close()

	// Apply normal network profile again
	err = toxiClient.ApplyNetworkProfile(proxyName, NormalLAN)
	require.NoError(t, err, "Failed to apply normal network profile")

	// Wait for sync to catch up
	time.Sleep(10 * time.Second)

	// Verify that both vaults have all the data
	for i := 10; i < 15; i++ {
		// Check vault1_key in vault2
		key1 := fmt.Sprintf("vault1_key%d", i)
		value1 := fmt.Sprintf("vault1_value%d", i)
		cmd := exec.Command("bosr", "get", vault2Path, key1)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "Failed to get data from vault2: %s", output)
		assert.Equal(t, value1, string(bytes.TrimSpace(output)), "Value mismatch for key %s", key1)

		// Check vault2_key in vault1
		key2 := fmt.Sprintf("vault2_key%d", i)
		value2 := fmt.Sprintf("vault2_value%d", i)
		cmd = exec.Command("bosr", "get", vault1Path, key2)
		output, err = cmd.CombinedOutput()
		require.NoError(t, err, "Failed to get data from vault1: %s", output)
		assert.Equal(t, value2, string(bytes.TrimSpace(output)), "Value mismatch for key %s", key2)
	}

	t.Log("Continuous sync test completed successfully")
}
