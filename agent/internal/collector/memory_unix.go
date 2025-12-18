//go:build !windows

package collector

// getMemoryDetails returns committed memory and paged/non-paged pool memory
// On Unix/Linux, these Windows-specific concepts don't apply, so we return 0
func getMemoryDetails() (committed, pagedPool, nonPagedPool uint64) {
	return 0, 0, 0
}
