package deployment

import (
	"context"
	"errors"
	"io"
	"slices"

	"golang.org/x/crypto/ssh"
)

type TerminalTarget struct {
	Name     string   `json:"name"`
	Services []string `json:"services,omitempty"`
	Service  string   `json:"service,omitempty"`
	host     credential
	command  string
}

func (m *Manager) TerminalTarget(kind, id, service string) (TerminalTarget, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch kind {
	case "instances":
		return m.instanceTerminal(id)
	case "deployments":
		return m.deploymentTerminal(id, service)
	default:
		return TerminalTarget{}, errors.New("invalid terminal target")
	}
}

func (m *Manager) instanceTerminal(id string) (TerminalTarget, error) {
	host, ok := m.db.Hosts[id]
	if !ok {
		return TerminalTarget{}, errors.New("SSH instance not found")
	}
	return TerminalTarget{Name: host.Name, host: m.hostCredential(host)}, nil
}

func (m *Manager) deploymentTerminal(id, service string) (TerminalTarget, error) {
	d, ok := m.db.Deployments[id]
	if !ok || d.HostID == "" {
		return TerminalTarget{}, errors.New("deployment has no managed SSH instance")
	}
	target, err := m.instanceTerminal(d.HostID)
	if err != nil {
		return target, err
	}
	target.Name = d.Name
	plugin := m.plugins[d.Plugin]
	services, _ := plugin.Recipe(d.config())
	for name := range services {
		target.Services = append(target.Services, name)
	}
	slices.Sort(target.Services)
	if service == "" {
		service = plugin.TerminalService
		if service == "" {
			service = plugin.ID
		}
	}
	if !slices.Contains(target.Services, service) {
		return target, errors.New("container service not found")
	}
	target.Service = service
	target.command = compose(target.host, d) + "exec -e TERM=xterm-256color " + service + " sh"
	return target, nil
}

func (t TerminalTarget) Run(ctx context.Context, input io.Reader, output io.Writer, sizes <-chan [2]int) error {
	return withSSH(ctx, t.host, func(session *ssh.Session) error {
		session.Stdin, session.Stdout, session.Stderr = input, output, output
		if err := startSSHSession(session, t.command); err != nil {
			return err
		}
		done := make(chan struct{})
		defer close(done)
		go resizeSSHSession(session, sizes, done)
		return session.Wait()
	})
}

func startSSHSession(session *ssh.Session, command string) error {
	if err := session.RequestPty("xterm-256color", 24, 80, ssh.TerminalModes{ssh.ECHO: 1}); err != nil {
		return err
	}
	if command == "" {
		return session.Shell()
	}
	return session.Start(command)
}

func resizeSSHSession(session *ssh.Session, sizes <-chan [2]int, done <-chan struct{}) {
	for {
		select {
		case size, ok := <-sizes:
			if !ok {
				return
			}
			_ = session.WindowChange(size[1], size[0])
		case <-done:
			return
		}
	}
}
