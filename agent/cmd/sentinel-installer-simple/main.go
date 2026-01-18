package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Command-line flags
var (
	flagCode   = flag.String("code", "", "Installation code (e.g., AB12-CD34)")
	flagServer = flag.String("server", "", "Server URL (overrides embedded/code config)")
	flagToken  = flag.String("token", "", "Enrollment token (overrides embedded/code config)")
	flagSilent = flag.Bool("silent", false, "Run in silent mode (no prompts)")
	flagHelp   = flag.Bool("help", false, "Show help message")
)

// Default server URL for code validation
const DefaultServerURL = "https://sentinelrmm.us:4443"

// Version is set at build time
var Version = "1.0.0"

// InstallConfig holds the configuration for installation
type InstallConfig struct {
	ServerURL       string
	EnrollmentToken string
	DeviceName      string
}

// CodeValidationResponse from server
type CodeValidationResponse struct {
	Valid           bool   `json:"valid"`
	ServerURL       string `json:"serverUrl"`
	EnrollmentToken string `json:"enrollmentToken"`
	DeviceName      string `json:"deviceName"`
	Error           string `json:"error"`
}

// AgentInfo from server
type AgentInfo struct {
	Version     string `json:"version"`
	Platform    string `json:"platform"`
	Arch        string `json:"arch"`
	DownloadURL string `json:"downloadUrl"`
	Checksum    string `json:"checksum"`
	Size        int64  `json:"size"`
}

func main() {
	// Global panic recovery
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "\n[FATAL ERROR] %v\n", r)
			fmt.Println("\nPress ENTER to exit...")
			bufio.NewReader(os.Stdin).ReadBytes('\n')
			os.Exit(1)
		}
	}()

	// Parse command-line flags
	flag.Parse()

	// Show help if requested
	if *flagHelp {
		fmt.Println("Sentinel Agent Installer (Simple)")
		fmt.Println()
		flag.PrintDefaults()
		os.Exit(0)
	}

	fmt.Println()
	fmt.Println("  ====================================")
	fmt.Println("       Sentinel Agent Installer")
	fmt.Printf("              v%s\n", Version)
	fmt.Println("  ====================================")
	fmt.Println()

	// Check if running as admin
	if !isAdmin() {
		fmt.Println("[WARNING] Not running as administrator.")
		fmt.Println("Please right-click and select 'Run as administrator'")
		fmt.Println()
		fmt.Println("Press ENTER to exit...")
		bufio.NewReader(os.Stdin).ReadBytes('\n')
		os.Exit(1)
	}

	// Get configuration
	config := getConfiguration()
	if config == nil {
		fmt.Println("[ERROR] Installation cancelled.")
		fmt.Println()
		fmt.Println("Press ENTER to exit...")
		bufio.NewReader(os.Stdin).ReadBytes('\n')
		os.Exit(1)
	}

	fmt.Printf("  Server: %s\n", config.ServerURL)
	if config.DeviceName != "" {
		fmt.Printf("  Device: %s\n", config.DeviceName)
	}
	fmt.Printf("  Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Println()

	// Proceed with installation
	if err := doInstall(config); err != nil {
		fmt.Printf("[ERROR] Installation failed: %v\n", err)
		fmt.Println()
		fmt.Println("Press ENTER to exit...")
		bufio.NewReader(os.Stdin).ReadBytes('\n')
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("  ====================================")
	fmt.Println("    INSTALLATION COMPLETED!")
	fmt.Println("  ====================================")
	fmt.Println()
	fmt.Println("Press ENTER to exit...")
	bufio.NewReader(os.Stdin).ReadBytes('\n')
}

func isAdmin() bool {
	_, err := os.Open("\\\\.\\PHYSICALDRIVE0")
	return err == nil
}

func getConfiguration() *InstallConfig {
	// Priority 1: Direct CLI arguments
	if *flagServer != "" && *flagToken != "" {
		return &InstallConfig{
			ServerURL:       *flagServer,
			EnrollmentToken: *flagToken,
		}
	}

	// Priority 2: Code from CLI argument
	if *flagCode != "" {
		config := validateInstallationCode(*flagCode)
		if config != nil {
			return config
		}
		fmt.Printf("[ERROR] Invalid installation code: %s\n", *flagCode)
		return nil
	}

	// Priority 3: Interactive code prompt
	if *flagSilent {
		fmt.Println("[ERROR] No configuration found and running in silent mode.")
		return nil
	}

	return promptForInstallationCode()
}

func promptForInstallationCode() *InstallConfig {
	fmt.Println("Please enter your installation code (e.g., AB12-CD34):")
	fmt.Println()

	maxAttempts := 3
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		fmt.Printf("Installation Code: ")
		reader := bufio.NewReader(os.Stdin)
		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf("[ERROR] Failed to read input: %v\n", err)
			continue
		}

		code := strings.TrimSpace(input)
		if code == "" {
			if attempt < maxAttempts {
				fmt.Printf("No code entered. Try again. (%d/%d)\n", attempt, maxAttempts)
			}
			continue
		}

		fmt.Println("Validating code...")
		config := validateInstallationCode(code)
		if config != nil {
			fmt.Println("[OK] Code validated!")
			return config
		}

		if attempt < maxAttempts {
			fmt.Printf("[ERROR] Invalid or expired code. (%d/%d)\n", attempt, maxAttempts)
			fmt.Println()
		}
	}

	fmt.Println("[ERROR] Maximum attempts exceeded.")
	return nil
}

func validateInstallationCode(code string) *InstallConfig {
	code = strings.ToUpper(strings.TrimSpace(code))
	code = strings.ReplaceAll(code, " ", "")
	code = strings.ReplaceAll(code, "-", "")

	if len(code) == 8 {
		code = code[0:4] + "-" + code[4:8]
	}

	serverURL := DefaultServerURL
	if *flagServer != "" {
		serverURL = *flagServer
	}

	apiURL := fmt.Sprintf("%s/api/public/install/validate-code?code=%s", serverURL, url.QueryEscape(code))

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(apiURL)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var result CodeValidationResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil
	}

	if !result.Valid {
		return nil
	}

	return &InstallConfig{
		ServerURL:       result.ServerURL,
		EnrollmentToken: result.EnrollmentToken,
		DeviceName:      result.DeviceName,
	}
}

func doInstall(config *InstallConfig) error {
	// Fetch agent info
	fmt.Println("[1/4] Fetching agent information...")
	agentInfo, err := fetchAgentInfo(config.ServerURL)
	if err != nil {
		return fmt.Errorf("failed to fetch agent info: %w", err)
	}
	fmt.Printf("      Agent version: %s\n", agentInfo.Version)

	// Download agent
	fmt.Println("[2/4] Downloading agent...")
	tempPath := filepath.Join(os.TempDir(), "sentinel-agent-download.exe")
	if err := downloadAgent(config.ServerURL, config.EnrollmentToken, tempPath, agentInfo); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer os.Remove(tempPath)
	fmt.Printf("      Downloaded %.2f MB\n", float64(agentInfo.Size)/1024/1024)

	// Verify checksum
	if agentInfo.Checksum != "" {
		fmt.Println("      Verifying checksum...")
		if err := verifyChecksum(tempPath, agentInfo.Checksum); err != nil {
			return fmt.Errorf("checksum failed: %w", err)
		}
	}

	// Install agent
	fmt.Println("[3/4] Installing agent...")
	installPath := filepath.Join(os.Getenv("ProgramFiles"), "Sentinel Agent")
	agentExe := filepath.Join(installPath, "sentinel-agent.exe")

	// Check for existing installation and stop services
	if _, err := os.Stat(agentExe); err == nil {
		fmt.Println("      Existing installation found - stopping services...")
		exec.Command("net", "stop", "SentinelAgent").Run()
		exec.Command("net", "stop", "SentinelWatchdog").Run()
		// Also try to kill any running processes
		exec.Command("taskkill", "/F", "/IM", "sentinel-agent.exe").Run()
		exec.Command("taskkill", "/F", "/IM", "sentinel-watchdog.exe").Run()
		time.Sleep(3 * time.Second)
	}

	if err := os.MkdirAll(installPath, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Try to remove existing file first (in case it's still locked)
	os.Remove(agentExe)
	time.Sleep(500 * time.Millisecond)

	if err := copyFile(tempPath, agentExe); err != nil {
		return fmt.Errorf("failed to copy agent: %w", err)
	}

	// Run agent install
	fmt.Println("[4/4] Configuring service...")
	agentURL := getAgentURL(config.ServerURL)
	fmt.Printf("      Agent server: %s\n", agentURL)
	cmd := exec.Command(agentExe, "--install", "--server="+agentURL, "--token="+config.EnrollmentToken)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("service configuration failed: %w", err)
	}

	return nil
}

func fetchAgentInfo(serverURL string) (*AgentInfo, error) {
	apiURL := fmt.Sprintf("%s/api/bootstrap/agent-info?platform=%s&arch=%s",
		serverURL, runtime.GOOS, runtime.GOARCH)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(apiURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}

	var info AgentInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}

	return &info, nil
}

func downloadAgent(serverURL, token, destPath string, info *AgentInfo) error {
	downloadURL := info.DownloadURL
	if downloadURL == "" {
		downloadURL = fmt.Sprintf("%s/api/bootstrap/agent?platform=%s&arch=%s",
			serverURL, runtime.GOOS, runtime.GOARCH)
	}

	if token != "" {
		if strings.Contains(downloadURL, "?") {
			downloadURL += "&token=" + token
		} else {
			downloadURL += "?token=" + token
		}
	}

	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Get(downloadURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func verifyChecksum(filePath, expected string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}

	actual := hex.EncodeToString(h.Sum(nil))
	if actual != expected {
		return fmt.Errorf("mismatch")
	}
	return nil
}

// getAgentURL converts the bootstrap URL (public API) to the agent mTLS URL
// This is a temporary workaround for router caching issues - can be removed once resolved
func getAgentURL(bootstrapURL string) string {
	// Convert port 4443 (public API) to port 8443 (agent mTLS)
	return strings.Replace(bootstrapURL, ":4443", ":8443", 1)
}

func copyFile(src, dst string) error {
	os.Remove(dst)

	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	dest, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dest.Close()

	_, err = io.Copy(dest, source)
	return err
}
