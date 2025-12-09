//go:build !windows

package collector

// getGPUInfo returns graphics card information on Unix-like systems
// This is a stub that returns empty results - GPU detection on Linux/macOS
// would require platform-specific tools like lspci or system_profiler
func (c *Collector) getGPUInfo() []GPUInfo {
	return make([]GPUInfo, 0)
}
