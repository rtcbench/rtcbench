package vp9

import "fmt"

// SubFrame is a single VP9 frame extracted from a superframe.
type SubFrame struct {
	Data  []byte
	Index int // 0-based position within superframe (corresponds to SID for SVC)
}

// ParseSuperframe splits a VP9 superframe into its constituent sub-frames.
// If the data is not a superframe (no valid superframe index), it returns
// a single SubFrame containing the entire data.
//
// VP9 superframe layout:
//
//	[frame_0][frame_1]...[frame_N][marker][size_0][size_1]...[size_N][marker]
//
// Marker byte: bits[7:5]=110, bits[4:2]=frames_minus_1, bits[1:0]=mag_minus_1
func ParseSuperframe(data []byte) ([]SubFrame, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("vp9: empty superframe data")
	}

	marker := data[len(data)-1]

	// Superframe marker: top 3 bits must be 110 (0xC0)
	if marker&0xE0 != 0xC0 {
		return []SubFrame{{Data: data, Index: 0}}, nil
	}

	numFrames := int((marker>>2)&0x07) + 1
	sizeBytes := int(marker&0x03) + 1
	indexSize := 2 + numFrames*sizeBytes // two marker bytes + size fields

	if indexSize > len(data) {
		// Index doesn't fit, not a valid superframe
		return []SubFrame{{Data: data, Index: 0}}, nil
	}

	// Opening marker must match closing marker
	if data[len(data)-indexSize] != marker {
		return []SubFrame{{Data: data, Index: 0}}, nil
	}

	// Parse frame sizes (little-endian)
	sizes := make([]int, numFrames)
	off := len(data) - indexSize + 1 // skip opening marker byte
	totalFrameBytes := 0
	for i := range numFrames {
		var size int
		for j := range sizeBytes {
			size |= int(data[off+j]) << uint(8*j)
		}
		sizes[i] = size
		totalFrameBytes += size
		off += sizeBytes
	}

	if totalFrameBytes+indexSize != len(data) {
		return nil, fmt.Errorf("vp9: superframe sizes (%d) + index (%d) != data length (%d)",
			totalFrameBytes, indexSize, len(data))
	}

	frames := make([]SubFrame, numFrames)
	pos := 0
	for i := range numFrames {
		frames[i] = SubFrame{
			Data:  data[pos : pos+sizes[i]],
			Index: i,
		}
		pos += sizes[i]
	}

	return frames, nil
}
