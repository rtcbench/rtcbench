package vp9

import (
	"bytes"
	"testing"
)

func TestParseSuperframe_Empty(t *testing.T) {
	_, err := ParseSuperframe(nil)
	if err == nil {
		t.Fatal("expected error for nil data")
	}
	_, err = ParseSuperframe([]byte{})
	if err == nil {
		t.Fatal("expected error for empty data")
	}
}

func TestParseSuperframe_NotSuperframe(t *testing.T) {
	// Last byte doesn't have superframe marker (top 3 bits != 110)
	data := []byte{0x01, 0x02, 0x03, 0x04}
	frames, err := ParseSuperframe(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("got %d frames, want 1", len(frames))
	}
	if !bytes.Equal(frames[0].Data, data) {
		t.Fatalf("frame data mismatch")
	}
	if frames[0].Index != 0 {
		t.Fatalf("got index %d, want 0", frames[0].Index)
	}
}

// buildSuperframe constructs a VP9 superframe from sub-frame data slices.
// sizeBytes is 1-4 (bytes per size field in the index).
func buildSuperframe(subFrames [][]byte, sizeBytes int) []byte {
	numFrames := len(subFrames)
	// marker: 110 | (numFrames-1) << 2 | (sizeBytes-1)
	marker := byte(0xC0) | byte((numFrames-1)<<2) | byte(sizeBytes-1)

	var buf []byte
	for _, f := range subFrames {
		buf = append(buf, f...)
	}

	// superframe index: [marker][sizes...][marker]
	buf = append(buf, marker)
	for _, f := range subFrames {
		size := len(f)
		for j := range sizeBytes {
			buf = append(buf, byte(size>>(8*j)))
		}
	}
	buf = append(buf, marker)

	return buf
}

func TestParseSuperframe_TwoFrames(t *testing.T) {
	frame0 := []byte{0x10, 0x20}
	frame1 := []byte{0x30, 0x40, 0x50}

	data := buildSuperframe([][]byte{frame0, frame1}, 1)

	frames, err := ParseSuperframe(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(frames) != 2 {
		t.Fatalf("got %d frames, want 2", len(frames))
	}
	if !bytes.Equal(frames[0].Data, frame0) {
		t.Fatalf("frame 0 data mismatch: got %x, want %x", frames[0].Data, frame0)
	}
	if frames[0].Index != 0 {
		t.Fatalf("frame 0 index: got %d, want 0", frames[0].Index)
	}
	if !bytes.Equal(frames[1].Data, frame1) {
		t.Fatalf("frame 1 data mismatch: got %x, want %x", frames[1].Data, frame1)
	}
	if frames[1].Index != 1 {
		t.Fatalf("frame 1 index: got %d, want 1", frames[1].Index)
	}
}

func TestParseSuperframe_ThreeFrames(t *testing.T) {
	frame0 := []byte{0xAA}
	frame1 := []byte{0xBB, 0xCC}
	frame2 := []byte{0xDD, 0xEE, 0xFF}

	data := buildSuperframe([][]byte{frame0, frame1, frame2}, 1)

	frames, err := ParseSuperframe(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(frames) != 3 {
		t.Fatalf("got %d frames, want 3", len(frames))
	}
	for i, want := range [][]byte{frame0, frame1, frame2} {
		if !bytes.Equal(frames[i].Data, want) {
			t.Fatalf("frame %d: got %x, want %x", i, frames[i].Data, want)
		}
		if frames[i].Index != i {
			t.Fatalf("frame %d: got index %d", i, frames[i].Index)
		}
	}
}

func TestParseSuperframe_TwoByteFrameSizes(t *testing.T) {
	// Frame larger than 255 bytes requires 2-byte sizes
	frame0 := bytes.Repeat([]byte{0x01}, 300)
	frame1 := bytes.Repeat([]byte{0x02}, 500)

	data := buildSuperframe([][]byte{frame0, frame1}, 2)

	frames, err := ParseSuperframe(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(frames) != 2 {
		t.Fatalf("got %d frames, want 2", len(frames))
	}
	if len(frames[0].Data) != 300 {
		t.Fatalf("frame 0 size: got %d, want 300", len(frames[0].Data))
	}
	if len(frames[1].Data) != 500 {
		t.Fatalf("frame 1 size: got %d, want 500", len(frames[1].Data))
	}
}

func TestParseSuperframe_MarkerMismatch(t *testing.T) {
	// Construct data where last byte looks like a marker but opening marker doesn't match
	data := []byte{0x01, 0x02, 0xFF, 0x02, 0xC4} // 0xC4 = marker for 2 frames, 1-byte sizes
	// The opening marker position would be at data[5 - (2+2*1)] = data[0] = 0x01 != 0xC4
	frames, err := ParseSuperframe(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("got %d frames, want 1 (fallback to single frame)", len(frames))
	}
}
