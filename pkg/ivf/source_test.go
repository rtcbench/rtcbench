package ivf

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadCameraPaths_SortOrder(t *testing.T) {
	dir := t.TempDir()

	// Create files in non-alphabetical order.
	for _, name := range []string{"c.ivf", "a.ivf", "b.ivf"} {
		writeTestIVF(t, filepath.Join(dir, name), [][]byte{{0xAA}})
	}

	paths, err := LoadCameraPaths(dir)
	if err != nil {
		t.Fatalf("LoadCameraPaths: %v", err)
	}
	if len(paths) != 3 {
		t.Fatalf("expected 3 paths, got %d", len(paths))
	}

	want := []string{"a.ivf", "b.ivf", "c.ivf"}
	for i, w := range want {
		got := filepath.Base(paths[i])
		if got != w {
			t.Fatalf("paths[%d]: got %q, want %q", i, got, w)
		}
	}
}

func TestLoadCameraPaths_SkipsDirectories(t *testing.T) {
	dir := t.TempDir()

	writeTestIVF(t, filepath.Join(dir, "camera.ivf"), [][]byte{{0x01}})

	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	paths, err := LoadCameraPaths(dir)
	if err != nil {
		t.Fatalf("LoadCameraPaths: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("expected 1 path, got %d: %v", len(paths), paths)
	}
	if filepath.Base(paths[0]) != "camera.ivf" {
		t.Fatalf("expected camera.ivf, got %q", filepath.Base(paths[0]))
	}
}

func TestLoadCameraPaths_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	paths, err := LoadCameraPaths(dir)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(paths) != 0 {
		t.Fatalf("expected empty slice, got %v", paths)
	}
}

func TestLoadCameraPaths_NonexistentDir(t *testing.T) {
	_, err := LoadCameraPaths("/nonexistent/path/12345")
	if err == nil {
		t.Fatal("expected error for nonexistent directory")
	}
}

func TestLoadCameras_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	_, err := LoadCameras(dir)
	if err == nil {
		t.Fatal("expected error for empty directory")
	}
}

func TestDiskSource_MultiFileWrapAround(t *testing.T) {
	dir := t.TempDir()

	// file1: 2 frames, file2: 1 frame → 3 frames per cycle.
	file1 := filepath.Join(dir, "a.ivf")
	file2 := filepath.Join(dir, "b.ivf")
	writeTestIVF(t, file1, [][]byte{{0x01, 0x01}, {0x02, 0x02}})
	writeTestIVF(t, file2, [][]byte{{0x03, 0x03}})

	src := NewDiskSource([]string{file1, file2})

	want := [][]byte{
		{0x01, 0x01}, {0x02, 0x02}, {0x03, 0x03}, // cycle 1
		{0x01, 0x01}, {0x02, 0x02}, {0x03, 0x03}, // cycle 2
	}

	for i, w := range want {
		data, _, err := src.NextFrame()
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if !bytes.Equal(data, w) {
			t.Fatalf("frame %d: got %v, want %v", i, data, w)
		}
	}
}

func TestMemSource_MultiFileWrapAround(t *testing.T) {
	dir := t.TempDir()

	// file1: 2 frames, file2: 1 frame → 3 frames per cycle.
	writeTestIVF(t, filepath.Join(dir, "a.ivf"), [][]byte{{0x01, 0x01}, {0x02, 0x02}})
	writeTestIVF(t, filepath.Join(dir, "b.ivf"), [][]byte{{0x03, 0x03}})

	cams, err := LoadCameras(dir)
	if err != nil {
		t.Fatalf("LoadCameras: %v", err)
	}

	src := NewMemSource(cams)

	want := [][]byte{
		{0x01, 0x01}, {0x02, 0x02}, {0x03, 0x03}, // cycle 1
		{0x01, 0x01}, {0x02, 0x02}, {0x03, 0x03}, // cycle 2
	}

	for i, w := range want {
		data, _, err := src.NextFrame()
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if !bytes.Equal(data, w) {
			t.Fatalf("frame %d: got %v, want %v", i, data, w)
		}
	}
}

func TestDiskSource_FrameDuration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cam.ivf")

	// writeTestIVF writes timebase bytes as: offset 16-19 = 1, offset 20-23 = 30.
	// Pion's ivfreader reads offset 16-19 as TimebaseDenominator and 20-23 as
	// TimebaseNumerator, so the effective timebase is num=30, den=1.
	// Duration per tick = 1 tick * 30/1 seconds = 30s.
	// Timestamps increment by 1 per frame, so inter-frame duration = 30s.
	writeTestIVF(t, path, [][]byte{{0xAA}, {0xBB}, {0xCC}})

	src := NewDiskSource([]string{path})

	// First frame: no prior timestamp, defaults to 33ms.
	_, dur0, err := src.NextFrame()
	if err != nil {
		t.Fatalf("frame 0: %v", err)
	}
	if dur0 != 33*time.Millisecond {
		t.Fatalf("frame 0: expected 33ms default, got %v", dur0)
	}

	// Second frame: computed from timestamps (1-0) * 30 / 1 = 30s.
	_, dur1, err := src.NextFrame()
	if err != nil {
		t.Fatalf("frame 1: %v", err)
	}
	expectedDur := 30 * time.Second
	if dur1 != expectedDur {
		t.Fatalf("frame 1: got duration %v, want %v", dur1, expectedDur)
	}

	// Third frame: same delta.
	_, dur2, err := src.NextFrame()
	if err != nil {
		t.Fatalf("frame 2: %v", err)
	}
	if dur2 != expectedDur {
		t.Fatalf("frame 2: got duration %v, want %v", dur2, expectedDur)
	}
}

func TestMemSource_SingleFileWrap(t *testing.T) {
	dir := t.TempDir()
	writeTestIVF(t, filepath.Join(dir, "single.ivf"), [][]byte{{0xAA}, {0xBB}})

	cams, err := LoadCameras(dir)
	if err != nil {
		t.Fatalf("LoadCameras: %v", err)
	}

	src := NewMemSource(cams)

	// 2 frames per cycle, read 5 frames: AA BB AA BB AA
	want := []byte{0xAA, 0xBB, 0xAA, 0xBB, 0xAA}
	for i, w := range want {
		data, _, err := src.NextFrame()
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if len(data) != 1 || data[0] != w {
			t.Fatalf("frame %d: got %v, want [%#x]", i, data, w)
		}
	}
}
