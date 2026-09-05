//go:build !go1.24

package statistic

// Go 1.20-1.23 use the comparable-hash fallback and older escape analysis.
// All four legacy CI versions measure eight full-lifecycle allocations.
const defaultTrackerLifecycleAllocs = 8.0
