package vp9

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// readFileBytes is a helper that reads an entire file and fails the test on error.
func readFileBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file %s: %v", path, err)
	}
	return data
}

// le16 is a helper that reads a little-endian uint16 at offset from data.
func le16(t *testing.T, data []byte, offset int) uint16 {
	t.Helper()
	if offset+2 > len(data) {
		t.Fatalf("le16: offset %d out of range (len=%d)", offset, len(data))
	}
	return binary.LittleEndian.Uint16(data[offset : offset+2])
}

// le32 is a helper that reads a little-endian uint32 at offset from data.
func le32(t *testing.T, data []byte, offset int) uint32 {
	t.Helper()
	if offset+4 > len(data) {
		t.Fatalf("le32: offset %d out of range (len=%d)", offset, len(data))
	}
	return binary.LittleEndian.Uint32(data[offset : offset+4])
}

// le64 is a helper that reads a little-endian uint64 at offset from data.
func le64(t *testing.T, data []byte, offset int) uint64 {
	t.Helper()
	if offset+8 > len(data) {
		t.Fatalf("le64: offset %d out of range (len=%d)", offset, len(data))
	}
	return binary.LittleEndian.Uint64(data[offset : offset+8])
}

func TestNewIvfWriter_Header(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.ivf")

	w, err := NewIvfWriter(path, 1920, 1080, 1, 90000)
	if err != nil {
		t.Fatalf("NewIvfWriter: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data := readFileBytes(t, path)
	if len(data) != 32 {
		t.Fatalf("header size: got %d, want 32", len(data))
	}

	// bytes 0-3: "DKIF" signature
	if string(data[0:4]) != "DKIF" {
		t.Fatalf("signature: got %q, want %q", string(data[0:4]), "DKIF")
	}

	// bytes 6-7: header size = 32
	if got := le16(t, data, 6); got != 32 {
		t.Fatalf("header size field: got %d, want 32", got)
	}

	// bytes 8-11: "VP90" codec
	if string(data[8:12]) != "VP90" {
		t.Fatalf("codec: got %q, want %q", string(data[8:12]), "VP90")
	}

	// bytes 12-13: width = 1920
	if got := le16(t, data, 12); got != 1920 {
		t.Fatalf("width: got %d, want 1920", got)
	}

	// bytes 14-15: height = 1080
	if got := le16(t, data, 14); got != 1080 {
		t.Fatalf("height: got %d, want 1080", got)
	}

	// bytes 16-19: timebase denominator = 90000
	if got := le32(t, data, 16); got != 90000 {
		t.Fatalf("tbDen: got %d, want 90000", got)
	}

	// bytes 20-23: timebase numerator = 1
	if got := le32(t, data, 20); got != 1 {
		t.Fatalf("tbNum: got %d, want 1", got)
	}

	// bytes 24-27: frame count = 0
	if got := le32(t, data, 24); got != 0 {
		t.Fatalf("frame count: got %d, want 0", got)
	}
}

func TestIvfWriter_WriteFrame_Single(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.ivf")

	w, err := NewIvfWriter(path, 320, 240, 1, 90000)
	if err != nil {
		t.Fatalf("NewIvfWriter: %v", err)
	}

	frame := make([]byte, 100)
	for i := range frame {
		frame[i] = byte(i)
	}
	if err := w.WriteFrame(frame); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data := readFileBytes(t, path)

	// total file size = 32 (header) + 12 (frame header) + 100 (frame data) = 144
	if len(data) != 144 {
		t.Fatalf("file size: got %d, want 144", len(data))
	}

	// frame header at offset 32: size = 100
	if got := le32(t, data, 32); got != 100 {
		t.Fatalf("frame size: got %d, want 100", got)
	}

	// frame header at offset 36: PTS = 0 (first frame)
	if got := le64(t, data, 36); got != 0 {
		t.Fatalf("PTS: got %d, want 0", got)
	}
}

func TestIvfWriter_WriteFrame_Multiple(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.ivf")

	w, err := NewIvfWriter(path, 640, 480, 1, 90000)
	if err != nil {
		t.Fatalf("NewIvfWriter: %v", err)
	}

	frame := []byte{0xDE, 0xAD}
	for i := 0; i < 10; i++ {
		if err := w.WriteFrame(frame); err != nil {
			t.Fatalf("WriteFrame(%d): %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data := readFileBytes(t, path)

	// frame count at offset 24 should be 10
	if got := le32(t, data, 24); got != 10 {
		t.Fatalf("frame count: got %d, want 10", got)
	}
}

func TestIvfWriter_WriteFrame_PTS(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.ivf")

	w, err := NewIvfWriter(path, 640, 480, 1, 90000)
	if err != nil {
		t.Fatalf("NewIvfWriter: %v", err)
	}

	frame := []byte{0x01}
	for i := 0; i < 3; i++ {
		if err := w.WriteFrame(frame); err != nil {
			t.Fatalf("WriteFrame(%d): %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data := readFileBytes(t, path)

	// Each frame: 12-byte header + 1-byte data = 13 bytes
	// Frame 0 header at offset 32, PTS at offset 36
	// Frame 1 header at offset 45, PTS at offset 49
	// Frame 2 header at offset 58, PTS at offset 62
	wantPTS := []uint64{0, 3600, 7200}
	for i, want := range wantPTS {
		ptsOffset := 32 + i*(12+1) + 4 // header + i*(frameHeader+frameData) + 4 bytes for size field
		got := le64(t, data, ptsOffset)
		if got != want {
			t.Fatalf("frame %d PTS: got %d, want %d", i, got, want)
		}
	}
}

func TestIvfWriter_Write_IOWriter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.ivf")

	w, err := NewIvfWriter(path, 640, 480, 1, 90000)
	if err != nil {
		t.Fatalf("NewIvfWriter: %v", err)
	}

	input := []byte("hello, ivf writer")
	n, err := w.Write(input)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len(input) {
		t.Fatalf("Write returned n=%d, want %d", n, len(input))
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestIvfWriter_WriteAfterClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.ivf")

	w, err := NewIvfWriter(path, 640, 480, 1, 90000)
	if err != nil {
		t.Fatalf("NewIvfWriter: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Write() after close
	_, writeErr := w.Write([]byte{0x01})
	if writeErr != os.ErrClosed {
		t.Fatalf("Write after close: got err=%v, want os.ErrClosed", writeErr)
	}

	// WriteFrame() after close
	frameErr := w.WriteFrame([]byte{0x01})
	if frameErr != os.ErrClosed {
		t.Fatalf("WriteFrame after close: got err=%v, want os.ErrClosed", frameErr)
	}
}

func TestIvfWriter_Close_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.ivf")

	w, err := NewIvfWriter(path, 640, 480, 1, 90000)
	if err != nil {
		t.Fatalf("NewIvfWriter: %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("second Close: got err=%v, want nil", err)
	}
}

func TestIvfWriter_Close_FrameCountCap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.ivf")

	w, err := NewIvfWriter(path, 640, 480, 1, 90000)
	if err != nil {
		t.Fatalf("NewIvfWriter: %v", err)
	}

	// Set frameCount above the cap (in-package access)
	w.frameCount = 0x80000000

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data := readFileBytes(t, path)

	// frame count at offset 24 should be capped at 0x7FFFFFFF
	if got := le32(t, data, 24); got != 0x7FFFFFFF {
		t.Fatalf("frame count cap: got 0x%X, want 0x7FFFFFFF", got)
	}
}

func TestNewIvfSegmenter_CreatesDirectory(t *testing.T) {
	base := t.TempDir()
	subDir := filepath.Join(base, "sub", "dir")

	_, err := NewIvfSegmenter(subDir)
	if err != nil {
		t.Fatalf("NewIvfSegmenter: %v", err)
	}

	info, err := os.Stat(subDir)
	if err != nil {
		t.Fatalf("Stat(%s): %v", subDir, err)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", subDir)
	}
}

func TestIvfSegmenter_DisabledByDefault(t *testing.T) {
	dir := t.TempDir()

	seg, err := NewIvfSegmenter(dir)
	if err != nil {
		t.Fatalf("NewIvfSegmenter: %v", err)
	}

	// Push a keyframe payload without enabling
	// First byte: I=0 P=0 L=0 F=0 B=0 E=0 V=0 Z=0 = 0x00 (keyframe since P=0, L=0)
	payload := []byte{0x00, 0xDE, 0xAD}
	desc := &PayloadDescriptor{P: 0, L: 0, ByteLength: 1}
	if err := seg.Push(payload, 1000, desc); err != nil {
		t.Fatalf("Push: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 files when disabled, got %d", len(entries))
	}
}

func TestIvfSegmenter_Enable_ThenPush(t *testing.T) {
	dir := t.TempDir()

	seg, err := NewIvfSegmenter(dir)
	if err != nil {
		t.Fatalf("NewIvfSegmenter: %v", err)
	}
	seg.Enable()

	// The segmenter only creates a file when a keyframe arrives at a *different*
	// timestamp than the current one. Push an initial non-keyframe to set the
	// initial timestamp, then push the keyframe at a new timestamp.
	initDesc := &PayloadDescriptor{P: 1, ByteLength: 1}
	if err := seg.Push([]byte{0x40, 0xFF}, 500, initDesc); err != nil {
		t.Fatalf("Push init: %v", err)
	}

	// Keyframe at different timestamp triggers file creation
	kfDesc := &PayloadDescriptor{P: 0, L: 0, ByteLength: 1}
	ts := uint32(1000)
	if err := seg.Push([]byte{0x00, 0xCA, 0xFE}, ts, kfDesc); err != nil {
		t.Fatalf("Push kf: %v", err)
	}
	if err := seg.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	expected := filepath.Join(dir, fmt.Sprintf("kf-%d.ivf", ts))
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("expected file %s not found: %v", expected, err)
	}
}

func TestIvfSegmenter_Push_IgnoresUntilKeyframe(t *testing.T) {
	dir := t.TempDir()

	seg, err := NewIvfSegmenter(dir)
	if err != nil {
		t.Fatalf("NewIvfSegmenter: %v", err)
	}
	seg.Enable()

	// Non-keyframe packet: P=1
	payload := []byte{0x40, 0xBE, 0xEF}
	desc := &PayloadDescriptor{P: 1, ByteLength: 1}

	if err := seg.Push(payload, 1000, desc); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if err := seg.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 files before first keyframe, got %d", len(entries))
	}
}

func TestIvfSegmenter_Push_KeyframeRotation(t *testing.T) {
	dir := t.TempDir()

	seg, err := NewIvfSegmenter(dir)
	if err != nil {
		t.Fatalf("NewIvfSegmenter: %v", err)
	}
	seg.Enable()

	kfDesc := &PayloadDescriptor{P: 0, L: 0, ByteLength: 1}
	nonKfDesc := &PayloadDescriptor{P: 1, ByteLength: 1}

	// Push initial non-keyframe to set starting timestamp
	if err := seg.Push([]byte{0x40, 0xFF}, 500, nonKfDesc); err != nil {
		t.Fatalf("Push init: %v", err)
	}

	// First keyframe at timestamp 1000 (different from 500, triggers file creation)
	if err := seg.Push([]byte{0x00, 0xAA}, 1000, kfDesc); err != nil {
		t.Fatalf("Push kf1 pkt1: %v", err)
	}
	// Another packet at same timestamp 1000 (same frame, appends to buffer)
	if err := seg.Push([]byte{0x40, 0xBB}, 1000, nonKfDesc); err != nil {
		t.Fatalf("Push kf1 pkt2: %v", err)
	}

	// Second keyframe at timestamp 5000 (new timestamp triggers file rotation)
	if err := seg.Push([]byte{0x00, 0xCC}, 5000, kfDesc); err != nil {
		t.Fatalf("Push kf2: %v", err)
	}

	if err := seg.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Verify both files exist
	for _, ts := range []uint32{1000, 5000} {
		path := filepath.Join(dir, fmt.Sprintf("kf-%d.ivf", ts))
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected file %s not found: %v", path, err)
		}
	}

	// Verify exactly 2 IVF files
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 files, got %d", len(entries))
	}
}

func TestIvfSegmenter_Push_StripsDescriptor(t *testing.T) {
	dir := t.TempDir()

	seg, err := NewIvfSegmenter(dir)
	if err != nil {
		t.Fatalf("NewIvfSegmenter: %v", err)
	}
	seg.Enable()

	nonKfDesc := &PayloadDescriptor{P: 1, ByteLength: 1}

	// Push initial non-keyframe to set starting timestamp
	if err := seg.Push([]byte{0x40, 0xFF}, 500, nonKfDesc); err != nil {
		t.Fatalf("Push init: %v", err)
	}

	// 2 bytes descriptor + 3 bytes VP9 data at a new timestamp (keyframe triggers file)
	descriptor := []byte{0x00, 0x00}    // 2-byte descriptor (keyframe: P=0, L=0)
	vp9Data := []byte{0xDE, 0xAD, 0xBE} // known VP9 data
	payload := append(descriptor, vp9Data...)

	kfDesc := &PayloadDescriptor{P: 0, L: 0, ByteLength: 2}

	if err := seg.Push(payload, 1000, kfDesc); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if err := seg.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Read the IVF file and extract the frame data
	path := filepath.Join(dir, "kf-1000.ivf")
	data := readFileBytes(t, path)

	// IVF header = 32 bytes, frame header = 12 bytes, then frame data
	if len(data) < 32+12 {
		t.Fatalf("file too short: got %d bytes", len(data))
	}

	frameSize := le32(t, data, 32)
	if int(frameSize) != len(vp9Data) {
		t.Fatalf("frame size: got %d, want %d", frameSize, len(vp9Data))
	}

	frameData := data[44 : 44+frameSize]
	for i, b := range frameData {
		if b != vp9Data[i] {
			t.Fatalf("frame data[%d]: got 0x%02X, want 0x%02X", i, b, vp9Data[i])
		}
	}
}

func TestIvfSegmenter_Close_FlushesBuffer(t *testing.T) {
	dir := t.TempDir()

	seg, err := NewIvfSegmenter(dir)
	if err != nil {
		t.Fatalf("NewIvfSegmenter: %v", err)
	}
	seg.Enable()

	nonKfDesc := &PayloadDescriptor{P: 1, ByteLength: 1}
	kfDesc := &PayloadDescriptor{P: 0, L: 0, ByteLength: 1}

	// Push initial non-keyframe to set starting timestamp
	if err := seg.Push([]byte{0x40, 0xFF}, 500, nonKfDesc); err != nil {
		t.Fatalf("Push init: %v", err)
	}

	// Push keyframe at new timestamp to create a file
	if err := seg.Push([]byte{0x00, 0xAA}, 1000, kfDesc); err != nil {
		t.Fatalf("Push kf: %v", err)
	}
	// Push another packet with same timestamp (appends to frame buffer)
	if err := seg.Push([]byte{0x00, 0xBB}, 1000, kfDesc); err != nil {
		t.Fatalf("Push pkt2: %v", err)
	}

	// Close should flush the buffered frame
	if err := seg.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	path := filepath.Join(dir, "kf-1000.ivf")
	data := readFileBytes(t, path)

	// Verify a frame was written: file should be larger than just the header
	if len(data) <= 32 {
		t.Fatalf("expected frame data in file, got only %d bytes (header only)", len(data))
	}

	// Verify frame count was patched to 1
	if got := le32(t, data, 24); got != 1 {
		t.Fatalf("frame count: got %d, want 1", got)
	}

	// Verify frame data is the combined VP9 payloads (0xAA + 0xBB)
	frameSize := le32(t, data, 32)
	if frameSize != 2 {
		t.Fatalf("frame size: got %d, want 2", frameSize)
	}
	frameData := data[44:46]
	if frameData[0] != 0xAA || frameData[1] != 0xBB {
		t.Fatalf("frame data: got [0x%02X, 0x%02X], want [0xAA, 0xBB]", frameData[0], frameData[1])
	}
}

func TestIvfSegmenter_Close_NoFile(t *testing.T) {
	dir := t.TempDir()

	seg, err := NewIvfSegmenter(dir)
	if err != nil {
		t.Fatalf("NewIvfSegmenter: %v", err)
	}
	seg.Enable()

	// Never push anything, just close
	if err := seg.Close(); err != nil {
		t.Fatalf("Close: got err=%v, want nil", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 files, got %d", len(entries))
	}
}
