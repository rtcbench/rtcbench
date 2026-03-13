package log

import (
	"fmt"
	"time"
)

// Logger emits leveled log messages to handlers via a Registry.
// Create loggers through Registry.NewLogger.
type Logger struct {
	stream   string
	prefix   string
	registry *Registry
}

// Debugf logs at DEBUG level.
func (l *Logger) Debugf(format string, args ...any) {
	l.emit(LevelDebug, format, args...)
}

// Infof logs at INFO level.
func (l *Logger) Infof(format string, args ...any) {
	l.emit(LevelInfo, format, args...)
}

// Errorf logs at ERROR level.
func (l *Logger) Errorf(format string, args ...any) {
	l.emit(LevelError, format, args...)
}

// With returns a child Logger with an appended prefix segment.
func (l *Logger) With(prefix string) *Logger {
	return &Logger{
		stream:   l.stream,
		prefix:   l.prefix + prefix,
		registry: l.registry,
	}
}

func (l *Logger) emit(level Level, format string, args ...any) {
	l.registry.emit(Record{
		Time:   time.Now(),
		Level:  level,
		Stream: l.stream,
		Prefix: l.prefix,
		Msg:    fmt.Sprintf(format, args...),
	})
}
