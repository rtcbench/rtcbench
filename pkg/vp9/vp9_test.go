package vp9

import (
	"errors"
	"testing"
)

func TestParseVP9PayloadDescriptor_EmptyPayload(t *testing.T) {
	var desc PayloadDescriptor

	// nil payload
	err := ParseVP9PayloadDescriptor(nil, &desc)
	if !errors.Is(err, ErrEmptyPayload) {
		t.Fatalf("nil payload: got err=%v, want ErrEmptyPayload", err)
	}

	// zero-length payload
	err = ParseVP9PayloadDescriptor([]byte{}, &desc)
	if !errors.Is(err, ErrEmptyPayload) {
		t.Fatalf("empty payload: got err=%v, want ErrEmptyPayload", err)
	}
}

func TestParseVP9PayloadDescriptor_MinimalOneByte(t *testing.T) {
	// First byte bit layout: I P L F B E V Z  (bits 7..0)
	tests := []struct {
		name    string
		b       byte
		wantI   uint8
		wantP   uint8
		wantL   uint8
		wantF   uint8
		wantB   uint8
		wantE   uint8
		wantV   uint8
		wantZ   uint8
	}{
		{
			name: "all zeros",
			b:    0x00, // 0000_0000
		},
		{
			name: "all ones",
			// I=1 P=1 L=1 F=1 B=1 E=1 V=1 Z=1 = 0xFF
			// But this is only 1 byte, so optional fields will be absent.
			// Parser hits ptr>=len(payload) and returns early.
			b:     0xFF,
			wantI: 1, wantP: 1, wantL: 1, wantF: 1,
			wantB: 1, wantE: 1, wantV: 1, wantZ: 1,
		},
		{
			name:  "only I set",
			b:     0x80, // 1000_0000
			wantI: 1,
		},
		{
			name:  "only P set",
			b:     0x40, // 0100_0000
			wantP: 1,
		},
		{
			name:  "only L set",
			b:     0x20, // 0010_0000
			wantL: 1,
		},
		{
			name:  "only F set",
			b:     0x10, // 0001_0000
			wantF: 1,
		},
		{
			name:  "only B set",
			b:     0x08, // 0000_1000
			wantB: 1,
		},
		{
			name:  "only E set",
			b:     0x04, // 0000_0100
			wantE: 1,
		},
		{
			name:  "only V set",
			b:     0x02, // 0000_0010
			wantV: 1,
		},
		{
			name:  "only Z set",
			b:     0x01, // 0000_0001
			wantZ: 1,
		},
		{
			name:  "B and E set",
			b:     0x0C, // 0000_1100
			wantB: 1, wantE: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var desc PayloadDescriptor
			err := ParseVP9PayloadDescriptor([]byte{tc.b}, &desc)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if desc.I != tc.wantI {
				t.Fatalf("I: got %d, want %d", desc.I, tc.wantI)
			}
			if desc.P != tc.wantP {
				t.Fatalf("P: got %d, want %d", desc.P, tc.wantP)
			}
			if desc.L != tc.wantL {
				t.Fatalf("L: got %d, want %d", desc.L, tc.wantL)
			}
			if desc.F != tc.wantF {
				t.Fatalf("F: got %d, want %d", desc.F, tc.wantF)
			}
			if desc.B != tc.wantB {
				t.Fatalf("B: got %d, want %d", desc.B, tc.wantB)
			}
			if desc.E != tc.wantE {
				t.Fatalf("E: got %d, want %d", desc.E, tc.wantE)
			}
			if desc.V != tc.wantV {
				t.Fatalf("V: got %d, want %d", desc.V, tc.wantV)
			}
			if desc.Z != tc.wantZ {
				t.Fatalf("Z: got %d, want %d", desc.Z, tc.wantZ)
			}
			if desc.ByteLength != 1 {
				t.Fatalf("ByteLength: got %d, want 1", desc.ByteLength)
			}
		})
	}
}

func TestParseVP9PayloadDescriptor_PictureID(t *testing.T) {
	tests := []struct {
		name          string
		payload       []byte
		wantPictureID uint16
		wantM         uint8
		wantByteLen   int
	}{
		{
			// 7-bit PictureID (M=0)
			// Byte 0: I=1 P=0 L=0 F=0 B=0 E=0 V=0 Z=0 = 0x80
			// Byte 1: M=0, PictureID[6:0] = 0x42 = 66
			// Byte 2: dummy payload byte so parser doesn't truncate
			name:          "7-bit PictureID",
			payload:       []byte{0x80, 0x42, 0x00},
			wantPictureID: 0x42,
			wantM:         0,
			wantByteLen:   2,
		},
		{
			// 15-bit PictureID (M=1)
			// Byte 0: I=1 P=0 L=0 F=0 B=0 E=0 V=0 Z=0 = 0x80
			// Byte 1: M=1, PictureID[14:8] = 0x01 => byte = 0x81
			// Byte 2: PictureID[7:0] = 0x23
			// Combined: (0x01 << 8) | 0x23 = 0x0123
			// Byte 3: dummy payload byte
			name:          "15-bit PictureID",
			payload:       []byte{0x80, 0x81, 0x23, 0x00},
			wantPictureID: 0x0123,
			wantM:         1,
			wantByteLen:   3,
		},
		{
			// 7-bit PictureID = 0 (M=0)
			// Byte 0: I=1 = 0x80
			// Byte 1: M=0, PictureID = 0x00
			// Byte 2: dummy
			name:          "7-bit PictureID zero",
			payload:       []byte{0x80, 0x00, 0x00},
			wantPictureID: 0,
			wantM:         0,
			wantByteLen:   2,
		},
		{
			// 7-bit PictureID max = 127 (M=0)
			// Byte 0: I=1 = 0x80
			// Byte 1: M=0, PictureID = 0x7F
			// Byte 2: dummy
			name:          "7-bit PictureID max",
			payload:       []byte{0x80, 0x7F, 0x00},
			wantPictureID: 0x7F,
			wantM:         0,
			wantByteLen:   2,
		},
		{
			// 15-bit PictureID max = 0x7FFF (M=1)
			// Byte 0: I=1 = 0x80
			// Byte 1: M=1, PictureID[14:8] = 0x7F => byte = 0xFF
			// Byte 2: PictureID[7:0] = 0xFF
			// Combined: (0x7F << 8) | 0xFF = 0x7FFF
			// Byte 3: dummy
			name:          "15-bit PictureID max",
			payload:       []byte{0x80, 0xFF, 0xFF, 0x00},
			wantPictureID: 0x7FFF,
			wantM:         1,
			wantByteLen:   3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var desc PayloadDescriptor
			err := ParseVP9PayloadDescriptor(tc.payload, &desc)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if desc.PictureID != tc.wantPictureID {
				t.Fatalf("PictureID: got 0x%04X, want 0x%04X", desc.PictureID, tc.wantPictureID)
			}
			if desc.M != tc.wantM {
				t.Fatalf("M: got %d, want %d", desc.M, tc.wantM)
			}
			if desc.ByteLength != tc.wantByteLen {
				t.Fatalf("ByteLength: got %d, want %d", desc.ByteLength, tc.wantByteLen)
			}
		})
	}
}

func TestParseVP9PayloadDescriptor_LayerIndices(t *testing.T) {
	tests := []struct {
		name          string
		payload       []byte
		wantTID       uint8
		wantUBit      uint8
		wantSID       uint8
		wantDBit      uint8
		wantTL0PicIdx uint8
		wantByteLen   int
	}{
		{
			// L=1, F=0 => layer byte + TL0PicIdx byte present
			// Byte 0: I=0 P=0 L=1 F=0 B=0 E=0 V=0 Z=0 = 0x20
			// Byte 1: layer byte
			//   TID[2:0]=5 (101), UBit=1, SID[2:0]=3 (011), DBit=1
			//   = 1011_0111 = 0xB7
			// Byte 2: TL0PicIdx = 0xAB
			// Byte 3: dummy payload
			name:          "L=1 F=0 with TL0PicIdx",
			payload:       []byte{0x20, 0xB7, 0xAB, 0x00},
			wantTID:       5,
			wantUBit:      1,
			wantSID:       3,
			wantDBit:      1,
			wantTL0PicIdx: 0xAB,
			wantByteLen:   3,
		},
		{
			// L=1, F=1 => layer byte present, no TL0PicIdx
			// Byte 0: I=0 P=0 L=1 F=1 B=0 E=0 V=0 Z=0 = 0x30
			// Byte 1: layer byte
			//   TID=2 (010), UBit=0, SID=1 (001), DBit=0
			//   = 0100_0010 = 0x42
			// Byte 2: dummy payload
			name:          "L=1 F=1 without TL0PicIdx",
			payload:       []byte{0x30, 0x42, 0x00},
			wantTID:       2,
			wantUBit:      0,
			wantSID:       1,
			wantDBit:      0,
			wantTL0PicIdx: 0, // not parsed
			wantByteLen:   2,
		},
		{
			// L=1, F=0, all layer fields zero
			// Byte 0: L=1 = 0x20
			// Byte 1: TID=0 U=0 SID=0 D=0 = 0x00
			// Byte 2: TL0PicIdx = 0x00
			// Byte 3: dummy
			name:          "L=1 F=0 all zeros",
			payload:       []byte{0x20, 0x00, 0x00, 0x00},
			wantTID:       0,
			wantUBit:      0,
			wantSID:       0,
			wantDBit:      0,
			wantTL0PicIdx: 0,
			wantByteLen:   3,
		},
		{
			// L=1, F=0, max layer values
			// Byte 0: L=1 = 0x20
			// Byte 1: TID=7 (111), U=1, SID=7 (111), D=1 = 1111_1111 = 0xFF
			// Byte 2: TL0PicIdx = 0xFF
			// Byte 3: dummy
			name:          "L=1 F=0 max values",
			payload:       []byte{0x20, 0xFF, 0xFF, 0x00},
			wantTID:       7,
			wantUBit:      1,
			wantSID:       7,
			wantDBit:      1,
			wantTL0PicIdx: 0xFF,
			wantByteLen:   3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var desc PayloadDescriptor
			err := ParseVP9PayloadDescriptor(tc.payload, &desc)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if desc.TID != tc.wantTID {
				t.Fatalf("TID: got %d, want %d", desc.TID, tc.wantTID)
			}
			if desc.UBit != tc.wantUBit {
				t.Fatalf("UBit: got %d, want %d", desc.UBit, tc.wantUBit)
			}
			if desc.SID != tc.wantSID {
				t.Fatalf("SID: got %d, want %d", desc.SID, tc.wantSID)
			}
			if desc.DBit != tc.wantDBit {
				t.Fatalf("DBit: got %d, want %d", desc.DBit, tc.wantDBit)
			}
			if desc.TL0PicIdx != tc.wantTL0PicIdx {
				t.Fatalf("TL0PicIdx: got %d, want %d", desc.TL0PicIdx, tc.wantTL0PicIdx)
			}
			if desc.ByteLength != tc.wantByteLen {
				t.Fatalf("ByteLength: got %d, want %d", desc.ByteLength, tc.wantByteLen)
			}
		})
	}
}

func TestParseVP9PayloadDescriptor_ReferenceIndices(t *testing.T) {
	// Reference indices are parsed when P=1 and F=1.
	// Each pdiff byte: bits[7:1]=pdiff value, bit[0]=continuation flag.
	// Up to 3 pdiff bytes. Stops early if continuation bit is 0.

	tests := []struct {
		name        string
		payload     []byte
		wantByteLen int
	}{
		{
			// 1 pdiff byte, continuation=0 (stops after first)
			// Byte 0: I=0 P=1 L=0 F=1 B=0 E=0 V=0 Z=0 = 0x50
			// Byte 1: pdiff=0x10 (value=8), continuation=0 => 0x10
			// Byte 2: dummy payload
			name:        "1 pdiff byte",
			payload:     []byte{0x50, 0x10, 0x00},
			wantByteLen: 2,
		},
		{
			// 2 pdiff bytes: first has continuation=1, second has continuation=0
			// Byte 0: P=1 F=1 = 0x50
			// Byte 1: pdiff continuation=1 => 0x11
			// Byte 2: pdiff continuation=0 => 0x20
			// Byte 3: dummy
			name:        "2 pdiff bytes",
			payload:     []byte{0x50, 0x11, 0x20, 0x00},
			wantByteLen: 3,
		},
		{
			// 3 pdiff bytes: first two have continuation=1, third stops regardless
			// Byte 0: P=1 F=1 = 0x50
			// Byte 1: pdiff continuation=1 => 0x11
			// Byte 2: pdiff continuation=1 => 0x21
			// Byte 3: pdiff continuation=1 => 0x31 (loop ends at i=2, max 3 iterations)
			// Byte 4: dummy
			name:        "3 pdiff bytes max",
			payload:     []byte{0x50, 0x11, 0x21, 0x31, 0x00},
			wantByteLen: 4,
		},
		{
			// 3 pdiff bytes: all have continuation=0 on third
			// Byte 0: P=1 F=1 = 0x50
			// Byte 1: continuation=1 => 0x01
			// Byte 2: continuation=1 => 0x01
			// Byte 3: continuation=0 => 0x00
			// Byte 4: dummy
			name:        "3 pdiff bytes last stops",
			payload:     []byte{0x50, 0x01, 0x01, 0x00, 0x00},
			wantByteLen: 4,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var desc PayloadDescriptor
			err := ParseVP9PayloadDescriptor(tc.payload, &desc)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if desc.P != 1 {
				t.Fatalf("P: got %d, want 1", desc.P)
			}
			if desc.F != 1 {
				t.Fatalf("F: got %d, want 1", desc.F)
			}
			if desc.ByteLength != tc.wantByteLen {
				t.Fatalf("ByteLength: got %d, want %d", desc.ByteLength, tc.wantByteLen)
			}
		})
	}
}

func TestParseVP9PayloadDescriptor_ScalabilityStructure(t *testing.T) {
	// V=1 triggers SS parsing.
	// SS byte: N_S[2:0] (bits 7-5), Y (bit 4), G (bit 3), reserved (bits 2-0)
	// If Y=1: (N_S+1) * 4 bytes of resolution data follow
	// If G=1: 1 byte N_G, then N_G PG entries (each 1 byte + R reference bytes)

	tests := []struct {
		name        string
		payload     []byte
		wantByteLen int
		wantSS      uint8
	}{
		{
			// V=1, Y=0, G=0 => just the SS byte, no resolution or PG data
			// Byte 0: I=0 P=0 L=0 F=0 B=0 E=0 V=1 Z=0 = 0x02
			// Byte 1: SS byte: N_S=0, Y=0, G=0 = 0x00
			// Byte 2: dummy
			name:        "V=1 Y=0 G=0",
			payload:     []byte{0x02, 0x00, 0x00},
			wantByteLen: 2,
			wantSS:      1,
		},
		{
			// V=1, Y=1, G=0, N_S=0 => 1 spatial layer, 4 bytes resolution data
			// Byte 0: V=1 = 0x02
			// Byte 1: SS: N_S=0 (000), Y=1, G=0 = 0001_0000 = 0x10
			// Bytes 2-5: resolution data (WIDTH_0_HI, WIDTH_0_LO, HEIGHT_0_HI, HEIGHT_0_LO)
			// Byte 6: dummy
			name:        "V=1 Y=1 G=0 N_S=0",
			payload:     []byte{0x02, 0x10, 0x07, 0x80, 0x04, 0x38, 0x00},
			wantByteLen: 6,
			wantSS:      1,
		},
		{
			// V=1, Y=1, G=0, N_S=2 => 3 spatial layers, 12 bytes resolution data
			// Byte 0: V=1 = 0x02
			// Byte 1: SS: N_S=2 (010), Y=1, G=0 = 0101_0000 = 0x50
			// Bytes 2-13: 12 bytes of resolution data (3 layers * 4 bytes)
			// Byte 14: dummy
			name: "V=1 Y=1 G=0 N_S=2",
			payload: []byte{
				0x02, 0x50,
				0x01, 0xE0, 0x01, 0x0E, // layer 0 resolution
				0x03, 0xC0, 0x02, 0x1C, // layer 1 resolution
				0x07, 0x80, 0x04, 0x38, // layer 2 resolution
				0x00, // dummy
			},
			wantByteLen: 14,
			wantSS:      1,
		},
		{
			// V=1, Y=0, G=1 => PG data present
			// Byte 0: V=1 = 0x02
			// Byte 1: SS: N_S=0, Y=0, G=1 = 0000_1000 = 0x08
			// Byte 2: N_G = 2
			// Byte 3: PG[0]: TID=0 U=0 R=0 reserved = 0000_0000 = 0x00 (R=0, no ref bytes)
			// Byte 4: PG[1]: TID=1 U=0 R=1 reserved = 0010_0100 = 0x24 (R=1)
			//   R field = bits[3:2] = 01 = 1
			// Byte 5: reference byte for PG[1]
			// Byte 6: dummy
			name:        "V=1 Y=0 G=1 with PG entries",
			payload:     []byte{0x02, 0x08, 0x02, 0x00, 0x24, 0x01, 0x00},
			wantByteLen: 6,
			wantSS:      1,
		},
		{
			// V=1, Y=1, G=1, N_S=0 => resolution data + PG data
			// Byte 0: V=1 = 0x02
			// Byte 1: SS: N_S=0, Y=1, G=1 = 0001_1000 = 0x18
			// Bytes 2-5: 4 bytes resolution (1 layer)
			// Byte 6: N_G = 1
			// Byte 7: PG[0]: R=0 => 0x00
			// Byte 8: dummy
			name:        "V=1 Y=1 G=1 N_S=0",
			payload:     []byte{0x02, 0x18, 0x07, 0x80, 0x04, 0x38, 0x01, 0x00, 0x00},
			wantByteLen: 8,
			wantSS:      1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var desc PayloadDescriptor
			err := ParseVP9PayloadDescriptor(tc.payload, &desc)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if desc.SSPresent != tc.wantSS {
				t.Fatalf("SSPresent: got %d, want %d", desc.SSPresent, tc.wantSS)
			}
			if desc.ByteLength != tc.wantByteLen {
				t.Fatalf("ByteLength: got %d, want %d", desc.ByteLength, tc.wantByteLen)
			}
		})
	}
}

func TestParseVP9PayloadDescriptor_TruncatedPayloads(t *testing.T) {
	// Start with a known-good payload that exercises multiple fields:
	// I=1, P=0, L=1, F=0, B=1, E=0, V=1, Z=0
	// Byte 0: 1010_1010 = 0xAA
	// Byte 1: PictureID M=1, upper=0x02 => 0x82
	// Byte 2: PictureID lower = 0x34
	// Byte 3: Layer byte: TID=1 U=0 SID=0 D=0 = 0010_0000 = 0x20
	// Byte 4: TL0PicIdx = 0x55 (present because F=0)
	// Byte 5: SS byte: N_S=0, Y=0, G=0 = 0x00
	// Byte 6: dummy payload
	fullPayload := []byte{0xAA, 0x82, 0x34, 0x20, 0x55, 0x00, 0x00}

	tests := []struct {
		name        string
		length      int // truncate fullPayload to this length
		wantByteLen int
	}{
		// 1 byte: only first byte parsed, ptr=1 hits end
		{"truncated after first byte", 1, 1},
		// 2 bytes: I=1, reads PictureID byte 0x82 (M=1), ptr=2 hits end
		// before reading second PictureID byte
		{"truncated mid PictureID", 2, 2},
		// 3 bytes: PictureID fully parsed (15-bit), ptr=3 hits end
		// before reading layer byte
		{"truncated before layer byte", 3, 3},
		// 4 bytes: layer byte parsed, ptr=4 hits end before TL0PicIdx
		{"truncated before TL0PicIdx", 4, 4},
		// 5 bytes: TL0PicIdx parsed, ptr=5 hits end before SS
		{"truncated before SS", 5, 5},
		// 6 bytes: SS byte parsed (Y=0,G=0), ptr=6 = full descriptor
		{"truncated after SS", 6, 6},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			truncated := fullPayload[:tc.length]
			var desc PayloadDescriptor
			err := ParseVP9PayloadDescriptor(truncated, &desc)
			if err != nil {
				t.Fatalf("unexpected error on truncated payload (len=%d): %v", tc.length, err)
			}
			if desc.ByteLength != tc.wantByteLen {
				t.Fatalf("ByteLength: got %d, want %d", desc.ByteLength, tc.wantByteLen)
			}
		})
	}
}

func TestParseVP9PayloadDescriptor_ZerosOutPreviousState(t *testing.T) {
	// First parse: a full payload with many fields set
	// I=1 P=0 L=1 F=0 B=1 E=1 V=0 Z=1 = 1010_1101 = 0xAD
	// PictureID: M=1, upper=0x03 => 0x83, lower=0xFF => PictureID=0x03FF
	// Layer: TID=3 U=1 SID=2 D=1 = 0111_0101 = 0x75
	// TL0PicIdx = 0xCC
	// Dummy payload byte
	fullPayload := []byte{0xAD, 0x83, 0xFF, 0x75, 0xCC, 0x00}

	var desc PayloadDescriptor
	err := ParseVP9PayloadDescriptor(fullPayload, &desc)
	if err != nil {
		t.Fatalf("first parse: unexpected error: %v", err)
	}

	// Verify first parse populated fields
	if desc.PictureID != 0x03FF {
		t.Fatalf("first parse PictureID: got 0x%04X, want 0x03FF", desc.PictureID)
	}
	if desc.TID != 3 {
		t.Fatalf("first parse TID: got %d, want 3", desc.TID)
	}
	if desc.SID != 2 {
		t.Fatalf("first parse SID: got %d, want 2", desc.SID)
	}
	if desc.DBit != 1 {
		t.Fatalf("first parse DBit: got %d, want 1", desc.DBit)
	}
	if desc.TL0PicIdx != 0xCC {
		t.Fatalf("first parse TL0PicIdx: got %d, want 0xCC", desc.TL0PicIdx)
	}

	// Second parse: minimal 1-byte payload — all optional fields should be zeroed
	// Byte 0: all flags zero = 0x00
	err = ParseVP9PayloadDescriptor([]byte{0x00}, &desc)
	if err != nil {
		t.Fatalf("second parse: unexpected error: %v", err)
	}

	// All flags and fields from the first parse should be zeroed
	if desc.I != 0 {
		t.Fatalf("after re-parse I: got %d, want 0", desc.I)
	}
	if desc.L != 0 {
		t.Fatalf("after re-parse L: got %d, want 0", desc.L)
	}
	if desc.PictureID != 0 {
		t.Fatalf("after re-parse PictureID: got 0x%04X, want 0", desc.PictureID)
	}
	if desc.M != 0 {
		t.Fatalf("after re-parse M: got %d, want 0", desc.M)
	}
	if desc.TID != 0 {
		t.Fatalf("after re-parse TID: got %d, want 0", desc.TID)
	}
	if desc.UBit != 0 {
		t.Fatalf("after re-parse UBit: got %d, want 0", desc.UBit)
	}
	if desc.SID != 0 {
		t.Fatalf("after re-parse SID: got %d, want 0", desc.SID)
	}
	if desc.DBit != 0 {
		t.Fatalf("after re-parse DBit: got %d, want 0", desc.DBit)
	}
	if desc.TL0PicIdx != 0 {
		t.Fatalf("after re-parse TL0PicIdx: got %d, want 0", desc.TL0PicIdx)
	}
	if desc.SSPresent != 0 {
		t.Fatalf("after re-parse SSPresent: got %d, want 0", desc.SSPresent)
	}
	if desc.B != 0 {
		t.Fatalf("after re-parse B: got %d, want 0", desc.B)
	}
	if desc.E != 0 {
		t.Fatalf("after re-parse E: got %d, want 0", desc.E)
	}
	if desc.Z != 0 {
		t.Fatalf("after re-parse Z: got %d, want 0", desc.Z)
	}
	if desc.ByteLength != 1 {
		t.Fatalf("after re-parse ByteLength: got %d, want 1", desc.ByteLength)
	}
}

func TestParseVP9PayloadDescriptor_RealisticPayload(t *testing.T) {
	// Simulate a realistic VP9 RTP payload descriptor with I and L fields:
	// Byte 0: I=1 P=0 L=1 F=0 B=1 E=1 V=0 Z=0 = 1010_1100 = 0xAC
	// Byte 1: PictureID M=1, upper=0x00 => 0x80 (M=1, PictureID[14:8]=0)
	// Byte 2: PictureID lower = 0x7A => full PictureID = (0<<8)|0x7A = 0x007A = 122
	// Byte 3: Layer byte: TID=0 U=0 SID=0 D=0 = 0000_0000 = 0x00
	// Byte 4: TL0PicIdx = 0x0F (F=0 so TL0PicIdx is present)
	// Bytes 5+: VP9 frame data (not parsed)
	payload := []byte{0xAC, 0x80, 0x7A, 0x00, 0x0F, 0xDE, 0xAD, 0xBE, 0xEF}

	var desc PayloadDescriptor
	err := ParseVP9PayloadDescriptor(payload, &desc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// First byte flags
	if desc.I != 1 {
		t.Fatalf("I: got %d, want 1", desc.I)
	}
	if desc.P != 0 {
		t.Fatalf("P: got %d, want 0", desc.P)
	}
	if desc.L != 1 {
		t.Fatalf("L: got %d, want 1", desc.L)
	}
	if desc.F != 0 {
		t.Fatalf("F: got %d, want 0", desc.F)
	}
	if desc.B != 1 {
		t.Fatalf("B: got %d, want 1", desc.B)
	}
	if desc.E != 1 {
		t.Fatalf("E: got %d, want 1", desc.E)
	}
	if desc.V != 0 {
		t.Fatalf("V: got %d, want 0", desc.V)
	}
	if desc.Z != 0 {
		t.Fatalf("Z: got %d, want 0", desc.Z)
	}

	// PictureID
	if desc.M != 1 {
		t.Fatalf("M: got %d, want 1", desc.M)
	}
	if desc.PictureID != 0x007A {
		t.Fatalf("PictureID: got 0x%04X, want 0x007A", desc.PictureID)
	}

	// Layer indices
	if desc.TID != 0 {
		t.Fatalf("TID: got %d, want 0", desc.TID)
	}
	if desc.UBit != 0 {
		t.Fatalf("UBit: got %d, want 0", desc.UBit)
	}
	if desc.SID != 0 {
		t.Fatalf("SID: got %d, want 0", desc.SID)
	}
	if desc.DBit != 0 {
		t.Fatalf("DBit: got %d, want 0", desc.DBit)
	}
	if desc.TL0PicIdx != 0x0F {
		t.Fatalf("TL0PicIdx: got %d, want 0x0F", desc.TL0PicIdx)
	}

	// No SS
	if desc.SSPresent != 0 {
		t.Fatalf("SSPresent: got %d, want 0", desc.SSPresent)
	}

	// Descriptor ends at byte 5; the VP9 frame data starts after
	if desc.ByteLength != 5 {
		t.Fatalf("ByteLength: got %d, want 5", desc.ByteLength)
	}
}

func TestPayloadDescriptor_IsKeyframe(t *testing.T) {
	tests := []struct {
		name string
		desc PayloadDescriptor
		want bool
	}{
		{
			// P=1 means inter-picture predicted, never a keyframe
			name: "P=1 always false",
			desc: PayloadDescriptor{P: 1},
			want: false,
		},
		{
			// P=1 even with L=0 is still not a keyframe
			name: "P=1 L=0 still false",
			desc: PayloadDescriptor{P: 1, L: 0},
			want: false,
		},
		{
			// P=1 with L=1 SID=0 DBit=0 is still not a keyframe (P overrides)
			name: "P=1 L=1 SID=0 DBit=0 still false",
			desc: PayloadDescriptor{P: 1, L: 1, SID: 0, DBit: 0},
			want: false,
		},
		{
			// P=0, L=0 => keyframe (no layer info, P=0 implies keyframe)
			name: "P=0 L=0 true",
			desc: PayloadDescriptor{P: 0, L: 0},
			want: true,
		},
		{
			// P=0, L=1, SID=0, DBit=0 => keyframe (base spatial layer)
			name: "P=0 L=1 SID=0 DBit=0 true",
			desc: PayloadDescriptor{P: 0, L: 1, SID: 0, DBit: 0},
			want: true,
		},
		{
			// P=0, L=1, SID=0, DBit=1 => true (SID==0 short-circuits)
			name: "P=0 L=1 SID=0 DBit=1 true",
			desc: PayloadDescriptor{P: 0, L: 1, SID: 0, DBit: 1},
			want: true,
		},
		{
			// P=0, L=1, SID=1, DBit=0 => true (no interlayer dependency)
			name: "P=0 L=1 SID=1 DBit=0 true",
			desc: PayloadDescriptor{P: 0, L: 1, SID: 1, DBit: 0},
			want: true,
		},
		{
			// P=0, L=1, SID=1, DBit=1 => false (higher spatial layer with dependency)
			name: "P=0 L=1 SID=1 DBit=1 false",
			desc: PayloadDescriptor{P: 0, L: 1, SID: 1, DBit: 1},
			want: false,
		},
		{
			// P=0, L=1, SID=2, DBit=0 => true
			name: "P=0 L=1 SID=2 DBit=0 true",
			desc: PayloadDescriptor{P: 0, L: 1, SID: 2, DBit: 0},
			want: true,
		},
		{
			// P=0, L=1, SID=2, DBit=1 => false
			name: "P=0 L=1 SID=2 DBit=1 false",
			desc: PayloadDescriptor{P: 0, L: 1, SID: 2, DBit: 1},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.desc.IsKeyframe()
			if got != tc.want {
				t.Fatalf("IsKeyframe(): got %v, want %v (P=%d L=%d SID=%d DBit=%d)",
					got, tc.want, tc.desc.P, tc.desc.L, tc.desc.SID, tc.desc.DBit)
			}
		})
	}
}
