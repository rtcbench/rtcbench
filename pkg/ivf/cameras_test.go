package ivf

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// writeTestIVF writes a minimal valid IVF file with the given frame payloads.
// Timestamps increment by 1 tick per frame.
// Pion ivfreader layout: offset 16-19 = TimebaseDenominator, 20-23 = TimebaseNumerator.
// We write den=30, num=1 (30fps), matching real ffmpeg VP9 IVF output.
func writeTestIVF(t *testing.T, path string, frames [][]byte) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	// IVF file header (32 bytes)
	var hdr [32]byte
	copy(hdr[0:4], "DKIF")           // signature
	binary.LittleEndian.PutUint16(hdr[6:8], 32)   // header size
	copy(hdr[8:12], "VP90")          // codec FourCC
	binary.LittleEndian.PutUint16(hdr[12:14], 640) // width
	binary.LittleEndian.PutUint16(hdr[14:16], 480) // height
	binary.LittleEndian.PutUint32(hdr[16:20], 30)  // timebase denominator (frame rate)
	binary.LittleEndian.PutUint32(hdr[20:24], 1)   // timebase numerator
	binary.LittleEndian.PutUint32(hdr[24:28], uint32(len(frames))) // frame count
	f.Write(hdr[:])

	// Frame headers (12 bytes each) + frame data
	for i, data := range frames {
		var fhdr [12]byte
		binary.LittleEndian.PutUint32(fhdr[0:4], uint32(len(data)))
		binary.LittleEndian.PutUint64(fhdr[4:12], uint64(i))
		f.Write(fhdr[:])
		f.Write(data)
	}
}

func TestNewCamerasDisk(t *testing.T) {
	dir := t.TempDir()
	writeTestIVF(t, filepath.Join(dir, "a.ivf"), [][]byte{{1, 2, 3}, {4, 5, 6}})
	writeTestIVF(t, filepath.Join(dir, "b.ivf"), [][]byte{{7, 8, 9}})

	cams, err := NewCameras(dir, false)
	if err != nil {
		t.Fatal(err)
	}

	src := cams.NewSource()

	// Read all 3 frames (2 from a.ivf + 1 from b.ivf)
	for i := 0; i < 3; i++ {
		data, dur, err := src.NextFrame()
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if len(data) == 0 {
			t.Fatalf("frame %d: empty data", i)
		}
		if dur <= 0 {
			t.Fatalf("frame %d: non-positive duration %v", i, dur)
		}
	}

	// Frame 4 should cycle back to a.ivf frame 0
	data, _, err := src.NextFrame()
	if err != nil {
		t.Fatal(err)
	}
	if data[0] != 1 || data[1] != 2 || data[2] != 3 {
		t.Fatalf("expected cycle back to first frame, got %v", data)
	}
}

func TestNewCamerasInMemory(t *testing.T) {
	dir := t.TempDir()
	writeTestIVF(t, filepath.Join(dir, "a.ivf"), [][]byte{{10, 20}, {30, 40}})

	cams, err := NewCameras(dir, true)
	if err != nil {
		t.Fatal(err)
	}

	// Two independent sources from the same Cameras should track position independently.
	src1 := cams.NewSource()
	src2 := cams.NewSource()

	d1, _, _ := src1.NextFrame()
	d2, _, _ := src2.NextFrame()

	// Both should read the same first frame.
	if d1[0] != d2[0] || d1[1] != d2[1] {
		t.Fatalf("independent sources diverged: %v vs %v", d1, d2)
	}

	// Advance src1 past all frames; src2 should still be at frame 1.
	src1.NextFrame() // frame 1
	d1, _, _ = src1.NextFrame() // wraps to frame 0
	d2, _, _ = src2.NextFrame() // frame 1

	if d1[0] != 10 || d2[0] != 30 {
		t.Fatalf("position tracking broken: src1=%v src2=%v", d1, d2)
	}
}

func TestNewCamerasEmptyDir(t *testing.T) {
	dir := t.TempDir()
	_, err := NewCameras(dir, false)
	// Empty dir returns no paths, which is not an error from LoadCameraPaths
	// but may be from LoadCameras in memory mode.
	_ = err

	_, err = NewCameras(dir, true)
	if err == nil {
		t.Fatal("expected error for empty dir in memory mode")
	}
}
