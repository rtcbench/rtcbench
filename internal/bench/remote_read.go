package bench

import (
	"context"
	"errors"
	"time"
)

func (e *experiment) readRemote(ctx context.Context, host, command string) (string, error) {
	for attempt := 1; ; attempt++ {
		data, err := e.remote.Run(ctx, host, command)
		if err == nil {
			return data, nil
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		var status interface{ ExitCode() int }
		if attempt == 5 || !errors.As(err, &status) || status.ExitCode() != 255 {
			return data, err
		}
		if err := wait(ctx, 2*time.Second); err != nil {
			return "", err
		}
	}
}
