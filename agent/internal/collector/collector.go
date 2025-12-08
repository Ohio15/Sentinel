package collector

import (
	"context"
	"runtime"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
	"github.com/shirou/gopsutil/v3/process"
)

// SystemInfo contains static system information
type SystemInfo struct {
	Hostname     string `json:"hostname"`
	OS           string `json:"os"`
	OSVersion    string `json:"os_version"`
	Platform     string `json:"platform"`
	Architecture string `json:"architecture"`
	CPUModel     string `json:"cpu_model"`
	CPUCores     int    `json:"cpu_cores"`
	TotalMemory  uint64 `json:"total_memory"`
	BootTime     uint64 `json:"boot_time"`
}

// Metrics contains current system metrics
type Metrics struct {
	Timestamp       time.Time `json:"timestamp"`
	CPUPercent      float64   `json:"cpu_percent"`
	MemoryPercent   float64   `json:"memory_percent"`
	MemoryUsed      uint64    `json:"memory_used"`
	MemoryAvailable uint64    `json:"memory_available"`
	DiskPercent     float64   `json:"disk_percent"`
	DiskUsed        uint64    `json:"disk_used"`
	DiskTotal       uint64    `json:"disk_total"`
	NetworkRxBytes  uint64    `json:"network_rx_bytes"`
	NetworkTxBytes  uint64    `json:"network_tx_bytes"`
	ProcessCount    int       `json:"process_count"`
	Uptime          uint64    `json:"uptime"`
}

// Collector handles system metrics collection
type Collector struct {
	lastNetRx uint64
	lastNetTx uint64
	lastCheck time.Time
}

// New creates a new metrics collector
func New() *Collector {
	return &Collector{
		lastCheck: time.Now(),
	}
}

// GetSystemInfo collects static system information
func (c *Collector) GetSystemInfo() (*SystemInfo, error) {
	hostInfo, err := host.Info()
	if err != nil {
		return nil, err
	}

	cpuInfo, err := cpu.Info()
	if err != nil {
		return nil, err
	}

	memInfo, err := mem.VirtualMemory()
	if err != nil {
		return nil, err
	}

	cpuModel := ""
	if len(cpuInfo) > 0 {
		cpuModel = cpuInfo[0].ModelName
	}

	return &SystemInfo{
		Hostname:     hostInfo.Hostname,
		OS:           hostInfo.OS,
		OSVersion:    hostInfo.PlatformVersion,
		Platform:     hostInfo.Platform,
		Architecture: runtime.GOARCH,
		CPUModel:     cpuModel,
		CPUCores:     runtime.NumCPU(),
		TotalMemory:  memInfo.Total,
		BootTime:     hostInfo.BootTime,
	}, nil
}

// Collect gathers current system metrics
func (c *Collector) Collect(ctx context.Context) (*Metrics, error) {
	metrics := &Metrics{
		Timestamp: time.Now(),
	}

	// CPU usage (averaged over 1 second)
	cpuPercent, err := cpu.PercentWithContext(ctx, time.Second, false)
	if err == nil && len(cpuPercent) > 0 {
		metrics.CPUPercent = cpuPercent[0]
	}

	// Memory usage
	memInfo, err := mem.VirtualMemoryWithContext(ctx)
	if err == nil {
		metrics.MemoryPercent = memInfo.UsedPercent
		metrics.MemoryUsed = memInfo.Used
		metrics.MemoryAvailable = memInfo.Available
	}

	// Disk usage (primary disk)
	diskPath := "/"
	if runtime.GOOS == "windows" {
		diskPath = "C:"
	}
	diskInfo, err := disk.UsageWithContext(ctx, diskPath)
	if err == nil {
		metrics.DiskPercent = diskInfo.UsedPercent
		metrics.DiskUsed = diskInfo.Used
		metrics.DiskTotal = diskInfo.Total
	}

	// Network I/O
	netIO, err := net.IOCountersWithContext(ctx, false)
	if err == nil && len(netIO) > 0 {
		metrics.NetworkRxBytes = netIO[0].BytesRecv
		metrics.NetworkTxBytes = netIO[0].BytesSent

		// Store for rate calculation
		c.lastNetRx = netIO[0].BytesRecv
		c.lastNetTx = netIO[0].BytesSent
	}

	// Process count
	procs, err := process.ProcessesWithContext(ctx)
	if err == nil {
		metrics.ProcessCount = len(procs)
	}

	// Uptime
	hostInfo, err := host.InfoWithContext(ctx)
	if err == nil {
		metrics.Uptime = hostInfo.Uptime
	}

	c.lastCheck = time.Now()
	return metrics, nil
}

// GetProcessList returns a list of running processes
func (c *Collector) GetProcessList(ctx context.Context) ([]map[string]interface{}, error) {
	procs, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]map[string]interface{}, 0, len(procs))
	for _, p := range procs {
		name, _ := p.NameWithContext(ctx)
		cmdline, _ := p.CmdlineWithContext(ctx)
		cpuPercent, _ := p.CPUPercentWithContext(ctx)
		memPercent, _ := p.MemoryPercentWithContext(ctx)
		memInfo, _ := p.MemoryInfoWithContext(ctx)
		status, _ := p.StatusWithContext(ctx)
		username, _ := p.UsernameWithContext(ctx)
		createTime, _ := p.CreateTimeWithContext(ctx)

		proc := map[string]interface{}{
			"pid":         p.Pid,
			"name":        name,
			"cmdline":     cmdline,
			"cpu_percent": cpuPercent,
			"mem_percent": memPercent,
			"status":      status,
			"username":    username,
			"create_time": createTime,
		}

		if memInfo != nil {
			proc["rss"] = memInfo.RSS
			proc["vms"] = memInfo.VMS
		}

		result = append(result, proc)
	}

	return result, nil
}

// GetDiskInfo returns disk partition information
func (c *Collector) GetDiskInfo(ctx context.Context) ([]map[string]interface{}, error) {
	partitions, err := disk.PartitionsWithContext(ctx, false)
	if err != nil {
		return nil, err
	}

	result := make([]map[string]interface{}, 0, len(partitions))
	for _, p := range partitions {
		usage, err := disk.UsageWithContext(ctx, p.Mountpoint)
		if err != nil {
			continue
		}

		diskInfo := map[string]interface{}{
			"device":      p.Device,
			"mountpoint":  p.Mountpoint,
			"fstype":      p.Fstype,
			"total":       usage.Total,
			"used":        usage.Used,
			"free":        usage.Free,
			"percent":     usage.UsedPercent,
			"inodes_used": usage.InodesUsed,
		}
		result = append(result, diskInfo)
	}

	return result, nil
}

// GetNetworkInterfaces returns network interface information
func (c *Collector) GetNetworkInterfaces(ctx context.Context) ([]map[string]interface{}, error) {
	interfaces, err := net.InterfacesWithContext(ctx)
	if err != nil {
		return nil, err
	}

	ioCounters, _ := net.IOCountersWithContext(ctx, true)
	ioMap := make(map[string]net.IOCountersStat)
	for _, io := range ioCounters {
		ioMap[io.Name] = io
	}

	result := make([]map[string]interface{}, 0, len(interfaces))
	for _, iface := range interfaces {
		addrs := make([]string, 0)
		for _, addr := range iface.Addrs {
			addrs = append(addrs, addr.Addr)
		}

		ifaceInfo := map[string]interface{}{
			"name":  iface.Name,
			"mac":   iface.HardwareAddr,
			"flags": iface.Flags,
			"addrs": addrs,
		}

		if io, ok := ioMap[iface.Name]; ok {
			ifaceInfo["bytes_sent"] = io.BytesSent
			ifaceInfo["bytes_recv"] = io.BytesRecv
			ifaceInfo["packets_sent"] = io.PacketsSent
			ifaceInfo["packets_recv"] = io.PacketsRecv
			ifaceInfo["errors_in"] = io.Errin
			ifaceInfo["errors_out"] = io.Errout
		}

		result = append(result, ifaceInfo)
	}

	return result, nil
}
