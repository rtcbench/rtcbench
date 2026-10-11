package ivf

import (
	"context"
	"path/filepath"
	"testing"
)

func TestCanceledLoopsCloseSource(t *testing.T) {
	for _, svc := range []bool{false, true} {
		t.Run(map[bool]string{false: "sample", true: "svc"}[svc], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "camera.ivf")
			writeTestIVF(t, path, [][]byte{{1}})
			src := NewDiskSource([]string{path})
			defer src.Close()
			if _, _, err := src.NextFrame(); err != nil {
				t.Fatal(err)
			}
			file := src.(*diskSource).file
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if svc {
				SVCLoopIntoTrackUntil(ctx, nil, nil, src, SVCConfig{}, nil)
			} else {
				LoopIntoTrackUntil(ctx, nil, nil, src)
			}
			if _, err := file.Stat(); err == nil {
				t.Fatal("source file is still open after loop exit")
			}
			if err := src.Close(); err != nil {
				t.Fatalf("closing again: %v", err)
			}
		})
	}
}
