package vp9

import (
	"fmt"
)

type PayloadDescriptor struct {
	I         uint8
	P         uint8
	L         uint8
	F         uint8
	B         uint8
	E         uint8
	V         uint8
	Z         uint8
	M         uint8
	PictureID uint16
	TID       uint8
	UBit      uint8
	SID       uint8
	DBit      uint8
	TL0PicIdx uint8
	SSPresent uint8
}

// ParseVP9PayloadDescriptor parses the VP9 payload descriptor from RTP payload
func ParseVP9PayloadDescriptor(payload []byte) (*PayloadDescriptor, error) {
	if len(payload) == 0 {
		return nil, fmt.Errorf("empty payload")
	}

	desc := &PayloadDescriptor{}

	firstByte := payload[0]
	desc.I = (firstByte >> 7) & 1
	desc.P = (firstByte >> 6) & 1
	desc.L = (firstByte >> 5) & 1
	desc.F = (firstByte >> 4) & 1
	desc.B = (firstByte >> 3) & 1
	desc.E = (firstByte >> 2) & 1
	desc.V = (firstByte >> 1) & 1
	desc.Z = firstByte & 1

	ptr := 1
	if ptr >= len(payload) {
		return desc, nil
	}

	// Parse optional Picture ID
	if desc.I != 0 {
		pictureIDByte := payload[ptr]
		ptr++
		if ptr >= len(payload) {
			return desc, nil
		}

		desc.PictureID = uint16(pictureIDByte & 0x7F) // Lower 7 bits
		if pictureIDByte&0x80 != 0 {                  // Extended Picture ID
			desc.PictureID = (desc.PictureID << 8) | uint16(payload[ptr])
			ptr++
			desc.M = 1
		} else {
			desc.M = 0
		}
	}

	// Parse Layer indices if L is set
	if desc.L != 0 {
		if ptr >= len(payload) {
			return desc, nil
		}

		layerByte := payload[ptr]
		ptr++
		if ptr >= len(payload) {
			return desc, nil
		}

		desc.TID = (layerByte >> 5) & 0x07  // Temporal ID (TID)
		desc.UBit = (layerByte >> 4) & 0x01 // U bit
		desc.SID = (layerByte >> 1) & 0x07  // Spatial ID (SID)
		desc.DBit = layerByte & 0x01        // D bit

		if ptr >= len(payload) {
			return desc, nil
		}

		desc.TL0PicIdx = payload[ptr] // Temporal Layer 0 Picture Index
		ptr++
	}

	// Parse Scalability Structure (SS) if V is set
	if desc.V != 0 {
		desc.SSPresent = 1
	} else {
		desc.SSPresent = 0
	}

	return desc, nil
}
