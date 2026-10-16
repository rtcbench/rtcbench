package bench

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

func (e *experiment) native(ctx context.Context, host string, group RoomGroup, sender, worker bool, index int) error {
	dir := e.remoteDir + fmt.Sprintf("/native-%t-%d", worker, index)
	data, err := nativeConfig(e.cluster, e.cell, e.topology.SFUIP, host, group, sender, dir)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(e.dir, fmt.Sprintf("container-%d.yml", len(e.containers))), data, 0600); err != nil {
		return err
	}
	if err := e.remote.Write(ctx, host, dir+"/config.yml", data); err != nil {
		return err
	}
	if _, err := e.remote.Run(ctx, host, "mkdir -p "+quote(dir+"/stats")+" "+quote(dir+"/logs")); err != nil {
		return err
	}
	name := "rtcbench-" + filepath.Base(e.remoteDir) + fmt.Sprintf("-native-%t-%d", worker, index)
	c := container{host: host, name: name, stats: dir + "/stats", worker: worker}
	e.containers = append(e.containers, c)
	if err := e.containerManifest(c); err != nil {
		return err
	}
	cmd := "docker run -d --name " + quote(name) + " --network host --ulimit nofile=65536:65536"
	if worker && e.cluster.Cores != "" {
		cmd += " --cpuset-cpus " + quote(e.cluster.Cores)
	}
	cmd += " -v " + quote(dir+":"+dir) + " -v " + quote(e.cluster.RemoteIVF+":"+e.cluster.RemoteIVF+":ro")
	cmd += " " + quote(e.cluster.RTCBenchImage) + " " + quote(dir+"/config.yml")
	_, err = e.remote.Run(ctx, host, cmd)
	return err
}

func (e *experiment) browser(ctx context.Context, host string) error {
	dir := e.remoteDir + "/browser"
	config, err := browserConfig(e.cluster, e.topology, e.cell, e.request.Mode, dir)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(e.dir, fmt.Sprintf("container-%d-config.json", len(e.containers))), config, 0600); err != nil {
		return err
	}
	for name, data := range map[string][]byte{
		"config.json": config,
		"rooms.js":    roomHandler(e.topology.PerRoom, BrowserURL(e.cluster, e.topology, e.cell, e.request.Mode)),
	} {
		if err := e.remote.Write(ctx, host, dir+"/"+name, data); err != nil {
			return err
		}
	}
	if _, err := e.remote.Run(ctx, host, "mkdir -p "+quote(dir+"/stats")); err != nil {
		return err
	}
	name := "rtcbench-" + filepath.Base(e.remoteDir) + "-browser"
	e.containers = append(e.containers, container{host: host, name: name, stats: dir + "/stats", browser: true, worker: true})
	if err := e.containerManifest(e.containers[len(e.containers)-1]); err != nil {
		return err
	}
	cmd := "docker run -d --name " + quote(name) + " --network host --shm-size=2g --ulimit nofile=65536:65536"
	if e.cluster.Cores != "" {
		cmd += " --cpuset-cpus " + quote(e.cluster.Cores)
	}
	cmd += " -v " + quote(dir+":"+dir) + " -v " + quote(e.cluster.RemoteIVF+":"+e.cluster.RemoteIVF+":ro")
	if e.request.Mode == "sender" {
		cmd += " -e " + quote("VIDEO_PATH="+e.cluster.VideoPath)
	}
	cmd += " " + quote(e.cluster.BrowserImage) + " --run-xvfb " + quote(dir+"/config.json")
	_, err = e.remote.Run(ctx, host, cmd)
	return err
}

func (e *experiment) containerManifest(c container) error {
	return writeJSONFile(filepath.Join(e.dir, fmt.Sprintf("container-%d.json", len(e.containers)-1)), struct {
		Host    string `json:"host"`
		Name    string `json:"name"`
		Stats   string `json:"stats"`
		Worker  bool   `json:"worker"`
		Browser bool   `json:"browser"`
	}{c.host, c.name, c.stats, c.worker, c.browser})
}
