package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var Version = "1.0.0"

// Embedded configuration placeholders - replaced at download time
// Format: SENTINEL_BOOTSTRAP_<KEY>:<64-char-value-padded-with-underscores>:END
var (
	EmbeddedServerURL = "SENTINEL_BOOTSTRAP_SERVER:________________________________________________________________:END"
	EmbeddedToken     = "SENTINEL_BOOTSTRAP_TOKEN:________________________________________________________________:END"
)

// Configuration flags
var (
	serverURL    = flag.String("server", "", "Sentinel server URL")
	token        = flag.String("token", "", "Enrollment token")
	installPath  = flag.String("path", "", "Installation path (default: platform-specific)")
	showVersion  = flag.Bool("version", false, "Show version")
	repairMode   = flag.Bool("repair", false, "Repair/re-download agent binary")
	verifyMode   = flag.Bool("verify", false, "Verify agent installation integrity")
	silentMode   = flag.Bool("silent", false, "Silent installation (no prompts)")
	upgradeMode  = flag.Bool("upgrade", false, "Upgrade existing installation")
	forceMode    = flag.Bool("force", false, "Force overwrite existing installation")
)

// AgentInfo contains version and download information
type AgentInfo struct {
	Version     string `json:"version"`
	Platform    string `json:"platform"`
	Arch        string `json:"arch"`
	DownloadURL string `json:"downloadUrl"`
	Checksum    string `json:"checksum"`
	Size        int64  `json:"size"`
}

func main() {
	flag.Parse()

	if *showVersion {
		fmt.Printf("Sentinel Bootstrap v%s\n", Version)
		fmt.Printf("OS: %s/%s\n", runtime.GOOS, runtime.GOARCH)
		os.Exit(0)
	}

	// Extract embedded configuration if present
	embServer, embToken := getEmbeddedConfig()

	// Command line overrides embedded config
	if *serverURL == "" && embServer != "" {
		*serverURL = embServer
	}
	if *token == "" && embToken != "" {
		*token = embToken
	}

	// Handle modes
	if *verifyMode {
		if err := verifyInstallation(); err != nil {
			printError("Verification failed: %v", err)
			os.Exit(1)
		}
		printSuccess("Agent installation verified successfully")
		os.Exit(0)
	}

	if *repairMode {
		if *serverURL == "" {
			// Try to read server URL from existing config
			configPath := getConfigPath()
			if cfg, err := loadExistingConfig(configPath); err == nil {
				*serverURL = cfg.ServerURL
			}
		}
		if *serverURL == "" {
			printError("Server URL required for repair. Use --server=URL")
			os.Exit(1)
		}
		if err := repairAgent(); err != nil {
			printError("Repair failed: %v", err)
			os.Exit(1)
		}
		printSuccess("Agent repaired successfully")
		os.Exit(0)
	}

	// Normal installation mode
	if *serverURL == "" {
		if !*silentMode {
			printHeader()
			printError("Server URL is required")
			fmt.Println("\nUsage:")
			fmt.Println("  sentinel-bootstrap --server=http://your-server:8080 --token=YOUR_TOKEN")
			fmt.Println("\nOr download a pre-configured bootstrapper from your Sentinel dashboard.")
		}
		os.Exit(1)
	}

	if *token == "" && !*upgradeMode {
		if !*silentMode {
			printError("Enrollment token is required for new installations")
			fmt.Println("Use --token=TOKEN or download a pre-configured bootstrapper")
		}
		os.Exit(1)
	}

	if err := runBootstrap(); err != nil {
		printError("Installation failed: %v", err)
		os.Exit(1)
	}
}

func printHeader() {
	fmt.Println("╔══════════════════════════════════════════╗")
	fmt.Println("║       Sentinel Agent Bootstrapper        ║")
	fmt.Printf("║              Version %-20s║\n", Version)
	fmt.Println("╚══════════════════════════════════════════╝")
	fmt.Println()
}

func printSuccess(format string, args ...interface{}) {
	fmt.Printf("[OK] "+format+"\n", args...)
}

func printInfo(format string, args ...interface{}) {
	fmt.Printf("[..] "+format+"\n", args...)
}

func printError(format string, args ...interface{}) {
	fmt.Printf("[!!] "+format+"\n", args...)
}

func getEmbeddedConfig() (serverURL, token string) {
	// Extract server URL
	if strings.HasPrefix(EmbeddedServerURL, "SENTINEL_BOOTSTRAP_SERVER:") && strings.HasSuffix(EmbeddedServerURL, ":END") {
		value := EmbeddedServerURL[26 : len(EmbeddedServerURL)-4]
		value = strings.TrimRight(value, "_")
		if value != "" && !strings.HasPrefix(value, "_") {
			serverURL = value
		}
	}

	// Extract token
	if strings.HasPrefix(EmbeddedToken, "SENTINEL_BOOTSTRAP_TOKEN:") && strings.HasSuffix(EmbeddedToken, ":END") {
		value := EmbeddedToken[25 : len(EmbeddedToken)-4]
		value = strings.TrimRight(value, "_")
		if value != "" && !strings.HasPrefix(value, "_") {
			token = value
		}
	}

	return
}

func getDefaultInstallPath() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("ProgramFiles"), "Sentinel Agent")
	case "darwin":
		return "/usr/local/sentinel"
	default:
		return "/opt/sentinel"
	}
}

func getConfigPath() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("ProgramData"), "Sentinel", "config.json")
	case "darwin":
		return "/Library/Application Support/Sentinel/config.json"
	default:
		return "/etc/sentinel/config.json"
	}
}

func getAgentBinaryName() string {
	if runtime.GOOS == "windows" {
		return "sentinel-agent.exe"
	}
	return "sentinel-agent"
}

func runBootstrap() error {
	if !*silentMode {
		printHeader()
		printInfo("Starting installation...")
		fmt.Printf("    Server: %s\n", *serverURL)
		fmt.Printf("    Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
		fmt.Println()
	}

	// Check admin/root privileges
	if !isElevated() {
		return fmt.Errorf("administrator/root privileges required")
	}

	// Determine installation path
	targetPath := *installPath
	if targetPath == "" {
		targetPath = getDefaultInstallPath()
	}

	// Check for existing installation
	agentBinary := filepath.Join(targetPath, getAgentBinaryName())
	existingInstall := false
	if _, err := os.Stat(agentBinary); err == nil {
		existingInstall = true
		if !*forceMode && !*upgradeMode {
			return fmt.Errorf("agent already installed at %s. Use --upgrade or --force to reinstall", targetPath)
		}
		if !*silentMode {
			printInfo("Existing installation found, will upgrade")
		}
	}

	// Get agent version info from server
	printInfo("Fetching agent information from server...")
	agentInfo, err := fetchAgentInfo(*serverURL)
	if err != nil {
		return fmt.Errorf("failed to get agent info: %w", err)
	}

	if !*silentMode {
		printSuccess("Agent version: %s (%d bytes)", agentInfo.Version, agentInfo.Size)
	}

	// Create installation directory
	if err := os.MkdirAll(targetPath, 0755); err != nil {
		return fmt.Errorf("failed to create installation directory: %w", err)
	}

	// Download agent binary
	printInfo("Downloading agent binary...")
	tempPath := filepath.Join(os.TempDir(), "sentinel-agent-download"+getAgentExt())
	if err := downloadAgent(*serverURL, tempPath, agentInfo); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer os.Remove(tempPath)

	// Verify checksum
	printInfo("Verifying checksum...")
	if agentInfo.Checksum != "" {
		if err := verifyChecksum(tempPath, agentInfo.Checksum); err != nil {
			return fmt.Errorf("checksum verification failed: %w", err)
		}
		printSuccess("Checksum verified")
	}

	// Stop existing service if upgrading or reinstalling
	if existingInstall {
		printInfo("Stopping existing services...")
		stopService()
		// Also try to kill any running processes
		if runtime.GOOS == "windows" {
			exec.Command("taskkill", "/F", "/IM", "sentinel-agent.exe").Run()
			exec.Command("taskkill", "/F", "/IM", "sentinel-watchdog.exe").Run()
		}
		time.Sleep(3 * time.Second)
	}

	// Move binary to installation directory
	printInfo("Installing agent...")
	if err := installBinary(tempPath, agentBinary); err != nil {
		return fmt.Errorf("installation failed: %w", err)
	}

	// Set executable permissions (Unix)
	if runtime.GOOS != "windows" {
		os.Chmod(agentBinary, 0755)
	}

	// Download additional components for Windows (desktop helper and OpenH264)
	if runtime.GOOS == "windows" {
		printInfo("Downloading WebRTC desktop helper...")
		helperPath := filepath.Join(targetPath, "sentinel-desktop.exe")
		if err := downloadFile(*serverURL, "/api/bootstrap/desktop-helper", helperPath); err != nil {
			printInfo("Warning: Desktop helper download failed (remote desktop may not work): %v", err)
		} else {
			printSuccess("Desktop helper installed")
		}

		printInfo("Downloading OpenH264 encoder...")
		dllPath := filepath.Join(targetPath, "openh264-2.4.1-win64.dll")
		if err := downloadFile(*serverURL, "/api/bootstrap/openh264", dllPath); err != nil {
			printInfo("Warning: OpenH264 download failed (remote desktop may not work): %v", err)
		} else {
			printSuccess("OpenH264 encoder installed")
		}
	}

	// Run agent installation
	printInfo("Configuring and starting service...")
	if err := runAgentInstall(agentBinary, *serverURL, *token); err != nil {
		return fmt.Errorf("service installation failed: %w", err)
	}

	printSuccess("Agent installed successfully!")

	if !*silentMode {
		fmt.Println()
		fmt.Printf("Installation path: %s\n", targetPath)
		fmt.Printf("Agent version: %s\n", agentInfo.Version)
		fmt.Println()
		fmt.Println("The agent is now running and will automatically")
		fmt.Println("start when the system boots.")
	}

	return nil
}

func fetchAgentInfo(serverURL string) (*AgentInfo, error) {
	url := fmt.Sprintf("%s/api/bootstrap/agent-info?platform=%s&arch=%s",
		serverURL, runtime.GOOS, runtime.GOARCH)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
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
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &info, nil
}

func downloadAgent(serverURL, destPath string, info *AgentInfo) error {
	downloadURL := info.DownloadURL
	if downloadURL == "" {
		downloadURL = fmt.Sprintf("%s/api/bootstrap/agent?platform=%s&arch=%s",
			serverURL, runtime.GOOS, runtime.GOARCH)
	}

	// If token is provided, include it for embedded configuration
	if *token != "" {
		if strings.Contains(downloadURL, "?") {
			downloadURL += "&token=" + *token
		} else {
			downloadURL += "?token=" + *token
		}
	}

	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Get(downloadURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned %d", resp.StatusCode)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	totalSize := resp.ContentLength
	if totalSize <= 0 && info.Size > 0 {
		totalSize = info.Size
	}

	var written int64
	buf := make([]byte, 32*1024)
	lastProgress := -1

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			out.Write(buf[:n])
			written += int64(n)

			// Show progress
			if totalSize > 0 && !*silentMode {
				progress := int(float64(written) / float64(totalSize) * 100)
				if progress != lastProgress && progress%10 == 0 {
					fmt.Printf("\r    Downloading... %d%%", progress)
					lastProgress = progress
				}
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}

	if !*silentMode && totalSize > 0 {
		fmt.Printf("\r    Downloaded: %.2f MB\n", float64(written)/1024/1024)
	}

	return nil
}

// downloadFile downloads a file from a server endpoint to a local path
func downloadFile(serverURL, endpoint, destPath string) error {
	url := serverURL + endpoint + fmt.Sprintf("?platform=%s&arch=%s", runtime.GOOS, runtime.GOARCH)

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}

	// Remove existing file if present
	os.Remove(destPath)

	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	written, err := io.Copy(out, resp.Body)
	if err != nil {
		return err
	}

	if !*silentMode {
		fmt.Printf("    Downloaded: %.2f MB\n", float64(written)/1024/1024)
	}

	return nil
}

func verifyChecksum(filePath, expectedChecksum string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return err
	}

	actualChecksum := hex.EncodeToString(hasher.Sum(nil))
	if actualChecksum != expectedChecksum {
		return fmt.Errorf("expected %s, got %s", expectedChecksum, actualChecksum)
	}

	return nil
}

func installBinary(srcPath, destPath string) error {
	// Read source file
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}

	// Remove destination if exists
	os.Remove(destPath)

	// Write to destination
	if err := os.WriteFile(destPath, data, 0755); err != nil {
		return err
	}

	return nil
}

func runAgentInstall(agentPath, serverURL, token string) error {
	args := []string{"--install", "--server=" + serverURL}
	if token != "" {
		args = append(args, "--token="+token)
	}

	cmd := exec.Command(agentPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func stopService() {
	if runtime.GOOS == "windows" {
		exec.Command("net", "stop", "SentinelAgent").Run()
		exec.Command("net", "stop", "SentinelWatchdog").Run()
	} else {
		exec.Command("systemctl", "stop", "sentinel-agent").Run()
		exec.Command("systemctl", "stop", "sentinel-watchdog").Run()
	}
}

func repairAgent() error {
	if !*silentMode {
		printHeader()
		printInfo("Starting repair...")
	}

	if !isElevated() {
		return fmt.Errorf("administrator/root privileges required")
	}

	targetPath := *installPath
	if targetPath == "" {
		targetPath = getDefaultInstallPath()
	}

	agentBinary := filepath.Join(targetPath, getAgentBinaryName())

	// Get agent info
	printInfo("Fetching agent information...")
	agentInfo, err := fetchAgentInfo(*serverURL)
	if err != nil {
		return fmt.Errorf("failed to get agent info: %w", err)
	}

	// Stop service
	printInfo("Stopping service...")
	stopService()
	time.Sleep(2 * time.Second)

	// Download fresh binary
	printInfo("Downloading fresh agent binary...")
	tempPath := filepath.Join(os.TempDir(), "sentinel-agent-repair"+getAgentExt())
	if err := downloadAgent(*serverURL, tempPath, agentInfo); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer os.Remove(tempPath)

	// Verify checksum
	if agentInfo.Checksum != "" {
		printInfo("Verifying checksum...")
		if err := verifyChecksum(tempPath, agentInfo.Checksum); err != nil {
			return fmt.Errorf("checksum verification failed: %w", err)
		}
	}

	// Replace binary
	printInfo("Replacing agent binary...")
	if err := installBinary(tempPath, agentBinary); err != nil {
		return fmt.Errorf("replacement failed: %w", err)
	}

	if runtime.GOOS != "windows" {
		os.Chmod(agentBinary, 0755)
	}

	// Re-download additional components for Windows (desktop helper and OpenH264)
	if runtime.GOOS == "windows" {
		printInfo("Re-downloading WebRTC desktop helper...")
		helperPath := filepath.Join(targetPath, "sentinel-desktop.exe")
		if err := downloadFile(*serverURL, "/api/bootstrap/desktop-helper", helperPath); err != nil {
			printInfo("Warning: Desktop helper download failed: %v", err)
		}

		printInfo("Re-downloading OpenH264 encoder...")
		dllPath := filepath.Join(targetPath, "openh264-2.4.1-win64.dll")
		if err := downloadFile(*serverURL, "/api/bootstrap/openh264", dllPath); err != nil {
			printInfo("Warning: OpenH264 download failed: %v", err)
		}
	}

	// Start service
	printInfo("Starting service...")
	if runtime.GOOS == "windows" {
		exec.Command("net", "start", "SentinelAgent").Run()
	} else {
		exec.Command("systemctl", "start", "sentinel-agent").Run()
	}

	return nil
}

func verifyInstallation() error {
	targetPath := *installPath
	if targetPath == "" {
		targetPath = getDefaultInstallPath()
	}

	agentBinary := filepath.Join(targetPath, getAgentBinaryName())

	// Check binary exists
	info, err := os.Stat(agentBinary)
	if err != nil {
		return fmt.Errorf("agent binary not found: %s", agentBinary)
	}

	fmt.Printf("Agent binary: %s (%d bytes)\n", agentBinary, info.Size())

	// Check config exists
	configPath := getConfigPath()
	if _, err := os.Stat(configPath); err != nil {
		return fmt.Errorf("config file not found: %s", configPath)
	}
	fmt.Printf("Config file: %s\n", configPath)

	// Check service status
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("sc", "query", "SentinelAgent")
	} else {
		cmd = exec.Command("systemctl", "is-active", "sentinel-agent")
	}

	output, err := cmd.Output()
	if err != nil {
		fmt.Printf("Service status: NOT RUNNING\n")
	} else {
		if runtime.GOOS == "windows" {
			if bytes.Contains(output, []byte("RUNNING")) {
				fmt.Printf("Service status: RUNNING\n")
			} else {
				fmt.Printf("Service status: STOPPED\n")
			}
		} else {
			status := strings.TrimSpace(string(output))
			fmt.Printf("Service status: %s\n", status)
		}
	}

	return nil
}

func getAgentExt() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// ExistingConfig represents minimal config structure for reading
type ExistingConfig struct {
	ServerURL string `json:"server_url"`
}

func loadExistingConfig(path string) (*ExistingConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg ExistingConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// isElevated checks if running with admin/root privileges
func isElevated() bool {
	switch runtime.GOOS {
	case "windows":
		return isElevatedWindows()
	default:
		return os.Geteuid() == 0
	}
}

// Platform-specific elevation check for Windows
func isElevatedWindows() bool {
	cmd := exec.Command("net", "session")
	err := cmd.Run()
	return err == nil
}
