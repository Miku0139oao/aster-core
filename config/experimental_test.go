package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseExperimentalIdleMemoryScavenge(t *testing.T) {
	got, err := parseExperimental(&RawConfig{Experimental: RawExperimental{
		IdleMemoryScavenge:     true,
		IdleMemoryScavengeIdle: 120,
	}})
	require.NoError(t, err)
	require.True(t, got.IdleMemoryScavenge)
	require.Equal(t, 120, got.IdleMemoryScavengeIdle)
}

func TestParseExperimentalIdleMemoryScavengeDefaultsOff(t *testing.T) {
	got, err := parseExperimental(&RawConfig{})
	require.NoError(t, err)
	require.False(t, got.IdleMemoryScavenge)
	require.Zero(t, got.IdleMemoryScavengeIdle)
}

func TestParseExperimentalRejectsNegativeIdleMemoryScavengeIdle(t *testing.T) {
	_, err := parseExperimental(&RawConfig{Experimental: RawExperimental{
		IdleMemoryScavengeIdle: -1,
	}})
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "idle-memory-scavenge-idle"))
}

func TestUnmarshalRawConfigIdleMemoryScavenge(t *testing.T) {
	raw, err := UnmarshalRawConfig([]byte(`
experimental:
  idle-memory-scavenge: true
  idle-memory-scavenge-idle: 90
`))
	require.NoError(t, err)
	require.True(t, raw.Experimental.IdleMemoryScavenge)
	require.Equal(t, 90, raw.Experimental.IdleMemoryScavengeIdle)
}
