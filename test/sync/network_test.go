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
	"testing"
	"time"

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
		addr = "localhost:8474"
	}
	return &ToxiproxyClient{
		BaseURL: fmt.Sprintf("http://%s", addr),
	}
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

// TestSyncWithNetworkProfiles tests synchronization with different network profiles
func TestSyncWithNetworkProfiles(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping network profile test in short mode")
	}

	// Get environment variables
	vault1Addr := os.Getenv("N1_VAULT1_ADDR")
	if vault1Addr == "" {
		vault1Addr = "vault1:7001"
	}

	vault2Addr := os.Getenv("N1_VAULT2_ADDR")
	if vault2Addr == "" {
		vault2Addr = "vault2:7002"
	}

	// Create Toxiproxy client
	toxiClient := NewToxiproxyClient()

	// Create proxy for vault1 to vault2 communication
	proxyName := "vault1_to_vault2"
	proxyListen := "0.0.0.0:7010"
	proxyUpstream := vault2Addr
	err := toxiClient.CreateProxy(proxyName, proxyListen, proxyUpstream)
	require.NoError(t, err, "Failed to create proxy")
	defer toxiClient.DeleteProxy(proxyName)

	// Test with different network profiles
	profiles := []NetworkProfile{NormalLAN, BadWiFi, MobileEdge}

	for _, profile := range profiles {
		t.Run(profile.Name, func(t *testing.T) {
			// Apply network profile
			err := toxiClient.ApplyNetworkProfile(proxyName, profile)
			require.NoError(t, err, "Failed to apply network profile")

			// Create test data directory
			testDir := filepath.Join(os.TempDir(), fmt.Sprintf("n1-sync-test-%s", profile.Name))
			err = os.MkdirAll(testDir, 0755)
			require.NoError(t, err, "Failed to create test directory")
			defer os.RemoveAll(testDir)

			// Initialize vault1
			vault1Path := filepath.Join(testDir, "vault1.db")
			cmd := exec.Command("bosr", "init", vault1Path)
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, "Failed to initialize vault1: %s", output)

			// Add test data to vault1
			for i := 0; i < 10; i++ {
				key := fmt.Sprintf("key%d", i)
				value := fmt.Sprintf("value%d", i)
				cmd := exec.Command("bosr", "put", vault1Path, key, value)
				output, err := cmd.CombinedOutput()
				require.NoError(t, err, "Failed to add data to vault1: %s", output)
			}

			// Initialize vault2
			vault2Path := filepath.Join(testDir, "vault2.db")
			cmd = exec.Command("bosr", "init", vault2Path)
			output, err = cmd.CombinedOutput()
			require.NoError(t, err, "Failed to initialize vault2: %s", output)

			// Start sync from vault1 to vault2 (using the proxy)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			cmd = exec.CommandContext(ctx, "bosr", "sync", vault1Path, fmt.Sprintf("toxiproxy:%s", proxyListen))
			output, err = cmd.CombinedOutput()
			require.NoError(t, err, "Sync failed: %s", output)

			// Verify that vault2 has the data from vault1
			for i := 0; i < 10; i++ {
				key := fmt.Sprintf("key%d", i)
				expectedValue := fmt.Sprintf("value%d", i)
				cmd := exec.Command("bosr", "get", vault2Path, key)
				output, err := cmd.CombinedOutput()
				require.NoError(t, err, "Failed to get data from vault2: %s", output)
				assert.Equal(t, expectedValue, string(bytes.TrimSpace(output)), "Value mismatch for key %s", key)
			}

			// Add data to vault2
			for i := 10; i < 20; i++ {
				key := fmt.Sprintf("key%d", i)
				value := fmt.Sprintf("value%d", i)
				cmd := exec.Command("bosr", "put", vault2Path, key, value)
				output, err := cmd.CombinedOutput()
				require.NoError(t, err, "Failed to add data to vault2: %s", output)
			}

			// Sync back from vault2 to vault1
			cmd = exec.CommandContext(ctx, "bosr", "sync", vault2Path, vault1Addr)
			output, err = cmd.CombinedOutput()
			require.NoError(t, err, "Sync failed: %s", output)

			// Verify that vault1 has the data from vault2
			for i := 10; i < 20; i++ {
				key := fmt.Sprintf("key%d", i)
				expectedValue := fmt.Sprintf("value%d", i)
				cmd := exec.Command("bosr", "get", vault1Path, key)
				output, err := cmd.CombinedOutput()
				require.NoError(t, err, "Failed to get data from vault1: %s", output)
				assert.Equal(t, expectedValue, string(bytes.TrimSpace(output)), "Value mismatch for key %s", key)
			}

			t.Logf("Sync test with %s profile completed successfully", profile.Name)
		})
	}
}

// TestSyncResumableWithNetworkInterruption tests resumable synchronization with network interruption
func TestSyncResumableWithNetworkInterruption(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping resumable sync test in short mode")
	}

	// Get environment variables
	vault1Addr := os.Getenv("N1_VAULT1_ADDR")
	if vault1Addr == "" {
		vault1Addr = "vault1:7001"
	}

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
	defer toxiClient.DeleteProxy(proxyName)

	// Create test data directory
	testDir := filepath.Join(os.TempDir(), "n1-sync-resumable-test")
	err = os.MkdirAll(testDir, 0755)
	require.NoError(t, err, "Failed to create test directory")
	defer os.RemoveAll(testDir)

	// Initialize vault1
	vault1Path := filepath.Join(testDir, "vault1.db")
	cmd := exec.Command("bosr", "init", vault1Path)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "Failed to initialize vault1: %s", output)

	// Create a large file (5MB) to add to vault1
	largeFilePath := filepath.Join(testDir, "large_file.bin")
	largeFile, err := os.Create(largeFilePath)
	require.NoError(t, err, "Failed to create large file")
	defer largeFile.Close()

	// Fill the file with random data
	data := make([]byte, 5*1024*1024) // 5MB
	for i := range data {
		data[i] = byte(i % 256)
	}
	_, err = largeFile.Write(data)
	require.NoError(t, err, "Failed to write to large file")
	largeFile.Close()

	// Add the large file to vault1
	cmd = exec.Command("bosr", "put", vault1Path, "large_file", fmt.Sprintf("@%s", largeFilePath))
	output, err = cmd.CombinedOutput()
	require.NoError(t, err, "Failed to add large file to vault1: %s", output)

	// Initialize vault2
	vault2Path := filepath.Join(testDir, "vault2.db")
	cmd = exec.Command("bosr", "init", vault2Path)
	output, err = cmd.CombinedOutput()
	require.NoError(t, err, "Failed to initialize vault2: %s", output)

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
		cmd := exec.Command("bosr", "sync", vault1Path, fmt.Sprintf("toxiproxy:%s", proxyListen))
		cmd.Run() // Ignore errors as we expect the sync to be interrupted
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
	cmd = exec.CommandContext(ctx, "bosr", "sync", vault1Path, fmt.Sprintf("toxiproxy:%s", proxyListen))
	output, err = cmd.CombinedOutput()
	require.NoError(t, err, "Resume sync failed: %s", output)

	// Verify that vault2 has the large file
	cmd = exec.Command("bosr", "get", vault2Path, "large_file")
	output, err = cmd.CombinedOutput()
	require.NoError(t, err, "Failed to get large file from vault2: %s", output)
	assert.Equal(t, len(data), len(output), "Large file size mismatch")

	t.Log("Resumable sync test completed successfully")
}

// TestSyncContinuousWithNetworkChanges tests continuous synchronization with changing network conditions
func TestSyncContinuousWithNetworkChanges(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping continuous sync test in short mode")
	}

	// Get environment variables
	vault1Addr := os.Getenv("N1_VAULT1_ADDR")
	if vault1Addr == "" {
		vault1Addr = "vault1:7001"
	}

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
	defer toxiClient.DeleteProxy(proxyName)

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

	// Initialize vault2
	vault2Path := filepath.Join(testDir, "vault2.db")
	cmd = exec.Command("bosr", "init", vault2Path)
	output, err = cmd.CombinedOutput()
	require.NoError(t, err, "Failed to initialize vault2: %s", output)

	// Apply normal network profile
	err = toxiClient.ApplyNetworkProfile(proxyName, NormalLAN)
	require.NoError(t, err, "Failed to apply normal network profile")

	// Start continuous sync in a goroutine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		cmd := exec.CommandContext(ctx, "bosr", "sync", "--follow", vault2Path, fmt.Sprintf("toxiproxy:%s", proxyListen))
		cmd.Run() // Ignore errors as we'll cancel the context
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

		// Wait for sync to propagate the change
		time.Sleep(5 * time.Second)

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
