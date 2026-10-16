package bench

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

type startupFailure struct{ reason string }

func (e *startupFailure) Error() string { return e.reason }

func (e *experiment) waitForStartup(ctx context.Context, started func(context.Context) (bool, error)) error {
	startup, cancel := context.WithTimeout(ctx, time.Duration(e.cluster.StartupSeconds)*time.Second)
	defer cancel()
	for {
		ready, err := started(startup)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if startup.Err() != nil {
			return &startupFailure{reason: "STARTUP_TIMEOUT"}
		}
		if err != nil {
			return err
		}
		if ready {
			return nil
		}
		if err := wait(startup, 5*time.Second); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return &startupFailure{reason: "STARTUP_TIMEOUT"}
		}
	}
}

func (e *experiment) waitForBrowsers(ctx context.Context) error {
	return e.waitForStartup(ctx, func(ctx context.Context) (bool, error) {
		for _, c := range e.containers {
			if !c.browser {
				continue
			}
			ready, err := e.browserStarted(ctx, c)
			if err != nil || !ready {
				return false, err
			}
		}
		return e.streamsStarted(ctx)
	})
}

func (e *experiment) waitForNative(ctx context.Context) error {
	return e.waitForStartup(ctx, e.streamsStarted)
}

func (e *experiment) streamsStarted(ctx context.Context) (bool, error) {
	for _, host := range e.measureHosts() {
		for _, c := range e.containers {
			if c.host != host {
				continue
			}
			state, err := e.inspectContainer(ctx, c)
			if err != nil {
				return false, err
			}
			if reason := state.failureReason(); reason != "" {
				return false, &startupFailure{reason: reason}
			}
			if c.browser {
				continue
			}
			files, err := e.readRemote(ctx, host, "find "+quote(c.stats)+" -maxdepth 1 -type f -name '*.jsonl' -print -quit")
			if err != nil {
				return false, err
			}
			if strings.TrimSpace(files) == "" {
				return false, nil
			}
		}
		quality, err := e.measure(ctx, host)
		if err != nil {
			return false, err
		}
		if quality.Reason == "OOM" || quality.Reason == "CONTAINER_EXITED" {
			return false, &startupFailure{reason: quality.Reason}
		}
		if quality.Reported < quality.Expected {
			return false, nil
		}
	}
	return true, nil
}

func (e *experiment) browserStarted(ctx context.Context, c container) (bool, error) {
	state, err := e.inspectContainer(ctx, c)
	if err != nil {
		return false, err
	}
	if reason := state.failureReason(); reason != "" {
		return false, &startupFailure{reason: reason}
	}
	file := quote(c.stats + "/wrp-stats.csv")
	command := "if test -f " + file + "; then date +%s; stat -c %Y " + file +
		"; awk 'NR==1 { header=$0 } END { print header; if (NR>1) print }' " + file + "; fi"
	data, err := e.readRemote(ctx, c.host, command)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(data) == "" {
		return false, nil
	}
	now, data, _ := strings.Cut(data, "\n")
	modified, data, _ := strings.Cut(data, "\n")
	n, err1 := strconv.ParseInt(strings.TrimSpace(now), 10, 64)
	m, err2 := strconv.ParseInt(strings.TrimSpace(modified), 10, 64)
	if err1 != nil || err2 != nil {
		return false, errors.New("invalid browser startup stats clock")
	}
	if n-m > int64(max(15, e.cluster.SampleSeconds*3)) {
		return false, nil
	}
	reader := csv.NewReader(strings.NewReader(data))
	header, err := reader.Read()
	if err == io.EOF {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("browser startup header: %w", err)
	}
	row, err := reader.Read()
	if err == io.EOF {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("browser startup stats: %w", err)
	}
	for i, name := range header {
		if name != "pages_sum" {
			continue
		}
		pages, err := strconv.ParseFloat(row[i], 64)
		if err != nil || math.IsNaN(pages) || math.IsInf(pages, 0) || pages < 0 || pages != math.Trunc(pages) {
			return false, fmt.Errorf("invalid browser page count %q", row[i])
		}
		return pages >= float64(e.topology.Load), nil
	}
	return false, errors.New("browser stats missing pages_sum")
}
