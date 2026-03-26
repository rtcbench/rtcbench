package vp9

import (
	"bytes"
	"testing"
)

func newTestPayloader() *SVCPayloader {
	return &SVCPayloader{
		Config: SVCLayerConfig{
			NumSpatialLayers:  3,
			NumTemporalLayers: 3,
			Widths:            []uint16{480, 960, 1920},
			Heights:           []uint16{270, 540, 1080},
		},
	}
}

func TestSVCPayloader_KeyframeBaseLayer(t *testing.T) {
	p := newTestPayloader()
	frameData := bytes.Repeat([]byte{0xFF}, 100)

	payloads := p.PayloadSVC(1200, frameData, SVCFrameInfo{
		SID: 0, TID: 0, IsKeyframe: true,
	})
	if len(payloads) != 1 {
		t.Fatalf("got %d payloads, want 1", len(payloads))
	}

	var desc PayloadDescriptor
	if err := ParseVP9PayloadDescriptor(payloads[0], &desc); err != nil {
		t.Fatalf("parse descriptor: %v", err)
	}

	assertBit(t, "I", desc.I, 1)
	assertBit(t, "P", desc.P, 0) // keyframe
	assertBit(t, "L", desc.L, 1)
	assertBit(t, "F", desc.F, 0)
	assertBit(t, "B", desc.B, 1) // first packet
	assertBit(t, "E", desc.E, 1) // last packet (single)
	assertBit(t, "V", desc.V, 1) // SS present on keyframe
	assertBit(t, "SID", desc.SID, 0)
	assertBit(t, "TID", desc.TID, 0)
	assertBit(t, "SSPresent", desc.SSPresent, 1)

	// Verify frame data is at the end
	payload := payloads[0][desc.ByteLength:]
	if !bytes.Equal(payload, frameData) {
		t.Fatalf("frame payload mismatch: got %d bytes, want %d", len(payload), len(frameData))
	}
}

func TestSVCPayloader_NonKeyframe(t *testing.T) {
	p := newTestPayloader()
	frameData := bytes.Repeat([]byte{0xAA}, 50)

	payloads := p.PayloadSVC(1200, frameData, SVCFrameInfo{
		SID: 1, TID: 2, IsKeyframe: false,
	})
	if len(payloads) != 1 {
		t.Fatalf("got %d payloads, want 1", len(payloads))
	}

	var desc PayloadDescriptor
	if err := ParseVP9PayloadDescriptor(payloads[0], &desc); err != nil {
		t.Fatalf("parse descriptor: %v", err)
	}

	assertBit(t, "P", desc.P, 1)   // non-keyframe
	assertBit(t, "L", desc.L, 1)   // always set
	assertBit(t, "V", desc.V, 0)   // no SS on non-keyframe
	assertBit(t, "SID", desc.SID, 1)
	assertBit(t, "TID", desc.TID, 2)
	assertBit(t, "DBit", desc.DBit, 1) // SID>0 → inter-layer dependency
	assertBit(t, "UBit", desc.UBit, 1) // TID>0 → switching up point
}

func TestSVCPayloader_Fragmentation(t *testing.T) {
	p := newTestPayloader()

	// 2000 bytes of frame data with MTU=1200 should fragment into 2 packets.
	// Header is 5 bytes base + SS (~13 bytes for keyframe) = 18 on first, 5 on rest.
	// First packet: 1200 - 18 = 1182 bytes of payload
	// Second packet: 2000 - 1182 = 818 bytes + 5 header = 823
	frameData := bytes.Repeat([]byte{0xBB}, 2000)

	payloads := p.PayloadSVC(1200, frameData, SVCFrameInfo{
		SID: 0, TID: 0, IsKeyframe: true,
	})
	if len(payloads) < 2 {
		t.Fatalf("expected at least 2 fragments, got %d", len(payloads))
	}

	// First packet: B=1, E=0
	var desc0 PayloadDescriptor
	if err := ParseVP9PayloadDescriptor(payloads[0], &desc0); err != nil {
		t.Fatalf("parse first: %v", err)
	}
	assertBit(t, "B", desc0.B, 1)
	assertBit(t, "E", desc0.E, 0)
	assertBit(t, "V", desc0.V, 1) // SS only on first

	// Last packet: B=0, E=1
	var descN PayloadDescriptor
	last := payloads[len(payloads)-1]
	if err := ParseVP9PayloadDescriptor(last, &descN); err != nil {
		t.Fatalf("parse last: %v", err)
	}
	assertBit(t, "B", descN.B, 0)
	assertBit(t, "E", descN.E, 1)
	assertBit(t, "V", descN.V, 0) // no SS on continuation

	// Verify all fragments reconstruct the original frame
	var reassembled []byte
	for _, pl := range payloads {
		var d PayloadDescriptor
		ParseVP9PayloadDescriptor(pl, &d)
		reassembled = append(reassembled, pl[d.ByteLength:]...)
	}
	if !bytes.Equal(reassembled, frameData) {
		t.Fatalf("reassembled data length %d != original %d", len(reassembled), len(frameData))
	}
}

func TestSVCPayloader_PictureIDAdvance(t *testing.T) {
	p := newTestPayloader()
	frameData := []byte{0x01}

	// First access unit: PID=0
	payloads := p.PayloadSVC(1200, frameData, SVCFrameInfo{SID: 0, TID: 0, IsKeyframe: true})
	var desc0 PayloadDescriptor
	ParseVP9PayloadDescriptor(payloads[0], &desc0)
	if desc0.PictureID != 0 {
		t.Fatalf("first PID: got %d, want 0", desc0.PictureID)
	}

	p.AdvancePicture(0)

	// Second access unit: PID=1
	payloads = p.PayloadSVC(1200, frameData, SVCFrameInfo{SID: 0, TID: 1, IsKeyframe: false})
	var desc1 PayloadDescriptor
	ParseVP9PayloadDescriptor(payloads[0], &desc1)
	if desc1.PictureID != 1 {
		t.Fatalf("second PID: got %d, want 1", desc1.PictureID)
	}
}

func TestSVCPayloader_TL0PicIdx(t *testing.T) {
	p := newTestPayloader()
	frameData := []byte{0x01}

	// TID=0 → TL0PICIDX should increment
	p.PayloadSVC(1200, frameData, SVCFrameInfo{SID: 0, TID: 0, IsKeyframe: true})
	p.AdvancePicture(0) // TID=0 → tl0PicIdx becomes 1

	payloads := p.PayloadSVC(1200, frameData, SVCFrameInfo{SID: 0, TID: 0, IsKeyframe: false})
	var desc PayloadDescriptor
	ParseVP9PayloadDescriptor(payloads[0], &desc)
	if desc.TL0PicIdx != 1 {
		t.Fatalf("TL0PICIDX: got %d, want 1", desc.TL0PicIdx)
	}

	p.AdvancePicture(0) // TID=0 again → tl0PicIdx becomes 2

	// TID=2 → TL0PICIDX should NOT increment
	p.PayloadSVC(1200, frameData, SVCFrameInfo{SID: 0, TID: 2, IsKeyframe: false})
	p.AdvancePicture(2) // TID=2 → tl0PicIdx stays at 2

	payloads = p.PayloadSVC(1200, frameData, SVCFrameInfo{SID: 0, TID: 0, IsKeyframe: false})
	ParseVP9PayloadDescriptor(payloads[0], &desc)
	if desc.TL0PicIdx != 2 {
		t.Fatalf("TL0PICIDX after TID=2: got %d, want 2", desc.TL0PicIdx)
	}
}

func TestSVCPayloader_EmptyData(t *testing.T) {
	p := newTestPayloader()
	payloads := p.PayloadSVC(1200, nil, SVCFrameInfo{})
	if payloads != nil {
		t.Fatalf("expected nil for empty data, got %d payloads", len(payloads))
	}
}

func TestSVCPayloader_SSResolutions(t *testing.T) {
	p := newTestPayloader()
	frameData := bytes.Repeat([]byte{0x01}, 10)

	payloads := p.PayloadSVC(1200, frameData, SVCFrameInfo{
		SID: 0, TID: 0, IsKeyframe: true,
	})

	var desc PayloadDescriptor
	ParseVP9PayloadDescriptor(payloads[0], &desc)
	assertBit(t, "V", desc.V, 1)

	// The SS data starts at byte 5 (after flags + PID + layer + TL0PICIDX).
	// SS format: [N_S|Y|G|---] then [W_hi, W_lo, H_hi, H_lo] * (N_S+1)
	ssOff := 5
	ssHeader := payloads[0][ssOff]
	nS := int(ssHeader>>5) + 1
	if nS != 3 {
		t.Fatalf("N_S+1: got %d, want 3", nS)
	}
	yBit := (ssHeader >> 4) & 1
	if yBit != 1 {
		t.Fatal("Y bit should be 1")
	}

	// Parse resolutions
	off := ssOff + 1
	for i, wantW := range []uint16{480, 960, 1920} {
		w := uint16(payloads[0][off])<<8 | uint16(payloads[0][off+1])
		if w != wantW {
			t.Fatalf("layer %d width: got %d, want %d", i, w, wantW)
		}
		off += 4 // skip height too
	}
	off = ssOff + 1 + 2 // skip first width
	for i, wantH := range []uint16{270, 540, 1080} {
		h := uint16(payloads[0][off])<<8 | uint16(payloads[0][off+1])
		if h != wantH {
			t.Fatalf("layer %d height: got %d, want %d", i, h, wantH)
		}
		off += 4 // next layer
	}
}

func assertBit(t *testing.T, name string, got, want uint8) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: got %d, want %d", name, got, want)
	}
}
