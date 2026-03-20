package pcap

import (
	"encoding/binary"
	"os"
	"time"
)

const (
	magicNumber      = 0xa1b2c3d4
	versionMajor     = 2
	versionMinor     = 4
	snapLen          = 65535
	linkTypeEthernet = 1

	ethHeaderSize = 14
	ipHeaderSize  = 20
	udpHeaderSize = 8
	frameOverhead = ethHeaderSize + ipHeaderSize + udpHeaderSize // 42 bytes

	rtpPort = 5004 // common RTP media port; helps Wireshark heuristic dissector
)

// Writer writes raw RTP packets to a pcap file.
// Each packet is wrapped in fake Ethernet/IPv4/UDP headers so Wireshark
// auto-dissects the contents as RTP without manual "Decode As" configuration.
type Writer struct {
	f   *os.File
	hdr [frameOverhead]byte // reusable per-packet frame header
}

// New creates a pcap file at path and writes the 24-byte global header.
func New(path string) (*Writer, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	w := &Writer{f: f}
	w.initFrameHeader()
	if err := w.writeGlobalHeader(); err != nil {
		_ = f.Close()
		return nil, err
	}
	return w, nil
}

// initFrameHeader populates the static portions of the Ethernet/IPv4/UDP
// frame header that are reused for every packet.
func (w *Writer) initFrameHeader() {
	h := &w.hdr

	// Ethernet: broadcast dst, zero src, EtherType IPv4.
	h[0], h[1], h[2], h[3], h[4], h[5] = 0xff, 0xff, 0xff, 0xff, 0xff, 0xff
	h[12], h[13] = 0x08, 0x00

	// IPv4: version 4, IHL 5 (20 bytes), TTL 64, protocol UDP (17),
	// src/dst 127.0.0.1. Total length and checksum filled per packet.
	off := ethHeaderSize
	h[off] = 0x45          // version + IHL
	h[off+8] = 0x40        // TTL = 64
	h[off+9] = 0x11        // protocol = UDP
	h[off+12] = 127        // src IP
	h[off+15] = 1          // src IP
	h[off+16] = 127        // dst IP
	h[off+19] = 1          // dst IP

	// UDP: src/dst port = rtpPort. Length filled per packet, checksum 0.
	off = ethHeaderSize + ipHeaderSize
	binary.BigEndian.PutUint16(h[off:], rtpPort)
	binary.BigEndian.PutUint16(h[off+2:], rtpPort)
}

func (w *Writer) writeGlobalHeader() error {
	var hdr [24]byte
	binary.LittleEndian.PutUint32(hdr[0:], magicNumber)
	binary.LittleEndian.PutUint16(hdr[4:], versionMajor)
	binary.LittleEndian.PutUint16(hdr[6:], versionMinor)
	// thiszone, sigfigs: 0
	binary.LittleEndian.PutUint32(hdr[16:], snapLen)
	binary.LittleEndian.PutUint32(hdr[20:], linkTypeEthernet)
	_, err := w.f.Write(hdr[:])
	return err
}

// WritePacket writes a single raw RTP packet to the pcap file.
// ts is the capture timestamp (typically time.Now() at packet arrival).
func (w *Writer) WritePacket(data []byte, ts time.Time) error {
	totalLen := frameOverhead + len(data)

	// Update IP total length.
	binary.BigEndian.PutUint16(w.hdr[ethHeaderSize+2:], uint16(ipHeaderSize+udpHeaderSize+len(data)))

	// Update UDP length.
	binary.BigEndian.PutUint16(w.hdr[ethHeaderSize+ipHeaderSize+4:], uint16(udpHeaderSize+len(data)))

	// Per-packet record header (16 bytes).
	sec := ts.Unix()
	usec := ts.UnixMicro() - sec*1_000_000

	var ph [16]byte
	binary.LittleEndian.PutUint32(ph[0:], uint32(sec))
	binary.LittleEndian.PutUint32(ph[4:], uint32(usec))
	binary.LittleEndian.PutUint32(ph[8:], uint32(totalLen))
	binary.LittleEndian.PutUint32(ph[12:], uint32(totalLen))

	if _, err := w.f.Write(ph[:]); err != nil {
		return err
	}
	if _, err := w.f.Write(w.hdr[:]); err != nil {
		return err
	}
	_, err := w.f.Write(data)
	return err
}

// Close closes the underlying file.
func (w *Writer) Close() error {
	return w.f.Close()
}
