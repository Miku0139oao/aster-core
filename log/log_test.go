package log

import (
	"io"
	"testing"
	"time"

	logrus "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

func TestEnabled(t *testing.T) {
	oldLevel := Level()
	SetLevel(INFO)
	t.Cleanup(func() { SetLevel(oldLevel) })

	require.False(t, Enabled(DEBUG))
	require.True(t, Enabled(INFO))

	sub := Subscribe()
	t.Cleanup(func() { UnSubscribe(sub) })
	require.True(t, Enabled(DEBUG))

	Debugln("debug event %d", 1)
	select {
	case event := <-sub:
		require.Equal(t, DEBUG, event.LogLevel)
		require.Equal(t, "debug event 1", event.Payload)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for debug event")
	}
}

func BenchmarkDisabledDebugLog(b *testing.B) {
	oldLevel := Level()
	SetLevel(INFO)
	b.Cleanup(func() { SetLevel(oldLevel) })
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Debugln("[Rule] use %s rules", "default")
	}
}

func benchmarkEnabledLog(b *testing.B, run func()) {
	b.Helper()
	oldLevel := Level()
	SetLevel(INFO)
	oldOut := logrus.StandardLogger().Out
	logrus.SetOutput(io.Discard)
	b.Cleanup(func() {
		SetLevel(oldLevel)
		logrus.SetOutput(oldOut)
	})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		run()
	}
}

func BenchmarkEnabledInfoLog(b *testing.B) {
	benchmarkEnabledLog(b, func() {
		Infoln("[Rule] use %s rules", "default")
	})
}

func BenchmarkEnabledInfoLogNoArgs(b *testing.B) {
	benchmarkEnabledLog(b, func() {
		Infoln("listener started")
	})
}

func BenchmarkSingLoggerInfo(b *testing.B) {
	benchmarkEnabledLog(b, func() {
		SingLogger.Info("listener", "started")
	})
}
