package log

import "fmt"

// Level represents log severity.
type Level int8

const (
	LevelDebug Level = iota
	LevelInfo
	LevelError
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelError:
		return "ERROR"
	default:
		return fmt.Sprintf("LEVEL(%d)", int(l))
	}
}

// ParseLevel converts a lowercase string ("debug", "info", "error") to a Level.
func ParseLevel(s string) (Level, error) {
	switch s {
	case "debug":
		return LevelDebug, nil
	case "info":
		return LevelInfo, nil
	case "error":
		return LevelError, nil
	default:
		return 0, fmt.Errorf("unknown log level %q", s)
	}
}
