package deployment

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

type operation struct {
	timeout  time.Duration
	status   string
	progress string
	run      func(*deploymentJob) error
}

var operations = map[string]operation{
	"deploy": {
		timeout:  30 * time.Minute,
		status:   "ready",
		progress: "All containers passed health checks",
		run:      (*deploymentJob).deploy,
	},
	"stop": {
		timeout:  2 * time.Minute,
		status:   "stopped",
		progress: "Containers stopped",
		run:      (*deploymentJob).stop,
	},
	"refresh": {
		timeout:  2 * time.Minute,
		status:   "ready",
		progress: "All containers passed health checks",
		run:      (*deploymentJob).refresh,
	},
	"logs": {
		timeout:  2 * time.Minute,
		progress: "Container logs updated",
		run:      (*deploymentJob).logs,
	},
}

type deploymentJob struct {
	manager *Manager
	d       deployment
	host    credential
	ctx     context.Context
	output  tail
}

// launch is called under the mutex, so shutdown cannot race WaitGroup.Add
func (m *Manager) launch(d deployment, action string) {
	job := &deploymentJob{manager: m, d: d, host: m.hostCredential(m.db.Hosts[d.HostID])}
	op := operations[action]
	m.jobs.Add(1)
	go func() {
		defer m.jobs.Done()
		ctx, cancel := context.WithTimeout(m.ctx, op.timeout)
		defer cancel()
		job.ctx = ctx
		err := op.run(job)
		m.finish(job, action, err)
	}()
}

func (j *deploymentJob) run(command string, input io.Reader) error {
	return j.manager.remote(j.ctx, j.host, command, input, &j.output)
}

func (j *deploymentJob) phase(text string) { j.manager.phase(j.d.ID, text) }

func (j *deploymentJob) deploy() error {
	j.phase("Loading Docker build context from the UI")
	if j.manager.LoadBuildContext == nil {
		return errors.New("SFU build context unavailable; configure a UI source")
	}
	files, err := j.manager.LoadBuildContext(j.ctx, j.d.Plugin)
	if err != nil {
		return err
	}
	archive, err := bundle(j.manager.plugins[j.d.Plugin], j.d, files)
	if err != nil {
		return err
	}
	if err := j.checkHost(); err != nil {
		return err
	}
	j.phase("Checking Docker and Compose")
	if err := j.run(preflight(j.host), nil); err != nil {
		return err
	}
	j.phase("Uploading Docker build contexts")
	dir := `"$HOME/.rtcbench-sfus/` + j.d.ID + `"`
	if err := j.run("umask 077; mkdir -p "+dir+" && tar -xf - -C "+dir, bytes.NewReader(archive)); err != nil {
		return err
	}
	j.phase("Building images (first deployment can take several minutes)")
	if err := j.run(compose(j.host, j.d)+"build", nil); err != nil {
		return err
	}
	j.phase("Starting containers and waiting for health checks")
	return j.run(compose(j.host, j.d)+"up -d --wait --wait-timeout 240", nil)
}

func (j *deploymentJob) checkHost() error {
	check := j.manager.plugins[j.d.Plugin].CheckHost
	if check == nil {
		return nil
	}
	j.phase("Checking SFU host requirements")
	return check(j.d.config(), func(command string) (string, error) {
		var output tail
		err := j.manager.remote(j.ctx, j.host, command, nil, &output)
		return output.String(), err
	})
}

func (j *deploymentJob) stop() error {
	// A preflight/upload failure may never have created a compose file
	command := fmt.Sprintf(`if [ -f "$HOME/.rtcbench-sfus/%s/compose.yaml" ]; then
  %sdown --remove-orphans --timeout 20
else
  ids=$(%s ps -aq --filter label=com.docker.compose.project=rtcbench-%s) || exit 1
  test -z "$ids"
fi`, j.d.ID, compose(j.host, j.d), docker(j.host), j.d.ID)
	return j.run(command, nil)
}

func (j *deploymentJob) refresh() error {
	if err := j.run(compose(j.host, j.d)+"ps --all --format json", nil); err != nil {
		return err
	}
	services, _ := j.manager.plugins[j.d.Plugin].Recipe(j.d.config())
	return healthy(j.output.String(), len(services))
}

func (j *deploymentJob) logs() error {
	return j.run(compose(j.host, j.d)+"logs --no-color --tail 150", nil)
}

func (m *Manager) finish(job *deploymentJob, action string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.db.Deployments[job.d.ID]
	d.Operation = ""
	if action != "logs" || err == nil {
		d.Logs = redact(job.output.String(), job.host, job.d)
	}
	if err != nil {
		d.failed(action, redact(err.Error(), job.host, job.d))
	} else {
		op := operations[action]
		d.Progress = op.progress
		if action != "logs" {
			now := time.Now()
			d.Status = op.status
			d.CheckedAt = &now
		}
	}
	m.db.Deployments[d.ID] = d
	if err := m.save(); err != nil {
		d.Error = "Could not persist deployment state: " + err.Error()
		m.db.Deployments[d.ID] = d
	}
}

func (d *deployment) failed(action, message string) {
	if action != "logs" || d.Error == "" {
		d.Error = action + ": " + message
	}
	d.Progress = "Operation failed; inspect logs, then check health or stop."
	if action == "logs" {
		return
	}
	d.Status = "unknown"
	if action == "deploy" {
		d.Status = "failed"
	}
}
