package deployment

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	deploy "github.com/rtcbench/rtcbench/pkg/deployment"
)

type object = map[string]any

func bundle(plugin deploy.Plugin, d deployment, contextFiles map[string]string) ([]byte, error) {
	if contextFiles[d.Plugin+"/Dockerfile"] == "" {
		return nil, errors.New("UI build context is missing the SFU Dockerfile")
	}
	services, files := plugin.Recipe(d.config())
	if files == nil {
		files = map[string][]byte{}
	}
	data, err := json.Marshal(object{"services": services}) // JSON is also valid YAML
	if err != nil {
		return nil, err
	}
	files["compose.yaml"] = data
	for name, content := range contextFiles {
		if !fs.ValidPath(name) || !strings.HasPrefix(name, d.Plugin+"/") || strings.ContainsAny(name, `\:`) {
			return nil, errors.New("UI build context contains a path outside the SFU directory")
		}
		files[name] = []byte(content)
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(content))}); err != nil {
			return nil, err
		}
		if _, err := tw.Write(content); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func docker(h credential) string {
	if h.Sudo {
		return "sudo -n docker"
	}
	return "docker"
}

func compose(h credential, d deployment) string {
	// Deployment IDs are generated UUIDs, so they are safe to use in shell commands
	return "cd \"$HOME/.rtcbench-sfus/" + d.ID + "\" && " + docker(h) + " compose -p rtcbench-" + d.ID + " -f compose.yaml "
}

func healthy(data string, expected int) error {
	type state struct{ Service, State, Health string }
	var rows []state
	if strings.HasPrefix(strings.TrimSpace(data), "[") {
		if err := json.Unmarshal([]byte(data), &rows); err != nil {
			return err
		}
	} else {
		dec := json.NewDecoder(strings.NewReader(data))
		for dec.More() {
			var row state
			if err := dec.Decode(&row); err != nil {
				return err
			}
			rows = append(rows, row)
		}
	}
	if len(rows) != expected {
		return fmt.Errorf("expected %d containers, found %d", expected, len(rows))
	}
	for _, row := range rows {
		if row.State != "running" || row.Health != "healthy" {
			return fmt.Errorf("%s: %s (%s)", row.Service, row.State, row.Health)
		}
	}
	return nil
}
