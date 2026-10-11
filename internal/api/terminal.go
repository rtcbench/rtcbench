package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/aymanbagabas/go-pty"
	"github.com/coder/websocket"
)

type terminalSession struct {
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

type terminalRun func(context.Context, io.Reader, io.Writer, <-chan [2]int) error

func (s *Server) terminal(w http.ResponseWriter, r *http.Request) {
	if err := s.checkTerminalOrigin(r); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	query := r.URL.Query()
	kind, project := query.Get("kind"), query.Get("project")
	if (kind != "" && kind != "instances" && kind != "deployments") ||
		(kind != "" && project != "") || (kind == "" && query.Get("id") != "") {
		writeError(w, http.StatusBadRequest, errors.New("invalid terminal target"))
		return
	}
	setting := kind
	if setting == "" {
		setting = "server"
	}
	if s.settings.Terminal[setting] != "enabled" {
		writeError(w, http.StatusForbidden, fmt.Errorf("%s terminals are disabled; set terminal.%s to enabled in rtcbench.yml and restart RTCBench", setting, setting))
		return
	}
	metadata, run, err := s.terminalTarget(query)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		writeJSON(w, http.StatusOK, metadata)
		return
	}
	session, err := s.openTerminal()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err)
		return
	}
	defer s.releaseTerminal(session)
	// Host and origin were checked above, including same-origin requests through a proxy
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	session.stream(conn, run)
}

func (s *Server) checkTerminalOrigin(r *http.Request) error {
	if !s.terminalHost(r.Host) {
		return errors.New("terminal host is not allowed; add the server hostname to allowed_hosts in rtcbench.yml")
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		value, err := url.Parse(origin)
		if err != nil || (value.Scheme != "http" && value.Scheme != "https") || !s.terminalHost(value.Host) {
			return errors.New("terminal origin is not allowed; add its hostname to allowed_hosts in rtcbench.yml")
		}
	}
	// WebSocket upgrades use GET but can start a shell, so apply unsafe-method checks
	check := *r
	check.Method = http.MethodPost
	return http.NewCrossOriginProtection().Check(&check)
}

func (s *Server) terminalTarget(query url.Values) (any, terminalRun, error) {
	if kind := query.Get("kind"); kind != "" {
		target, err := s.deployments.TerminalTarget(kind, query.Get("id"), query.Get("service"))
		return target, target.Run, err
	}
	name := "Server"
	dir, err := filepath.Abs(s.dir)
	if project := query.Get("project"); project != "" {
		target, readErr := s.readPlugin(project)
		name, dir, err = target.Name, target.Path, readErr
	}
	run := func(ctx context.Context, input io.Reader, output io.Writer, sizes <-chan [2]int) error {
		return localTerminal(ctx, dir, input, output, sizes)
	}
	return map[string]string{"name": name, "directory": dir}, run, err
}

func (s *Server) openTerminal() (*terminalSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing || len(s.terminals) >= 16 {
		return nil, errors.New("no terminal sessions available")
	}
	ctx, cancel := context.WithCancel(context.Background())
	session := &terminalSession{ctx: ctx, cancel: cancel, done: make(chan struct{})}
	s.terminals[session] = true
	return session, nil
}

func (s *Server) releaseTerminal(session *terminalSession) {
	session.cancel()
	s.mu.Lock()
	delete(s.terminals, session)
	s.mu.Unlock()
	close(session.done)
}

func (s *terminalSession) stream(conn *websocket.Conn, run terminalRun) {
	conn.SetReadLimit(64 << 10)
	input, writer := io.Pipe()
	defer input.Close()
	defer writer.Close()
	sizes := make(chan [2]int, 1)
	go func() {
		defer s.cancel()
		readTerminalInput(s.ctx, conn, writer, sizes)
	}()
	go func() {
		defer s.cancel()
		pingTerminal(s.ctx, conn)
	}()
	err := run(s.ctx, input, terminalWriter{s.ctx, conn, s.cancel}, sizes)
	if err != nil && s.ctx.Err() == nil {
		_ = conn.Write(s.ctx, websocket.MessageText, []byte(err.Error()))
	}
	conn.Close(websocket.StatusNormalClosure, "shell exited")
}

func readTerminalInput(ctx context.Context, conn *websocket.Conn, writer io.Writer, sizes chan [2]int) {
	for {
		kind, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if kind == websocket.MessageBinary {
			if _, err := writer.Write(data); err != nil {
				return
			}
			continue
		}
		var size struct{ Cols, Rows int }
		if json.Unmarshal(data, &size) != nil || size.Cols < 1 || size.Cols > 1024 || size.Rows < 1 || size.Rows > 512 {
			conn.Close(websocket.StatusPolicyViolation, "invalid terminal size")
			return
		}
		select {
		case <-sizes:
		default:
		}
		sizes <- [2]int{size.Cols, size.Rows}
	}
}

func pingTerminal(ctx context.Context, conn *websocket.Conn) {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := conn.Ping(pingCtx)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

func (s *Server) terminalHost(authority string) bool {
	u, err := url.Parse("http://" + authority)
	if err != nil || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") || net.ParseIP(host) != nil {
		return true
	}
	for _, allowed := range s.settings.AllowedHosts {
		if strings.EqualFold(host, allowed) {
			return true
		}
	}
	return false
}

type terminalWriter struct {
	ctx    context.Context
	conn   *websocket.Conn
	cancel context.CancelFunc
}

func (w terminalWriter) Write(data []byte) (int, error) {
	if w.ctx.Err() == nil {
		ctx, cancel := context.WithTimeout(w.ctx, 30*time.Second)
		defer cancel()
		if w.conn.Write(ctx, websocket.MessageBinary, data) != nil {
			w.cancel()
		}
	}
	return len(data), nil
}

func localTerminal(ctx context.Context, dir string, input io.Reader, output io.Writer, sizes <-chan [2]int) error {
	terminal, err := pty.New()
	if err != nil {
		return err
	}
	closePTY := sync.OnceFunc(func() { _ = terminal.Close() })
	defer closePTY()
	shell, args := terminalShell()
	shell, err = exec.LookPath(shell)
	if err != nil {
		return err
	}
	cmd := terminal.CommandContext(ctx, shell, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")
	if err := cmd.Start(); err != nil {
		return err
	}
	done, drained := make(chan struct{}), make(chan struct{})
	defer close(done)
	go func() {
		_, _ = io.Copy(terminal, input)
	}()
	go func() {
		defer close(drained)
		_, _ = io.Copy(output, terminal)
	}()
	go func() {
		for {
			select {
			case size, ok := <-sizes:
				if !ok {
					return
				}
				_ = terminal.Resize(size[0], size[1])
			case <-done:
				return
			}
		}
	}()
	err = cmd.Wait()
	// Keep draining ConPTY while closing it, or Windows can deadlock
	closePTY()
	<-drained
	return err
}

func terminalShell() (string, []string) {
	if runtime.GOOS == "windows" {
		return "powershell.exe", []string{"-NoLogo", "-NoProfile"}
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	return shell, []string{"-i"}
}

func (s *Server) closeTerminals(ctx context.Context) {
	s.mu.Lock()
	s.closing = true
	sessions := make([]*terminalSession, 0, len(s.terminals))
	for session := range s.terminals {
		sessions = append(sessions, session)
		session.cancel()
	}
	s.mu.Unlock()
	for _, session := range sessions {
		select {
		case <-session.done:
		case <-ctx.Done():
			return
		}
	}
}
