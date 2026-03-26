package ivf

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// LoadCameraPaths returns the sorted absolute paths of all regular files in dir.
func LoadCameraPaths(dir string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("ivf: reading camera directory %q: %w", dir, err)
	}

	names := make([]string, 0, len(ents))
	for _, ent := range ents {
		if ent.Type().IsRegular() {
			names = append(names, ent.Name())
		}
	}
	sort.Strings(names)

	paths := make([]string, 0, len(names))
	for _, name := range names {
		paths = append(paths, filepath.Join(dir, name))
	}
	return paths, nil
}

// Cameras holds loaded camera data for sending. It encapsulates the
// disk-vs-memory decision so plugins don't need to handle it.
type Cameras struct {
	paths []string
	cams  *PreloadedCameras
}

// NewCameras loads IVF files from dir. If inMemory is true, all frames
// are pre-parsed into memory; otherwise only file paths are resolved.
func NewCameras(dir string, inMemory bool) (*Cameras, error) {
	if inMemory {
		cams, err := LoadCameras(dir)
		if err != nil {
			return nil, err
		}
		return &Cameras{cams: cams}, nil
	}
	paths, err := LoadCameraPaths(dir)
	if err != nil {
		return nil, err
	}
	return &Cameras{paths: paths}, nil
}

// Paths returns the IVF file paths. Returns nil for in-memory cameras.
func (c *Cameras) Paths() []string {
	return c.paths
}

// NewSource returns a new FrameSource for a sender goroutine.
// Each call returns an independent source with its own position,
// so multiple senders can share the same Cameras safely.
func (c *Cameras) NewSource() FrameSource {
	if c.cams != nil {
		return NewMemSource(c.cams)
	}
	return NewDiskSource(c.paths)
}
