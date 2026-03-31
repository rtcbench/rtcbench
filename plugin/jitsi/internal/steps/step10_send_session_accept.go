package steps

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/rtcbench/rtcbench/plugin/jitsi/internal/model"
)

type HostCandidate struct {
	IP       string
	Port     int
	Priority int
}

func extractSDPValue(sdp, key string) string {
	re := regexp.MustCompile(key + `:(.*)`)
	match := re.FindStringSubmatch(sdp)
	if len(match) >= 2 {
		return strings.TrimSpace(match[1])
	}
	return ""
}

func extractSDPFingerprint(sdp string) string {
	re := regexp.MustCompile(`a=fingerprint:sha-256 (.*)`)
	match := re.FindStringSubmatch(sdp)
	if len(match) >= 2 {
		return strings.TrimSpace(match[1])
	}
	return ""
}

func extractHostCandidate(sdp string) HostCandidate {
	re := regexp.MustCompile(`a=candidate:\S+ \d+ udp \d+ ([0-9.]+) (\d+) typ host`)
	match := re.FindStringSubmatch(sdp)
	if len(match) >= 3 {
		port, _ := strconv.Atoi(match[2])
		return HostCandidate{IP: match[1], Port: port, Priority: 2130706431}
	}
	return HostCandidate{}
}

func buildSessionAcceptIQ(state *model.ConnectionState, videoSenders string, videoSources []RTPSource) (IQJingle, error) {
	if state.LocalSDP == "" {
		return IQJingle{}, fmt.Errorf("LocalSDP is empty")
	}

	ufrag := extractSDPValue(state.LocalSDP, "a=ice-ufrag")
	pwd := extractSDPValue(state.LocalSDP, "a=ice-pwd")
	fingerprint := extractSDPFingerprint(state.LocalSDP)

	var candidate HostCandidate
	if state.ForcedCandidateIP != "" {
		candidate = HostCandidate{
			IP:       state.ForcedCandidateIP,
			Port:     state.ForcedCandidatePort,
			Priority: state.ForcedCandidatePriority,
		}
	} else {
		candidate = extractHostCandidate(state.LocalSDP)
	}

	state.Log.Debugf("[Step10] Using ufrag=%s pwd=%s fingerprint=%s hostIP=%s port=%d priority=%d",
		ufrag, pwd, fingerprint, candidate.IP, candidate.Port, candidate.Priority)

	candElem := &ICECandidateElem{
		Foundation: "1",
		Component:  "1",
		Protocol:   "udp",
		IP:         candidate.IP,
		Port:       strconv.Itoa(candidate.Port),
		Priority:   strconv.Itoa(candidate.Priority),
		Type:       "host",
	}

	fp := &DTLSFingerprint{Hash: "sha-256", Setup: "active", Value: fingerprint}

	audioDesc := &RTPDescription{
		Media: "audio",
		PayloadTypes: []PayloadType{
			{
				ID: "111", Name: "opus", Clockrate: "48000", Channels: "2",
				Parameters: []RTPParameter{
					{Name: "minptime", Value: "10"},
					{Name: "useinbandfec", Value: "1"},
				},
				RTCPFBs: []RTCPFeedback{
					{Type: "transport-cc"},
				},
			},
		},
		RTCPMux: &RTCPMuxElem{},
		HeaderExts: []RTPHeaderExt{
			{ID: "1", URI: "urn:ietf:params:rtp-hdrext:ssrc-audio-level"},
			{ID: "5", URI: "http://www.ietf.org/id/draft-holmer-rmcat-transport-wide-cc-extensions-01"},
		},
		ExtmapAllowMixed: &ExtmapAllowMixed{},
	}

	videoDesc := &RTPDescription{
		Media: "video",
		PayloadTypes: []PayloadType{
			{
				ID: "101", Name: "VP9", Clockrate: "90000",
				Parameters: []RTPParameter{
					{Name: "profile-id", Value: "0"},
				},
				RTCPFBs: []RTCPFeedback{
					{Type: "transport-cc"},
					{Type: "nack"},
					{Type: "nack", Subtype: "pli"},
					{Type: "ccm", Subtype: "fir"},
				},
			},
		},
		Sources: videoSources,
		RTCPMux: &RTCPMuxElem{},
		HeaderExts: []RTPHeaderExt{
			{ID: "3", URI: "http://www.webrtc.org/experiments/rtp-hdrext/abs-send-time"},
			{ID: "5", URI: "http://www.ietf.org/id/draft-holmer-rmcat-transport-wide-cc-extensions-01"},
		},
		ExtmapAllowMixed: &ExtmapAllowMixed{},
	}

	iq := IQJingle{
		ID:   "session-accept",
		To:   state.FocusJid,
		Type: "set",
		Jingle: JingleElem{
			Action:    "session-accept",
			Initiator: state.FocusJid,
			Responder: state.Jid,
			SID:       state.JingleSID,
			Group: JingleGroup{
				Semantics: "BUNDLE",
				Contents: []JingleGroupContent{
					{Name: "audio"},
					{Name: "video"},
					{Name: "data"},
				},
			},
			Contents: []JingleContent{
				{
					Creator:     "responder",
					Name:        "audio",
					Senders:     "responder",
					Description: audioDesc,
					Transport: ICETransport{
						Ufrag:       ufrag,
						Pwd:         pwd,
						Fingerprint: fp,
						Candidate:   candElem,
					},
				},
				{
					Creator:     "responder",
					Name:        "video",
					Senders:     videoSenders,
					Description: videoDesc,
					Transport: ICETransport{
						Ufrag:       ufrag,
						Pwd:         pwd,
						Fingerprint: fp,
						Candidate:   candElem,
					},
				},
				{
					Creator: "responder",
					Name:    "data",
					Transport: ICETransport{
						Ufrag: ufrag,
						Pwd:   pwd,
						SCTPMap: &SCTPMapElem{
							Number:   "5000",
							Protocol: "webrtc-datachannel",
							Streams:  "0",
						},
						Fingerprint: fp,
					},
				},
			},
		},
	}

	return iq, nil
}

func Step10_SendSessionAccept(state *model.ConnectionState) error {
	state.RID++

	iq, err := buildSessionAcceptIQ(state, "responder", nil)
	if err != nil {
		return err
	}

	requestBody, err := marshalBOSH(state.RID, state.Sid, nil, iq)
	if err != nil {
		return fmt.Errorf("marshal session-accept: %w", err)
	}

	state.Log.Debugf("=== BEGIN session-accept ===\n%s\n=== END session-accept ===", requestBody)

	respXML, err := state.BOSHSender.Send(state.BoshURL, requestBody)
	if err != nil {
		return fmt.Errorf("session-accept failed: %w", err)
	}

	state.Log.Infof("[Step10] session-accept sent OK: %s", respXML)
	return nil
}
