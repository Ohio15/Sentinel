//go:build !windows

package collector

import (
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// getGPUInfo returns graphics card information on Unix-like systems
// This is a stub that returns empty results - GPU detection on Linux/macOS
// would require platform-specific tools like lspci or system_profiler
func (c *Collector) getGPUInfo() []GPUInfo {
	return make([]GPUInfo, 0)
}

var (
	gpuMetricsCache struct {
		metrics   []GPUMetrics
		timestamp time.Time
	}
	gpuMetricsCacheMu  sync.Mutex
	gpuMetricsCacheTTL = 1 * time.Second // Cache for 1 second
)

// getGPUMetrics returns real-time GPU utilization metrics on Unix
func getGPUMetrics() []GPUMetrics {
	gpuMetricsCacheMu.Lock()
	defer gpuMetricsCacheMu.Unlock()

	// Return cached values if still valid
	if time.Since(gpuMetricsCache.timestamp) < gpuMetricsCacheTTL {
		return gpuMetricsCache.metrics
	}

	metrics := make([]GPUMetrics, 0)

	// Try NVIDIA nvidia-smi (works on Linux with NVIDIA drivers)
	if nvidiaMetrics := getNvidiaMetrics(); len(nvidiaMetrics) > 0 {
		metrics = append(metrics, nvidiaMetrics...)
	}

	// Update cache
	gpuMetricsCache.metrics = metrics
	gpuMetricsCache.timestamp = time.Now()

	return metrics
}

// getNvidiaMetrics queries nvidia-smi for NVIDIA GPU metrics on Linux
func getNvidiaMetrics() []GPUMetrics {
	metrics := make([]GPUMetrics, 0)

	// nvidia-smi query for utilization, memory, temperature, power
	cmd := exec.Command("nvidia-smi",
		"--query-gpu=name,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw",
		"--format=csv,noheader,nounits")

	output, err := cmd.Output()
	if err != nil {
		return metrics
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}

		parts := strings.Split(line, ", ")
		if len(parts) >= 4 {
			metric := GPUMetrics{
				Name: strings.TrimSpace(parts[0]),
			}

			// Utilization percentage
			if val, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64); err == nil {
				metric.Utilization = val
			}

			// Memory used (MiB to bytes)
			if val, err := strconv.ParseUint(strings.TrimSpace(parts[2]), 10, 64); err == nil {
				metric.MemoryUsed = val * 1024 * 1024
			}

			// Memory total (MiB to bytes)
			if val, err := strconv.ParseUint(strings.TrimSpace(parts[3]), 10, 64); err == nil {
				metric.MemoryTotal = val * 1024 * 1024
			}

			// Temperature (optional)
			if len(parts) >= 5 {
				if val, err := strconv.ParseFloat(strings.TrimSpace(parts[4]), 64); err == nil {
					metric.Temperature = val
				}
			}

			// Power draw (optional)
			if len(parts) >= 6 {
				if val, err := strconv.ParseFloat(strings.TrimSpace(parts[5]), 64); err == nil {
					metric.PowerDraw = val
				}
			}

			metrics = append(metrics, metric)
		}
	}

	return metrics
}
