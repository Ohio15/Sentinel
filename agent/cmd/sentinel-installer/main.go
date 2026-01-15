package main

import (
	"bufio"
	"crypto/sha256"
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

// Command-line flags
var (
	flagCode   = flag.String("code", "", "Installation code (e.g., AB12-CD34)")
	flagServer = flag.String("server", "", "Server URL (overrides embedded/code config)")
	flagToken  = flag.String("token", "", "Enrollment token (overrides embedded/code config)")
	flagSilent = flag.Bool("silent", false, "Run in silent mode (no prompts)")
	flagHelp   = flag.Bool("help", false, "Show help message")
)

// Default server URL for code validation (used when no embedded config)
const DefaultServerURL = "https://sentinelrmm.us"

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

// Embedded configuration - these placeholders are replaced when generating the installer
// The format uses fixed-width fields that can be binary-patched
var (
	// SENTINEL_CONFIG_SERVER:http://_______________________________________________:END
	EmbeddedServer = "SENTINEL_CONFIG_SERVER:http://_______________________________________________:END"
	// SENTINEL_CONFIG_TOKEN:__________________________________________________________:END
	EmbeddedToken = "SENTINEL_CONFIG_TOKEN:__________________________________________________________:END"
)

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
// 3. Embedded config (binary-patched installer)
// 4. Interactive prompt for code
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

	// Priority 3: Embedded config (backward compatibility)
	embeddedServer := extractConfig(EmbeddedServer, "SENTINEL_CONFIG_SERVER:")
	embeddedToken := extractConfig(EmbeddedToken, "SENTINEL_CONFIG_TOKEN:")
	if embeddedServer != "" && embeddedToken != "" {
		logMsg("[DEBUG] Using embedded configuration")
		return &InstallConfig{
			ServerURL:       embeddedServer,
			EnrollmentToken: embeddedToken,
		}
	}

	// Priority 4: Interactive code prompt (only if not silent mode)
	if *flagSilent {
		printError("No configuration found and running in silent mode.")
		printError("Provide --code, --server/--token, or use a pre-configured installer.")
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
	serverURL := DefaultServerURL
	if *flagServer != "" {
		serverURL = *flagServer
	}

	// Call validation API
	apiURL := fmt.Sprintf("%s/api/public/install/validate-code?code=%s", serverURL, url.QueryEscape(code))
	logMsg("[DEBUG] Validating code at: %s", apiURL)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(apiURL)
	if err != nil {
		logMsg("[DEBUG] Validation request failed: %v", err)
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

	// Check for existing installation
	printStep(2, 6, "Checking for existing installation")
	installPath := filepath.Join(os.Getenv("ProgramFiles"), "Sentinel Agent")
	agentExe := filepath.Join(installPath, "sentinel-agent.exe")

	if _, err := os.Stat(agentExe); err == nil {
		printInfo("Existing installation found - will upgrade")
		printInfo("Stopping existing service...")
		exec.Command("net", "stop", "SentinelAgent").Run()
		exec.Command("net", "stop", "SentinelWatchdog").Run()
		time.Sleep(2 * time.Second)
	} else {
		printSuccess("No existing installation found")
	}

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
	printSuccess("Downloaded %.2f MB", float64(agentInfo.Size)/1024/1024)

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

	if err := os.MkdirAll(installPath, 0755); err != nil {
		printError("Failed to create installation directory: %v", err)
		if !*flagSilent {
			waitForKey()
		}
		os.Exit(1)
	}

	if err := copyFile(tempPath, agentExe); err != nil {
		printError("Failed to install agent binary: %v", err)
		if !*flagSilent {
			waitForKey()
		}
		os.Exit(1)
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

func extractConfig(embedded, prefix string) string {
	if !strings.HasPrefix(embedded, prefix) {
		return ""
	}
	endIdx := strings.LastIndex(embedded, ":END")
	if endIdx == -1 {
		return ""
	}
	value := embedded[len(prefix):endIdx]
	value = strings.TrimRight(value, "_")
	if value == "" || strings.HasPrefix(value, "_") {
		return ""
	}
	return value
}

func setConsoleTitle(title string) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	setConsoleTitleW := kernel32.NewProc("SetConsoleTitleW")
	titlePtr, _ := syscall.UTF16PtrFromString(title)
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
