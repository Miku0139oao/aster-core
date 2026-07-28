package net

import (
	"github.com/Miku0139oao/aster-core/common/net/deadline"
	"github.com/Miku0139oao/aster-core/common/net/packet"
)

type (
	EnhancePacketConn = packet.EnhancePacketConn
	WaitReadFrom      = packet.WaitReadFrom
)

var (
	NewEnhancePacketConn    = packet.NewEnhancePacketConn
	NewThreadSafePacketConn = packet.NewThreadSafePacketConn
	NewRefPacketConn        = packet.NewRefPacketConn
)

var (
	NewDeadlineNetPacketConn         = deadline.NewNetPacketConn
	NewDeadlineEnhancePacketConn     = deadline.NewEnhancePacketConn
	NewDeadlineSingPacketConn        = deadline.NewSingPacketConn
	NewDeadlineEnhanceSingPacketConn = deadline.NewEnhanceSingPacketConn
)
