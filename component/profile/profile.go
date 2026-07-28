package profile

import (
	"github.com/Miku0139oao/aster-core/common/atomic"
)

// StoreSelected is a global switch for storing selected proxy to cache
var StoreSelected = atomic.NewBool(true)
