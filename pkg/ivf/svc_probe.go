package ivf

import (
	"fmt"
	"os"

	"call.zip/pkg/vp9"
	"github.com/pion/webrtc/v4/pkg/media/ivfreader"
)

// ProbeSVCLayers reads the first frame of an IVF file and returns the number
// of spatial layers (sub-frames in the VP9 superframe). Returns 1 if the file
// does not contain superframes.
func ProbeSVCLayers(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	reader, _, err := ivfreader.NewWith(f)
	if err != nil {
		return 0, fmt.Errorf("read header: %w", err)
	}

	frame, _, err := reader.ParseNextFrame()
	if err != nil || frame == nil {
		return 0, fmt.Errorf("read first frame: %w", err)
	}

	subs, err := vp9.ParseSuperframe(frame)
	if err != nil {
		return 1, nil
	}
	return len(subs), nil
}
