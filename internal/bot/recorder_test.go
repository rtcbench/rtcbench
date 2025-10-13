package bot

import (
	"bytes"
	"os"
	"reflect"
	"testing"

	"github.com/pion/rtp"
)

func newPkt(ssrc uint32, seq uint16, ts uint32, payload []byte) *rtp.Packet {
	return &rtp.Packet{
		Header: rtp.Header{
			SSRC:           ssrc,
			SequenceNumber: seq,
			Timestamp:      ts,
		},
		Payload: append([]byte(nil), payload...),
	}
}

func TestRecordPacket_Capacity(t *testing.T) {
	tmp := t.TempDir()
	path := tmp + "/out.ndjson"

	r := New(path, 5, 0) // 5 bytes max

	// 4-byte payload fits
	ok := r.RecordPacket(newPkt(1, 1, 100, []byte("abcd")))
	if !ok {
		t.Fatalf("expected first packet to be accepted")
	}

	// 1-byte payload should still fit exactly
	ok = r.RecordPacket(newPkt(1, 2, 101, []byte("x")))
	if !ok {
		t.Fatalf("expected second packet to be accepted to hit exact cap")
	}

	// Additional 1-byte packet should be rejected
	ok = r.RecordPacket(newPkt(1, 3, 102, []byte("y")))
	if ok {
		t.Fatalf("expected third packet to be rejected due to capacity")
	}

	if got, want := len(r.saveBuffer), 2; got != want {
		t.Fatalf("buffer length = %d, want %d", got, want)
	}
	if got, want := r.totalBytes, int64(5); got != want {
		t.Fatalf("totalBytes = %d, want %d", got, want)
	}
}

func TestRecordPacket_NilIsNoop(t *testing.T) {
	r := New("", 10, 0)
	if !r.RecordPacket(nil) {
		t.Fatalf("nil packet should be treated as success no-op")
	}
	if got := len(r.saveBuffer); got != 0 {
		t.Fatalf("buffer length = %d, want 0", got)
	}
	if got := r.totalBytes; got != 0 {
		t.Fatalf("totalBytes = %d, want 0", got)
	}
}

func TestOnShutdownAndLoad_RoundTrip(t *testing.T) {
	tmp := t.TempDir()
	path := tmp + "/packets.json"

	r := New(path, 0, 3)

	p1 := newPkt(111, 10, 1000, []byte{0x01, 0x02, 0x03})
	p2 := newPkt(222, 11, 1001, []byte("hello"))
	p3 := newPkt(333, 12, 1002, []byte{0xFF, 0x00})

	if !r.RecordPacket(p1) || !r.RecordPacket(p2) || !r.RecordPacket(p3) {
		t.Fatalf("unexpected rejection while recording packets (cap is unlimited)")
	}

	if err := r.OnShutdown(); err != nil {
		t.Fatalf("OnShutdown error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading output file: %v", err)
	}

	// NDJSON: number of lines should equal number of packets
	if got := bytes.Count(data, []byte{'\n'}); got != 3 {
		t.Fatalf("lines in NDJSON = %d, want 3", got)
	}

	loaded, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile error: %v", err)
	}
	if len(loaded) != 3 {
		t.Fatalf("loaded length = %d, want 3", len(loaded))
	}

	// Verify fields and payloads
	want := []SaveData{
		{SSRC: 111, SequenceNo: 10, RTPTimestamp: 1000, RTPPayload: p1.Payload},
		{SSRC: 222, SequenceNo: 11, RTPTimestamp: 1001, RTPPayload: p2.Payload},
		{SSRC: 333, SequenceNo: 12, RTPTimestamp: 1002, RTPPayload: p3.Payload},
	}
	if !reflect.DeepEqual(loaded, want) {
		t.Fatalf("loaded mismatch:\n got: %#v\nwant: %#v", loaded, want)
	}
}
