package ivf

import (
	"fmt"
	"os"
	"time"

	"github.com/pion/webrtc/v4/pkg/media/ivfreader"
)

// Frame holds a single pre-parsed IVF frame ready for sending.
type Frame struct {
	Data     []byte
	Duration time.Duration
}

// PreloadedCameras holds all IVF files pre-loaded into memory as parsed frames.
type PreloadedCameras struct {
	Files [][]Frame // one slice of frames per IVF file
}

// LoadCameras reads all IVF files from dir into memory, pre-parsing frames and
// computing inter-frame durations. The returned PreloadedCameras can be shared
// across multiple sender goroutines since the frame data is read-only.
func LoadCameras(dir string) (*PreloadedCameras, error) {
	paths, err := LoadCameraPaths(dir)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("ivf: no files found in %q", dir)
	}

	cams := &PreloadedCameras{
		Files: make([][]Frame, 0, len(paths)),
	}

	for _, path := range paths {
		frames, err := loadOneFile(path)
		if err != nil {
			return nil, fmt.Errorf("ivf: loading %q: %w", path, err)
		}
		cams.Files = append(cams.Files, frames)
	}

	return cams, nil
}

func loadOneFile(path string) ([]Frame, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r, header, err := ivfreader.NewWith(f)
	if err != nil {
		return nil, err
	}

	defaultDur := 33 * time.Millisecond
	num := header.TimebaseNumerator
	den := header.TimebaseDenominator
	var prevTS *uint64

	var frames []Frame
	for {
		frame, fh, err := r.ParseNextFrame()
		if err != nil {
			break
		}
		if frame == nil && fh == nil {
			break
		}

		dur := defaultDur
		// pion v4 pre-converts: Timestamp = raw_pts * den / num.
		// Duration = delta * num^2 / den^2 seconds.
		ts := fh.Timestamp
		if prevTS != nil && ts > *prevTS && den != 0 {
			delta := ts - *prevTS
			dur = time.Duration(int64(time.Second) * int64(delta) * int64(num) * int64(num) / (int64(den) * int64(den)))
			if dur <= 0 {
				dur = defaultDur
			}
		}
		prevTS = &ts

		// Copy frame data so it doesn't alias the reader's internal buffer.
		data := make([]byte, len(frame))
		copy(data, frame)

		frames = append(frames, Frame{Data: data, Duration: dur})
	}

	if len(frames) == 0 {
		return nil, fmt.Errorf("no frames found")
	}
	return frames, nil
}

