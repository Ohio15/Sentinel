//go:build windows

package collector

import (
	"os/exec"
	"strconv"
	"strings"
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
