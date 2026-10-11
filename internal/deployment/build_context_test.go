package deployment

import (
	"archive/tar"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestDeploymentUsesUIBuildContext(t *testing.T) {
	m := manager(t)
	h := savedHost(t, m)
	m.LoadBuildContext = func(_ context.Context, plugin string) (map[string]string, error) {
		failIf(t, plugin != "livekit", "incorrect context requested")
		return map[string]string{"livekit/Dockerfile": "FROM custom-image"}, nil
	}
	uploaded := make(chan string, 1)
	m.remote = func(_ context.Context, _ credential, cmd string, in io.Reader, out io.Writer) error {
		if in != nil {
			archive := tar.NewReader(in)
			for {
				header, err := archive.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					return err
				}
				if header.Name == "livekit/Dockerfile" {
					data, err := io.ReadAll(archive)
					if err != nil {
						return err
					}
					uploaded <- string(data)
				}
			}
		}
		if strings.Contains(cmd, "ps --all") {
			_, _ = io.WriteString(out, `{"Service":"livekit","State":"running","Health":"healthy"}`)
		}
		return nil
	}
	d, err := m.start(Start{HostID: h.ID, Plugin: "livekit", Address: "10.0.0.2"})
	must(t, err)
	ready := wait(t, m, d.ID)
	failIf(t, ready.Status != "ready", ready)
	select {
	case source := <-uploaded:
		failIf(t, source != "FROM custom-image", source)
	default:
		t.Fatal("UI Dockerfile was not uploaded")
	}
}

func TestBuildContextFailureDoesNotConnect(t *testing.T) {
	for _, files := range []map[string]string{
		nil,
		{},
		{"livekit/Dockerfile": "FROM custom-image", "../escape": "bad"},
		{"livekit/Dockerfile": "FROM custom-image", "livekit/../escape": "bad"},
		{"livekit/Dockerfile": "FROM custom-image", "janus/Dockerfile": "bad"},
		{"livekit/Dockerfile": "FROM custom-image", `livekit/back\slash`: "bad"},
		{"livekit/Dockerfile": "FROM custom-image", "livekit/stream:ads": "bad"},
	} {
		m := manager(t)
		h := savedHost(t, m)
		m.LoadBuildContext = func(context.Context, string) (map[string]string, error) {
			if files == nil {
				return nil, errors.New("UI unavailable")
			}
			return files, nil
		}
		m.remote = func(context.Context, credential, string, io.Reader, io.Writer) error {
			t.Error("connected to SSH before validating build context")
			return nil
		}
		d, err := m.start(Start{HostID: h.ID, Plugin: "livekit", Address: "10.0.0.2"})
		must(t, err)
		failed := wait(t, m, d.ID)
		failIf(t, failed.Status != "failed" || failed.Error == "", failed)
	}
}
