package sing_tun

import (
	"testing"

	C "github.com/Miku0139oao/aster-core/constant"
	LC "github.com/Miku0139oao/aster-core/listener/config"

	"github.com/stretchr/testify/require"
)

func TestNewRejectsInvalidCongestionControllerBeforeUsingTunnel(t *testing.T) {
	// A nil tunnel would panic at the type assertion if validation ran after
	// listener initialization. No interface or other resource may be touched.
	listener, err := New(LC.Tun{Stack: C.TunMips, CongestionController: "not-a-controller"}, nil)
	require.Nil(t, listener)
	require.ErrorContains(t, err, "invalid tun congestion-controller")
}
