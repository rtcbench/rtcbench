package vp9

import (
	"errors"
)

var ErrEmptyPayload = errors.New("empty payload")

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

	// ByteLength total VP9 payload descriptor size in bytes
	ByteLength int
}

// ParseVP9PayloadDescriptor parses the VP9 payload descriptor from RTP payload
func ParseVP9PayloadDescriptor(payload []byte, desc *PayloadDescriptor) error {
	if len(payload) == 0 {
		return ErrEmptyPayload
	}

	*desc = PayloadDescriptor{} // zero out caller's memory

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
		desc.ByteLength = ptr
		return nil
	}

	// Parse optional Picture ID
	if desc.I != 0 {
		pictureIDByte := payload[ptr]
		ptr++
		if ptr >= len(payload) {
			desc.ByteLength = ptr
			return nil
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
			desc.ByteLength = ptr
			return nil
		}

		layerByte := payload[ptr]
		ptr++

		desc.TID = (layerByte >> 5) & 0x07  // Temporal ID (TID)
		desc.UBit = (layerByte >> 4) & 0x01 // U bit
		desc.SID = (layerByte >> 1) & 0x07  // Spatial ID (SID)
		desc.DBit = layerByte & 0x01        // D bit

		if desc.F == 0 {
			if ptr >= len(payload) {
				desc.ByteLength = ptr
				return nil
			}
			desc.TL0PicIdx = payload[ptr] // Temporal Layer 0 Picture Index
			ptr++
		}
	}

	if desc.P != 0 && desc.F != 0 {
		for i := 0; i < 3; i++ {
			if ptr >= len(payload) {
				desc.ByteLength = ptr
				return nil
			}
			pdiff := payload[ptr]
			ptr++
			if (pdiff & 0x01) == 0 {
				break
			}
		}
	}

	// Parse Scalability Structure (SS) if V is set
	if desc.V != 0 {
		desc.SSPresent = 1

		if ptr >= len(payload) {
			desc.ByteLength = ptr
			return nil
		}

		ss := payload[ptr]
		ptr++

		nS := (ss >> 5) & 0x07
		y := (ss >> 4) & 0x01
		g := (ss >> 3) & 0x01

		if y != 0 {
			need := int(nS+1) * 4
			if ptr+need > len(payload) {
				desc.ByteLength = ptr
				return nil
			}
			ptr += need
		}

		if g != 0 {
			if ptr >= len(payload) {
				desc.ByteLength = ptr
				return nil
			}

			nG := payload[ptr]
			ptr++

			for i := 0; i < int(nG); i++ {
				if ptr >= len(payload) {
					desc.ByteLength = ptr
					return nil
				}

				pgByte := payload[ptr]
				ptr++

				r := (pgByte >> 1) & 0x03
				if ptr+int(r) > len(payload) {
					desc.ByteLength = ptr
					return nil
				}
				ptr += int(r)
			}
		}
	} else {
		desc.SSPresent = 0
	}

	desc.ByteLength = ptr
	return nil
}

func (desc *PayloadDescriptor) IsKeyframe() bool {
	// P bit set => inter-picture predicted => not a keyframe
	if desc.P != 0 {
		return false
	}

	// No layer indices => P=0 implies keyframe
	if desc.L == 0 {
		return true
	}

	// Layered VP9:
	// Keyframe if base spatial layer, or no interlayer dependency
	return desc.SID == 0 || desc.DBit == 0
}
