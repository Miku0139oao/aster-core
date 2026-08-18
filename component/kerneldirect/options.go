package kerneldirect

import "fmt"

// NormalizeMaxEntries applies the user-facing kernel-direct-max-entries contract:
// omit/0 becomes DefaultMaxEntries; values above MaximumMaxEntries are rejected.
func NormalizeMaxEntries(maxEntries uint32) (uint32, error) {
	if maxEntries == 0 {
		maxEntries = DefaultMaxEntries
	}
	if maxEntries > MaximumMaxEntries {
		return 0, fmt.Errorf("tun kernel-direct-max-entries exceeds maximum %d", MaximumMaxEntries)
	}
	return maxEntries, nil
}
