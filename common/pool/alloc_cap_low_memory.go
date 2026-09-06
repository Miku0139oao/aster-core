//go:build with_low_memory

package pool

const (
	largePoolProcFactor = 1
	largePoolMin        = 2
	largePoolBudget     = 512 << 10 // 512 KiB per 16 KiB+ size class
)
