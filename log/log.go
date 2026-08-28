package log

import (
	"fmt"
	"os"
	"sync/atomic"

	"github.com/Miku0139oao/aster-core/common/observable"

	log "github.com/sirupsen/logrus"
)

var (
	logCh  = make(chan Event)
	source = observable.NewObservable[Event](logCh)
	level  atomic.Int32
)

func init() {
	level.Store(int32(INFO))
	log.SetOutput(os.Stdout)
	log.SetLevel(log.DebugLevel)
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp:             true,
		TimestampFormat:           "2006-01-02T15:04:05.000000000Z07:00",
		EnvironmentOverrideColors: true,
	})
}

type Event struct {
	LogLevel LogLevel
	Payload  string
}

func (e *Event) Type() string {
	return e.LogLevel.String()
}

func Infoln(format string, v ...any) {
	emit(INFO, format, v...)
}

func Warnln(format string, v ...any) {
	emit(WARNING, format, v...)
}

func Errorln(format string, v ...any) {
	emit(ERROR, format, v...)
}

func Debugln(format string, v ...any) {
	emit(DEBUG, format, v...)
}

func Fatalln(format string, v ...any) {
	log.Fatalf(format, v...)
}

func Subscribe() observable.Subscription[Event] {
	sub, _ := source.Subscribe()
	return sub
}

func UnSubscribe(sub observable.Subscription[Event]) {
	source.UnSubscribe(sub)
}

func Level() LogLevel {
	return LogLevel(level.Load())
}

func SetLevel(newLevel LogLevel) {
	level.Store(int32(newLevel))
}

// Enabled reports whether a log call at logLevel has an active destination.
// Controller log subscribers receive all levels and apply their own filter,
// so their presence also enables event creation.
func Enabled(logLevel LogLevel) bool {
	return logLevel >= Level() || source.HasSubscribers()
}

func emit(logLevel LogLevel, format string, v ...any) {
	if !Enabled(logLevel) {
		return
	}
	payload := format
	if len(v) > 0 {
		payload = fmt.Sprintf(format, v...)
	}
	dispatch(Event{LogLevel: logLevel, Payload: payload})
}

func emitArgs(logLevel LogLevel, args ...any) {
	if !Enabled(logLevel) {
		return
	}
	dispatch(Event{LogLevel: logLevel, Payload: fmt.Sprint(args...)})
}

func dispatch(event Event) {
	if source.HasSubscribers() {
		logCh <- event
	}
	print(event)
}

func print(data Event) {
	if data.LogLevel < Level() {
		return
	}

	switch data.LogLevel {
	case INFO:
		log.Infoln(data.Payload)
	case WARNING:
		log.Warnln(data.Payload)
	case ERROR:
		log.Errorln(data.Payload)
	case DEBUG:
		log.Debugln(data.Payload)
	}
}
