package bench

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func (e *experiment) capture(ctx context.Context, stage string) error {
	hosts := append([]string{e.topology.Observer}, e.topology.Workers...)
	hosts = append(hosts, e.cluster.SFU...)
	plugin, err := e.cluster.plugin(e.cell.SFU, "")
	if err != nil {
		return err
	}
	hosts = append(hosts, plugin.Hosts...)
	slices.Sort(hosts)
	for i, host := range slices.Compact(hosts) {
		data, err := e.readRemote(ctx, host, "uname -a; cat /proc/cpuinfo /proc/net/dev /proc/net/snmp /proc/stat")
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(e.dir, fmt.Sprintf("host-%d-%s.txt", i, stage)), []byte(host+"\n"+data), 0o600); err != nil {
			return err
		}
	}
	for i, c := range e.containers {
		command := "docker inspect --format '{{json .State}} {{json .HostConfig.CpusetCpus}} {{json .Image}}' " + quote(c.name)
		data, err := e.readRemote(ctx, c.host, command)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(e.dir, fmt.Sprintf("container-%d-%s.txt", i, stage)), []byte(data), 0o600); err != nil {
			return err
		}
	}
	return nil
}

func (e *experiment) collect(ctx context.Context) error {
	var errs []error
	if e.provisioned {
		plugin, err := e.cluster.plugin(e.cell.SFU, e.provisioner().id)
		if err != nil {
			return err
		}
		for i, service := range plugin.Services {
			name := quote(service.Name)
			command := "docker info >/dev/null && if docker container inspect " + name + " >/dev/null 2>&1; then docker logs " + name + " 2>&1; fi"
			errs = append(errs, e.saveRemote(ctx, service.Host, command, fmt.Sprintf("sfu-%d.log", i)))
		}
		for _, artifact := range plugin.Artifacts {
			file := quote(artifact.Path)
			errs = append(errs, e.saveRemote(ctx, artifact.Host,
				"if test -f "+file+"; then cat "+file+"; fi", artifact.Name))
		}
	}
	for i, c := range e.containers {
		pattern, ext := "*.jsonl", "jsonl"
		if c.browser {
			pattern, ext = "wrp-stats.csv", "csv"
		}
		command := "find " + quote(c.stats) + " -maxdepth 1 -type f -name " + quote(pattern) + " -exec cat {} +"
		errs = append(errs, e.saveRemote(ctx, c.host, command, fmt.Sprintf("container-%d.%s", i, ext)))
		errs = append(errs, e.saveRemote(ctx, c.host, "docker logs "+quote(c.name)+" 2>&1", fmt.Sprintf("container-%d.log", i)))
		if c.browser {
			command := "find " + quote(c.stats) + " -maxdepth 1 -type f -name 'wrp-tracks.csv' -exec cat {} +"
			filename := fmt.Sprintf("container-%d-tracks.csv", i)
			errs = append(errs, e.saveRemote(ctx, c.host, command, filename))
		}
		if !c.browser {
			directory := quote(strings.TrimSuffix(c.stats, "/stats") + "/logs")
			command := "find " + directory + " -type f -exec tail -n 1000 {} +"
			filename := fmt.Sprintf("container-%d-events.log", i)
			errs = append(errs, e.saveRemote(ctx, c.host, command, filename))
		}
	}
	return errors.Join(errs...)
}

func (e *experiment) saveRemote(ctx context.Context, host, command, filename string) error {
	f, err := os.OpenFile(filepath.Join(e.dir, filename), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if remote, ok := e.remote.(interface {
		Stream(context.Context, string, string, io.Writer) error
	}); ok {
		err = remote.Stream(ctx, host, command, f)
	} else {
		var data string
		data, err = e.remote.Run(ctx, host, command)
		if err == nil {
			_, err = io.WriteString(f, data)
		}
	}
	return errors.Join(err, f.Close())
}

func (e *experiment) removeArtifacts(ctx context.Context) error {
	const prefix = "/dev/shm/rtcbench-bench/"
	if filepath.ToSlash(filepath.Clean(e.remoteDir)) != e.remoteDir ||
		!strings.HasPrefix(e.remoteDir, prefix) ||
		!hostName.MatchString(strings.TrimPrefix(e.remoteDir, prefix)) {
		return fmt.Errorf("invalid trial directory %q", e.remoteDir)
	}
	hosts := map[string]bool{}
	var errs []error
	for _, c := range e.containers {
		hosts[c.host] = true
	}
	if e.provisioned {
		plugin, err := e.cluster.plugin(e.cell.SFU, e.provisioner().id)
		if err != nil {
			return err
		}
		for _, artifact := range plugin.Artifacts {
			hosts[artifact.Host] = true
		}
	}
	for host := range hosts {
		_, err := e.remote.Run(ctx, host, "rm -rf -- "+quote(e.remoteDir))
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}
