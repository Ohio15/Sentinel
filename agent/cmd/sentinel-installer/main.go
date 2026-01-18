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

// Ports to try in order of preference (4443 first to bypass router caching)
var DefaultPorts = []string{"4443", "443", "8443"}

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
// 1. CLI arguments (--server + --token)
// 2. CLI code argument (--code) -> validates with server
// 3. Interactive prompt for code
func getConfiguration() *InstallConfig {
	// Priority 1: Direct CLI arguments
	if *flagServer != "" && *flagToken != "" {
		logMsg("[DEBUG] Using CLI arguments for config")
		return &InstallConfig{
			ServerURL:       *flagServer,
			EnrollmentToken: *flagToken,
		}
	}

	// Priority 2: Code from CLI argument
	if *flagCode != "" {
		logMsg("[DEBUG] Validating installation code from CLI: %s", *flagCode)
		config := validateInstallationCode(*flagCode)
		if config != nil {
			return config
		}
		printError("Invalid installation code: %s", *flagCode)
		return nil
	}

	// Priority 3: Interactive code prompt (only if not silent mode)
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
// This aggressively stops services, kills processes, and removes locked files
func prepareInstallDirectory(installPath, agentExe, watchdogExe string) error {
	logMsg("[DEBUG] Preparing install directory: %s", installPath)

	// Step 1: Stop services using SC (more reliable than net stop)
	stopService("SentinelWatchdog")
	stopService("SentinelAgent")

	// Step 2: Kill any running processes (multiple attempts)
	for i := 0; i < 3; i++ {
		killProcess("sentinel-agent.exe")
		killProcess("sentinel-watchdog.exe")
		time.Sleep(500 * time.Millisecond)
	}

	// Step 3: Wait for processes to fully terminate
	waitForProcessExit("sentinel-agent.exe", 10*time.Second)
	waitForProcessExit("sentinel-watchdog.exe", 5*time.Second)

	// Step 4: Delete services if they exist (allows clean reinstall)
	deleteService("SentinelAgent")
	deleteService("SentinelWatchdog")

	// Step 5: Create install directory
	if err := os.MkdirAll(installPath, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Step 6: Force remove existing files
	forceRemoveFile(agentExe)
	forceRemoveFile(watchdogExe)
	forceRemoveFile(filepath.Join(installPath, "config.json"))
	forceRemoveFile(filepath.Join(installPath, "protection.dat"))

	// Step 7: Verify files are actually gone
	if _, err := os.Stat(agentExe); err == nil {
		return fmt.Errorf("could not remove existing agent binary - file is locked")
	}

	return nil
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
