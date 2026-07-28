package route

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSetUIPathEmptyDisablesUI(t *testing.T) {
	SetUIPath("")
	require.Empty(t, uiPath)
}
