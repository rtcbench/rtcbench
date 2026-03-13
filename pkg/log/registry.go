package log

import "errors"

// Registry holds handlers and per-stream level configuration.
// It is the central coordinator for the logging system.
type Registry struct {
	handlers []Handler
	levels   map[string]Level // stream name -> minimum level
}

// NewRegistry creates a Registry with the given handlers and per-stream levels.
// Streams not in the levels map default to LevelInfo.
func NewRegistry(handlers []Handler, levels map[string]Level) *Registry {
	return &Registry{
		handlers: handlers,
		levels:   levels,
	}
}

// NewLogger creates a Logger for the given stream and prefix.
func (r *Registry) NewLogger(stream, prefix string) *Logger {
	return &Logger{
		stream:   stream,
		prefix:   prefix,
		registry: r,
	}
}

// Close closes all handlers and returns the first error encountered.
func (r *Registry) Close() error {
	var errs []error
	for _, h := range r.handlers {
		if err := h.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (r *Registry) emit(rec Record) {
	minLevel, ok := r.levels[rec.Stream]
	if !ok {
		minLevel = LevelInfo
	}
	if rec.Level < minLevel {
		return
	}
	for _, h := range r.handlers {
		h.Handle(rec)
	}
}
