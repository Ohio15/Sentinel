package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	// Set console title
	setConsoleTitle("Sentinel Agent Installer")

	// Print banner
	printBanner()

	// Extract embedded config
	serverURL := extractConfig(EmbeddedServer, "SENTINEL_CONFIG_SERVER:")
	token := extractConfig(EmbeddedToken, "SENTINEL_CONFIG_TOKEN:")

	if serverURL == "" {
		printError("This installer was not configured properly.")
		printError("Server URL is missing from embedded configuration.")
		waitForKey()
		os.Exit(1)
	}

	if token == "" {
		printError("This installer was not configured properly.")
		printError("Enrollment token is missing from embedded configuration.")
		waitForKey()
		os.Exit(1)
	}

	fmt.Printf("  Server: %s\n", serverURL)
	fmt.Printf("  Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Println()

	// Check for admin privileges
	printStep(1, 6, "Checking administrator privileges")
	if !isAdmin() {
		fmt.Println()
		printWarning("Administrator privileges required!")
		fmt.Println()
		fmt.Println("  Attempting to restart with elevated privileges...")
		fmt.Println()

		if err := runAsAdmin(); err != nil {
			printError("Failed to elevate: %v", err)
			printError("Please right-click this installer and select 'Run as administrator'")
			waitForKey()
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
		// Stop existing service
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
		printError("Please check that the server URL is correct and the server is running.")
		waitForKey()
		os.Exit(1)
	}
	printSuccess("Agent version: %s", agentInfo.Version)

	// Download agent
	printStep(4, 6, "Downloading Sentinel Agent")
	tempPath := filepath.Join(os.TempDir(), "sentinel-agent-download.exe")
	if err := downloadAgent(serverURL, token, tempPath, agentInfo); err != nil {
		printError("Download failed: %v", err)
		waitForKey()
		os.Exit(1)
	}
	defer os.Remove(tempPath)
	printSuccess("Downloaded %.2f MB", float64(agentInfo.Size)/1024/1024)

	// Verify checksum if provided
	if agentInfo.Checksum != "" {
		printInfo("Verifying checksum...")
		if err := verifyChecksum(tempPath, agentInfo.Checksum); err != nil {
			printError("Checksum verification failed: %v", err)
			waitForKey()
			os.Exit(1)
		}
		printSuccess("Checksum verified")
	}

	// Install agent
	printStep(5, 6, "Installing Sentinel Agent")

	// Create installation directory
	if err := os.MkdirAll(installPath, 0755); err != nil {
		printError("Failed to create installation directory: %v", err)
		waitForKey()
		os.Exit(1)
	}

	// Copy agent binary
	if err := copyFile(tempPath, agentExe); err != nil {
		printError("Failed to install agent binary: %v", err)
		waitForKey()
		os.Exit(1)
	}

	// Run agent install command
	printInfo("Configuring service...")
	cmd := exec.Command(agentExe, "--install", "--server="+serverURL, "--token="+token)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		printError("Service installation failed: %v", err)
		waitForKey()
		os.Exit(1)
	}
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
	pct := current * 100 / total
	bar := strings.Repeat("=", current*40/total) + strings.Repeat("-", 40-current*40/total)
	fmt.Printf("\n  [%s] %d%%\n", bar, pct)
	fmt.Printf("  Step %d/%d: %s\n", current, total, message)
}

func printSuccess(format string, args ...interface{}) {
	fmt.Printf("  [OK] "+format+"\n", args...)
}

func printInfo(format string, args ...interface{}) {
	fmt.Printf("  [..] "+format+"\n", args...)
}

func printWarning(format string, args ...interface{}) {
	fmt.Printf("  [!!] "+format+"\n", args...)
}

func printError(format string, args ...interface{}) {
	fmt.Printf("  [ERROR] "+format+"\n", args...)
}

func waitForKey() {
	fmt.Println()
	fmt.Println("  Press ENTER to close this window...")
	bufio.NewReader(os.Stdin).ReadBytes('\n')
}

func extractConfig(embedded, prefix string) string {
	if !strings.HasPrefix(embedded, prefix) {
		return ""
	}
	// Find :END suffix
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

	verbPtr, _ := syscall.UTF16PtrFromString("runas")
	exePtr, _ := syscall.UTF16PtrFromString(exe)
	cwdPtr, _ := syscall.UTF16PtrFromString(cwd)
	argPtr, _ := syscall.UTF16PtrFromString("")

	var showCmd int32 = 1 // SW_NORMAL

	err = windows.ShellExecute(0, verbPtr, exePtr, argPtr, cwdPtr, showCmd)
	return err
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

			if totalSize > 0 {
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

	fmt.Println()
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
	// Remove destination first
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
