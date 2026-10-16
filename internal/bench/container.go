package bench

import (
	"context"
	"encoding/json"
	"fmt"
)

type containerState struct{ Running, OOMKilled bool }

func (s containerState) failureReason() string {
	if s.OOMKilled {
		return "OOM"
	}
	if !s.Running {
		return "CONTAINER_EXITED"
	}
	return ""
}

func (e *experiment) inspectContainer(ctx context.Context, c container) (containerState, error) {
	var state containerState
	data, err := e.readRemote(ctx, c.host, "docker inspect --format '{{json .State}}' "+quote(c.name))
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal([]byte(data), &state); err != nil {
		return state, fmt.Errorf("container state: %w", err)
	}
	return state, nil
}
