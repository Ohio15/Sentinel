package api

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sentinel/server/internal/constants"
)

// InstallerConfig represents the configuration embedded in the installer
type InstallerConfig struct {
	ServerURL       string `json:"server_url"`
	GRPCEndpoint    string `json:"grpc_endpoint"`
	EnrollmentToken string `json:"enrollment_token"`
	OrganizationID  int    `json:"organization_id"`
	GeneratedAt     string `json:"generated_at"`
}

// Config marker for appending JSON to binary
const installerConfigMarker = "---SENTINEL-CONFIG---"

// generateInstallerDownloadHandler generates a customized installer with embedded configuration
// GET /api/agent/installer?platform=windows&arch=amd64
// Requires JWT authentication
func generateInstallerDownloadHandler(services *Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get query parameters
		platform := c.DefaultQuery("platform", "")
		arch := c.DefaultQuery("arch", "amd64")

		// Validate platform
		if platform == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":             "platform parameter is required",
				"supported_values":  []string{"windows", "linux-deb", "linux-rpm", "macos", "synology"},
			})
			return
		}

		platform = strings.ToLower(platform)
		arch = strings.ToLower(arch)

		// Validate platform values
		validPlatforms := map[string]bool{
			"windows":   true,
			"linux-deb": true,
			"linux-rpm": true,
			"macos":     true,
			"synology":  true,
		}
		if !validPlatforms[platform] {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":            "invalid platform",
				"supported_values": []string{"windows", "linux-deb", "linux-rpm", "macos", "synology"},
			})
			return
		}

		// Validate arch values
		validArch := map[string]bool{
			"amd64": true,
			"arm64": true,
		}
		if !validArch[arch] {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":            "invalid architecture",
				"supported_values": []string{"amd64", "arm64"},
			})
			return
		}

		// Get user info from context for logging/tracking
		var userID *uuid.UUID
		if uid, exists := c.Get("userID"); exists {
			if u, ok := uid.(uuid.UUID); ok {
				userID = &u
			}
		}

		// Build server URL
		serverURL := services.Config.PublicURL
		if serverURL == "" {
			serverURL = services.Config.ServerURL
		}
		if serverURL == "" {
			// Derive from request
			scheme := c.GetHeader("X-Forwarded-Proto")
			if scheme == "" {
				if c.Request.TLS != nil {
					scheme = "https"
				} else {
					scheme = "https" // Default to https for production
				}
			}
			host := c.GetHeader("X-Forwarded-Host")
			if host == "" {
				host = c.Request.Host
			}
			serverURL = fmt.Sprintf("%s://%s", scheme, host)
		}

		// Build gRPC endpoint
		grpcEndpoint := buildGRPCEndpoint(services, c)

		// Generate or retrieve enrollment token for this organization
		enrollmentToken, err := mintOneTimeInstallerToken(c, services, userID)
		if err != nil {
			log.Printf("[Installer] Failed to get enrollment token: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate enrollment token"})
			return
		}

		// Build the configuration to embed
		config := InstallerConfig{
			ServerURL:       serverURL,
			GRPCEndpoint:    grpcEndpoint,
			EnrollmentToken: enrollmentToken,
			OrganizationID:  constants.CurrentOrganizationID,
			GeneratedAt:     time.Now().UTC().Format(time.RFC3339),
		}

		// Get the base installer path
		installerPath := getBaseInstallerPath(platform, arch)
		if installerPath == "" {
			c.JSON(http.StatusNotFound, gin.H{
				"error":    "Base installer not found for this platform/architecture",
				"platform": platform,
				"arch":     arch,
			})
			return
		}

		// Read the base installer
		installerData, err := os.ReadFile(installerPath)
		if err != nil {
			log.Printf("[Installer] Failed to read base installer %s: %v", installerPath, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read base installer"})
			return
		}

		// Inject configuration based on platform
		var outputData []byte
		var filename string
		var contentType string

		switch platform {
		case "windows":
			outputData, err = injectConfigWindows(installerData, config)
			filename = fmt.Sprintf("sentinel-setup-%s-%s.exe", platform, arch)
			contentType = "application/octet-stream"

		case "linux-deb":
			// TODO: For Linux DEB packages, configuration injection requires modifying
			// the package contents (adding a config file inside). For now, return the
			// base package with instructions to configure via command-line.
			outputData = installerData
			filename = fmt.Sprintf("sentinel-agent-%s.deb", arch)
			contentType = "application/vnd.debian.binary-package"
			log.Printf("[Installer] Linux DEB config injection not yet implemented - returning base package")

		case "linux-rpm":
			// TODO: For Linux RPM packages, configuration injection requires modifying
			// the package contents (adding a config file inside). For now, return the
			// base package with instructions to configure via command-line.
			outputData = installerData
			filename = fmt.Sprintf("sentinel-agent-%s.rpm", arch)
			contentType = "application/x-rpm"
			log.Printf("[Installer] Linux RPM config injection not yet implemented - returning base package")

		case "macos":
			// TODO: For macOS PKG packages, configuration injection requires modifying
			// the package contents (adding a config file inside). For now, return the
			// base package with instructions to configure via command-line.
			outputData = installerData
			filename = fmt.Sprintf("sentinel-agent-%s.pkg", arch)
			contentType = "application/octet-stream"
			log.Printf("[Installer] macOS PKG config injection not yet implemented - returning base package")

		case "synology":
			outputData, err = injectConfigSynology(installerData, config)
			filename = fmt.Sprintf("sentinel-agent-%s.spk", arch)
			contentType = "application/octet-stream"

		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "Unsupported platform"})
			return
		}

		if err != nil {
			log.Printf("[Installer] Failed to inject config: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to prepare installer"})
			return
		}

		// Log the download
		logInstallerDownload(c, services, platform, arch, userID)

		// Set response headers
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
		c.Header("Content-Type", contentType)
		c.Header("Content-Length", fmt.Sprintf("%d", len(outputData)))
		c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
		c.Header("X-Sentinel-Config-Injected", fmt.Sprintf("%v", platform == "windows"))

		// Send the installer
		c.Data(http.StatusOK, contentType, outputData)
	}
}

// buildGRPCEndpoint constructs the gRPC endpoint URL
func buildGRPCEndpoint(services *Services, c *gin.Context) string {
	// Check if there's a specific gRPC endpoint configured
	if services.Config.GRPCPort > 0 {
		// Extract host from PublicURL or request
		host := ""
		if services.Config.PublicURL != "" {
			// Parse host from URL
			host = extractHostFromURL(services.Config.PublicURL)
		}
		if host == "" && services.Config.ServerURL != "" {
			host = extractHostFromURL(services.Config.ServerURL)
		}
		if host == "" {
			host = c.GetHeader("X-Forwarded-Host")
			if host == "" {
				host = c.Request.Host
			}
			// Remove port if present
			if idx := strings.Index(host, ":"); idx != -1 {
				host = host[:idx]
			}
		}
		return fmt.Sprintf("%s:%d", host, services.Config.GRPCPort)
	}

	// Default gRPC endpoint (same host, port 4444)
	host := extractHostFromURL(services.Config.PublicURL)
	if host == "" {
		host = "sentinelrmm.us"
	}
	return fmt.Sprintf("%s:4444", host)
}

// extractHostFromURL extracts the hostname from a URL
func extractHostFromURL(urlStr string) string {
	if urlStr == "" {
		return ""
	}
	// Remove protocol
	urlStr = strings.TrimPrefix(urlStr, "https://")
	urlStr = strings.TrimPrefix(urlStr, "http://")
	// Remove path
	if idx := strings.Index(urlStr, "/"); idx != -1 {
		urlStr = urlStr[:idx]
	}
	// Remove port
	if idx := strings.Index(urlStr, ":"); idx != -1 {
		urlStr = urlStr[:idx]
	}
	return urlStr
}

// mintOneTimeInstallerToken creates a fresh single-use enrollment token bound
// to one installer download. The token expires in 24h and max_uses=1 — bootstrap
// flow increments use_count, after which the token is rejected by validation
// (agents.go:317, mobile_handlers.go:311).
//
// Replaces the prior org-shared 30-day getOrCreate model that allowed any user
// who downloaded the installer to extract a long-lived token via `strings`.
// Re-downloading mints a new token; the old one is left in place to allow the
// in-flight installer to still enroll once.
func mintOneTimeInstallerToken(c *gin.Context, services *Services, userID *uuid.UUID) (string, error) {
	ctx := c.Request.Context()
	newToken := uuid.New().String()
	expiresAt := time.Now().Add(24 * time.Hour)
	maxUses := 1

	_, err := services.DB.Pool().Exec(ctx, `
		INSERT INTO enrollment_tokens (
			organization_id, token, name, description, created_by,
			expires_at, max_uses, is_active, is_legacy
		) VALUES ($1, $2, $3, $4, $5, $6, $7, TRUE, FALSE)
	`, constants.CurrentOrganizationID,
		newToken,
		fmt.Sprintf("Installer Download - %s", time.Now().UTC().Format(time.RFC3339)),
		"Auto-generated one-time token for installer download API",
		userID,
		expiresAt,
		maxUses)

	if err != nil {
		return "", fmt.Errorf("failed to create enrollment token: %w", err)
	}

	return newToken, nil
}

// getBaseInstallerPath returns the path to the base installer template.
// Delegates to the canonical resolver — see installer_paths.go.
func getBaseInstallerPath(platform, arch string) string {
	return findBaseInstaller(platform, arch)
}

// injectConfigWindows appends JSON configuration to Windows EXE with marker
func injectConfigWindows(installerData []byte, config InstallerConfig) ([]byte, error) {
	// First, try the existing XYZCFG binary patching method (same as buildPatchedInstallerWithCode)
	// This is for installers that have the config block placeholder
	placeholder := make([]byte, 200)
	copy(placeholder[0:6], []byte("XYZCFG"))
	copy(placeholder[6:59], []byte("https://config-placeholder-url.local_________________"))
	copy(placeholder[59:112], []byte("token-placeholder-value-replace-me___________________"))
	copy(placeholder[112:121], []byte("CODE_____"))

	if bytes.Contains(installerData, placeholder) {
		// Use binary patching method
		patched := make([]byte, 200)
		copy(patched[0:6], []byte("XYZCFG"))
		copy(patched[6:59], []byte(padConfigString(config.ServerURL, 53)))
		copy(patched[59:112], []byte(padConfigString(config.EnrollmentToken, 53)))
		copy(patched[112:121], []byte(padConfigString("API_DL___", 9))) // Mark as API download

		result := bytes.Replace(installerData, placeholder, patched, 1)
		log.Printf("[Installer] Windows: Used XYZCFG binary patching method")
		return result, nil
	}

	// Fallback: Append JSON config with marker to end of binary
	// This method appends the config after the exe, which the installer can read
	configJSON, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config: %w", err)
	}

	// Create the config block: marker + JSON + newline
	configBlock := []byte(installerConfigMarker)
	configBlock = append(configBlock, configJSON...)
	configBlock = append(configBlock, '\n')

	// Append to installer
	result := make([]byte, len(installerData)+len(configBlock))
	copy(result, installerData)
	copy(result[len(installerData):], configBlock)

	log.Printf("[Installer] Windows: Appended config marker with %d bytes of config", len(configJSON))
	return result, nil
}

// injectConfigSynology replaces placeholder values in the config.json inside an SPK package.
// SPK structure: outer tar containing INFO, package.tgz, and scripts/.
// package.tgz is a gzipped tar containing binaries and config.json.
func injectConfigSynology(spkData []byte, config InstallerConfig) ([]byte, error) {
	// Build the agent-compatible config JSON (field names match agent Config struct)
	agentConfig := map[string]interface{}{
		"server_url":       config.ServerURL,
		"grpc_address":     config.GRPCEndpoint,
		"enrollment_token": config.EnrollmentToken,
	}
	configJSON, err := json.MarshalIndent(agentConfig, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal agent config: %w", err)
	}
	configJSON = append(configJSON, '\n')

	// Parse the outer SPK tar
	spkReader := tar.NewReader(bytes.NewReader(spkData))
	var spkBuf bytes.Buffer
	spkBuf.Grow(len(spkData) + 4096) // Pre-allocate
	spkWriter := tar.NewWriter(&spkBuf)

	for {
		header, err := spkReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read SPK tar entry: %w", err)
		}

		if header.Name == "package.tgz" {
			// Read the full package.tgz to rewrite it
			tgzData, err := io.ReadAll(spkReader)
			if err != nil {
				return nil, fmt.Errorf("failed to read package.tgz: %w", err)
			}
			log.Printf("[Installer] SPK: Read package.tgz (%d bytes)", len(tgzData))

			newTgz, err := rewritePackageTgz(tgzData, configJSON)
			if err != nil {
				return nil, fmt.Errorf("failed to rewrite package.tgz: %w", err)
			}
			log.Printf("[Installer] SPK: Rewritten package.tgz (%d bytes -> %d bytes)", len(tgzData), len(newTgz))

			// Write with clean header and updated size
			newHeader := &tar.Header{
				Typeflag: tar.TypeReg,
				Name:     header.Name,
				Size:     int64(len(newTgz)),
				Mode:     header.Mode,
				ModTime:  header.ModTime,
				Format:   tar.FormatGNU,
			}
			if err := spkWriter.WriteHeader(newHeader); err != nil {
				return nil, fmt.Errorf("failed to write package.tgz header: %w", err)
			}
			n, err := spkWriter.Write(newTgz)
			if err != nil {
				return nil, fmt.Errorf("failed to write package.tgz data: %w", err)
			}
			if n != len(newTgz) {
				return nil, fmt.Errorf("short write for package.tgz: wrote %d of %d bytes", n, len(newTgz))
			}
		} else {
			// Stream other entries through with clean headers to avoid format issues
			typeflag := header.Typeflag
			name := header.Name
			if typeflag == tar.TypeDir {
				if !strings.HasSuffix(name, "/") {
					name += "/"
				}
			}
			newHeader := &tar.Header{
				Typeflag: typeflag,
				Name:     name,
				Size:     header.Size,
				Mode:     header.Mode,
				ModTime:  header.ModTime,
				Format:   tar.FormatGNU,
			}
			if err := spkWriter.WriteHeader(newHeader); err != nil {
				return nil, fmt.Errorf("failed to write SPK header %s: %w", header.Name, err)
			}
			if header.Size > 0 {
				n, err := io.Copy(spkWriter, spkReader)
				if err != nil {
					return nil, fmt.Errorf("failed to stream SPK entry %s: %w", header.Name, err)
				}
				if n != header.Size {
					return nil, fmt.Errorf("short copy for SPK entry %s: %d of %d bytes", header.Name, n, header.Size)
				}
			}
		}
	}

	if err := spkWriter.Close(); err != nil {
		return nil, fmt.Errorf("failed to finalize SPK tar: %w", err)
	}

	log.Printf("[Installer] Synology SPK: Injected config (%d bytes), output size: %d bytes", len(configJSON), spkBuf.Len())
	return spkBuf.Bytes(), nil
}

// rewritePackageTgz unpacks a gzipped tar, replaces config.json, and repacks it.
// Uses streaming io.Copy for large entries (binaries) and clean tar headers to avoid
// format-specific issues that can cause truncation with Go's archive/tar package.
func rewritePackageTgz(tgzData, configJSON []byte) ([]byte, error) {
	// Decompress
	gzReader, err := gzip.NewReader(bytes.NewReader(tgzData))
	if err != nil {
		return nil, fmt.Errorf("failed to open gzip: %w", err)
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)

	// Repack into new tgz with pre-allocated buffer
	var outBuf bytes.Buffer
	outBuf.Grow(len(tgzData) + 4096)
	gzWriter := gzip.NewWriter(&outBuf)
	tarWriter := tar.NewWriter(gzWriter)

	configReplaced := false

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read package.tgz entry: %w", err)
		}

		baseName := filepath.Base(header.Name)

		if baseName == "config.json" {
			// Replace config.json with injected config
			newHeader := &tar.Header{
				Typeflag: tar.TypeReg,
				Name:     header.Name,
				Size:     int64(len(configJSON)),
				Mode:     header.Mode,
				ModTime:  header.ModTime,
				Format:   tar.FormatGNU,
			}
			if err := tarWriter.WriteHeader(newHeader); err != nil {
				return nil, fmt.Errorf("failed to write config.json header: %w", err)
			}
			if _, err := tarWriter.Write(configJSON); err != nil {
				return nil, fmt.Errorf("failed to write config.json data: %w", err)
			}
			// Drain the original config.json data from the reader
			if _, err := io.Copy(io.Discard, tarReader); err != nil {
				return nil, fmt.Errorf("failed to drain original config.json: %w", err)
			}
			configReplaced = true
			log.Printf("[Installer] package.tgz: Replaced config.json (%d bytes)", len(configJSON))
		} else {
			// Stream entry through with clean header (avoids format-specific field issues)
			typeflag := header.Typeflag
			if typeflag == 0 {
				typeflag = tar.TypeReg // Normalize TypeRegA ('\0') to TypeReg ('0')
			}
			newHeader := &tar.Header{
				Typeflag: typeflag,
				Name:     header.Name,
				Size:     header.Size,
				Mode:     header.Mode,
				ModTime:  header.ModTime,
				Format:   tar.FormatGNU,
			}
			if typeflag == tar.TypeDir && !strings.HasSuffix(newHeader.Name, "/") {
				newHeader.Name += "/"
			}
			if err := tarWriter.WriteHeader(newHeader); err != nil {
				return nil, fmt.Errorf("failed to write header %s: %w", header.Name, err)
			}
			if header.Size > 0 {
				// Stream data directly from tar reader to tar writer (no buffering)
				n, err := io.Copy(tarWriter, tarReader)
				if err != nil {
					return nil, fmt.Errorf("failed to stream entry %s: %w", header.Name, err)
				}
				if n != header.Size {
					return nil, fmt.Errorf("short copy for %s: copied %d of %d bytes", header.Name, n, header.Size)
				}
				log.Printf("[Installer] package.tgz: Streamed %s (%d bytes)", header.Name, n)
			}
		}
	}

	// If config.json wasn't in the original archive, add it
	if !configReplaced {
		header := &tar.Header{
			Typeflag: tar.TypeReg,
			Name:     "config.json",
			Size:     int64(len(configJSON)),
			Mode:     0640,
			ModTime:  time.Now(),
			Format:   tar.FormatGNU,
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			return nil, fmt.Errorf("failed to write new config.json header: %w", err)
		}
		if _, err := tarWriter.Write(configJSON); err != nil {
			return nil, fmt.Errorf("failed to write new config.json: %w", err)
		}
		log.Printf("[Installer] package.tgz: Added new config.json (%d bytes)", len(configJSON))
	}

	if err := tarWriter.Close(); err != nil {
		return nil, fmt.Errorf("failed to finalize inner tar: %w", err)
	}
	if err := gzWriter.Close(); err != nil {
		return nil, fmt.Errorf("failed to finalize gzip: %w", err)
	}

	return outBuf.Bytes(), nil
}

// padConfigString pads a string to exactly the specified length
func padConfigString(s string, length int) string {
	if len(s) >= length {
		return s[:length]
	}
	return s + strings.Repeat("_", length-len(s))
}

// logInstallerDownload logs the installer download for auditing
func logInstallerDownload(c *gin.Context, services *Services, platform, arch string, userID *uuid.UUID) {
	logArtifactDownload(c, services, "installer-template", platform, arch, nil)
	_ = userID // retained for backward compatibility with existing callers
}

// logArtifactDownload records a single agent-artifact download in
// agent_downloads, distinguished by the artifact column added in migration
// 000057. Used by both the JWT-protected installer endpoint and the public
// bootstrap endpoints — closes the bootstrap-handlers audit-logging gap
// identified by the 2026-05-21 download-handler audit.
func logArtifactDownload(c *gin.Context, services *Services, artifact, platform, arch string, tokenID *uuid.UUID) {
	ctx := c.Request.Context()
	_, err := services.DB.Pool().Exec(ctx, `
		INSERT INTO agent_downloads (
			token_id, artifact, platform, architecture, ip_address, user_agent
		) VALUES ($1, $2, $3, $4, $5, $6)
	`, tokenID, artifact, platform, arch, c.ClientIP(), c.Request.UserAgent())

	if err != nil {
		log.Printf("[Download] artifact=%s platform=%s arch=%s ip=%s (db log failed: %v)",
			artifact, platform, arch, c.ClientIP(), err)
	} else {
		log.Printf("[Download] artifact=%s platform=%s arch=%s ip=%s",
			artifact, platform, arch, c.ClientIP())
	}
}
