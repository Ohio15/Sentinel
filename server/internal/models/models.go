package models

import (
	"time"

	"github.com/google/uuid"
)

// User represents an admin user
type User struct {
	ID           uuid.UUID  `json:"id"`
	Email        string     `json:"email"`
	PasswordHash string     `json:"-"`
	FirstName    string     `json:"firstName"`
	LastName     string     `json:"lastName"`
	Role         string     `json:"role"`
	IsActive     bool       `json:"isActive"`
	LastLogin    *time.Time `json:"lastLogin"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

// GPUInfo contains graphics card information
type GPUInfo struct {
	Name          string `json:"name"`
	Vendor        string `json:"vendor"`
	Memory        uint64 `json:"memory"`
	DriverVersion string `json:"driver_version"`
}

// StorageInfo contains disk/partition information
type StorageInfo struct {
	Device     string  `json:"device"`
	Mountpoint string  `json:"mountpoint"`
	FSType     string  `json:"fstype"`
	Total      uint64  `json:"total"`
	Used       uint64  `json:"used"`
	Free       uint64  `json:"free"`
	Percent    float64 `json:"percent"`
}

// Device represents a monitored endpoint
type Device struct {
	ID             uuid.UUID         `json:"id"`
	AgentID        string            `json:"agentId"`
	Hostname       string            `json:"hostname"`
	DisplayName    string            `json:"displayName"`
	OSType         string            `json:"osType"`
	OSVersion      string            `json:"osVersion"`
	OSBuild        string            `json:"osBuild"`
	Platform       string            `json:"platform"`
	PlatformFamily string            `json:"platformFamily"`
	Architecture   string            `json:"architecture"`
	CPUModel       string            `json:"cpuModel"`
	CPUCores       int               `json:"cpuCores"`
	CPUThreads     int               `json:"cpuThreads"`
	CPUSpeed       float64           `json:"cpuSpeed"`
	TotalMemory    uint64            `json:"totalMemory"`
	BootTime       uint64            `json:"bootTime"`
	GPU            []GPUInfo         `json:"gpu"`
	Storage        []StorageInfo     `json:"storage"`
	SerialNumber   string            `json:"serialNumber"`
	Manufacturer   string            `json:"manufacturer"`
	Model          string            `json:"model"`
	Domain         string            `json:"domain"`
	AgentVersion   string            `json:"agentVersion"`
	LastSeen       *time.Time        `json:"lastSeen"`
	Status         string            `json:"status"`
	IPAddress      string            `json:"ipAddress"`
	PublicIP       string            `json:"publicIp"`
	MACAddress     string            `json:"macAddress"`
	Tags           []string          `json:"tags"`
	Metadata       map[string]string `json:"metadata"`
	CreatedAt      time.Time         `json:"createdAt"`
	UpdatedAt      time.Time         `json:"updatedAt"`
}

// DeviceMetrics represents system metrics from an agent
type DeviceMetrics struct {
	DeviceID        uuid.UUID `json:"deviceId"`
	Timestamp       time.Time `json:"timestamp"`
	CPUPercent      float64   `json:"cpuPercent"`
	MemoryPercent   float64   `json:"memoryPercent"`
	MemoryUsedBytes int64     `json:"memoryUsedBytes"`
	MemoryTotalBytes int64    `json:"memoryTotalBytes"`
	DiskPercent     float64   `json:"diskPercent"`
	DiskUsedBytes   int64     `json:"diskUsedBytes"`
	DiskTotalBytes  int64     `json:"diskTotalBytes"`
	NetworkRxBytes  int64     `json:"networkRxBytes"`
	NetworkTxBytes  int64     `json:"networkTxBytes"`
	ProcessCount    int       `json:"processCount"`
}

// Command represents a command sent to an agent
type Command struct {
	ID           uuid.UUID  `json:"id"`
	DeviceID     uuid.UUID  `json:"deviceId"`
	CommandType  string     `json:"commandType"`
	Command      string     `json:"command"`
	Status       string     `json:"status"`
	Output       string     `json:"output"`
	ErrorMessage string     `json:"errorMessage"`
	ExitCode     *int       `json:"exitCode"`
	CreatedBy    *uuid.UUID `json:"createdBy"`
	CreatedAt    time.Time  `json:"createdAt"`
	StartedAt    *time.Time `json:"startedAt"`
	CompletedAt  *time.Time `json:"completedAt"`
}

// Script represents a reusable script
type Script struct {
	ID          uuid.UUID   `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Language    string      `json:"language"`
	Content     string      `json:"content"`
	OSTypes     []string    `json:"osTypes"`
	Parameters  []Parameter `json:"parameters"`
	CreatedBy   *uuid.UUID  `json:"createdBy"`
	CreatedAt   time.Time   `json:"createdAt"`
	UpdatedAt   time.Time   `json:"updatedAt"`
}

// Parameter represents a script parameter
type Parameter struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Default     string `json:"default"`
	Description string `json:"description"`
}

// Alert represents an alert instance
type Alert struct {
	ID             uuid.UUID  `json:"id"`
	DeviceID       uuid.UUID  `json:"deviceId"`
	DeviceName     string     `json:"deviceName"`
	RuleID         *uuid.UUID `json:"ruleId"`
	Severity       string     `json:"severity"`
	Title          string     `json:"title"`
	Message        string     `json:"message"`
	Status         string     `json:"status"`
	AcknowledgedBy *uuid.UUID `json:"acknowledgedBy"`
	AcknowledgedAt *time.Time `json:"acknowledgedAt"`
	ResolvedAt     *time.Time `json:"resolvedAt"`
	CreatedAt      time.Time  `json:"createdAt"`
}

// AlertRule represents an alert rule definition
type AlertRule struct {
	ID                   uuid.UUID `json:"id"`
	Name                 string    `json:"name"`
	Description          string    `json:"description"`
	Enabled              bool      `json:"enabled"`
	Metric               string    `json:"metric"`
	Operator             string    `json:"operator"`
	Threshold            float64   `json:"threshold"`
	DurationSeconds      int       `json:"durationSeconds"`
	Severity             string    `json:"severity"`
	CooldownMinutes      int       `json:"cooldownMinutes"`
	NotificationChannels []string  `json:"notificationChannels"`
	CreatedAt            time.Time `json:"createdAt"`
	UpdatedAt            time.Time `json:"updatedAt"`
}

// Session represents a user session
type Session struct {
	ID               uuid.UUID `json:"id"`
	UserID           uuid.UUID `json:"userId"`
	RefreshTokenHash string    `json:"-"`
	UserAgent        string    `json:"userAgent"`
	IPAddress        string    `json:"ipAddress"`
	ExpiresAt        time.Time `json:"expiresAt"`
	CreatedAt        time.Time `json:"createdAt"`
}

// AgentEnrollment represents agent enrollment data
type AgentEnrollment struct {
	AgentID        string        `json:"agentId"`
	Hostname       string        `json:"hostname"`
	OSType         string        `json:"osType"`
	OSVersion      string        `json:"osVersion"`
	OSBuild        string        `json:"osBuild"`
	Platform       string        `json:"platform"`
	PlatformFamily string        `json:"platformFamily"`
	Architecture   string        `json:"architecture"`
	CPUModel       string        `json:"cpuModel"`
	CPUCores       int           `json:"cpuCores"`
	CPUThreads     int           `json:"cpuThreads"`
	CPUSpeed       float64       `json:"cpuSpeed"`
	TotalMemory    uint64        `json:"totalMemory"`
	BootTime       uint64        `json:"bootTime"`
	GPU            []GPUInfo     `json:"gpu"`
	Storage        []StorageInfo `json:"storage"`
	SerialNumber   string        `json:"serialNumber"`
	Manufacturer   string        `json:"manufacturer"`
	Model          string        `json:"model"`
	Domain         string        `json:"domain"`
	AgentVersion   string        `json:"agentVersion"`
	IPAddress      string        `json:"ipAddress"`
	MACAddress     string        `json:"macAddress"`
}

// EnrollmentToken represents a token for agent enrollment
type EnrollmentToken struct {
	ID          uuid.UUID         `json:"id"`
	Token       string            `json:"token"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	CreatedBy   *uuid.UUID        `json:"createdBy"`
	ExpiresAt   *time.Time        `json:"expiresAt"`
	MaxUses     *int              `json:"maxUses"`
	UseCount    int               `json:"useCount"`
	IsActive    bool              `json:"isActive"`
	Tags        []string          `json:"tags"`
	Metadata    map[string]string `json:"metadata"`
	CreatedAt   time.Time         `json:"createdAt"`
	UpdatedAt   time.Time         `json:"updatedAt"`
}

// AgentDownload represents a download audit record
type AgentDownload struct {
	ID           uuid.UUID  `json:"id"`
	TokenID      *uuid.UUID `json:"tokenId"`
	Platform     string     `json:"platform"`
	Architecture string     `json:"architecture"`
	IPAddress    string     `json:"ipAddress"`
	UserAgent    string     `json:"userAgent"`
	CreatedAt    time.Time  `json:"createdAt"`
}

// AgentInstaller represents available installer information
type AgentInstaller struct {
	Platform     string `json:"platform"`
	Architecture string `json:"architecture"`
	Filename     string `json:"filename"`
	Size         int64  `json:"size"`
	Version      string `json:"version"`
	DownloadURL  string `json:"downloadUrl"`
}
