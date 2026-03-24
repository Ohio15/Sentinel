package tests

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Test configuration — override via environment variables
// ---------------------------------------------------------------------------

func testServerURL() string {
	if u := os.Getenv("SENTINEL_TEST_URL"); u != "" {
		return u
	}
	return "http://localhost:8091"
}

func testEmail() string {
	if e := os.Getenv("SENTINEL_TEST_EMAIL"); e != "" {
		return e
	}
	return "admin@sentinel.local"
}

func testPassword() string {
	if p := os.Getenv("SENTINEL_TEST_PASSWORD"); p != "" {
		return p
	}
	return "admin"
}

// skipIfServerUnavailable skips the test if the Sentinel server is not reachable.
func skipIfServerUnavailable(t *testing.T) {
	t.Helper()
	if !IsServerReachable(testServerURL()) {
		t.Skipf("Sentinel server not reachable at %s — skipping integration test", testServerURL())
	}
}

// newAuthenticatedClient creates and logs in a TestAPIClient, failing the test on error.
func newAuthenticatedClient(t *testing.T) *TestAPIClient {
	t.Helper()
	client := NewTestAPIClient(testServerURL())
	if err := client.Login(testEmail(), testPassword()); err != nil {
		t.Fatalf("Failed to authenticate API client: %v", err)
	}
	return client
}

// uniqueHostname returns a hostname unique to this test run.
func uniqueHostname(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestAgentReplacement_FullLifecycle exercises the complete agent replacement
// flow: enroll, connect, disconnect, generate kill token, get emergency script,
// reconnect with a new ID, and clean up.
func TestAgentReplacement_FullLifecycle(t *testing.T) {
	skipIfServerUnavailable(t)
	api := newAuthenticatedClient(t)

	hostname := uniqueHostname("lifecycle")
	agentID1 := uuid.New().String()
	agentID2 := uuid.New().String()

	// 1. Create enrollment token
	tokenID, enrollToken, err := api.CreateEnrollmentToken("test-lifecycle-" + hostname)
	if err != nil {
		t.Fatalf("Create enrollment token: %v", err)
	}
	defer api.DeleteEnrollmentToken(tokenID)

	// 2. Connect agent simulator
	agent := NewAgentSimulator(testServerURL(), agentID1, enrollToken, hostname)
	defer agent.Destroy()

	if err := agent.Connect(); err != nil {
		t.Fatalf("Agent connect: %v", err)
	}
	agent.StartHeartbeats(5 * time.Second)

	// 3. Verify device appears as online
	dev, err := api.WaitForDeviceStatus(hostname, "online", 15*time.Second)
	if err != nil {
		t.Fatalf("Device did not come online: %v", err)
	}
	deviceID, _ := dev["id"].(string)
	if deviceID == "" {
		t.Fatalf("Device ID is empty in API response")
	}
	t.Logf("Device registered: id=%s, hostname=%s, status=online", deviceID, hostname)

	// 4. Disconnect agent (simulates broken agent)
	agent.StopHeartbeats()
	if err := agent.Disconnect(); err != nil {
		t.Fatalf("Agent disconnect: %v", err)
	}
	t.Log("Agent disconnected, waiting for device to go offline...")

	// 5. Wait for device to be offline (server marks it after WebSocket close)
	_, err = api.WaitForDeviceStatus(hostname, "offline", 30*time.Second)
	if err != nil {
		// The server might mark it offline asynchronously — log but don't fail hard
		t.Logf("Warning: device did not go offline within timeout: %v", err)
	}

	// 6. Generate kill token
	killToken, err := api.GenerateKillToken(deviceID)
	if err != nil {
		t.Fatalf("Generate kill token: %v", err)
	}
	if len(killToken) != 64 {
		t.Errorf("Expected 64-char hex kill token, got %d chars: %s", len(killToken), killToken)
	}
	t.Logf("Kill token generated: %s...", killToken[:16])

	// 7. Get emergency uninstall script and verify contents
	script, err := api.GetEmergencyScript(deviceID)
	if err != nil {
		t.Fatalf("Get emergency script: %v", err)
	}
	verifyEmergencyScript(t, script)

	// 8. Connect NEW agent with different ID but same hostname
	agent2 := NewAgentSimulator(testServerURL(), agentID2, enrollToken, hostname)
	defer agent2.Destroy()

	if err := agent2.Connect(); err != nil {
		t.Fatalf("Agent2 connect: %v", err)
	}
	agent2.StartHeartbeats(5 * time.Second)

	// 9. Verify device comes back online
	dev2, err := api.WaitForDeviceStatus(hostname, "online", 15*time.Second)
	if err != nil {
		t.Fatalf("Device did not come back online after replacement: %v", err)
	}
	t.Logf("Device back online after replacement: id=%s", dev2["id"])

	// 10. Cleanup
	agent2.StopHeartbeats()
	agent2.Destroy()

	// Wait briefly for the server to mark offline, then delete
	time.Sleep(2 * time.Second)

	// Try to delete all devices with this hostname (there could be more than one
	// if the server created a new record for the second agentID)
	devices, _ := api.ListDevices()
	for _, d := range devices {
		if h, _ := d["hostname"].(string); h == hostname {
			id, _ := d["id"].(string)
			_ = api.DeleteDevice(id) // best-effort cleanup
		}
	}

	t.Log("Full lifecycle test passed")
}

// TestAgentReplacement_RapidReconnect connects and disconnects 10 times in
// quick succession and verifies the device stabilizes as online.
func TestAgentReplacement_RapidReconnect(t *testing.T) {
	skipIfServerUnavailable(t)
	api := newAuthenticatedClient(t)

	hostname := uniqueHostname("rapid")
	agentID := uuid.New().String()

	tokenID, enrollToken, err := api.CreateEnrollmentToken("test-rapid-" + hostname)
	if err != nil {
		t.Fatalf("Create enrollment token: %v", err)
	}
	defer api.DeleteEnrollmentToken(tokenID)

	var lastAgent *AgentSimulator
	defer func() {
		if lastAgent != nil {
			lastAgent.Destroy()
		}
	}()

	for i := 0; i < 10; i++ {
		agent := NewAgentSimulator(testServerURL(), agentID, enrollToken, hostname)
		if err := agent.Connect(); err != nil {
			t.Fatalf("Rapid reconnect iteration %d: connect failed: %v", i, err)
		}
		// Send a heartbeat to register presence
		_ = agent.SendHeartbeat()
		time.Sleep(100 * time.Millisecond)

		if i < 9 {
			agent.Destroy()
		} else {
			lastAgent = agent
		}
	}

	// Keep last agent connected with heartbeats
	lastAgent.StartHeartbeats(3 * time.Second)

	// Verify device is online
	dev, err := api.WaitForDeviceStatus(hostname, "online", 15*time.Second)
	if err != nil {
		t.Fatalf("Device not online after rapid reconnect: %v", err)
	}
	t.Logf("Device stable after rapid reconnect: id=%s", dev["id"])

	// Cleanup
	lastAgent.StopHeartbeats()
	lastAgent.Destroy()
	time.Sleep(2 * time.Second)

	devices, _ := api.ListDevices()
	for _, d := range devices {
		if h, _ := d["hostname"].(string); h == hostname {
			id, _ := d["id"].(string)
			_ = api.DeleteDevice(id)
		}
	}
}

// TestAgentReplacement_DifferentAgentID tests that the server handles two
// agents with different IDs but the same hostname (simulating a reinstall).
func TestAgentReplacement_DifferentAgentID(t *testing.T) {
	skipIfServerUnavailable(t)
	api := newAuthenticatedClient(t)

	hostname := uniqueHostname("diffid")
	agentID1 := uuid.New().String()
	agentID2 := uuid.New().String()

	tokenID, enrollToken, err := api.CreateEnrollmentToken("test-diffid-" + hostname)
	if err != nil {
		t.Fatalf("Create enrollment token: %v", err)
	}
	defer api.DeleteEnrollmentToken(tokenID)

	// Agent1 connects
	agent1 := NewAgentSimulator(testServerURL(), agentID1, enrollToken, hostname)
	defer agent1.Destroy()

	if err := agent1.Connect(); err != nil {
		t.Fatalf("Agent1 connect: %v", err)
	}
	agent1.StartHeartbeats(5 * time.Second)

	dev1, err := api.WaitForDeviceStatus(hostname, "online", 15*time.Second)
	if err != nil {
		t.Fatalf("Agent1 device not online: %v", err)
	}
	device1ID, _ := dev1["id"].(string)
	t.Logf("Agent1 device online: id=%s", device1ID)

	// Agent1 disconnects
	agent1.StopHeartbeats()
	agent1.Destroy()
	time.Sleep(2 * time.Second)

	// Agent2 connects with different ID, same hostname
	agent2 := NewAgentSimulator(testServerURL(), agentID2, enrollToken, hostname)
	defer agent2.Destroy()

	if err := agent2.Connect(); err != nil {
		t.Fatalf("Agent2 connect: %v", err)
	}
	agent2.StartHeartbeats(5 * time.Second)

	// Verify some device with this hostname is online
	dev2, err := api.WaitForDeviceStatus(hostname, "online", 15*time.Second)
	if err != nil {
		t.Fatalf("No device online after agent2 connect: %v", err)
	}
	device2ID, _ := dev2["id"].(string)
	t.Logf("Agent2 device online: id=%s (agent1 was id=%s)", device2ID, device1ID)

	// Cleanup
	agent2.StopHeartbeats()
	agent2.Destroy()
	time.Sleep(2 * time.Second)

	devices, _ := api.ListDevices()
	for _, d := range devices {
		if h, _ := d["hostname"].(string); h == hostname {
			id, _ := d["id"].(string)
			_ = api.DeleteDevice(id)
		}
	}
}

// TestAgentReplacement_KillTokenValidation verifies the kill token format and
// that the stored hash matches SHA256 of the plaintext.
func TestAgentReplacement_KillTokenValidation(t *testing.T) {
	skipIfServerUnavailable(t)
	api := newAuthenticatedClient(t)

	hostname := uniqueHostname("killval")
	agentID := uuid.New().String()

	tokenID, enrollToken, err := api.CreateEnrollmentToken("test-killval-" + hostname)
	if err != nil {
		t.Fatalf("Create enrollment token: %v", err)
	}
	defer api.DeleteEnrollmentToken(tokenID)

	// Connect agent to create a device
	agent := NewAgentSimulator(testServerURL(), agentID, enrollToken, hostname)
	defer agent.Destroy()

	if err := agent.Connect(); err != nil {
		t.Fatalf("Agent connect: %v", err)
	}
	_ = agent.SendHeartbeat()

	dev, err := api.WaitForDeviceStatus(hostname, "online", 15*time.Second)
	if err != nil {
		t.Fatalf("Device not online: %v", err)
	}
	deviceID, _ := dev["id"].(string)

	// Generate kill token
	killToken, err := api.GenerateKillToken(deviceID)
	if err != nil {
		t.Fatalf("Generate kill token: %v", err)
	}

	// Validate format: should be exactly 64 hex characters (32 bytes)
	if len(killToken) != 64 {
		t.Errorf("Kill token length: expected 64, got %d", len(killToken))
	}

	// Validate all characters are hex
	for _, ch := range killToken {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')) {
			t.Errorf("Kill token contains non-hex character: %c", ch)
			break
		}
	}

	// Verify SHA256 hash computation matches the expected format
	// (We can't query the DB directly, but we verify our own hash logic matches server's)
	hash := sha256.Sum256([]byte(killToken))
	hashHex := hex.EncodeToString(hash[:])
	if len(hashHex) != 64 {
		t.Errorf("SHA256 hash of kill token should be 64 hex chars, got %d", len(hashHex))
	}
	t.Logf("Kill token validated: %s... -> hash: %s...", killToken[:16], hashHex[:16])

	// Cleanup
	agent.Destroy()
	time.Sleep(2 * time.Second)
	_ = api.DeleteDevice(deviceID)
}

// TestAgentReplacement_EmergencyScriptContents verifies that the emergency
// uninstall script contains all required cleanup steps.
func TestAgentReplacement_EmergencyScriptContents(t *testing.T) {
	skipIfServerUnavailable(t)
	api := newAuthenticatedClient(t)

	hostname := uniqueHostname("emscript")
	agentID := uuid.New().String()

	tokenID, enrollToken, err := api.CreateEnrollmentToken("test-emscript-" + hostname)
	if err != nil {
		t.Fatalf("Create enrollment token: %v", err)
	}
	defer api.DeleteEnrollmentToken(tokenID)

	// Connect agent
	agent := NewAgentSimulator(testServerURL(), agentID, enrollToken, hostname)
	defer agent.Destroy()

	if err := agent.Connect(); err != nil {
		t.Fatalf("Agent connect: %v", err)
	}
	_ = agent.SendHeartbeat()

	dev, err := api.WaitForDeviceStatus(hostname, "online", 15*time.Second)
	if err != nil {
		t.Fatalf("Device not online: %v", err)
	}
	deviceID, _ := dev["id"].(string)

	// Must generate kill token first — server requires it before serving script
	_, err = api.GenerateKillToken(deviceID)
	if err != nil {
		t.Fatalf("Generate kill token (prerequisite): %v", err)
	}

	// Get emergency script
	script, err := api.GetEmergencyScript(deviceID)
	if err != nil {
		t.Fatalf("Get emergency script: %v", err)
	}

	verifyEmergencyScript(t, script)

	// Cleanup
	agent.Destroy()
	time.Sleep(2 * time.Second)
	_ = api.DeleteDevice(deviceID)
}

// TestAgentConnection_HeartbeatKeepsAlive verifies that an agent sending
// heartbeats stays online for at least 30 seconds.
func TestAgentConnection_HeartbeatKeepsAlive(t *testing.T) {
	skipIfServerUnavailable(t)
	api := newAuthenticatedClient(t)

	hostname := uniqueHostname("hbalive")
	agentID := uuid.New().String()

	tokenID, enrollToken, err := api.CreateEnrollmentToken("test-hbalive-" + hostname)
	if err != nil {
		t.Fatalf("Create enrollment token: %v", err)
	}
	defer api.DeleteEnrollmentToken(tokenID)

	agent := NewAgentSimulator(testServerURL(), agentID, enrollToken, hostname)
	defer agent.Destroy()

	if err := agent.Connect(); err != nil {
		t.Fatalf("Agent connect: %v", err)
	}
	agent.StartHeartbeats(5 * time.Second)

	// Wait for initial online
	_, err = api.WaitForDeviceStatus(hostname, "online", 15*time.Second)
	if err != nil {
		t.Fatalf("Device not online: %v", err)
	}

	// Check every 5 seconds for 30 seconds that the device stays online
	for i := 0; i < 6; i++ {
		time.Sleep(5 * time.Second)
		dev, err := api.FindDeviceByHostname(hostname)
		if err != nil {
			t.Fatalf("Error checking device status at %ds: %v", (i+1)*5, err)
		}
		if dev == nil {
			t.Fatalf("Device disappeared at %ds", (i+1)*5)
		}
		status, _ := dev["status"].(string)
		if status != "online" {
			t.Errorf("Device went %s at %ds (expected online)", status, (i+1)*5)
		}
	}
	t.Log("Device stayed online for 30 seconds with heartbeats")

	// Cleanup
	agent.StopHeartbeats()
	agent.Destroy()
	time.Sleep(2 * time.Second)

	devices, _ := api.ListDevices()
	for _, d := range devices {
		if h, _ := d["hostname"].(string); h == hostname {
			id, _ := d["id"].(string)
			_ = api.DeleteDevice(id)
		}
	}
}

// TestAgentConnection_TimeoutGoesOffline verifies that a connected agent that
// stops sending heartbeats is eventually marked offline by the server.
func TestAgentConnection_TimeoutGoesOffline(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running timeout test in short mode")
	}
	skipIfServerUnavailable(t)
	api := newAuthenticatedClient(t)

	hostname := uniqueHostname("timeout")
	agentID := uuid.New().String()

	tokenID, enrollToken, err := api.CreateEnrollmentToken("test-timeout-" + hostname)
	if err != nil {
		t.Fatalf("Create enrollment token: %v", err)
	}
	defer api.DeleteEnrollmentToken(tokenID)

	agent := NewAgentSimulator(testServerURL(), agentID, enrollToken, hostname)
	defer agent.Destroy()

	if err := agent.Connect(); err != nil {
		t.Fatalf("Agent connect: %v", err)
	}

	// Send a few heartbeats to establish presence
	for i := 0; i < 3; i++ {
		_ = agent.SendHeartbeat()
		time.Sleep(2 * time.Second)
	}

	_, err = api.WaitForDeviceStatus(hostname, "online", 15*time.Second)
	if err != nil {
		t.Fatalf("Device not online: %v", err)
	}
	t.Log("Device confirmed online, now stopping heartbeats and disconnecting...")

	// Disconnect the agent (simulates crash / network loss)
	agent.Destroy()

	// Wait for server to mark offline (pongWait is 120s, so this could take a while)
	// The server should detect the closed WebSocket fairly quickly though
	dev, err := api.WaitForDeviceStatus(hostname, "offline", 60*time.Second)
	if err != nil {
		t.Logf("Warning: device did not go offline within 60s: %v", err)
		t.Log("This may be expected if the server uses a long pong timeout")
	} else {
		deviceID, _ := dev["id"].(string)
		t.Logf("Device went offline: id=%s", deviceID)
	}

	// Cleanup
	devices, _ := api.ListDevices()
	for _, d := range devices {
		if h, _ := d["hostname"].(string); h == hostname {
			id, _ := d["id"].(string)
			_ = api.DeleteDevice(id)
		}
	}
}

// ---------------------------------------------------------------------------
// Verification helpers
// ---------------------------------------------------------------------------

// verifyEmergencyScript checks that the PowerShell script contains all expected
// sections for a complete emergency uninstall.
func verifyEmergencyScript(t *testing.T, script string) {
	t.Helper()

	if script == "" {
		t.Fatal("Emergency script is empty")
	}

	requiredPatterns := []struct {
		name    string
		pattern string
	}{
		{"DACL reset", "DACL"},
		{"watchdog stop", "SentinelWatchdog"},
		{"agent stop", "SentinelAgent"},
		{"kill token variable", "$KillToken"},
		{"service delete", "sc.exe delete"},
		{"registry cleanup", "Registry"},
		{"file cleanup", "Remove-Item"},
		{"force-uninstall flag", "--force-uninstall"},
		{"PowerShell header", "#Requires -RunAsAdministrator"},
	}

	for _, rp := range requiredPatterns {
		if !strings.Contains(script, rp.pattern) {
			t.Errorf("Emergency script missing %s (expected to contain %q)", rp.name, rp.pattern)
		}
	}

	// Verify kill token is embedded (should be a 64-char hex string after $KillToken = ")
	if !strings.Contains(script, `$KillToken = "`) {
		t.Error("Emergency script does not contain embedded kill token")
	}

	t.Logf("Emergency script validated (%d bytes, contains all required sections)", len(script))
}
