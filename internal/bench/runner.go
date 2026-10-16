package bench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type Report struct {
	Simulated bool                    `json:"simulated"`
	Phase     string                  `json:"phase"`
	Results   map[string]SearchResult `json:"results"`
	Trials    []Trial                 `json:"trials"`
}

type TrialSpec struct {
	Cell
	Topology        Topology
	Repeat          int
	Directory       string
	RemoteDirectory string
}

type TrialExecutor func(context.Context, TrialSpec) (Trial, error)

type Options struct {
	Directory string
	ID        string
	Log       io.Writer
	Remote    Remote
	Execute   TrialExecutor
	Update    func(Report) error
}

type coordinator struct {
	request Request
	cluster Cluster
	options Options
	report  Report
}

func Run(ctx context.Context, request Request, options Options) (Report, error) {
	cluster, err := request.Validate()
	if err != nil {
		return Report{}, err
	}
	if options.Log == nil {
		options.Log = io.Discard
	}
	if options.Remote == nil {
		options.Remote = SSH{Key: cluster.SSHKey, User: cluster.SSHUser}
	}
	if options.ID == "" {
		options.ID = strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	if !hostName.MatchString(options.ID) {
		return Report{}, fmt.Errorf("invalid experiment ID")
	}
	if err := os.MkdirAll(options.Directory, 0o700); err != nil {
		return Report{}, err
	}
	if err := createManifest(filepath.Join(options.Directory, "request.json"), request); err != nil {
		return Report{}, fmt.Errorf("create run manifest (use an unused result directory): %w", err)
	}
	if err := writeJSONFile(filepath.Join(options.Directory, "cluster.json"), cluster); err != nil {
		return Report{}, err
	}
	c := coordinator{
		request: request,
		cluster: cluster,
		options: options,
		report:  Report{Simulated: request.DryRun, Results: map[string]SearchResult{}, Trials: []Trial{}},
	}
	if c.options.Execute == nil {
		c.options.Execute = c.execute
	}
	err = c.run(ctx)
	c.report.Phase = "Completed"
	if err != nil {
		c.report.Phase = "Failed"
	}
	if ctx.Err() != nil {
		c.report.Phase = "Stopped"
	}
	encodeErr := writeJSONFile(filepath.Join(options.Directory, "summary.json"), c.report)
	return c.report, errors.Join(err, encodeErr, c.update())
}

func (c *coordinator) run(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := c.phase("Preparing"); err != nil {
		return err
	}
	if c.request.UploadAssets && !c.request.DryRun {
		p := provisioner{cluster: c.cluster, remote: c.options.Remote, id: c.options.ID}
		if err := p.upload(ctx, c.request.Cells); err != nil {
			return err
		}
	}
	for _, cell := range c.request.Cells {
		bounds := c.request.Bounds
		if hint, ok := c.request.Hints[cell.Key()]; ok {
			bounds.Hint = hint
		}
		limitReason := "search bound"
		if upper, ok := c.cluster.InitialHi[cell.Client]; ok {
			if upper < bounds.Max {
				bounds.Max, limitReason = upper, "initial_hi"
			}
		}
		if c.cluster.NICMbps > 0 {
			topology, err := Layout(c.cluster, c.request.Mode, cell.SFU, bounds.Min)
			if err != nil {
				return err
			}
			upper := c.cluster.NICMbps / (c.cluster.VideoMbps * float64(len(topology.Workers)))
			if upper < float64(bounds.Min) {
				return fmt.Errorf("configured NIC ceiling is below the minimum load")
			}
			if upper < float64(bounds.Max) {
				bounds.Max, limitReason = int(upper), "configured NIC ceiling"
			}
		}
		result, err := Search(ctx, bounds, func(ctx context.Context, load int) (bool, error) {
			return c.runLoad(ctx, cell, load)
		})
		result.LimitReason = limitReason
		c.report.Results[cell.Key()] = result
		if err := errors.Join(err, c.update()); err != nil {
			return err
		}
	}
	return nil
}

func (c *coordinator) runLoad(ctx context.Context, cell Cell, load int) (bool, error) {
	topology, err := Layout(c.cluster, c.request.Mode, cell.SFU, load)
	if err != nil {
		return false, err
	}
	passed := true
	for repeat := 1; repeat <= c.request.Repetitions; repeat++ {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		index := len(c.report.Trials) + 1
		artifact := fmt.Sprintf("sfu=%s/client=%s/trial-%03d", cell.SFU, cell.Client, index)
		spec := TrialSpec{
			Cell:            cell,
			Topology:        topology,
			Repeat:          repeat,
			Directory:       filepath.Join(c.options.Directory, filepath.FromSlash(artifact)),
			RemoteDirectory: fmt.Sprintf("/dev/shm/rtcbench-bench/%s-%03d", c.options.ID, index),
		}
		started := time.Now().UTC()
		var trial Trial
		if c.request.DryRun {
			trial = Trial{Passed: load <= 20, Reason: "SIMULATED", Samples: []Observation{}}
		} else {
			trial, err = c.options.Execute(ctx, spec)
		}
		trial.Cell, trial.Load, trial.Repeat = cell, load, repeat
		trial.Repetitions = c.request.Repetitions
		trial.Topology, trial.Artifacts = topology, artifact
		if trial.StartedAt.IsZero() {
			trial.StartedAt = started
		}
		if trial.EndedAt.IsZero() {
			trial.EndedAt = time.Now().UTC()
		}
		if err != nil {
			trial.Passed, trial.Reason, trial.Error = false, "EXPERIMENT_ERROR", err.Error()
		}
		c.report.Trials = append(c.report.Trials, trial)
		passed = passed && trial.Passed
		if err := errors.Join(err, AppendTrial(c.options.Directory, trial), c.update()); err != nil {
			return false, err
		}
		if !c.request.DryRun {
			if err := wait(ctx, time.Duration(c.cluster.TeardownSeconds)*time.Second); err != nil {
				return false, err
			}
		}
	}
	return passed, nil
}

func (c *coordinator) execute(ctx context.Context, spec TrialSpec) (Trial, error) {
	experiment := experiment{
		request:   c.request,
		cluster:   c.cluster,
		remote:    c.options.Remote,
		topology:  spec.Topology,
		cell:      spec.Cell,
		dir:       spec.Directory,
		remoteDir: spec.RemoteDirectory,
		log:       c.options.Log,
		progress:  c.phase,
	}
	return experiment.run(ctx, spec.Repeat)
}

func (c *coordinator) update() error {
	if c.options.Update == nil {
		return nil
	}
	return c.options.Update(c.report)
}

func (c *coordinator) phase(value string) error {
	c.report.Phase = value
	if _, err := fmt.Fprintln(c.options.Log, value); err != nil {
		return err
	}
	return c.update()
}

func AppendTrial(dir string, trial Trial) error {
	f, err := os.OpenFile(filepath.Join(dir, "experiments.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	entry := struct {
		Trial
		Timestamp float64 `json:"timestamp"`
		R         int     `json:"r"`
		Result    bool    `json:"result"`
	}{
		Trial:     trial,
		Timestamp: float64(trial.StartedAt.UnixMilli()) / 1000,
		R:         trial.Load,
		Result:    trial.Passed,
	}
	return errors.Join(json.NewEncoder(f).Encode(entry), f.Close())
}
