package bench

import (
	"context"
	"errors"
	"io"
	"os"
	"strconv"
	"time"

	"golang.org/x/sync/errgroup"
)

type Observation struct {
	At      time.Time `json:"at"`
	Host    string    `json:"host"`
	Quality Quality   `json:"quality"`
}

type Trial struct {
	Cell
	Load        int           `json:"load"`
	Repeat      int           `json:"repeat"`
	Repetitions int           `json:"repetitions"`
	StartedAt   time.Time     `json:"startedAt"`
	EndedAt     time.Time     `json:"endedAt"`
	Passed      bool          `json:"passed"`
	Reason      string        `json:"reason"`
	Error       string        `json:"error,omitempty"`
	Topology    Topology      `json:"topology"`
	Samples     []Observation `json:"samples"`
	Artifacts   string        `json:"artifacts"`
}

type container struct {
	host, name, stats string
	browser, worker   bool
}

type experiment struct {
	request     Request
	cluster     Cluster
	remote      Remote
	topology    Topology
	cell        Cell
	dir         string
	remoteDir   string
	containers  []container
	provisioned bool
	log         io.Writer
	progress    func(string) error
}

func (e *experiment) run(ctx context.Context, repeat int) (trial Trial, err error) {
	trial = Trial{
		Cell:      e.cell,
		Load:      e.topology.Load,
		Repeat:    repeat,
		StartedAt: time.Now().UTC(),
		Topology:  e.topology,
		Samples:   []Observation{},
		Reason:    "OK",
	}
	defer func() {
		err = errors.Join(err, e.finish())
		trial.EndedAt = time.Now().UTC()
		if err != nil {
			trial.Passed, trial.Reason, trial.Error = false, "EXPERIMENT_ERROR", err.Error()
		}
	}()
	if err = e.prepare(ctx); err != nil {
		var failure *startupFailure
		if errors.As(err, &failure) {
			trial.Reason = failure.reason
			err = e.capture(ctx, "after")
		}
		return
	}
	err = e.observe(ctx, &trial)
	return
}

func (e *experiment) prepare(ctx context.Context) (err error) {
	if err = os.MkdirAll(e.dir, 0o755); err != nil {
		return
	}
	if err = e.progress("Starting " + e.cell.Key() + " at " + strconv.Itoa(e.topology.Load) + " per machine"); err != nil {
		return
	}
	if e.request.Provision {
		e.provisioned = true
		if err = e.provisioner().start(ctx, e.cell.SFU); err != nil {
			return
		}
	}
	plugin, err := e.cluster.plugin(e.cell.SFU, "")
	if err != nil {
		return err
	}
	if plugin.PrepareRooms != nil {
		if err = plugin.PrepareRooms(ctx, e.remote, e.topology.Rooms); err != nil {
			return
		}
	}
	observerSender := e.request.Mode == "receiver"
	startObserver := func() error {
		return e.native(ctx, e.topology.Observer, RoomGroup{Rooms: e.topology.Rooms, Users: 1}, observerSender, false, 0)
	}
	startWorkers := func() error {
		for _, host := range e.topology.Workers {
			if e.cell.Client == "rtcbench" {
				for groupIndex, group := range e.topology.Groups {
					if err := e.native(ctx, host, group, !observerSender, true, groupIndex); err != nil {
						return err
					}
				}
			} else if err := e.browser(ctx, host); err != nil {
				return err
			}
		}
		return nil
	}
	startPublishers, startReceivers := startObserver, startWorkers
	if !observerSender {
		startPublishers, startReceivers = startWorkers, startObserver
	}
	if err = startPublishers(); err != nil {
		return
	}
	if err = wait(ctx, time.Duration(e.cluster.StartLeadSeconds)*time.Second); err != nil {
		return
	}
	if err = startReceivers(); err != nil {
		return
	}
	if err = e.capture(ctx, "before"); err != nil {
		return
	}
	if e.cell.Client == "rtcbench" {
		if err = e.progress("Waiting for native streams " + e.cell.Key()); err != nil {
			return
		}
		if err = e.waitForNative(ctx); err != nil {
			return
		}
	} else {
		if err = e.progress("Waiting for browser sessions " + e.cell.Key()); err != nil {
			return
		}
		if err = e.waitForBrowsers(ctx); err != nil {
			return
		}
	}
	if err = e.progress("Warming up " + e.cell.Key()); err != nil {
		return
	}
	if err = wait(ctx, time.Duration(e.cluster.WarmupSeconds)*time.Second); err != nil {
		return
	}
	if err = e.progress("Measuring " + e.cell.Key()); err != nil {
		return
	}
	return nil
}

func (e *experiment) observe(ctx context.Context, trial *Trial) (err error) {
	trial.Passed = true
	end := time.Now().Add(time.Duration(e.cluster.DurationSeconds) * time.Second)
	for {
		if ctx.Err() != nil {
			err = ctx.Err()
			return
		}
		for _, host := range e.measureHosts() {
			var q Quality
			q, err = e.measure(ctx, host)
			if err != nil {
				return
			}
			trial.Samples = append(trial.Samples, Observation{At: time.Now().UTC(), Host: host, Quality: q})
			if !q.Passed {
				trial.Passed = false
				if trial.Reason == "OK" {
					trial.Reason = q.Reason
				}
			}
		}
		remaining := time.Until(end)
		if remaining <= 0 {
			break
		}
		if err = wait(ctx, min(remaining, time.Duration(e.cluster.SampleSeconds)*time.Second)); err != nil {
			return
		}
	}
	if err = e.capture(ctx, "after"); err != nil {
		return
	}
	return
}

func (e *experiment) finish() error {
	haltCtx, haltCancel := context.WithTimeout(context.Background(), 20*time.Second)
	haltErr := e.containersCommand(haltCtx, "docker stop --time 2 ")
	haltCancel()
	collectCtx, collectCancel := context.WithTimeout(context.Background(), time.Minute)
	collectErr := e.collect(collectCtx)
	collectCancel()
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cleanupErr := e.containersCommand(cleanupCtx, "docker rm -f ")
	if e.provisioned {
		cleanupErr = errors.Join(cleanupErr, e.provisioner().stop(cleanupCtx, e.cell.SFU))
	}
	if haltErr == nil && collectErr == nil && cleanupErr == nil {
		cleanupErr = e.removeArtifacts(cleanupCtx)
	}
	return errors.Join(haltErr, collectErr, cleanupErr)
}

func (e *experiment) containersCommand(ctx context.Context, command string) error {
	var group errgroup.Group
	group.SetLimit(8)
	for _, c := range e.containers {
		group.Go(func() error { _, err := e.remote.Run(ctx, c.host, command+quote(c.name)); return err })
	}
	return group.Wait()
}
