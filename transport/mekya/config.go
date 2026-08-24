package mekya

import (
	"fmt"

	"github.com/Miku0139oao/aster-core/transport/mkcp"
)

const (
	defaultMaxRequestSize                 = 96_000
	defaultMaxWriteDelayMs                = 20
	defaultPollingIntervalMs              = 20
	defaultMaxWriteSize                   = 1 << 20
	defaultMaxWriteDurationMs             = 100
	defaultMaxSimultaneousWriteConnection = 128
	defaultPacketWritingBuffer            = 256
	defaultMaxSessions                    = 256
	maximumRequestSize                    = 8 << 20
	maximumWriteSize                      = 64 << 20
	maximumPacketWritingBuffer            = 65_536
	maximumMaxSessions                    = 4096
	maximumTimingMilliseconds             = 10 * 60 * 1000
)

type Config struct {
	KCP                            mkcp.Config
	URL                            string
	H2PoolSize                     int
	MaxWriteDelay                  int
	MaxRequestSize                 int
	PollingIntervalInitial         int
	MaxWriteSize                   int
	MaxWriteDurationMs             int
	MaxSimultaneousWriteConnection int
	PacketWritingBuffer            int
	MaxSessions                    int
}

func normalizeConfig(config Config) (Config, error) {
	if config.H2PoolSize < 0 || config.H2PoolSize > 64 {
		return Config{}, fmt.Errorf("mekya: h2-pool-size %d is outside 0..64", config.H2PoolSize)
	}
	if config.MaxWriteDelay < 0 || config.MaxWriteDelay > maximumTimingMilliseconds ||
		config.PollingIntervalInitial < 0 || config.PollingIntervalInitial > maximumTimingMilliseconds ||
		config.MaxWriteDurationMs < 0 || config.MaxWriteDurationMs > maximumTimingMilliseconds {
		return Config{}, fmt.Errorf("mekya: timing values must be within 0..%d milliseconds", maximumTimingMilliseconds)
	}
	if config.MaxRequestSize < 0 || config.MaxRequestSize > maximumRequestSize {
		return Config{}, fmt.Errorf("mekya: max-request-size %d is outside 0..%d", config.MaxRequestSize, maximumRequestSize)
	}
	if config.MaxWriteSize < 0 || config.MaxWriteSize > maximumWriteSize {
		return Config{}, fmt.Errorf("mekya: max-write-size %d is outside 0..%d", config.MaxWriteSize, maximumWriteSize)
	}
	if config.MaxSimultaneousWriteConnection < 0 || config.MaxSimultaneousWriteConnection > 128 {
		return Config{}, fmt.Errorf("mekya: max-simultaneous-write-connection %d is outside 0..128", config.MaxSimultaneousWriteConnection)
	}
	if config.PacketWritingBuffer < 0 || config.PacketWritingBuffer > maximumPacketWritingBuffer {
		return Config{}, fmt.Errorf("mekya: packet-writing-buffer %d is outside 0..%d", config.PacketWritingBuffer, maximumPacketWritingBuffer)
	}
	if config.MaxSessions < 0 || config.MaxSessions > maximumMaxSessions {
		return Config{}, fmt.Errorf("mekya: max-sessions %d is outside 0..%d", config.MaxSessions, maximumMaxSessions)
	}
	if config.MaxWriteDelay == 0 {
		config.MaxWriteDelay = defaultMaxWriteDelayMs
	}
	if config.MaxRequestSize == 0 {
		config.MaxRequestSize = defaultMaxRequestSize
	}
	if config.PollingIntervalInitial == 0 {
		config.PollingIntervalInitial = defaultPollingIntervalMs
	}
	if config.MaxWriteSize == 0 {
		config.MaxWriteSize = defaultMaxWriteSize
	}
	if config.MaxWriteDurationMs == 0 {
		config.MaxWriteDurationMs = defaultMaxWriteDurationMs
	}
	if config.MaxSimultaneousWriteConnection == 0 {
		config.MaxSimultaneousWriteConnection = defaultMaxSimultaneousWriteConnection
	}
	if config.PacketWritingBuffer == 0 {
		config.PacketWritingBuffer = defaultPacketWritingBuffer
	}
	if config.MaxSessions == 0 {
		config.MaxSessions = defaultMaxSessions
	}
	return config, nil
}
