package ivf

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// LoadCameraPaths returns the sorted absolute paths of all regular files in dir.
// Plugins use this to build the list of IVF files to stream from the cameras directory.
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
