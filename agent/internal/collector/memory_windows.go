//go:build windows

package collector

import (
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	memoryDetailsCache struct {
		committed    uint64
		pagedPool    uint64
		nonPagedPool uint64
		timestamp    time.Time
	}
	memoryDetailsCacheMu  sync.Mutex
	memoryDetailsCacheTTL = 5 * time.Second // Cache for 5 seconds (WMI is expensive)
)

// getMemoryDetails returns Windows committed memory and paged/non-paged pool memory using PowerShell/WMI
func getMemoryDetails() (committed, pagedPool, nonPagedPool uint64) {
	memoryDetailsCacheMu.Lock()
	defer memoryDetailsCacheMu.Unlock()

	// Return cached values if still valid
	if time.Since(memoryDetailsCache.timestamp) < memoryDetailsCacheTTL {
		return memoryDetailsCache.committed, memoryDetailsCache.pagedPool, memoryDetailsCache.nonPagedPool
	}

	// Query WMI for memory data
	// Win32_PerfFormattedData_PerfOS_Memory provides real-time performance data
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
		"Get-CimInstance -ClassName Win32_PerfFormattedData_PerfOS_Memory | Select-Object -Property CommittedBytes,PoolPagedBytes,PoolNonpagedBytes | Format-List")

	output, err := cmd.Output()
	if err != nil {
		return 0, 0, 0
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "CommittedBytes") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				if val, err := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 64); err == nil {
					committed = val
				}
			}
		} else if strings.HasPrefix(line, "PoolPagedBytes") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				if val, err := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 64); err == nil {
					pagedPool = val
				}
			}
		} else if strings.HasPrefix(line, "PoolNonpagedBytes") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				if val, err := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 64); err == nil {
					nonPagedPool = val
				}
			}
		}
	}

	// Update cache
	memoryDetailsCache.committed = committed
	memoryDetailsCache.pagedPool = pagedPool
	memoryDetailsCache.nonPagedPool = nonPagedPool
	memoryDetailsCache.timestamp = time.Now()

	return committed, pagedPool, nonPagedPool
}
