package log

import "time"

// Record is a single log entry passed to handlers.
type Record struct {
	Time   time.Time
	Level  Level
	Stream string
	Prefix string
	Msg    string
}
