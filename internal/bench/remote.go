package bench

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Remote implementations must support concurrent calls.
type Remote interface {
	Run(context.Context, string, string) (string, error)
	Write(context.Context, string, string, []byte) error
}

type SSH struct{ Key, User string }

func quote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

func (s SSH) command(ctx context.Context, host, command string, input io.Reader, output io.Writer) error {
	key := s.Key
	if strings.HasPrefix(key, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		key = filepath.Join(home, key[2:])
	}
	cmd := exec.CommandContext(ctx, "ssh",
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ConnectTimeout=10",
		"-o", "ServerAliveInterval=10",
		"-o", "ServerAliveCountMax=2",
		"-o", "LogLevel=ERROR",
		"-i", key,
		s.User+"@"+host, command,
	)
	configureProcess(cmd)
	cmd.Stdin = input
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = output, &stderr
	cmd.WaitDelay = 3 * time.Second
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("SSH %s: %w: %s", host, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (s SSH) Run(ctx context.Context, host, command string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	var output bytes.Buffer
	err := s.command(ctx, host, command, nil, &output)
	return output.String(), err
}

func (s SSH) Stream(ctx context.Context, host, command string, output io.Writer) error {
	return s.command(ctx, host, command, nil, output)
}

func (s SSH) Write(ctx context.Context, host, path string, data []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return s.command(ctx, host, "mkdir -p "+quote(filepath.ToSlash(filepath.Dir(path)))+" && cat > "+quote(path), bytes.NewReader(data), io.Discard)
}

func wait(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
