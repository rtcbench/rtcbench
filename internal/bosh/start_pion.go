package bosh

import (
	"fmt"
	"log"
	"net"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"call.zip/internal/bosh/model"
	"call.zip/internal/record"
	"call.zip/internal/vp9"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3"
)

const (
	vbotErrReadFromTrackID int = iota
	vbotErrUnmarshalPacketID
	vbotErrParseVP9PayloadID
	vbotErrSeqNoJumpID
	nVbotErrIDs

	vbotErrUnmarshalPacketMsg = "failed to unmarshal packet"
	vbotErrReadFromTrackMsg   = "failed to read from track id"
	vbotErrParseVP9PayloadMsg = "failed to parse vp9 payload"
	vbotErrSeqNoJumpMsg       = "detected sequence number gap"
)

func parseIceCredentials(sdp string) (ufrag, pwd, fingerprint string) {
	ufragRe := regexp.MustCompile(`a=ice-ufrag:(.+)`)
	pwdRe := regexp.MustCompile(`a=ice-pwd:(.+)`)
	fpRe := regexp.MustCompile(`a=fingerprint:[^\s]+ (.+)`)

	if m := ufragRe.FindStringSubmatch(sdp); len(m) > 1 {
		ufrag = m[1]
	}
	if m := pwdRe.FindStringSubmatch(sdp); len(m) > 1 {
		pwd = m[1]
	}
	if m := fpRe.FindStringSubmatch(sdp); len(m) > 1 {
		fingerprint = m[1]
	}
	return
}

var i atomic.Uint64

func startPion(state *model.ConnectionState) (*webrtc.PeerConnection, error) {
	state.PionAOF.LogPrintln("[startPion] Initializing pion PeerConnection...")

	var hostIP = state.LANClientIP
	var hostPort = 5000 + int(i.Add(1))

	conn, err := net.ListenPacket("udp4", fmt.Sprintf("%s:%d", hostIP, hostPort))
	if err != nil {
		return nil, fmt.Errorf("failed to bind UDP: %w", err)
	}
	state.PionAOF.LogPrintf("[startPion] Listening on UDP %s:%d", hostIP, hostPort)

	se := webrtc.SettingEngine{}
	se.SetICEUDPMux(webrtc.NewICEUDPMux(nil, conn))
	se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})

	m := &webrtc.MediaEngine{}
	m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeVP9,
			ClockRate:   90000,
			SDPFmtpLine: "profile-id=0",
		},
		PayloadType: 101,
	}, webrtc.RTPCodecTypeVideo)

	api := webrtc.NewAPI(
		webrtc.WithSettingEngine(se),
		webrtc.WithMediaEngine(m),
	)

	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, fmt.Errorf("create PeerConnection failed: %w", err)
	}

	pc.OnICEConnectionStateChange(func(iceConnState webrtc.ICEConnectionState) {
		state.PionAOF.LogPrintf("[pion] ICEConnectionState: %s", iceConnState.String())
	})

	pc.OnICEGatheringStateChange(func(iceGathererState webrtc.ICEGathererState) {
		state.PionAOF.LogPrintf("[pion] ICEGatheringState: %s", iceGathererState.String())
	})

	pc.OnConnectionStateChange(func(peerConnState webrtc.PeerConnectionState) {
		state.PionAOF.LogPrintf("[pion] PeerConnectionState: %s", peerConnState.String())
	})

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c != nil {
			state.PionAOF.LogPrintf("[pion] Local ICE candidate: %s", c.ToJSON().Candidate)
		}
	})

	_, err = pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionRecvonly,
	})
	if err != nil {
		return nil, fmt.Errorf("AddTransceiver failed: %w", err)
	}

	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  state.RemoteSDP,
	}
	if err := pc.SetRemoteDescription(offer); err != nil {
		return nil, fmt.Errorf("SetRemoteDescription failed: %w", err)
	}

	state.PionAOF.LogPrintf("[after SetRemoteDescription] Received SDP Offer:\n%s", pc.RemoteDescription().SDP)

	if len(state.Candidates) == 0 {
		staticCandidate := fmt.Sprintf("candidate:1 1 udp 2130706431 %s 10000 typ host", state.LANServerIP)
		for _, mid := range []string{"video"} {
			if err := pc.AddICECandidate(webrtc.ICECandidateInit{
				Candidate: staticCandidate,
				SDPMid:    &mid,
			}); err != nil {
				return nil, fmt.Errorf("AddICECandidate (fallback) failed: %w", err)
			}
		}
		state.PionAOF.LogPrintf("[pion] ✅ Added static remote candidate: %s", staticCandidate)
	} else {
		for _, cand := range state.Candidates {
			candidateLine := fmt.Sprintf("candidate:%s %d %s %d %s %d typ %s",
				cand.Foundation, cand.Component, cand.Protocol, cand.Priority,
				cand.IP, cand.Port, cand.Type,
			)
			for _, mid := range []string{"video"} {
				if err := pc.AddICECandidate(webrtc.ICECandidateInit{
					Candidate: candidateLine,
					SDPMid:    &mid,
				}); err != nil {
					return nil, fmt.Errorf("AddICECandidate failed: %w", err)
				}
			}
			state.PionAOF.LogPrintf("[pion] Added remote candidate: %s", candidateLine)
		}
	}

	answer, err := pc.CreateAnswer(nil)
	answer.SDP = strings.Replace(answer.SDP, "a=setup:actpass", "a=setup:active", 1)
	if err != nil {
		return nil, fmt.Errorf("CreateAnswer failed: %w", err)
	}
	if err := pc.SetLocalDescription(answer); err != nil {
		return nil, fmt.Errorf("SetLocalDescription failed: %w", err)
	}

	state.PionAOF.LogPrintf("[after SetLocalDescription] Generated SDP Answer:\n%s", pc.LocalDescription().SDP)

	ufrag, pwd, fingerprint := parseIceCredentials(pc.LocalDescription().SDP)
	state.LocalSDP = pc.LocalDescription().SDP
	state.ICEUfrag = ufrag
	state.ICEPwd = pwd
	state.Fingerprint = fingerprint

	state.PionAOF.LogPrintf("[startPion] ✅ Local SDP ufrag=%s pwd=%s fingerprint=%s", ufrag, pwd, fingerprint)
	state.PionAOF.LogPrintln("[startPion] ✅ pion PeerConnection ready with SDP answer generated.")

	var packetsCSV *os.File
	if state.LogsDir != nil && state.DumpPackets {
		packetsCSV, err = record.CreateOrOpenAOF(fmt.Sprintf("%s/pkt-%s.csv", *state.LogsDir, state.Nickname))
		if err != nil {
			state.PionAOF.LogFatalf("❌ Failed to open AOF file: %v", err)
		}
	}

	pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		if state.EnableClientLoopV2 {
			go clientLoopV2(track)
		} else {
			go clientLoopV1(track, packetsCSV, state)
		}
	})

	return pc, nil
}

// clientLoopV2 new version of track read loop replacing clientLoopV1
// this version features in-memory packet and error stats
func clientLoopV2(track *webrtc.TrackRemote) {
	var n int
	var err error
	var raw []byte

	var vp9Desc vp9.PayloadDescriptor

	var lastTrackReadTime, nErrsTotal int64
	var errCountsByID = make([]int64, nVbotErrIDs)

	var buf = make([]byte, 1500)
	var lastSeq uint16 = 0
	var pkt rtp.Packet

	for {
		n, _, err = track.Read(buf)
		lastTrackReadTime = time.Now().UnixMicro()

		if err != nil {
			errCountsByID[vbotErrReadFromTrackID]++
			nErrsTotal++
			continue
		}

		raw = buf[:n]
		if err = pkt.Unmarshal(raw); err != nil {
			errCountsByID[vbotErrUnmarshalPacketID]++
			nErrsTotal++
			continue
		}

		if lastSeq != 0 && pkt.SequenceNumber != lastSeq+1 {
			errCountsByID[vbotErrSeqNoJumpID]++
			nErrsTotal++
			// fall-through
		}
		lastSeq = pkt.SequenceNumber

		if err = vp9.ParseVP9PayloadDescriptor(pkt.Payload, &vp9Desc); err != nil {
			errCountsByID[vbotErrParseVP9PayloadID]++
			nErrsTotal++
			continue
		}

		log.Printf("[clientLoopV2] SID=%d TID=%d lastTrackReadTime=%d rtp_timestamp=%d",
			vp9Desc.SID, vp9Desc.TID, lastTrackReadTime, pkt.Timestamp)
	}
}

// clientLoopV1 original version of track read loop
// Deprecated: will be removed in v0.3.0
func clientLoopV1(track *webrtc.TrackRemote, packetsCSV *os.File, state *model.ConnectionState) {
	buf := make([]byte, 1500)
	var lastSeq uint16 = 0
	for {
		n, _, readErr := track.Read(buf)
		if readErr != nil {
			state.PktRecvAOF.LogPrintf("[OnTrack] 🚨 Read error: %s", readErr)
			return
		}
		raw := buf[:n]
		pkt := &rtp.Packet{}
		if err := pkt.Unmarshal(raw); err != nil {
			state.PktRecvAOF.LogPrintf("[OnTrack] 🚨 Failed to unmarshal packet! Error: %s", err)
			continue
		}
		if lastSeq != 0 && pkt.SequenceNumber != lastSeq+1 {
			state.PktRecvAOF.LogPrintf("[OnTrack] 🚨 RTP SeqNo jump: last=%d now=%d", lastSeq, pkt.SequenceNumber)
		}
		lastSeq = pkt.SequenceNumber
		state.PktRecvAOF.LogPrintf("[OnTrack] ✅ Got RTP PT=%d SeqNo=%d Payload=%d bytes", pkt.PayloadType, pkt.SequenceNumber, len(pkt.Payload))

		if packetsCSV != nil {
			if err := record.LogPacketAOF(int(pkt.PayloadType), raw, packetsCSV); err != nil { // 12 byte rtp header is included
				state.PktRecvAOF.LogPrintf("[OnTrack] ❌ Failed to write packets CSV: %s", err)
			}
		}

		sid := -1
		tid := -1
		var vp9desc vp9.PayloadDescriptor
		err := vp9.ParseVP9PayloadDescriptor(pkt.Payload, &vp9desc)
		if err != nil {
			state.PktRecvAOF.LogPrintf("[OnTrack] ❌❌ Not VP9 Payload Descriptor: %s", err)
		} else {
			sid = int(vp9desc.SID)
			tid = int(vp9desc.TID)
		}

		state.NetStatsAOF.Printf("%s,%d,%d,%d,%d,%d,%d,%d\n", time.Now().Format(time.RFC3339), pkt.PayloadType, pkt.SequenceNumber, len(pkt.Payload), sid, tid, pkt.Timestamp, time.Now().UnixMicro())
	}
}
