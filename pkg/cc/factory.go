package cc

import "fmt"

// New returns a Controller for the named algorithm, or nil for "none".
// Valid names: "remb", "gcc", "none" (empty string is treated as "remb").
func New(algorithm string) (Controller, error) {
	switch algorithm {
	case "", "none":
		return nil, nil
	case "remb":
		return NewREMB(), nil
	case "gcc":
		return NewGCC(), nil
	default:
		return nil, fmt.Errorf("unknown congestion-control algorithm %q (want remb, gcc, or none)", algorithm)
	}
}
