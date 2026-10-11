package deployment

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestTerminalTargets(t *testing.T) {
	m := manager(t)
	m.db.Hosts["host"] = credential{Host: Host{ID: "host", Name: "Instance", Sudo: true}}
	m.db.Deployments["deployment"] = deployment{Deployment: Deployment{
		ID: "deployment", Name: "Meeting", HostID: "host", Plugin: "jitsi",
	}}
	m.db.Deployments["external"] = deployment{}
	instance, err := m.TerminalTarget("instances", "host", "")
	must(t, err)
	failIf(t, instance.command != "", "instance did not select SSH shell")
	if instance.Name != "Instance" {
		t.Errorf("instance name = %q, want Instance", instance.Name)
	}
	container, err := m.TerminalTarget("deployments", "deployment", "")
	must(t, err)
	failIf(t, container.Service != "jvb", container.Service)
	failIf(t, len(container.Services) != 4, container.Services)
	want := "sudo -n docker compose -p rtcbench-deployment -f compose.yaml exec -e TERM=xterm-256color jvb sh"
	failIf(t, !strings.HasSuffix(container.command, want), container.command)
	for _, name := range []string{"invalid", "jvb; echo injected"} {
		t.Run(name, func(t *testing.T) {
			_, err := m.TerminalTarget("deployments", "deployment", name)
			failIf(t, err == nil, "invalid container accepted")
		})
	}
	_, err = m.TerminalTarget("deployments", "external", "")
	failIf(t, err == nil, "external deployment used controller shell")
}

func TestTerminalSSH(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	requestsSeen := make(chan string, 8)
	server := newTestSSHServer(t, func(channels <-chan ssh.NewChannel) {
		ch, reqs := acceptTestSSHSession(t, channels)
		if ch == nil {
			return
		}
		defer ch.Close()
		for req := range reqs {
			requestsSeen <- req.Type
			_ = req.Reply(true, nil)
			if req.Type == "window-change" {
				var size struct{ Cols, Rows, Width, Height uint32 }
				if err := ssh.Unmarshal(req.Payload, &size); err != nil {
					t.Error(err)
					return
				}
				if size.Cols != 100 || size.Rows != 30 {
					t.Errorf("terminal size = %dx%d, want 100x30", size.Cols, size.Rows)
					return
				}
				_, _ = io.WriteString(ch, "resized")
				_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
				return
			}
		}
	})
	target := TerminalTarget{host: server.host}
	sizes := make(chan [2]int, 1)
	sizes <- [2]int{100, 30}
	var out strings.Builder
	must(t, target.Run(ctx, strings.NewReader(""), &out, sizes))
	failIf(t, out.String() != "resized", out.String())
	for _, want := range []string{"pty-req", "shell", "window-change"} {
		select {
		case got := <-requestsSeen:
			if got != want {
				t.Errorf("SSH request = %q, want %q", got, want)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}
