package main

import (
	"bufio"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// httpClient is a shared HTTP client that skips TLS verification for self-signed certs
var httpClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	},
}

// Command-line flags
var (
	flagCode   = flag.String("code", "", "Installation code (e.g., AB12-CD34)")
	flagServer = flag.String("server", "", "Server URL (overrides embedded/code config)")
	flagToken  = flag.String("token", "", "Enrollment token (overrides embedded/code config)")
	flagSilent = flag.Bool("silent", false, "Run in silent mode (no prompts)")
	flagHelp   = flag.Bool("help", false, "Show help message")
)

// Default server host for code validation (used when no embedded config)
const DefaultServerHost = "sentinelrmm.us"

// Ports to try in order of preference (443 first for universal firewall compatibility)
var DefaultPorts = []string{"443", "4443", "8443"}

var logFile *os.File

func initLog() {
	logPath := filepath.Join(os.TempDir(), "sentinel-installer.log")
	var err error
	logFile, err = os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		log.SetOutput(io.MultiWriter(os.Stdout, logFile))
		log.SetFlags(log.Ldate | log.Ltime)
	}
}

func logMsg(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if !*flagSilent {
		fmt.Println(msg)
	}
	if logFile != nil {
		log.Println(msg)
	}
}

// Version is set at build time
var Version = "1.0.0"

// Embedded configuration - using a unique approach to survive Go compiler optimizations
// We store config in a way that the binary patcher can find and replace
// The magic bytes XYZCFG help locate the config block in the binary

// ConfigBlock is a fixed-size structure that gets patched by the server
// Total size: 200 bytes (must stay constant for binary patching to work)
// Layout:
//   [0:6]   = "XYZCFG" magic header
//   [6:59]  = Server URL (53 bytes, null-padded)
//   [59:112] = Token (53 bytes, null-padded)
//   [112:121] = Code (9 bytes, null-padded)
//   [121:200] = Reserved/padding
var configBlock = [200]byte{
	// Magic header: XYZCFG
	'X', 'Y', 'Z', 'C', 'F', 'G',
	// Server URL placeholder (53 bytes): "https://config-placeholder-url.local________________"
	'h', 't', 't', 'p', 's', ':', '/', '/', 'c', 'o', 'n', 'f', 'i', 'g', '-', 'p',
	'l', 'a', 'c', 'e', 'h', 'o', 'l', 'd', 'e', 'r', '-', 'u', 'r', 'l', '.', 'l',
	'o', 'c', 'a', 'l', '_', '_', '_', '_', '_', '_', '_', '_', '_', '_', '_', '_',
	'_', '_', '_', '_', '_',
	// Token placeholder (53 bytes): "token-placeholder-value-replace-me___________________"
	't', 'o', 'k', 'e', 'n', '-', 'p', 'l', 'a', 'c', 'e', 'h', 'o', 'l', 'd', 'e',
	'r', '-', 'v', 'a', 'l', 'u', 'e', '-', 'r', 'e', 'p', 'l', 'a', 'c', 'e', '-',
	'm', 'e', '_', '_', '_', '_', '_', '_', '_', '_', '_', '_', '_', '_', '_', '_',
	'_', '_', '_', '_', '_',
	// Code placeholder (9 bytes): "CODE_____"
	'C', 'O', 'D', 'E', '_', '_', '_', '_', '_',
	// Padding to 200 bytes (79 bytes of zeros follow)
}

// EmbeddedConfig holds configuration extracted from embedded variables
type EmbeddedConfig struct {
	ServerURL       string
	EnrollmentToken string
	InstallCode     string
}

// readEmbeddedConfig reads configuration from the configBlock array
// The server patches this block when generating the installer download
func readEmbeddedConfig() *EmbeddedConfig {
	// Read directly from the config block (which may have been patched)
	config := &EmbeddedConfig{}

	// Check magic header
	magic := string(configBlock[0:6])
	if magic != "XYZCFG" {
		logMsg("[DEBUG] Config block magic header invalid: %s", magic)
		return nil
	}

	// Extract server URL (bytes 6-59, 53 chars, trimmed)
	serverURL := strings.TrimRight(string(configBlock[6:59]), "_\x00")
	if serverURL != "" && !strings.Contains(serverURL, "placeholder") {
		config.ServerURL = serverURL
		logMsg("[DEBUG] Found embedded server URL: %s", serverURL)
	}

	// Extract token (bytes 59-112, 53 chars, trimmed)
	token := strings.TrimRight(string(configBlock[59:112]), "_\x00")
	if token != "" && !strings.Contains(token, "placeholder") {
		config.EnrollmentToken = token
		logMsg("[DEBUG] Found embedded token: %s...", token[:min(10, len(token))])
	}

	// Extract code (bytes 112-121, 9 chars, trimmed)
	code := strings.TrimRight(string(configBlock[112:121]), "_\x00")
	if code != "" && code != "CODE" && !strings.HasPrefix(code, "CODE_") {
		config.InstallCode = code
		logMsg("[DEBUG] Found embedded code: %s", code)
	}

	// Return nil if no valid config found
	if config.ServerURL == "" && config.EnrollmentToken == "" && config.InstallCode == "" {
		logMsg("[DEBUG] No embedded configuration found (placeholders not patched)")
		return nil
	}

	return config
}

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
	// Global panic recovery for unexpected errors
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "\n  [FATAL ERROR] The installer encountered an unexpected error: %v\n", r)
			fmt.Println("\n  This may be caused by security software. Try:")
			fmt.Println("  1. Right-click the installer and select 'Run as administrator'")
			fmt.Println("  2. Temporarily disable antivirus software")
			fmt.Println("  3. Download the installer again")
			fmt.Println("\n  Press ENTER to close this window...")
			bufio.NewReader(os.Stdin).ReadBytes('\n')
			os.Exit(1)
		}
	}()

	// Remove Zone.Identifier (download warning) from self immediately
	removeSelfZoneIdentifier()

	// Parse command-line flags first
	flag.Parse()

	// Show help if requested
	if *flagHelp {
		fmt.Println("Sentinel Agent Installer")
		fmt.Println()
		fmt.Println("Usage:")
		fmt.Println("  sentinel-installer.exe [options]")
		fmt.Println()
		fmt.Println("Options:")
		flag.PrintDefaults()
		fmt.Println()
		fmt.Println("Examples:")
		fmt.Println("  sentinel-installer.exe --code=AB12-CD34")
		fmt.Println("  sentinel-installer.exe --code=AB12-CD34 --silent")
		fmt.Println("  sentinel-installer.exe --server=https://rmm.example.com --token=abc123")
		os.Exit(0)
	}

	// Initialize logging
	initLog()
	logMsg("=== Sentinel Installer Started ===")
	logMsg("Version: %s", Version)
	logMsg("Log file: %s", filepath.Join(os.TempDir(), "sentinel-installer.log"))

	// Set console title
	setConsoleTitle("Sentinel Agent Installer")

	// Print banner (unless silent mode)
	if !*flagSilent {
		printBanner()
	}

	// Get configuration through priority chain
	config := getConfiguration()
	if config == nil {
		printError("Installation cancelled.")
		if !*flagSilent {
			waitForKey()
		}
		os.Exit(1)
	}

	if !*flagSilent {
		fmt.Printf("  Server: %s\n", config.ServerURL)
		if config.DeviceName != "" {
			fmt.Printf("  Device: %s\n", config.DeviceName)
		}
		fmt.Printf("  Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
		fmt.Println()
	}

	// Proceed with installation using config
	proceedWithInstall(config)
}

// getConfiguration obtains installation configuration through priority chain:
// 0. Embedded config in binary (server + token patched by server on download)
// 1. CLI arguments (--server + --token)
// 2. Embedded code in binary -> validates with server
// 3. CLI code argument (--code) -> validates with server
// 4. Interactive prompt for code
func getConfiguration() *InstallConfig {
	// Priority 0: Embedded config (server + token) patched into binary
	embedded := readEmbeddedConfig()
	if embedded != nil && embedded.ServerURL != "" && embedded.EnrollmentToken != "" {
		logMsg("[DEBUG] Using embedded config from binary (server + token)")
		printInfo("Using pre-configured installation settings...")
		return &InstallConfig{
			ServerURL:       embedded.ServerURL,
			EnrollmentToken: embedded.EnrollmentToken,
		}
	}

	// Priority 1: Direct CLI arguments
	if *flagServer != "" && *flagToken != "" {
		logMsg("[DEBUG] Using CLI arguments for config")
		return &InstallConfig{
			ServerURL:       *flagServer,
			EnrollmentToken: *flagToken,
		}
	}

	// Priority 2: Embedded installation code in binary -> validate with server
	if embedded != nil && embedded.InstallCode != "" {
		logMsg("[DEBUG] Validating embedded installation code: %s", embedded.InstallCode)
		printInfo("Validating pre-configured installation code...")
		config := validateInstallationCode(embedded.InstallCode)
		if config != nil {
			return config
		}
		printWarning("Embedded installation code is invalid or expired, trying other methods...")
	}

	// Priority 3: Code from CLI argument
	if *flagCode != "" {
		logMsg("[DEBUG] Validating installation code from CLI: %s", *flagCode)
		config := validateInstallationCode(*flagCode)
		if config != nil {
			return config
		}
		printError("Invalid installation code: %s", *flagCode)
		return nil
	}

	// Priority 4: Interactive code prompt (only if not silent mode)
	if *flagSilent {
		printError("No configuration found and running in silent mode.")
		printError("Provide --code or --server/--token arguments.")
		return nil
	}

	logMsg("[DEBUG] Prompting for installation code")
	return promptForInstallationCode()
}

// promptForInstallationCode shows a dialog/prompt for the user to enter their code
func promptForInstallationCode() *InstallConfig {
	fmt.Println()
	fmt.Println("  ================================================================")
	fmt.Println("  =            Enter Installation Code                           =")
	fmt.Println("  ================================================================")
	fmt.Println()
	fmt.Println("  Please enter the installation code provided by your IT administrator.")
	fmt.Println("  The code looks like: XXXX-XXXX (e.g., AB12-CD34)")
	fmt.Println()

	maxAttempts := 3
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		fmt.Printf("  Installation Code: ")
		reader := bufio.NewReader(os.Stdin)
		input, err := reader.ReadString('\n')
		if err != nil {
			printError("Failed to read input: %v", err)
			continue
		}

		code := strings.TrimSpace(input)
		if code == "" {
			if attempt < maxAttempts {
				printWarning("No code entered. Please try again. (%d/%d)", attempt, maxAttempts)
			}
			continue
		}

		// Validate the code
		fmt.Println()
		printInfo("Validating code...")
		config := validateInstallationCode(code)
		if config != nil {
			printSuccess("Code validated successfully!")
			return config
		}

		if attempt < maxAttempts {
			printError("Invalid or expired code. Please check and try again. (%d/%d)", attempt, maxAttempts)
			fmt.Println()
		}
	}

	printError("Maximum attempts exceeded.")
	return nil
}

// discoverServerURL tries multiple ports to find a reachable server
func discoverServerURL(host string) string {
	// Use a shorter timeout for discovery
	discoverClient := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	for _, port := range DefaultPorts {
		var serverURL string
		if port == "443" {
			serverURL = fmt.Sprintf("https://%s", host)
		} else {
			serverURL = fmt.Sprintf("https://%s:%s", host, port)
		}

		// Try a quick health check or just see if we can connect
		testURL := fmt.Sprintf("%s/api/health", serverURL)
		logMsg("[DEBUG] Trying server at %s", serverURL)

		resp, err := discoverClient.Get(testURL)
		if err != nil {
			logMsg("[DEBUG] Port %s: connection failed - %v", port, err)
			continue
		}
		resp.Body.Close()

		// Any response (even 404) means the server is reachable
		logMsg("[DEBUG] Port %s: server reachable (status %d)", port, resp.StatusCode)
		return serverURL
	}

	// Fallback to first port if nothing works
	logMsg("[DEBUG] No ports responded, using default port %s", DefaultPorts[0])
	return fmt.Sprintf("https://%s:%s", host, DefaultPorts[0])
}

// validateInstallationCode validates a code against the server and returns config
func validateInstallationCode(code string) *InstallConfig {
	// Normalize code: uppercase, remove spaces
	code = strings.ToUpper(strings.TrimSpace(code))
	code = strings.ReplaceAll(code, " ", "")

	// Ensure proper format (add dash if missing)
	if len(code) == 8 && !strings.Contains(code, "-") {
		code = code[0:4] + "-" + code[4:8]
	}

	// Determine server URL for validation
	var serverURL string
	if *flagServer != "" {
		serverURL = *flagServer
	} else {
		// Auto-discover the best port
		printInfo("Discovering server...")
		serverURL = discoverServerURL(DefaultServerHost)
	}

	// Call validation API
	apiURL := fmt.Sprintf("%s/api/public/install/validate-code?code=%s", serverURL, url.QueryEscape(code))
	logMsg("[DEBUG] Validating code at: %s", apiURL)

	resp, err := httpClient.Get(apiURL)
	if err != nil {
		logMsg("[DEBUG] Validation request failed: %v", err)
		printError("Could not reach server. Please check your network connection.")
		return nil
	}
	defer resp.Body.Close()

	var result CodeValidationResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		logMsg("[DEBUG] Failed to parse validation response: %v", err)
		return nil
	}

	if !result.Valid {
		logMsg("[DEBUG] Code validation failed: %s", result.Error)
		return nil
	}

	logMsg("[DEBUG] Code validated, server: %s", result.ServerURL)
	return &InstallConfig{
		ServerURL:       result.ServerURL,
		EnrollmentToken: result.EnrollmentToken,
		DeviceName:      result.DeviceName,
	}
}

func proceedWithInstall(config *InstallConfig) {
	serverURL := config.ServerURL
	token := config.EnrollmentToken

	// Check for admin privileges
	printStep(1, 6, "Checking administrator privileges")
	if !isAdmin() {
		if !*flagSilent {
			fmt.Println()
			printWarning("Administrator privileges required!")
			fmt.Println()
			fmt.Println("  Attempting to restart with elevated privileges...")
			fmt.Println()
		}

		if err := runAsAdmin(); err != nil {
			printError("Failed to elevate: %v", err)
			printError("Please right-click this installer and select 'Run as administrator'")
			if !*flagSilent {
				waitForKey()
			}
			os.Exit(1)
		}
		os.Exit(0)
	}
	printSuccess("Running with administrator privileges")

	// Prepare installation environment
	printStep(2, 6, "Preparing installation environment")
	installPath := filepath.Join(os.Getenv("ProgramFiles"), "Sentinel Agent")
	agentExe := filepath.Join(installPath, "sentinel-agent.exe")
	watchdogExe := filepath.Join(installPath, "sentinel-watchdog.exe")

	// Add Windows Defender exclusion for install path (prevents interference)
	printInfo("Configuring security exclusions...")
	addDefenderExclusion(installPath)

	// Aggressively stop and clean up any existing installation
	if err := prepareInstallDirectory(installPath, agentExe, watchdogExe); err != nil {
		printError("Failed to prepare installation directory: %v", err)
		printError("Please restart your computer and try again.")
		if !*flagSilent {
			waitForKey()
		}
		os.Exit(1)
	}
	printSuccess("Installation environment ready")

	// Fetch agent info from server
	printStep(3, 6, "Fetching agent information")
	agentInfo, err := fetchAgentInfo(serverURL)
	if err != nil {
		printError("Failed to connect to server: %v", err)
		printError("Please check your network connection and try again.")
		if !*flagSilent {
			waitForKey()
		}
		os.Exit(1)
	}
	printSuccess("Agent version: %s", agentInfo.Version)

	// Download agent
	printStep(4, 6, "Downloading Sentinel Agent")
	tempPath := filepath.Join(os.TempDir(), "sentinel-agent-download.exe")
	if err := downloadAgent(serverURL, token, tempPath, agentInfo); err != nil {
		printError("Download failed: %v", err)
		if !*flagSilent {
			waitForKey()
		}
		os.Exit(1)
	}
	defer os.Remove(tempPath)
	printSuccess("Downloaded agent: %.2f MB", float64(agentInfo.Size)/1024/1024)

	// Download watchdog
	printInfo("Downloading watchdog service...")
	watchdogTempPath := filepath.Join(os.TempDir(), "sentinel-watchdog-download.exe")
	if err := downloadWatchdog(serverURL, watchdogTempPath); err != nil {
		printWarning("Could not download watchdog: %v", err)
		// Continue without watchdog - it's not critical
	} else {
		defer os.Remove(watchdogTempPath)
		printSuccess("Downloaded watchdog service")
	}

	// Verify checksum if provided
	if agentInfo.Checksum != "" {
		printInfo("Verifying checksum...")
		if err := verifyChecksum(tempPath, agentInfo.Checksum); err != nil {
			printError("Checksum verification failed: %v", err)
			if !*flagSilent {
				waitForKey()
			}
			os.Exit(1)
		}
		printSuccess("Checksum verified")
	}

	// Install agent
	printStep(5, 6, "Installing Sentinel Agent")

	// Copy agent binary with retries
	printInfo("Installing agent binary...")
	if err := robustFileCopy(tempPath, agentExe); err != nil {
		printError("Failed to install agent binary: %v", err)
		printError("Attempting emergency cleanup...")
		emergencyCleanup(installPath)
		// Try one more time after emergency cleanup
		if err := robustFileCopy(tempPath, agentExe); err != nil {
			printError("Installation failed after emergency cleanup: %v", err)
			printError("Please restart your computer and try again.")
			if !*flagSilent {
				waitForKey()
			}
			os.Exit(1)
		}
	}
	printSuccess("Agent binary installed")

	// Copy watchdog if downloaded
	if _, err := os.Stat(watchdogTempPath); err == nil {
		printInfo("Installing watchdog binary...")
		if err := robustFileCopy(watchdogTempPath, watchdogExe); err != nil {
			printWarning("Failed to install watchdog binary: %v (will continue without watchdog)", err)
		} else {
			removeZoneIdentifier(watchdogExe)
			printSuccess("Watchdog binary installed")
		}
	}

	// Remove Zone.Identifier from installed agent to prevent security warnings
	removeZoneIdentifier(agentExe)

	// Run agent install command
	printInfo("Configuring service...")
	logMsg("[DEBUG] Running: %s --install --server=%s --token=%s...", agentExe, serverURL, token[:min(10, len(token))])
	cmd := exec.Command(agentExe, "--install", "--server="+serverURL, "--token="+token)

	var stdout, stderr strings.Builder
	cmd.Stdout = io.MultiWriter(os.Stdout, &stdout)
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)

	if err := cmd.Run(); err != nil {
		logMsg("[DEBUG] Agent install stdout: %s", stdout.String())
		logMsg("[DEBUG] Agent install stderr: %s", stderr.String())
		printError("Service installation failed: %v", err)
		if !*flagSilent {
			waitForKey()
		}
		os.Exit(1)
	}
	logMsg("[DEBUG] Agent install completed successfully")
	printSuccess("Agent installed and service configured")

	// Verify installation
	printStep(6, 6, "Verifying installation")
	time.Sleep(2 * time.Second)

	agentRunning := isServiceRunning("SentinelAgent")
	watchdogRunning := isServiceRunning("SentinelWatchdog")

	if agentRunning {
		printSuccess("SentinelAgent service is running")
	} else {
		printWarning("SentinelAgent service is not running")
	}

	if watchdogRunning {
		printSuccess("SentinelWatchdog service is running")
	} else {
		printWarning("SentinelWatchdog service is not running")
	}

	// Show completion
	if !*flagSilent {
		fmt.Println()
		fmt.Println("  ================================================================")
		fmt.Println("  =                                                              =")
		fmt.Println("  =          INSTALLATION COMPLETED SUCCESSFULLY                 =")
		fmt.Println("  =                                                              =")
		fmt.Println("  ================================================================")
		fmt.Println()
		fmt.Println("  The Sentinel Agent is now running and will start automatically")
		fmt.Println("  when Windows boots.")
		fmt.Println()
		fmt.Println("  Installation path:", installPath)
		fmt.Println("  Agent version:", agentInfo.Version)
		fmt.Println()

		waitForKey()
	}
}

func printBanner() {
	fmt.Println()
	fmt.Println("  ____             _   _            _")
	fmt.Println(" / ___|  ___ _ __ | |_(_)_ __   ___| |")
	fmt.Println(" \\___ \\ / _ \\ '_ \\| __| | '_ \\ / _ \\ |")
	fmt.Println("  ___) |  __/ | | | |_| | | | |  __/ |")
	fmt.Println(" |____/ \\___|_| |_|\\__|_|_| |_|\\___|_|")
	fmt.Println()
	fmt.Println("       Remote Monitoring & Management")
	fmt.Printf("              Installer v%s\n", Version)
	fmt.Println()
	fmt.Println("  ================================================================")
	fmt.Println()
}

func printStep(current, total int, message string) {
	if !*flagSilent {
		pct := current * 100 / total
		bar := strings.Repeat("=", current*40/total) + strings.Repeat("-", 40-current*40/total)
		fmt.Printf("\n  [%s] %d%%\n", bar, pct)
		fmt.Printf("  Step %d/%d: %s\n", current, total, message)
	}
	logMsg("[STEP %d/%d] %s", current, total, message)
}

func printSuccess(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if !*flagSilent {
		fmt.Printf("  [OK] %s\n", msg)
	}
	logMsg("[OK] %s", msg)
}

func printInfo(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if !*flagSilent {
		fmt.Printf("  [..] %s\n", msg)
	}
	logMsg("[INFO] %s", msg)
}

func printWarning(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if !*flagSilent {
		fmt.Printf("  [!!] %s\n", msg)
	}
	logMsg("[WARN] %s", msg)
}

func printError(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if !*flagSilent {
		fmt.Printf("  [ERROR] %s\n", msg)
	}
	logMsg("[ERROR] %s", msg)
}

func waitForKey() {
	fmt.Println()
	fmt.Println("  Press ENTER to close this window...")
	bufio.NewReader(os.Stdin).ReadBytes('\n')
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func setConsoleTitle(title string) {
	defer func() {
		if r := recover(); r != nil {
			// Silently ignore console title errors - not critical
		}
	}()
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	setConsoleTitleW := kernel32.NewProc("SetConsoleTitleW")
	if err := setConsoleTitleW.Find(); err != nil {
		return // Proc not available
	}
	titlePtr, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	setConsoleTitleW.Call(uintptr(unsafe.Pointer(titlePtr)))
}

func isAdmin() bool {
	_, err := os.Open("\\\\.\\PHYSICALDRIVE0")
	return err == nil
}

func runAsAdmin() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	// Build args string to pass through flags
	args := ""
	if *flagCode != "" {
		args += fmt.Sprintf("--code=%s ", *flagCode)
	}
	if *flagServer != "" {
		args += fmt.Sprintf("--server=%s ", *flagServer)
	}
	if *flagToken != "" {
		args += fmt.Sprintf("--token=%s ", *flagToken)
	}
	if *flagSilent {
		args += "--silent "
	}

	verbPtr, _ := syscall.UTF16PtrFromString("runas")
	exePtr, _ := syscall.UTF16PtrFromString(exe)
	cwdPtr, _ := syscall.UTF16PtrFromString(cwd)
	argPtr, _ := syscall.UTF16PtrFromString(strings.TrimSpace(args))

	var showCmd int32 = 1 // SW_NORMAL

	err = windows.ShellExecute(0, verbPtr, exePtr, argPtr, cwdPtr, showCmd)
	return err
}

func fetchAgentInfo(serverURL string) (*AgentInfo, error) {
	apiURL := fmt.Sprintf("%s/api/bootstrap/agent-info?platform=%s&arch=%s",
		serverURL, runtime.GOOS, runtime.GOARCH)

	resp, err := httpClient.Get(apiURL)
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

	// Use longer timeout for downloads
	downloadClient := &http.Client{
		Timeout: 10 * time.Minute,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	resp, err := downloadClient.Get(downloadURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned HTTP %d", resp.StatusCode)
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

			if totalSize > 0 && !*flagSilent {
				progress := int(float64(written) / float64(totalSize) * 100)
				if progress != lastProgress && progress%5 == 0 {
					bar := strings.Repeat("=", progress*30/100) + strings.Repeat("-", 30-progress*30/100)
					fmt.Printf("\r  [%s] %d%% (%.1f MB)", bar, progress, float64(written)/1024/1024)
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

	if !*flagSilent {
		fmt.Println()
	}
	return nil
}

func downloadWatchdog(serverURL, destPath string) error {
	downloadURL := fmt.Sprintf("%s/api/bootstrap/watchdog?platform=%s&arch=%s",
		serverURL, runtime.GOOS, runtime.GOARCH)

	logMsg("[DEBUG] Downloading watchdog from: %s", downloadURL)

	// Use longer timeout for downloads
	downloadClient := &http.Client{
		Timeout: 5 * time.Minute,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	resp, err := downloadClient.Get(downloadURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned HTTP %d", resp.StatusCode)
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
		return fmt.Errorf("expected %s, got %s", expected, actual)
	}
	return nil
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

func isServiceRunning(serviceName string) bool {
	cmd := exec.Command("sc", "query", serviceName)
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(output), "RUNNING")
}

// =============================================================================
// Robust Installation Functions
// These functions ensure the installer succeeds regardless of target machine state
// =============================================================================

// addDefenderExclusion adds Windows Defender exclusion for the install path
func addDefenderExclusion(path string) {
	// Add folder exclusion
	cmd := exec.Command("powershell", "-Command",
		fmt.Sprintf("Add-MpPreference -ExclusionPath '%s' -ErrorAction SilentlyContinue", path))
	cmd.Run()

	// Add process exclusions
	exec.Command("powershell", "-Command",
		"Add-MpPreference -ExclusionProcess 'sentinel-agent.exe' -ErrorAction SilentlyContinue").Run()
	exec.Command("powershell", "-Command",
		"Add-MpPreference -ExclusionProcess 'sentinel-watchdog.exe' -ErrorAction SilentlyContinue").Run()

	logMsg("[DEBUG] Added Windows Defender exclusions")
}

// prepareInstallDirectory ensures the install directory is ready for installation
// Uses DIRECTORY SWAP strategy to sidestep file locking issues:
// 1. Install to "Sentinel Agent.new"
// 2. Stop services (best effort)
// 3. Rename "Sentinel Agent" → "Sentinel Agent.old"
// 4. Rename "Sentinel Agent.new" → "Sentinel Agent"
// 5. Schedule "Sentinel Agent.old" for deletion on reboot
func prepareInstallDirectory(installPath, agentExe, watchdogExe string) error {
	logMsg("[DEBUG] Preparing install directory using DIRECTORY SWAP strategy: %s", installPath)

	// Define paths for the swap strategy
	newPath := installPath + ".new"
	oldPath := installPath + ".old"

	// Step 1: Clean up any leftover .new or .old directories from previous attempts
	printInfo("Cleaning up previous installation attempts...")
	cleanupOldDirectories(newPath, oldPath)

	// Step 2: Check if this is an upgrade (existing installation present)
	existingInstall := false
	if _, err := os.Stat(agentExe); err == nil {
		existingInstall = true
		logMsg("[DEBUG] Existing installation detected - will use directory swap")
	}

	if existingInstall {
		// UPGRADE PATH: Use directory swap strategy
		return prepareUpgradeWithSwap(installPath, newPath, oldPath, agentExe, watchdogExe)
	}

	// FRESH INSTALL PATH: Simple directory creation
	logMsg("[DEBUG] Fresh installation - creating directory directly")
	printInfo("Creating installation directory...")

	// Remove any remnants
	os.RemoveAll(installPath)

	if err := os.MkdirAll(installPath, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	return nil
}

// prepareUpgradeWithSwap handles upgrade using directory swap to avoid file locking
func prepareUpgradeWithSwap(installPath, newPath, oldPath, agentExe, watchdogExe string) error {
	logMsg("[DEBUG] Starting upgrade with directory swap strategy")

	// Step 1: Stop services and kill processes (best effort - don't fail if this doesn't work)
	printInfo("Stopping existing services...")
	stopService("SentinelWatchdog")
	stopService("SentinelAgent")

	// Kill service host processes
	killServiceProcess("SentinelAgent")
	killServiceProcess("SentinelWatchdog")

	// Kill any running processes
	for i := 0; i < 3; i++ {
		killProcess("sentinel-agent.exe")
		killProcess("sentinel-watchdog.exe")
		killProcessByPath(agentExe)
		killProcessByPath(watchdogExe)
		time.Sleep(300 * time.Millisecond)
	}

	// Brief wait for handles to release
	time.Sleep(1 * time.Second)

	// Step 2: Delete services with proper waiting
	printInfo("Removing existing services...")
	deleteServiceAndWait("SentinelAgent")
	deleteServiceAndWait("SentinelWatchdog")

	// Step 3: Create the new installation directory
	printInfo("Creating new installation directory...")
	os.RemoveAll(newPath) // Clean any leftover
	if err := os.MkdirAll(newPath, 0755); err != nil {
		return fmt.Errorf("failed to create new directory: %w", err)
	}

	// Step 4: Try direct cleanup first (may work if services properly stopped)
	printInfo("Attempting direct file removal...")
	directCleanupSuccess := tryDirectCleanup(installPath, agentExe, watchdogExe)

	if directCleanupSuccess {
		// Direct cleanup worked! Remove .new directory and use original path
		logMsg("[DEBUG] Direct cleanup successful, using original directory")
		os.RemoveAll(newPath)
		return nil
	}

	// Step 5: Direct cleanup failed - use directory swap strategy
	logMsg("[DEBUG] Direct cleanup failed, proceeding with directory swap")
	printInfo("Using directory swap strategy for locked files...")

	// Remove any existing .old directory first
	os.RemoveAll(oldPath)

	// THE KEY SWAP: Rename directories (Windows allows this even with open handles!)
	// Step 5a: Rename current → old
	logMsg("[DEBUG] Renaming %s → %s", installPath, oldPath)
	if err := os.Rename(installPath, oldPath); err != nil {
		logMsg("[DEBUG] Directory rename failed: %v", err)
		// If we can't even rename the directory, try one more aggressive cleanup
		closeProcessesUsingFile(agentExe)
		closeFileHandles(agentExe)
		time.Sleep(2 * time.Second)

		// Try rename again
		if err := os.Rename(installPath, oldPath); err != nil {
			// Last resort: schedule for reboot and create new directory anyway
			logMsg("[DEBUG] Directory rename still failed, scheduling for reboot deletion")
			scheduleDirDeleteOnReboot(installPath)
			// Create the install directory fresh
			os.MkdirAll(installPath, 0755)
			os.RemoveAll(newPath)
			printWarning("Some files are locked. Old files will be removed on next reboot.")
			return nil
		}
	}

	// Step 5b: Rename new → current
	logMsg("[DEBUG] Renaming %s → %s", newPath, installPath)
	if err := os.Rename(newPath, installPath); err != nil {
		// This shouldn't fail, but if it does, try to recover
		logMsg("[DEBUG] New directory rename failed: %v - attempting recovery", err)
		// Try to rename old back
		os.Rename(oldPath, installPath)
		return fmt.Errorf("directory swap failed: %w", err)
	}

	// Step 6: Schedule old directory for deletion on reboot
	logMsg("[DEBUG] Scheduling old directory for deletion: %s", oldPath)
	scheduleDirDeleteOnReboot(oldPath)

	// Also try to delete it now (might work for some files)
	go func() {
		time.Sleep(2 * time.Second)
		os.RemoveAll(oldPath)
	}()

	printSuccess("Directory swap completed successfully")
	return nil
}

// tryDirectCleanup attempts to remove files directly, returns true if successful
func tryDirectCleanup(installPath, agentExe, watchdogExe string) bool {
	// Try to remove the agent binary
	if _, err := os.Stat(agentExe); err == nil {
		// File exists, try to remove it
		forceRemoveFile(agentExe)

		// Check if it's gone
		if _, err := os.Stat(agentExe); err == nil {
			// Still exists - direct cleanup failed
			return false
		}
	}

	// Try to remove watchdog
	if _, err := os.Stat(watchdogExe); err == nil {
		forceRemoveFile(watchdogExe)
		// Don't fail if watchdog can't be removed - it's less critical
	}

	// Remove other files
	forceRemoveFile(filepath.Join(installPath, "config.json"))
	forceRemoveFile(filepath.Join(installPath, "protection.dat"))

	return true
}

// cleanupOldDirectories removes leftover .new and .old directories
func cleanupOldDirectories(newPath, oldPath string) {
	// Try to remove .new directory
	if _, err := os.Stat(newPath); err == nil {
		logMsg("[DEBUG] Removing leftover .new directory")
		os.RemoveAll(newPath)
	}

	// Try to remove .old directory
	if _, err := os.Stat(oldPath); err == nil {
		logMsg("[DEBUG] Removing leftover .old directory")
		os.RemoveAll(oldPath)
		// If removal fails, schedule for reboot
		if _, err := os.Stat(oldPath); err == nil {
			scheduleDirDeleteOnReboot(oldPath)
		}
	}
}

// deleteServiceAndWait deletes a service and waits for it to be fully removed
func deleteServiceAndWait(name string) {
	logMsg("[DEBUG] Deleting service with wait: %s", name)

	// First check if service exists
	cmd := exec.Command("sc", "query", name)
	if err := cmd.Run(); err != nil {
		logMsg("[DEBUG] Service %s does not exist", name)
		return
	}

	// Delete the service
	exec.Command("sc", "delete", name).Run()

	// Poll until service is gone or marked for deletion (max 15 seconds)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		cmd := exec.Command("sc", "query", name)
		output, err := cmd.CombinedOutput()
		outputStr := string(output)

		// Service no longer exists (error 1060)
		if err != nil || strings.Contains(outputStr, "1060") || strings.Contains(outputStr, "does not exist") {
			logMsg("[DEBUG] Service %s fully deleted", name)
			return
		}

		// Service is marked for deletion (will complete on reboot or when handles close)
		if strings.Contains(outputStr, "MARKED") || strings.Contains(outputStr, "DELETE_PENDING") {
			logMsg("[DEBUG] Service %s marked for deletion", name)
			return
		}

		time.Sleep(500 * time.Millisecond)
	}

	logMsg("[DEBUG] Timeout waiting for service %s deletion", name)
}

// scheduleDirDeleteOnReboot schedules a directory and its contents for deletion on reboot
func scheduleDirDeleteOnReboot(dirPath string) {
	logMsg("[DEBUG] Scheduling directory for deletion on reboot: %s", dirPath)

	// Use PowerShell to add to PendingFileRenameOperations
	// This is the proper Windows way to delete locked files/directories
	script := fmt.Sprintf(`
$dirPath = '%s'

# First, schedule all files in the directory
if (Test-Path $dirPath) {
    Get-ChildItem -Path $dirPath -Recurse -File -ErrorAction SilentlyContinue | ForEach-Object {
        $filePath = $_.FullName
        # Use MoveFileEx to schedule deletion
        $code = @'
using System;
using System.Runtime.InteropServices;
public class FileOps {
    [DllImport("kernel32.dll", SetLastError=true, CharSet=CharSet.Unicode)]
    public static extern bool MoveFileEx(string lpExistingFileName, string lpNewFileName, int dwFlags);
    public const int MOVEFILE_DELAY_UNTIL_REBOOT = 0x4;
}
'@
        try {
            Add-Type -TypeDefinition $code -ErrorAction SilentlyContinue
        } catch {}
        [FileOps]::MoveFileEx($filePath, $null, [FileOps]::MOVEFILE_DELAY_UNTIL_REBOOT) | Out-Null
    }

    # Then schedule the directory itself
    [FileOps]::MoveFileEx($dirPath, $null, [FileOps]::MOVEFILE_DELAY_UNTIL_REBOOT) | Out-Null
}
`, strings.ReplaceAll(dirPath, "'", "''"))

	cmd := exec.Command("powershell", "-NoProfile", "-Command", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		logMsg("[DEBUG] Error scheduling directory deletion: %v - %s", err, string(output))
	} else {
		logMsg("[DEBUG] Directory scheduled for deletion on reboot")
	}
}

// closeProcessesUsingFile uses Windows Restart Manager to find and close processes using a file
func closeProcessesUsingFile(filePath string) {
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return
	}

	logMsg("[DEBUG] Using Restart Manager to find processes using: %s", filePath)

	// PowerShell script using Restart Manager API
	script := fmt.Sprintf(`
$filePath = '%s'
try {
    $processes = @()

    # Method 1: Check if file is locked by trying to open it exclusively
    try {
        $fs = [System.IO.File]::Open($filePath, 'Open', 'ReadWrite', 'None')
        $fs.Close()
    } catch {
        Write-Host "File is locked, searching for process..."
    }

    # Method 2: Find processes with modules matching the path
    Get-Process | ForEach-Object {
        try {
            if ($_.Modules | Where-Object { $_.FileName -eq $filePath }) {
                $processes += $_
            }
        } catch {}
    }

    # Method 3: Find processes with the same name as the file
    $fileName = [System.IO.Path]::GetFileNameWithoutExtension($filePath)
    $procs = Get-Process -Name $fileName -ErrorAction SilentlyContinue
    if ($procs) { $processes += $procs }

    # Kill found processes
    $processes | Select-Object -Unique | ForEach-Object {
        Write-Host "Killing process: $($_.Name) (PID: $($_.Id))"
        Stop-Process -Id $_.Id -Force -ErrorAction SilentlyContinue
    }
} catch {
    Write-Host "Error: $_"
}
`, strings.ReplaceAll(filePath, "'", "''"))

	cmd := exec.Command("powershell", "-NoProfile", "-Command", script)
	output, _ := cmd.CombinedOutput()
	if len(output) > 0 {
		logMsg("[DEBUG] Restart Manager output: %s", string(output))
	}
}

// closeFileHandles attempts to close open handles to a file using various methods
func closeFileHandles(filePath string) {
	logMsg("[DEBUG] Attempting to close file handles for: %s", filePath)

	// Method 1: Use handle.exe if available (Sysinternals)
	handleExe := filepath.Join(os.TempDir(), "handle64.exe")
	if _, err := os.Stat(handleExe); err == nil {
		cmd := exec.Command(handleExe, "-c", "-p", "*", "-nobanner", filePath)
		cmd.Run()
	}

	// Method 2: Use PowerShell to find and kill processes by handle
	script := fmt.Sprintf(`
$filePath = '%s'
$fileName = [System.IO.Path]::GetFileName($filePath)

# Find all processes and check their handles
Get-Process | ForEach-Object {
    $proc = $_
    try {
        # Check if process has any handle to our file by checking open files
        $handles = $proc.HandleCount
        if ($proc.MainModule.FileName -like "*sentinel*") {
            Write-Host "Found Sentinel process: $($proc.Name) PID: $($proc.Id)"
            Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue
        }
    } catch {}
}

# Also try to find by command line
Get-WmiObject Win32_Process | Where-Object {
    $_.CommandLine -like "*$fileName*" -or $_.ExecutablePath -like "*$fileName*"
} | ForEach-Object {
    Write-Host "Found by WMI: $($_.Name) PID: $($_.ProcessId)"
    Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue
}
`, strings.ReplaceAll(filePath, "'", "''"))

	cmd := exec.Command("powershell", "-NoProfile", "-Command", script)
	output, _ := cmd.CombinedOutput()
	if len(output) > 0 {
		logMsg("[DEBUG] Handle close output: %s", string(output))
	}
}

// killServiceProcess finds and kills the process running a Windows service
func killServiceProcess(serviceName string) {
	logMsg("[DEBUG] Finding process for service: %s", serviceName)

	// Get the PID of the service process using sc queryex
	cmd := exec.Command("sc", "queryex", serviceName)
	output, err := cmd.Output()
	if err != nil {
		return
	}

	// Parse the PID from output
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "PID") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				pid := strings.TrimSpace(parts[len(parts)-1])
				if pid != "0" {
					logMsg("[DEBUG] Killing service process PID: %s", pid)
					exec.Command("taskkill", "/F", "/PID", pid).Run()
				}
			}
		}
	}
}

// killProcessByPath kills any process whose executable path matches the given path
func killProcessByPath(exePath string) {
	// Use WMIC to find processes by path
	cmd := exec.Command("wmic", "process", "where", fmt.Sprintf("ExecutablePath='%s'", strings.ReplaceAll(exePath, "\\", "\\\\")), "get", "ProcessId")
	output, err := cmd.Output()
	if err != nil {
		return
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		pid := strings.TrimSpace(line)
		if pid != "" && pid != "ProcessId" {
			logMsg("[DEBUG] Killing process by path, PID: %s", pid)
			exec.Command("taskkill", "/F", "/PID", pid).Run()
		}
	}
}

// scheduleDeleteOnReboot uses MoveFileEx API to delete file on next reboot
// This is the CORRECT Windows way to delete locked files
func scheduleDeleteOnReboot(path string) {
	logMsg("[DEBUG] Scheduling file for deletion on reboot: %s", path)

	// Use PowerShell to call MoveFileEx via P/Invoke
	script := fmt.Sprintf(`
$code = @'
using System;
using System.Runtime.InteropServices;
public class FileOps {
    [DllImport("kernel32.dll", SetLastError=true, CharSet=CharSet.Unicode)]
    public static extern bool MoveFileEx(string lpExistingFileName, string lpNewFileName, int dwFlags);
    public const int MOVEFILE_DELAY_UNTIL_REBOOT = 0x4;
}
'@
try { Add-Type -TypeDefinition $code -ErrorAction SilentlyContinue } catch {}
$result = [FileOps]::MoveFileEx('%s', $null, [FileOps]::MOVEFILE_DELAY_UNTIL_REBOOT)
if ($result) { Write-Host "Scheduled for deletion" } else { Write-Host "Failed: $([System.Runtime.InteropServices.Marshal]::GetLastWin32Error())" }
`, strings.ReplaceAll(path, "'", "''"))

	cmd := exec.Command("powershell", "-NoProfile", "-Command", script)
	output, _ := cmd.CombinedOutput()
	logMsg("[DEBUG] MoveFileEx result: %s", strings.TrimSpace(string(output)))
}

// moveFileWithRetry tries to move a file with retries
func moveFileWithRetry(src, dst string) error {
	for i := 0; i < 5; i++ {
		// Try using cmd /c move
		cmd := exec.Command("cmd", "/c", "move", "/Y", src, dst)
		if err := cmd.Run(); err == nil {
			return nil
		}

		// Try using PowerShell
		cmd = exec.Command("powershell", "-Command",
			fmt.Sprintf("Move-Item -Path '%s' -Destination '%s' -Force", src, dst))
		if err := cmd.Run(); err == nil {
			return nil
		}

		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("could not move file after 5 attempts")
}

// stopService stops a Windows service and waits for it to stop
func stopService(name string) {
	logMsg("[DEBUG] Stopping service: %s", name)

	// First try graceful stop
	exec.Command("sc", "stop", name).Run()

	// Wait up to 10 seconds for service to stop
	for i := 0; i < 20; i++ {
		cmd := exec.Command("sc", "query", name)
		output, err := cmd.Output()
		if err != nil {
			return // Service doesn't exist
		}
		if strings.Contains(string(output), "STOPPED") {
			logMsg("[DEBUG] Service %s stopped", name)
			return
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Force stop if still running
	exec.Command("sc", "stop", name).Run()
	time.Sleep(1 * time.Second)
}

// deleteService removes a Windows service
func deleteService(name string) {
	logMsg("[DEBUG] Deleting service: %s", name)
	exec.Command("sc", "delete", name).Run()
	time.Sleep(500 * time.Millisecond)
}

// killProcess forcefully terminates a process by name
func killProcess(name string) {
	logMsg("[DEBUG] Killing process: %s", name)

	// Use taskkill with force flag
	exec.Command("taskkill", "/F", "/IM", name).Run()

	// Also try WMIC for stubborn processes
	exec.Command("wmic", "process", "where", fmt.Sprintf("name='%s'", name), "delete").Run()
}

// waitForProcessExit waits for a process to fully exit
func waitForProcessExit(name string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		cmd := exec.Command("tasklist", "/FI", fmt.Sprintf("IMAGENAME eq %s", name), "/NH")
		output, _ := cmd.Output()
		if !strings.Contains(strings.ToLower(string(output)), strings.ToLower(name)) {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	logMsg("[DEBUG] Timeout waiting for %s to exit", name)
}

// forceRemoveFile aggressively removes a file, handling locks and permissions
func forceRemoveFile(path string) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return
	}

	logMsg("[DEBUG] Force removing: %s", path)

	// Try standard remove first
	if err := os.Remove(path); err == nil {
		return
	}

	// Take ownership and reset permissions
	exec.Command("takeown", "/F", path).Run()
	exec.Command("icacls", path, "/grant", "administrators:F").Run()

	// Try remove again
	if err := os.Remove(path); err == nil {
		return
	}

	// Use cmd /c del with force
	exec.Command("cmd", "/c", "del", "/F", "/Q", path).Run()

	// Use PowerShell Remove-Item with force
	exec.Command("powershell", "-Command",
		fmt.Sprintf("Remove-Item -Path '%s' -Force -ErrorAction SilentlyContinue", path)).Run()

	// Schedule delete on reboot if still exists (last resort)
	if _, err := os.Stat(path); err == nil {
		exec.Command("cmd", "/c", "move", "/Y", path, path+".old").Run()
		logMsg("[DEBUG] Renamed locked file to .old")
	}
}

// robustFileCopy copies a file with retries and error handling
func robustFileCopy(src, dst string) error {
	var lastErr error

	for attempt := 1; attempt <= 5; attempt++ {
		logMsg("[DEBUG] Copy attempt %d: %s -> %s", attempt, src, dst)

		// Ensure destination doesn't exist
		forceRemoveFile(dst)
		time.Sleep(100 * time.Millisecond)

		// Try to copy
		err := copyFileWithSync(src, dst)
		if err == nil {
			// Verify the copy
			srcInfo, _ := os.Stat(src)
			dstInfo, dstErr := os.Stat(dst)
			if dstErr == nil && dstInfo.Size() == srcInfo.Size() {
				logMsg("[DEBUG] Copy successful on attempt %d", attempt)
				return nil
			}
			lastErr = fmt.Errorf("size mismatch after copy")
		} else {
			lastErr = err
		}

		logMsg("[DEBUG] Copy attempt %d failed: %v", attempt, lastErr)

		// Wait before retry (exponential backoff)
		time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
	}

	return fmt.Errorf("failed after 5 attempts: %w", lastErr)
}

// copyFileWithSync copies a file and syncs to disk
func copyFileWithSync(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer source.Close()

	dest, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return fmt.Errorf("create dest: %w", err)
	}

	_, err = io.Copy(dest, source)
	if err != nil {
		dest.Close()
		os.Remove(dst)
		return fmt.Errorf("copy data: %w", err)
	}

	// Sync to disk
	if err := dest.Sync(); err != nil {
		dest.Close()
		return fmt.Errorf("sync: %w", err)
	}

	return dest.Close()
}

// emergencyCleanup performs aggressive cleanup when normal methods fail
func emergencyCleanup(installPath string) {
	logMsg("[DEBUG] Performing emergency cleanup")

	// Kill ALL sentinel processes
	exec.Command("taskkill", "/F", "/IM", "sentinel-agent.exe").Run()
	exec.Command("taskkill", "/F", "/IM", "sentinel-watchdog.exe").Run()
	exec.Command("taskkill", "/F", "/IM", "sentinel-installer.exe").Run()

	// Stop and delete services
	exec.Command("sc", "stop", "SentinelAgent").Run()
	exec.Command("sc", "stop", "SentinelWatchdog").Run()
	time.Sleep(2 * time.Second)
	exec.Command("sc", "delete", "SentinelAgent").Run()
	exec.Command("sc", "delete", "SentinelWatchdog").Run()

	// Wait for processes to die
	time.Sleep(3 * time.Second)

	// Force remove entire directory using PowerShell
	exec.Command("powershell", "-Command",
		fmt.Sprintf("Remove-Item -Path '%s' -Recurse -Force -ErrorAction SilentlyContinue", installPath)).Run()

	// Recreate directory
	os.MkdirAll(installPath, 0755)

	// Add defender exclusion again
	addDefenderExclusion(installPath)

	time.Sleep(1 * time.Second)
}
