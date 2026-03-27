package ivf

import (
	"encoding/binary"
	"fmt"
	"os"

	"github.com/pion/webrtc/v3/pkg/media/ivfreader"
)

// BuildSuperframe constructs a VP9 superframe from the given sub-frame data.
// The superframe format is:
//
//	[frame_0][frame_1]...[frame_N][marker][size_0]...[size_N][marker]
//
// Marker byte: bits[7:5]=110, bits[4:2]=frames_minus_1, bits[1:0]=mag_minus_1
func BuildSuperframe(subFrames [][]byte) []byte {
	// Determine the number of bytes needed per size field.
	maxSize := 0
	totalData := 0
	for _, f := range subFrames {
		if len(f) > maxSize {
			maxSize = len(f)
		}
		totalData += len(f)
	}

	sizeBytes := 1
	for maxSize >= (1 << (8 * sizeBytes)) {
		sizeBytes++
	}

	numFrames := len(subFrames)
	indexSize := 2 + numFrames*sizeBytes
	out := make([]byte, 0, totalData+indexSize)

	// Write frame data
	for _, f := range subFrames {
		out = append(out, f...)
	}

	// marker: 110 | (numFrames-1)<<2 | (sizeBytes-1)
	marker := byte(0xC0) | byte((numFrames-1)<<2) | byte(sizeBytes-1)
	out = append(out, marker)

	// Write sizes (little-endian)
	for _, f := range subFrames {
		size := len(f)
		for j := range sizeBytes {
			out = append(out, byte(size>>(8*j)))
		}
	}

	out = append(out, marker)
	return out
}

// RepackageSVCIVFMulti reads one IVF per spatial layer (S0, S1, ..., S_N) and
// writes an output IVF where each frame is a VP9 superframe combining one
// frame from each layer. srcPaths[0] is the lowest spatial layer (S0) and
// srcPaths[len-1] is the highest. All source IVFs must have the same number
// of frames; the output header uses the dimensions of the highest layer.
//
// Unlike RepackageSVCIVF (which duplicates a single frame), this function
// produces genuinely different-sized sub-frames per layer, matching what a
// real VP9 SVC encoder produces. The total output bitrate is the sum of all
// per-layer IVF bitrates.
func RepackageSVCIVFMulti(srcPaths []string, dstPath string) error {
	n := len(srcPaths)
	if n < 1 || n > 3 {
		return fmt.Errorf("srcPaths must have 1-3 entries, got %d", n)
	}

	files := make([]*os.File, n)
	readers := make([]*ivfreader.IVFReader, n)
	headers := make([]*ivfreader.IVFFileHeader, n)
	for i, path := range srcPaths {
		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open layer %d (%s): %w", i, path, err)
		}
		defer f.Close()
		r, h, err := ivfreader.NewWith(f)
		if err != nil {
			return fmt.Errorf("read IVF header layer %d: %w", i, err)
		}
		files[i], readers[i], headers[i] = f, r, h
	}

	// Output header uses dimensions of the highest (last) spatial layer.
	topHdr := headers[n-1]

	df, err := os.Create(dstPath)
	if err != nil {
		return fmt.Errorf("create dest: %w", err)
	}
	defer df.Close()

	var hdr [32]byte
	copy(hdr[0:4], "DKIF")
	binary.LittleEndian.PutUint16(hdr[4:6], 0)
	binary.LittleEndian.PutUint16(hdr[6:8], 32)
	copy(hdr[8:12], "VP90")
	binary.LittleEndian.PutUint16(hdr[12:14], topHdr.Width)
	binary.LittleEndian.PutUint16(hdr[14:16], topHdr.Height)
	binary.LittleEndian.PutUint32(hdr[16:20], topHdr.TimebaseDenominator)
	binary.LittleEndian.PutUint32(hdr[20:24], topHdr.TimebaseNumerator)
	if _, err := df.Write(hdr[:]); err != nil {
		return err
	}

	for {
		subFrames := make([][]byte, n)
		var ts uint64
		for i, r := range readers {
			frame, fh, err := r.ParseNextFrame()
			if err != nil || (frame == nil && fh == nil) {
				return nil // EOF on any layer — stop
			}
			subFrames[i] = frame
			if i == 0 {
				ts = fh.Timestamp
			}
		}
		superframe := BuildSuperframe(subFrames)
		var frameHdr [12]byte
		binary.LittleEndian.PutUint32(frameHdr[0:4], uint32(len(superframe)))
		binary.LittleEndian.PutUint64(frameHdr[4:12], ts)
		if _, err := df.Write(frameHdr[:]); err != nil {
			return err
		}
		if _, err := df.Write(superframe); err != nil {
			return err
		}
	}
}

// RepackageSVCIVF reads a single-layer IVF file and writes a new IVF where
// each frame is a VP9 superframe containing numSpatialLayers copies of the
// source frame. This produces valid superframe structure that the send loop
// can parse into per-layer sub-frames.
//
// The output IVF uses the same timebase and dimensions as the input.
func RepackageSVCIVF(srcPath, dstPath string, numSpatialLayers int) error {
	if numSpatialLayers < 1 || numSpatialLayers > 3 {
		return fmt.Errorf("numSpatialLayers must be 1-3, got %d", numSpatialLayers)
	}

	sf, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer sf.Close()

	reader, header, err := ivfreader.NewWith(sf)
	if err != nil {
		return fmt.Errorf("read IVF header: %w", err)
	}

	df, err := os.Create(dstPath)
	if err != nil {
		return fmt.Errorf("create dest: %w", err)
	}
	defer df.Close()

	// Write IVF header (32 bytes)
	var hdr [32]byte
	copy(hdr[0:4], "DKIF")
	binary.LittleEndian.PutUint16(hdr[4:6], 0)    // version
	binary.LittleEndian.PutUint16(hdr[6:8], 32)   // header size
	copy(hdr[8:12], "VP90")                        // codec
	binary.LittleEndian.PutUint16(hdr[12:14], header.Width)
	binary.LittleEndian.PutUint16(hdr[14:16], header.Height)
	binary.LittleEndian.PutUint32(hdr[16:20], header.TimebaseDenominator)
	binary.LittleEndian.PutUint32(hdr[20:24], header.TimebaseNumerator)
	// frames count and unused bytes left as 0
	if _, err := df.Write(hdr[:]); err != nil {
		return err
	}

	for {
		frame, fh, err := reader.ParseNextFrame()
		if err != nil || (frame == nil && fh == nil) {
			break
		}

		// Build a superframe with numSpatialLayers copies of this frame.
		// For SVC, lower spatial layers would be lower resolution, but since
		// we only need valid superframe structure for the send loop to parse
		// and the viewer measures payload descriptors (not raw VP9 content),
		// using identical copies is sufficient.
		subFrames := make([][]byte, numSpatialLayers)
		for i := range numSpatialLayers {
			subFrames[i] = frame
		}
		superframe := BuildSuperframe(subFrames)

		// Write IVF frame header (12 bytes) + superframe data
		var frameHdr [12]byte
		binary.LittleEndian.PutUint32(frameHdr[0:4], uint32(len(superframe)))
		binary.LittleEndian.PutUint64(frameHdr[4:12], fh.Timestamp)
		if _, err := df.Write(frameHdr[:]); err != nil {
			return err
		}
		if _, err := df.Write(superframe); err != nil {
			return err
		}
	}

	return nil
}
