package ivf

import (
	"fmt"
	"os"
	"time"

	"github.com/pion/webrtc/v4/pkg/media/ivfreader"
)

// FrameSource provides frames for the send loop. Each call to NextFrame
// returns the next frame's data and duration. Implementations cycle
// through their content indefinitely.
type FrameSource interface {
	NextFrame() (data []byte, dur time.Duration, err error)
}

// diskSource reads IVF files from disk, cycling through paths.
type diskSource struct {
	paths  []string
	idx    int
	file   *os.File
	reader *ivfreader.IVFReader
	num    uint32
	den    uint32
	prevTS *uint64
}

// NewDiskSource returns a FrameSource that reads IVF files from disk.
// The paths slice is shared read-only; each source maintains its own
// file handle and position.
func NewDiskSource(paths []string) FrameSource {
	return &diskSource{paths: paths}
}

func (s *diskSource) NextFrame() ([]byte, time.Duration, error) {
	defaultDur := 33 * time.Millisecond

	for {
		if s.reader == nil {
			f, err := os.Open(s.paths[s.idx])
			if err != nil {
				return nil, 0, fmt.Errorf("open %s: %w", s.paths[s.idx], err)
			}
			s.file = f

			r, header, err := ivfreader.NewWith(f)
			if err != nil {
				s.file.Close()
				s.file = nil
				return nil, 0, fmt.Errorf("read %s: %w", s.paths[s.idx], err)
			}
			s.reader = r
			s.num = header.TimebaseNumerator
			s.den = header.TimebaseDenominator
			s.prevTS = nil
		}

		frame, fh, err := s.reader.ParseNextFrame()
		if err != nil || (frame == nil && fh == nil) {
			// End of file — close and advance to next.
			s.file.Close()
			s.file = nil
			s.reader = nil
			s.idx = (s.idx + 1) % len(s.paths)
			continue
		}

		dur := defaultDur
		// pion/webrtc v4 pre-converts timestamps: Timestamp = raw_pts * den / num.
		// Duration between v4 timestamps: delta * num^2 / den^2 seconds.
		ts := fh.Timestamp
		if s.prevTS != nil && ts > *s.prevTS && s.den != 0 {
			delta := ts - *s.prevTS
			dur = time.Duration(int64(time.Second) * int64(delta) * int64(s.num) * int64(s.num) / (int64(s.den) * int64(s.den)))
			if dur <= 0 {
				dur = defaultDur
			}
		}
		s.prevTS = &ts

		return frame, dur, nil
	}
}

// memSource reads from preloaded camera data, cycling through files and frames.
type memSource struct {
	cams     *PreloadedCameras
	fileIdx  int
	frameIdx int
}

// NewMemSource returns a FrameSource backed by preloaded camera data.
// The PreloadedCameras data is shared read-only; each source maintains
// its own position.
func NewMemSource(cams *PreloadedCameras) FrameSource {
	return &memSource{cams: cams}
}

func (s *memSource) NextFrame() ([]byte, time.Duration, error) {
	f := s.cams.Files[s.fileIdx][s.frameIdx]
	s.frameIdx++
	if s.frameIdx >= len(s.cams.Files[s.fileIdx]) {
		s.frameIdx = 0
		s.fileIdx = (s.fileIdx + 1) % len(s.cams.Files)
	}
	return f.Data, f.Duration, nil
}
