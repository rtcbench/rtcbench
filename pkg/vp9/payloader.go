package vp9

// SVCLayerConfig describes the SVC layer structure for the payloader.
type SVCLayerConfig struct {
	NumSpatialLayers  int
	NumTemporalLayers int
	Widths            []uint16 // resolution per spatial layer (len == NumSpatialLayers)
	Heights           []uint16 // resolution per spatial layer (len == NumSpatialLayers)
}

// SVCFrameInfo describes a single VP9 frame (or sub-frame) with SVC metadata.
type SVCFrameInfo struct {
	SID        uint8
	TID        uint8
	IsKeyframe bool
}

// SVCPayloader creates VP9 RTP payloads with SVC layer indices (L=1).
// It uses non-flexible mode (F=0) to match Chrome's behavior.
type SVCPayloader struct {
	Config      SVCLayerConfig
	pictureID   uint16
	tl0PicIdx   uint8
	initialized bool
}

// PayloadSVC fragments a VP9 frame into RTP payload chunks with SVC layer
// indices in every packet's VP9 payload descriptor.
//
// Header format per packet (non-flexible, L=1):
//
//	Byte 0:   [I=1 | P | L=1 | F=0 | B | E | V | Z]
//	Byte 1-2: [M=1 | PictureID (15-bit)]
//	Byte 3:   [TID(3) | U(1) | SID(3) | D(1)]
//	Byte 4:   [TL0PICIDX]
//	Byte 5+:  [SS data if V=1] [VP9 frame data]
//
// Call AdvancePicture() after all spatial layers of an access unit are payloaded.
func (p *SVCPayloader) PayloadSVC(mtu uint16, data []byte, info SVCFrameInfo) [][]byte {
	if !p.initialized {
		p.pictureID = 0
		p.initialized = true
	}

	if len(data) == 0 {
		return nil
	}

	// Include Scalability Structure on keyframe base layer, first packet only
	includeSSOnFirst := info.IsKeyframe && info.SID == 0

	ssData := p.buildSS()

	// Base header: 1 (flags) + 2 (PictureID) + 1 (layer) + 1 (TL0PICIDX) = 5
	const baseHeaderSize = 5
	firstHeaderSize := baseHeaderSize
	if includeSSOnFirst {
		firstHeaderSize += len(ssData)
	}

	payloadRemaining := len(data)
	payloadIndex := 0
	var payloads [][]byte

	for payloadRemaining > 0 {
		isFirst := payloadIndex == 0

		headerSize := baseHeaderSize
		if isFirst && includeSSOnFirst {
			headerSize = firstHeaderSize
		}

		maxFragment := int(mtu) - headerSize
		if maxFragment <= 0 {
			break
		}
		fragSize := min(payloadRemaining, maxFragment)
		isLast := fragSize == payloadRemaining

		out := make([]byte, headerSize+fragSize)

		// Byte 0: I=1, L=1 (always); P, B, E, V conditional
		out[0] = 0x80 | 0x20 // I=1, L=1
		if !info.IsKeyframe {
			out[0] |= 0x40 // P=1 (inter-picture predicted)
		}
		if isFirst {
			out[0] |= 0x08 // B=1 (start of frame)
		}
		if isLast {
			out[0] |= 0x04 // E=1 (end of frame)
		}
		if isFirst && includeSSOnFirst {
			out[0] |= 0x02 // V=1 (scalability structure present)
		}

		// Bytes 1-2: PictureID (15-bit, M=1)
		out[1] = byte(p.pictureID>>8) | 0x80
		out[2] = byte(p.pictureID)

		// Byte 3: [TID(3) | U(1) | SID(3) | D(1)]
		var uBit, dBit uint8
		if info.TID > 0 {
			uBit = 1 // switching up point for enhancement temporal layers
		}
		if info.SID > 0 {
			dBit = 1 // inter-layer dependency
		}
		out[3] = (info.TID << 5) | (uBit << 4) | (info.SID << 1) | dBit

		// Byte 4: TL0PICIDX
		out[4] = p.tl0PicIdx

		off := 5

		// Scalability Structure (V=1)
		if isFirst && includeSSOnFirst {
			copy(out[off:], ssData)
			off += len(ssData)
		}

		// VP9 frame payload
		copy(out[off:], data[payloadIndex:payloadIndex+fragSize])

		payloads = append(payloads, out)
		payloadRemaining -= fragSize
		payloadIndex += fragSize
	}

	return payloads
}

// AdvancePicture increments the picture ID and (if TID==0) TL0PICIDX.
// Call once per access unit (after all spatial layers are payloaded).
func (p *SVCPayloader) AdvancePicture(tid uint8) {
	p.pictureID++
	if p.pictureID >= 0x8000 {
		p.pictureID = 0
	}
	if tid == 0 {
		p.tl0PicIdx++
	}
}

// buildSS creates the Scalability Structure bytes for keyframes.
//
//	[N_S(3)|Y(1)|G(0)|reserved(3)] [Width(2)+Height(2)] * (N_S+1)
func (p *SVCPayloader) buildSS() []byte {
	ns := p.Config.NumSpatialLayers
	if ns <= 0 {
		ns = 1
	}

	ssLen := 1 + ns*4 // header + 4 bytes per layer (Y=1)
	ss := make([]byte, ssLen)

	// N_S = numSpatialLayers - 1, Y=1, G=0
	ss[0] = byte((ns-1)<<5) | 0x10

	off := 1
	for i := range ns {
		var w, h uint16
		if i < len(p.Config.Widths) {
			w = p.Config.Widths[i]
		}
		if i < len(p.Config.Heights) {
			h = p.Config.Heights[i]
		}
		ss[off] = byte(w >> 8)
		ss[off+1] = byte(w)
		ss[off+2] = byte(h >> 8)
		ss[off+3] = byte(h)
		off += 4
	}

	return ss
}
