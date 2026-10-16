package bench

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"

	benchmark "github.com/rtcbench/rtcbench/pkg/bench"
)

type uploader interface {
	Upload(context.Context, string, string, io.Reader, bool) error
}

func (s SSH) Upload(ctx context.Context, host, destination string, content io.Reader, executable bool) error {
	mode := "644"
	if executable {
		mode = "755"
	}
	command := "mkdir -p " + quote(path.Dir(destination)) +
		" && cat > " + quote(destination) + " && chmod " + mode + " " + quote(destination)
	return s.command(ctx, host, command, content, io.Discard)
}

func (p provisioner) upload(ctx context.Context, cells []Cell) error {
	target, ok := p.remote.(uploader)
	if !ok {
		return errors.New("remote does not support asset uploads")
	}
	c := p.cluster
	workers := append(append([]string{}, c.Senders...), c.Receivers...)
	publishers := append([]string{}, c.Senders...)
	for _, host := range []string{c.Sender, c.Viewer} {
		if host != "" {
			workers = append(workers, host)
		}
	}
	if c.Sender != "" {
		publishers = append(publishers, c.Sender)
	}
	slices.Sort(workers)
	workers = slices.Compact(workers)
	slices.Sort(publishers)
	publishers = slices.Compact(publishers)
	assets := []benchmark.Asset{
		{
			Local:      filepath.Join(c.BinariesDir, "rtcbench"),
			Remote:     "/tmp/bench-bin/rtcbench",
			Hosts:      workers,
			Executable: true,
		},
	}
	seen := map[string]bool{}
	for _, cell := range cells {
		if seen[cell.SFU] {
			continue
		}
		seen[cell.SFU] = true
		plugin, err := c.plugin(cell.SFU, p.id)
		if err != nil {
			return err
		}
		assets = append(assets, plugin.Assets...)
	}
	if err := filepath.WalkDir(c.IVFDir, func(local string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("asset must be a regular file: %s", local)
		}
		relative, err := filepath.Rel(c.IVFDir, local)
		if err != nil {
			return err
		}
		assets = append(assets, benchmark.Asset{
			Local:  local,
			Remote: path.Join(c.RemoteIVF, filepath.ToSlash(relative)),
			Hosts:  publishers,
		})
		return nil
	}); err != nil {
		return fmt.Errorf("media directory: %w", err)
	}
	for _, a := range assets {
		info, err := os.Lstat(a.Local)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("asset must be a regular file: %s", a.Local)
		}
	}
	for _, a := range assets {
		for _, host := range a.Hosts {
			if err := ctx.Err(); err != nil {
				return err
			}
			file, err := os.Open(a.Local)
			if err != nil {
				return err
			}
			err = target.Upload(ctx, host, a.Remote, file, a.Executable)
			if err = errors.Join(err, file.Close()); err != nil {
				return fmt.Errorf("upload %s to %s: %w", a.Local, host, err)
			}
		}
	}
	return nil
}
