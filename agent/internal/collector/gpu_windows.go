//go:build windows

package collector

import (
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// getGPUInfo returns graphics card information on Windows
func (c *Collector) getGPUInfo() []GPUInfo {
	gpus := make([]GPUInfo, 0)

	// Use WMIC to get GPU info
	cmd := exec.Command("wmic", "path", "win32_videocontroller", "get", "Name,AdapterRAM,DriverVersion,AdapterCompatibility", "/format:csv")
	output, err := cmd.Output()
	if err != nil {
		return gpus
	}

	lines := strings.Split(string(output), "\n")
	for i, line := range lines {
		// Skip header and empty lines
		if i == 0 || strings.TrimSpace(line) == "" || strings.HasPrefix(line, "Node") {
			continue
		}

		parts := strings.Split(line, ",")
		if len(parts) >= 5 {
			memory, _ := strconv.ParseUint(strings.TrimSpace(parts[2]), 10, 64)
			gpus = append(gpus, GPUInfo{
				Name:          strings.TrimSpace(parts[4]),
				Vendor:        strings.TrimSpace(parts[1]),
				Memory:        memory,
				DriverVersion: strings.TrimSpace(parts[3]),
			})
		}
	}

	return gpus
}

var (
	gpuMetricsCache struct {
		metrics   []GPUMetrics
		timestamp time.Time
	}
	gpuMetricsCacheMu  sync.Mutex
	gpuMetricsCacheTTL = 1 * time.Second // Cache for 1 second
)

// getGPUMetrics returns real-time GPU utilization metrics
func getGPUMetrics() []GPUMetrics {
	gpuMetricsCacheMu.Lock()
	defer gpuMetricsCacheMu.Unlock()

	// Return cached values if still valid
	if time.Since(gpuMetricsCache.timestamp) < gpuMetricsCacheTTL {
		return gpuMetricsCache.metrics
	}

	metrics := make([]GPUMetrics, 0)

	// Try NVIDIA nvidia-smi first (most common discrete GPU)
	if nvidiaMetrics := getNvidiaMetrics(); len(nvidiaMetrics) > 0 {
		metrics = append(metrics, nvidiaMetrics...)
	}

	// If no NVIDIA GPUs found, try to get basic info from WMI
	if len(metrics) == 0 {
		metrics = getBasicGPUMetrics()
	}

	// Update cache
	gpuMetricsCache.metrics = metrics
	gpuMetricsCache.timestamp = time.Now()

	return metrics
}

// getNvidiaMetrics queries nvidia-smi for NVIDIA GPU metrics
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

// getBasicGPUMetrics returns basic GPU info when nvidia-smi is not available
func getBasicGPUMetrics() []GPUMetrics {
	metrics := make([]GPUMetrics, 0)

	// Use WMIC to get GPU info
	cmd := exec.Command("wmic", "path", "win32_videocontroller", "get", "Name,AdapterRAM", "/format:csv")
	output, err := cmd.Output()
	if err != nil {
		return metrics
	}

	lines := strings.Split(string(output), "\n")
	for i, line := range lines {
		if i == 0 || strings.TrimSpace(line) == "" || strings.HasPrefix(line, "Node") {
			continue
		}

		parts := strings.Split(line, ",")
		if len(parts) >= 3 {
			metric := GPUMetrics{
				Name: strings.TrimSpace(parts[2]),
			}

			// Memory total from AdapterRAM
			if val, err := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 64); err == nil {
				metric.MemoryTotal = val
			}

			// Note: Without nvidia-smi, we can't get real-time utilization
			// Utilization will be 0

			metrics = append(metrics, metric)
		}
	}

	return metrics
}
