package vp9

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
)

// IvfSegmenter pushes VP9 packet payloads belonging to a single logical video stream into IVF files.
type IvfSegmenter struct {
	enabled   atomic.Bool
	outputDir string

	frameBuffer bytes.Buffer

	ivf                   *IvfWriter
	currentFrameTimestamp uint32
	hasCurrentTimestamp   bool
}

func NewIvfSegmenter(outputDir string) (*IvfSegmenter, error) {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("Vp9IvfSegmenter directory cannot be created: %w", err)
	}

	return &IvfSegmenter{
		outputDir: outputDir,
	}, nil
}

func (s *IvfSegmenter) Enable() {
	s.enabled.Store(true)
}

// Push ingests a single RTP payload (VP9 payload descriptor + VP9 payload).
// timestamp is the RTP timestamp associated with this packet.
func (s *IvfSegmenter) Push(payload []byte, timestamp uint32, desc *PayloadDescriptor) error {
	if !s.enabled.Load() {
		return nil
	}

	usable := len(payload) // we assume 16 byte AEAD padding is already stripped from the end
	if usable <= 0 {
		return nil
	}

	pdSize := desc.ByteLength
	if pdSize <= 0 || pdSize > usable {
		return nil
	}

	vp9Start := pdSize
	vp9Len := usable - pdSize
	if vp9Len <= 0 {
		return nil
	}

	if !s.hasCurrentTimestamp {
		s.currentFrameTimestamp = timestamp
		s.hasCurrentTimestamp = true
	}

	// new frame boundary
	if timestamp != s.currentFrameTimestamp {
		// flush previous frame
		if s.ivf != nil && s.frameBuffer.Len() > 0 {
			if err := s.ivf.WriteFrame(s.frameBuffer.Bytes()); err != nil {
				return err
			}
		}

		// reset for new frame
		s.frameBuffer.Reset()
		s.currentFrameTimestamp = timestamp

		// if the *new* packet is a keyframe, rotate file
		if desc.IsKeyframe() {
			if s.ivf != nil {
				_ = s.ivf.Close()
				s.ivf = nil
			}

			out := filepath.Join(s.outputDir, fmt.Sprintf("kf-%d.ivf", timestamp))
			w, err := NewIvfWriter(out, 0, 0, 1, 90000)
			if err != nil {
				return err
			}
			s.ivf = w
		}
	}

	// if we have not started an IVF yet, ignore until first keyframe file is created
	if s.ivf == nil {
		return nil
	}

	_, _ = s.frameBuffer.Write(payload[vp9Start : vp9Start+vp9Len])
	return nil
}

// Close flushes any buffered frame and closes the current IVF file.
func (s *IvfSegmenter) Close() error {
	if s.ivf != nil && s.frameBuffer.Len() > 0 {
		if err := s.ivf.WriteFrame(s.frameBuffer.Bytes()); err != nil {
			_ = s.ivf.Close()
			s.ivf = nil
			s.frameBuffer.Reset()
			return err
		}
	}
	s.frameBuffer.Reset()

	if s.ivf != nil {
		err := s.ivf.Close()
		s.ivf = nil
		return err
	}
	return nil
}

type IvfWriter struct {
	f      *os.File
	width  uint16
	height uint16
	tbNum  uint32
	tbDen  uint32

	frameCount uint64
	closed     bool
}

func NewIvfWriter(path string, width, height int, tbNum, tbDen uint32) (*IvfWriter, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}

	w := &IvfWriter{
		f:      f,
		width:  uint16(width),
		height: uint16(height),
		tbNum:  tbNum,
		tbDen:  tbDen,
	}
	if err := w.writeHeader(); err != nil {
		_ = f.Close()
		return nil, err
	}
	return w, nil
}

func (w *IvfWriter) Write(p []byte) (n int, err error) {
	if w.closed {
		return 0, os.ErrClosed
	}
	return w.f.Write(p)
}

func (w *IvfWriter) WriteFrame(frame []byte) error {
	if w.closed {
		return os.ErrClosed
	}

	var fh [12]byte
	binary.LittleEndian.PutUint32(fh[0:4], uint32(len(frame)))

	// TODO hardcoded fps: frameCount * 3600 (~25fps against 90kHz clock)
	pts := w.frameCount * 3600
	binary.LittleEndian.PutUint64(fh[4:12], pts)

	if _, err := w.f.Write(fh[:]); err != nil {
		return err
	}
	if _, err := w.f.Write(frame); err != nil {
		return err
	}

	w.frameCount++
	return nil
}

func (w *IvfWriter) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true

	// patch frame count into IVF header at offset 24 (little-endian uint32)
	if _, err := w.f.Seek(24, 0); err != nil {
		_ = w.f.Close()
		return err
	}

	var cnt [4]byte
	if w.frameCount > 0x7FFFFFFF {
		binary.LittleEndian.PutUint32(cnt[:], 0x7FFFFFFF)
	} else {
		binary.LittleEndian.PutUint32(cnt[:], uint32(w.frameCount))
	}

	if _, err := w.f.Write(cnt[:]); err != nil {
		_ = w.f.Close()
		return err
	}

	if err := w.f.Sync(); err != nil {
		_ = w.f.Close()
		return err
	}
	return w.f.Close()
}

func (w *IvfWriter) writeHeader() error {
	var hdr [32]byte
	hdr[0], hdr[1], hdr[2], hdr[3] = 'D', 'K', 'I', 'F'

	binary.LittleEndian.PutUint16(hdr[4:6], 0)
	binary.LittleEndian.PutUint16(hdr[6:8], 32)

	hdr[8], hdr[9], hdr[10], hdr[11] = 'V', 'P', '9', '0'

	binary.LittleEndian.PutUint16(hdr[12:14], w.width)
	binary.LittleEndian.PutUint16(hdr[14:16], w.height)
	binary.LittleEndian.PutUint32(hdr[16:20], w.tbNum)
	binary.LittleEndian.PutUint32(hdr[20:24], w.tbDen)

	_, err := w.f.Write(hdr[:])
	return err
}
