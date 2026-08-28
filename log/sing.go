package log

import (
	"context"
	"fmt"

	L "github.com/metacubex/sing/common/logger"
)

type singLogger struct{}

func (l singLogger) TraceContext(ctx context.Context, args ...any) {
	emitArgs(DEBUG, args...)
}

func (l singLogger) DebugContext(ctx context.Context, args ...any) {
	emitArgs(DEBUG, args...)
}

func (l singLogger) InfoContext(ctx context.Context, args ...any) {
	emitArgs(INFO, args...)
}

func (l singLogger) WarnContext(ctx context.Context, args ...any) {
	emitArgs(WARNING, args...)
}

func (l singLogger) ErrorContext(ctx context.Context, args ...any) {
	emitArgs(ERROR, args...)
}

func (l singLogger) FatalContext(ctx context.Context, args ...any) {
	Fatalln(fmt.Sprint(args...))
}

func (l singLogger) PanicContext(ctx context.Context, args ...any) {
	Fatalln(fmt.Sprint(args...))
}

func (l singLogger) Trace(args ...any) {
	emitArgs(DEBUG, args...)
}

func (l singLogger) Debug(args ...any) {
	emitArgs(DEBUG, args...)
}

func (l singLogger) Info(args ...any) {
	emitArgs(INFO, args...)
}

func (l singLogger) Warn(args ...any) {
	emitArgs(WARNING, args...)
}

func (l singLogger) Error(args ...any) {
	emitArgs(ERROR, args...)
}

func (l singLogger) Fatal(args ...any) {
	Fatalln(fmt.Sprint(args...))
}

func (l singLogger) Panic(args ...any) {
	Fatalln(fmt.Sprint(args...))
}

type singInfoToDebugLogger struct {
	singLogger
}

func (l singInfoToDebugLogger) InfoContext(ctx context.Context, args ...any) {
	emitArgs(DEBUG, args...)
}

func (l singInfoToDebugLogger) Info(args ...any) {
	emitArgs(DEBUG, args...)
}

var (
	SingLogger            L.ContextLogger = singLogger{}
	SingInfoToDebugLogger L.ContextLogger = singInfoToDebugLogger{}
)
