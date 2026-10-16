package bench

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

func (e *experiment) measureHosts() []string {
	if e.request.Mode == "sender" {
		return []string{e.topology.Observer}
	}
	return e.topology.Workers
}

func (e *experiment) measure(ctx context.Context, host string) (Quality, error) {
	expected := e.topology.Load
	if e.request.Mode == "sender" {
		expected = e.topology.ExpectedStreams
	}
	var paths []string
	for _, c := range e.containers {
		if c.host != host {
			continue
		}
		state, err := e.inspectContainer(ctx, c)
		if err != nil {
			return Quality{}, err
		}
		if reason := state.failureReason(); reason != "" {
			return Quality{Expected: expected, Reason: reason}, nil
		}
		if c.browser {
			return e.browserQuality(ctx, c, expected)
		}
		paths = append(paths, quote(c.stats)+"/*.jsonl")
	}
	for lines := max(expected*3, 100); ; lines *= 2 {
		data, err := e.readRemote(ctx, host, fmt.Sprintf("date +%%s; tail -v -n %d %s", lines, strings.Join(paths, " ")))
		if err != nil {
			return Quality{}, err
		}
		clock, tail, ok := strings.Cut(data, "\n")
		now, err := strconv.ParseFloat(strings.TrimSpace(clock), 64)
		if !ok || err != nil {
			return Quality{}, fmt.Errorf("invalid remote stats clock")
		}
		since := now - float64(max(15, e.cluster.SampleSeconds*3))
		stats, complete, err := nativeTail(tail, lines, since)
		if err != nil {
			return Quality{}, err
		}
		quality, err := NativeQuality(strings.NewReader(stats), expected, e.cluster, since)
		if err != nil || quality.Reported >= expected || complete {
			return quality, err
		}
	}
}
