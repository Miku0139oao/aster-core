package listener

import (
	"testing"

	IN "github.com/Miku0139oao/aster-core/listener/inbound"

	"github.com/stretchr/testify/require"
)

func TestParseListenerTunCongestionControllerValidation(t *testing.T) {
	for _, tc := range []struct {
		stack   string
		control string
		wantErr bool
	}{
		{stack: "mips", control: ""},
		{stack: "mips", control: "BbR3"},
		{stack: "mips", control: "not-a-controller", wantErr: true},
		{stack: "mips", control: "bbr ", wantErr: true},
		{stack: "gvisor", control: "not-a-controller"},
		{stack: "system", control: "not-a-controller"},
		{stack: "mixed", control: "not-a-controller"},
	} {
		t.Run(tc.stack+"/"+tc.control, func(t *testing.T) {
			parsed, err := ParseListener(map[string]any{
				"name":                  "test-tun",
				"type":                  "tun",
				"stack":                 tc.stack,
				"congestion-controller": tc.control,
			})
			if tc.wantErr {
				require.ErrorContains(t, err, "invalid tun congestion-controller")
				return
			}
			require.NoError(t, err)
			option, ok := parsed.Config().(*IN.TunOption)
			require.True(t, ok)
			require.Equal(t, tc.control, option.CongestionController)
			require.NoError(t, parsed.Close())
		})
	}
}
