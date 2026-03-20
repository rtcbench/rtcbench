package pcap

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func le16(b []byte) uint16 { return binary.LittleEndian.Uint16(b) }
func le32(b []byte) uint32 { return binary.LittleEndian.Uint32(b) }
func be16(b []byte) uint16 { return binary.BigEndian.Uint16(b) }

func TestNew_WritesGlobalHeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.pcap")
	w, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(data) != 24 {
		t.Fatalf("expected 24-byte global header, got %d bytes", len(data))
	}

	if got := le32(data[0:]); got != magicNumber {
		t.Fatalf("magic: got 0x%08x, want 0x%08x", got, magicNumber)
	}
	if got := le16(data[4:]); got != versionMajor {
		t.Fatalf("major: got %d, want %d", got, versionMajor)
	}
	if got := le16(data[6:]); got != versionMinor {
		t.Fatalf("minor: got %d, want %d", got, versionMinor)
	}
	if got := le32(data[16:]); got != snapLen {
		t.Fatalf("snaplen: got %d, want %d", got, snapLen)
	}
	if got := le32(data[20:]); got != linkTypeEthernet {
		t.Fatalf("link type: got %d, want %d", got, linkTypeEthernet)
	}
}

func TestWritePacket_RecordHeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.pcap")
	w, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	payload := []byte{0x80, 0x60, 0x00, 0x01, 0x00, 0x00, 0x00, 0xa0, 0xde, 0xad, 0xbe, 0xef}
	ts := time.Date(2026, 3, 20, 15, 30, 45, 123456000, time.UTC)

	if err := w.WritePacket(payload, ts); err != nil {
		t.Fatalf("WritePacket: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// 24 global header + 16 record header + 42 frame header + 12 payload = 94
	expectedSize := 24 + 16 + frameOverhead + len(payload)
	if len(data) != expectedSize {
		t.Fatalf("file size: got %d, want %d", len(data), expectedSize)
	}

	// Check packet record header at offset 24.
	rec := data[24:]
	sec := le32(rec[0:])
	usec := le32(rec[4:])
	capturedLen := le32(rec[8:])
	originalLen := le32(rec[12:])

	wantSec := uint32(ts.Unix())
	wantUsec := uint32(ts.UnixMicro() - ts.Unix()*1_000_000)
	wantLen := uint32(frameOverhead + len(payload))

	if sec != wantSec {
		t.Fatalf("sec: got %d, want %d", sec, wantSec)
	}
	if usec != wantUsec {
		t.Fatalf("usec: got %d, want %d", usec, wantUsec)
	}
	if capturedLen != wantLen {
		t.Fatalf("captured_len: got %d, want %d", capturedLen, wantLen)
	}
	if originalLen != wantLen {
		t.Fatalf("original_len: got %d, want %d", originalLen, wantLen)
	}
}

func TestWritePacket_FrameHeaders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.pcap")
	w, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	payload := make([]byte, 100)
	for i := range payload {
		payload[i] = byte(i)
	}
	ts := time.Now()

	if err := w.WritePacket(payload, ts); err != nil {
		t.Fatalf("WritePacket: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// Frame starts at offset 24 (global) + 16 (record) = 40.
	frame := data[40:]

	// Ethernet: dst broadcast, ethertype 0x0800.
	if frame[0] != 0xff || frame[5] != 0xff {
		t.Fatal("ethernet dst not broadcast")
	}
	if frame[12] != 0x08 || frame[13] != 0x00 {
		t.Fatal("ethertype not IPv4")
	}

	// IPv4: version+IHL, protocol UDP, src/dst 127.0.0.1.
	ip := frame[ethHeaderSize:]
	if ip[0] != 0x45 {
		t.Fatalf("ip version+ihl: got 0x%02x, want 0x45", ip[0])
	}
	if ip[9] != 0x11 {
		t.Fatalf("ip protocol: got %d, want 17 (UDP)", ip[9])
	}
	ipTotalLen := be16(ip[2:])
	wantIPLen := uint16(ipHeaderSize + udpHeaderSize + len(payload))
	if ipTotalLen != wantIPLen {
		t.Fatalf("ip total len: got %d, want %d", ipTotalLen, wantIPLen)
	}

	// UDP: ports, length.
	udp := frame[ethHeaderSize+ipHeaderSize:]
	if srcPort := be16(udp[0:]); srcPort != rtpPort {
		t.Fatalf("udp src port: got %d, want %d", srcPort, rtpPort)
	}
	if dstPort := be16(udp[2:]); dstPort != rtpPort {
		t.Fatalf("udp dst port: got %d, want %d", dstPort, rtpPort)
	}
	udpLen := be16(udp[4:])
	wantUDPLen := uint16(udpHeaderSize + len(payload))
	if udpLen != wantUDPLen {
		t.Fatalf("udp len: got %d, want %d", udpLen, wantUDPLen)
	}

	// Payload data preserved.
	rtpData := frame[frameOverhead:]
	for i := range payload {
		if rtpData[i] != payload[i] {
			t.Fatalf("payload byte %d: got %d, want %d", i, rtpData[i], payload[i])
		}
	}
}

func TestWritePacket_MultiplePackets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.pcap")
	w, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ts := time.Now()
	for i := 0; i < 100; i++ {
		pkt := []byte{0x80, 0x60, byte(i >> 8), byte(i)}
		if err := w.WritePacket(pkt, ts.Add(time.Duration(i)*time.Millisecond)); err != nil {
			t.Fatalf("WritePacket #%d: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// 24 global + 100 * (16 record + 42 frame + 4 payload) = 24 + 100*62 = 6224
	expected := 24 + 100*(16+frameOverhead+4)
	if len(data) != expected {
		t.Fatalf("file size: got %d, want %d", len(data), expected)
	}
}
