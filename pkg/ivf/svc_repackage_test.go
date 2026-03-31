package ivf

import (
	"testing"

	"github.com/rtcbench/rtcbench/pkg/vp9"
)

func TestBuildSuperframeRoundTrip(t *testing.T) {
	frames := [][]byte{
		{0x01, 0x02, 0x03},
		{0x04, 0x05, 0x06, 0x07},
		{0x08, 0x09},
	}

	sf := BuildSuperframe(frames)

	parsed, err := vp9.ParseSuperframe(sf)
	if err != nil {
		t.Fatalf("ParseSuperframe: %v", err)
	}
	if len(parsed) != 3 {
		t.Fatalf("got %d sub-frames, want 3", len(parsed))
	}

	for i, f := range parsed {
		if string(f.Data) != string(frames[i]) {
			t.Errorf("sub-frame %d: got %v, want %v", i, f.Data, frames[i])
		}
		if f.Index != i {
			t.Errorf("sub-frame %d: got index %d", i, f.Index)
		}
	}
}

func TestBuildSuperframeSingleFrame(t *testing.T) {
	frames := [][]byte{{0xAA, 0xBB}}

	sf := BuildSuperframe(frames)

	parsed, err := vp9.ParseSuperframe(sf)
	if err != nil {
		t.Fatalf("ParseSuperframe: %v", err)
	}
	if len(parsed) != 1 {
		t.Fatalf("got %d sub-frames, want 1", len(parsed))
	}
	if string(parsed[0].Data) != string(frames[0]) {
		t.Errorf("data mismatch")
	}
}

func TestBuildSuperframeLargeFrames(t *testing.T) {
	// Frames larger than 255 bytes need multi-byte size fields.
	big := make([]byte, 300)
	for i := range big {
		big[i] = byte(i % 256)
	}
	frames := [][]byte{big, {0x01}, big}

	sf := BuildSuperframe(frames)

	parsed, err := vp9.ParseSuperframe(sf)
	if err != nil {
		t.Fatalf("ParseSuperframe: %v", err)
	}
	if len(parsed) != 3 {
		t.Fatalf("got %d sub-frames, want 3", len(parsed))
	}
	if len(parsed[0].Data) != 300 {
		t.Errorf("frame 0: got %d bytes, want 300", len(parsed[0].Data))
	}
	if len(parsed[1].Data) != 1 {
		t.Errorf("frame 1: got %d bytes, want 1", len(parsed[1].Data))
	}
}
