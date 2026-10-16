package bench

import (
	"context"
	"fmt"
	"path"

	benchmark "github.com/rtcbench/rtcbench/pkg/bench"
	"golang.org/x/sync/errgroup"
)

type provisioner struct {
	cluster Cluster
	remote  Remote
	id      string
}

func (p provisioner) start(ctx context.Context, sfu string) error {
	plugin, err := p.cluster.plugin(sfu, p.id)
	if err != nil {
		return err
	}
	for _, host := range p.cluster.Hosts() {
		command := "sudo -n sysctl -w" +
			" net.core.rmem_max=134217728 net.core.rmem_default=134217728" +
			" net.core.wmem_max=134217728 net.core.wmem_default=134217728"
		if _, err := p.remote.Run(ctx, host, command); err != nil {
			return fmt.Errorf("configure UDP buffers on %s: %w", host, err)
		}
	}
	if plugin.Start != nil {
		if err := plugin.Start(ctx, p.remote); err != nil {
			return err
		}
	}
	for _, service := range plugin.Services {
		command := "docker run -d --network host --ulimit nofile=65536:65536 --name " + quote(service.Name)
		for _, env := range service.Env {
			command += " -e " + quote(env)
		}
		if _, err := p.remote.Run(ctx, service.Host, command+" "+quote(service.Image)); err != nil {
			return err
		}
		if service.Health != "" {
			if err := benchmark.Ready(ctx, p.remote, service.Host, service.Health); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p provisioner) stop(ctx context.Context, sfu string) error {
	plugin, err := p.cluster.plugin(sfu, p.id)
	if err != nil {
		return err
	}
	var group errgroup.Group
	group.SetLimit(8)
	for _, service := range plugin.Services {
		group.Go(func() error {
			name := quote(service.Name)
			command := "docker info >/dev/null && if docker container inspect " + name +
				" >/dev/null 2>&1; then docker rm -f " + name + "; fi"
			_, err := p.remote.Run(ctx, service.Host, command)
			return err
		})
	}
	if plugin.Stop != nil {
		group.Go(func() error { return plugin.Stop(ctx, p.remote) })
	}
	return group.Wait()
}

func (e *experiment) provisioner() provisioner {
	return provisioner{
		cluster: e.cluster,
		remote:  e.remote,
		id:      path.Base(e.remoteDir),
	}
}
