//go:build !with_low_memory

package pool

const (
	largePoolProcFactor = 2
	largePoolMin        = 4
	largePoolBudget     = 2 << 20 // 2 MiB per 16 KiB+ size class
)
