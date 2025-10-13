package bot

import (
	"encoding/json"
	"io"
	"os"

	"github.com/pion/rtp"
)

type Recorder struct {
	OutputFile string `json:"output_file"`
	MaxBytes   int64  `json:"max_bytes"`

	totalBytes int64
	saveBuffer []SaveData
}

type SaveData struct {
	SSRC         uint32 `json:"ssrc"`
	SequenceNo   uint16 `json:"sequence_no"`
	RTPTimestamp uint32 `json:"rtp_timestamp"`
	RTPPayload   []byte `json:"rtp_payload"`
}

func New(outputFile string, maxBytes int64, preallocatePackets int) *Recorder {
	r := &Recorder{
		OutputFile: outputFile,
		MaxBytes:   maxBytes,
	}
	if preallocatePackets > 0 {
		r.saveBuffer = make([]SaveData, 0, preallocatePackets)
	}
	return r
}

func LoadFromFile(path string) ([]SaveData, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	var out []SaveData
	for {
		var s SaveData
		if err := dec.Decode(&s); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func (r *Recorder) RecordPacket(pkt *rtp.Packet) bool {
	if pkt == nil {
		return true
	}

	payloadCopy := append([]byte(nil), pkt.Payload...)
	addBytes := int64(len(payloadCopy))

	if r.MaxBytes > 0 && r.totalBytes+addBytes > r.MaxBytes {
		return false
	}

	r.saveBuffer = append(r.saveBuffer, SaveData{
		SSRC:         pkt.SSRC,
		SequenceNo:   pkt.SequenceNumber,
		RTPTimestamp: pkt.Timestamp,
		RTPPayload:   payloadCopy,
	})
	r.totalBytes += addBytes
	return true
}

func (r *Recorder) OnShutdown() error {
	f, err := os.Create(r.OutputFile)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, pkt := range r.saveBuffer {
		if err := enc.Encode(pkt); err != nil {
			return err
		}
	}
	return nil
}
