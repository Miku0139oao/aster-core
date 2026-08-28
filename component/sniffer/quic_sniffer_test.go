package sniffer

import (
	"crypto"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHkdfExpandLabelRejectsOversized(t *testing.T) {
	secret := make([]byte, 32)
	_, err := hkdfExpandLabel(secret, "test", 255*crypto.SHA256.Size()+1)
	require.Error(t, err)
}

func TestNewQUICInitialKeySucceedsForKnownVersions(t *testing.T) {
	for _, s := range []*quicStructure{&quicDraft29, &quicV1, &quicV2} {
		key, err := newQUICInitialKey([]byte{0x01, 0x02, 0x03, 0x04}, s)
		require.NoError(t, err, "version %s", s.labelPrefix)
		require.NotEmpty(t, key.hp[:])
		require.NotEmpty(t, key.key[:])
		require.NotEmpty(t, key.iv[:])
		require.NotNil(t, key.hpBlock)
		require.NotNil(t, key.aead)
		require.Equal(t, int64(-1), key.largestPacketNumber)
	}
}
