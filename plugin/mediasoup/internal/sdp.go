package internal

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// BuildSendAnswerSDP constructs a fake SDP answer from mediasoup's transport
// parameters to pair with pion's SDP offer for a send transport.
//
// The answer uses mediasoup's ICE/DTLS credentials and candidates while
// mirroring the offer's codec negotiation. This tricks pion into establishing
// an ICE/DTLS connection directly to mediasoup.
func BuildSendAnswerSDP(offerSDP string, transport TransportOptions) string {
	mid := extractAttr(offerSDP, "mid")
	if mid == "" {
		mid = "0"
	}
	mLine := extractMLine(offerSDP)
	pts := extractPayloadTypes(mLine)
	codecAttrs := extractCodecAttributes(offerSDP, pts)

	var sb strings.Builder
	// Session level.
	sb.WriteString("v=0\r\n")
	sb.WriteString("o=mediasoup-answer 10000 1 IN IP4 0.0.0.0\r\n")
	sb.WriteString("s=-\r\n")
	sb.WriteString("t=0 0\r\n")
	sb.WriteString(fmt.Sprintf("a=group:BUNDLE %s\r\n", mid))
	sb.WriteString("a=msid-semantic: WMS *\r\n")
	if transport.IceParameters.IceLite {
		sb.WriteString("a=ice-lite\r\n")
	}

	// Media section.
	sb.WriteString(mLine + "\r\n")
	sb.WriteString("c=IN IP4 0.0.0.0\r\n")
	sb.WriteString("a=rtcp:9 IN IP4 0.0.0.0\r\n")

	writeTransportAttrs(&sb, transport, mid, "recvonly", "passive")

	for _, attr := range codecAttrs {
		sb.WriteString(attr + "\r\n")
	}

	writeCandidates(&sb, transport.IceCandidates)

	return sb.String()
}

// BuildRecvOfferSDP constructs a fake SDP offer with one media section per
// consumer. Used to feed mediasoup's consumer tracks into pion's recv PC.
func BuildRecvOfferSDP(transport TransportOptions, consumers []ConsumerData, sdpVersion int) string {
	if len(consumers) == 0 {
		return ""
	}

	mids := make([]string, len(consumers))
	for i := range consumers {
		mids[i] = strconv.Itoa(i)
	}

	var sb strings.Builder
	// Session level.
	sb.WriteString("v=0\r\n")
	sb.WriteString(fmt.Sprintf("o=mediasoup-offer 10000 %d IN IP4 0.0.0.0\r\n", sdpVersion))
	sb.WriteString("s=-\r\n")
	sb.WriteString("t=0 0\r\n")
	sb.WriteString(fmt.Sprintf("a=group:BUNDLE %s\r\n", strings.Join(mids, " ")))
	sb.WriteString("a=msid-semantic: WMS *\r\n")
	if transport.IceParameters.IceLite {
		sb.WriteString("a=ice-lite\r\n")
	}

	for i, consumer := range consumers {
		if len(consumer.RtpParameters.Codecs) == 0 {
			continue
		}
		mid := strconv.Itoa(i)

		ptList := make([]string, 0, len(consumer.RtpParameters.Codecs))
		for _, c := range consumer.RtpParameters.Codecs {
			ptList = append(ptList, strconv.Itoa(c.PayloadType))
		}

		sb.WriteString(fmt.Sprintf("m=video 7 UDP/TLS/RTP/SAVPF %s\r\n", strings.Join(ptList, " ")))
		sb.WriteString("c=IN IP4 0.0.0.0\r\n")
		sb.WriteString("a=rtcp:9 IN IP4 0.0.0.0\r\n")

		writeTransportAttrs(&sb, transport, mid, "sendonly", "actpass")

		// Header extensions.
		for _, ext := range consumer.RtpParameters.HeaderExtensions {
			sb.WriteString(fmt.Sprintf("a=extmap:%d %s\r\n", ext.ID, ext.URI))
		}

		// Codec attributes.
		for _, c := range consumer.RtpParameters.Codecs {
			name := codecShortName(c.MimeType)
			sb.WriteString(fmt.Sprintf("a=rtpmap:%d %s/%d", c.PayloadType, name, c.ClockRate))
			if c.Channels > 0 {
				sb.WriteString(fmt.Sprintf("/%d", c.Channels))
			}
			sb.WriteString("\r\n")

			if fmtp := buildFmtp(c.Parameters); fmtp != "" {
				sb.WriteString(fmt.Sprintf("a=fmtp:%d %s\r\n", c.PayloadType, fmtp))
			}

			for _, fb := range c.RtcpFeedback {
				if fb.Parameter != "" {
					sb.WriteString(fmt.Sprintf("a=rtcp-fb:%d %s %s\r\n", c.PayloadType, fb.Type, fb.Parameter))
				} else {
					sb.WriteString(fmt.Sprintf("a=rtcp-fb:%d %s\r\n", c.PayloadType, fb.Type))
				}
			}
		}

		// SSRC.
		if len(consumer.RtpParameters.Encodings) > 0 && consumer.RtpParameters.Encodings[0].SSRC != nil {
			ssrc := *consumer.RtpParameters.Encodings[0].SSRC
			cname := "mediasoup"
			if consumer.RtpParameters.Rtcp != nil && consumer.RtpParameters.Rtcp.CNAME != "" {
				cname = consumer.RtpParameters.Rtcp.CNAME
			}
			sb.WriteString(fmt.Sprintf("a=ssrc:%d cname:%s\r\n", ssrc, cname))
			sb.WriteString(fmt.Sprintf("a=ssrc:%d msid:mediasoup mediasoup\r\n", ssrc))
		}

		// Candidates only in first section (shared via BUNDLE).
		if i == 0 {
			writeCandidates(&sb, transport.IceCandidates)
		}
	}

	return sb.String()
}

// ExtractDtlsFingerprint extracts the first DTLS fingerprint from an SDP.
func ExtractDtlsFingerprint(sdp string) DtlsFingerprint {
	re := regexp.MustCompile(`a=fingerprint:(\S+)\s+(\S+)`)
	m := re.FindStringSubmatch(sdp)
	if m == nil {
		return DtlsFingerprint{}
	}
	return DtlsFingerprint{Algorithm: m[1], Value: m[2]}
}

// ExtractSSRC extracts the first a=ssrc value from an SDP.
func ExtractSSRC(sdp string) uint32 {
	re := regexp.MustCompile(`a=ssrc:(\d+)`)
	m := re.FindStringSubmatch(sdp)
	if m == nil {
		return 0
	}
	v, _ := strconv.ParseUint(m[1], 10, 32)
	return uint32(v)
}

// ExtractRtpParameters builds mediasoup RtpParameters from pion's offer SDP.
func ExtractRtpParameters(offerSDP string, routerCaps RtpCapabilities) RtpParameters {
	ssrc := ExtractSSRC(offerSDP)
	mid := extractAttr(offerSDP, "mid")
	if mid == "" {
		mid = "0"
	}

	mLine := extractMLine(offerSDP)
	pts := extractPayloadTypes(mLine)
	pt := 96
	if len(pts) > 0 {
		pt = pts[0]
	}

	// Build codec entry matching router's VP9 feedback mechanisms.
	var rtcpFB []RtcpFeedback
	for _, c := range routerCaps.Codecs {
		if strings.EqualFold(c.MimeType, "video/VP9") {
			rtcpFB = c.RtcpFeedback
			break
		}
	}

	params := RtpParameters{
		Codecs: []RtpCodecParameters{
			{
				MimeType:     "video/VP9",
				PayloadType:  pt,
				ClockRate:    90000,
				Parameters:   map[string]any{"profile-id": 0},
				RtcpFeedback: rtcpFB,
			},
		},
		Encodings: []RtpEncoding{
			{SSRC: &ssrc},
		},
		Mid: mid,
	}

	// Map header extensions from offer to router-supported ones.
	offerExts := extractExtmaps(offerSDP)
	for _, ext := range routerCaps.HeaderExtensions {
		if ext.Kind != "" && ext.Kind != "video" {
			continue
		}
		if id, ok := offerExts[ext.URI]; ok {
			params.HeaderExtensions = append(params.HeaderExtensions, RtpHeaderExtParam{
				URI: ext.URI,
				ID:  id,
			})
		}
	}

	return params
}

// ---------------------------------------------------------------------------
// internal helpers
// ---------------------------------------------------------------------------

func writeTransportAttrs(sb *strings.Builder, t TransportOptions, mid, direction, setup string) {
	sb.WriteString(fmt.Sprintf("a=ice-ufrag:%s\r\n", t.IceParameters.UsernameFragment))
	sb.WriteString(fmt.Sprintf("a=ice-pwd:%s\r\n", t.IceParameters.Password))
	if len(t.DtlsParameters.Fingerprints) > 0 {
		fp := t.DtlsParameters.Fingerprints[len(t.DtlsParameters.Fingerprints)-1]
		sb.WriteString(fmt.Sprintf("a=fingerprint:%s %s\r\n", fp.Algorithm, fp.Value))
	}
	sb.WriteString(fmt.Sprintf("a=setup:%s\r\n", setup))
	sb.WriteString(fmt.Sprintf("a=mid:%s\r\n", mid))
	sb.WriteString(fmt.Sprintf("a=%s\r\n", direction))
	sb.WriteString("a=rtcp-mux\r\n")
}

func writeCandidates(sb *strings.Builder, candidates []IceCandidate) {
	for _, c := range candidates {
		sb.WriteString(fmt.Sprintf("a=candidate:%s 1 %s %d %s %d typ %s\r\n",
			c.Foundation, c.Protocol, c.Priority, c.Addr(), c.Port, c.Type))
	}
}

func extractAttr(sdp, name string) string {
	re := regexp.MustCompile(fmt.Sprintf(`a=%s:(\S+)`, regexp.QuoteMeta(name)))
	m := re.FindStringSubmatch(sdp)
	if m == nil {
		return ""
	}
	return m[1]
}

func extractMLine(sdp string) string {
	for _, line := range strings.Split(sdp, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "m=video") {
			return line
		}
	}
	return "m=video 9 UDP/TLS/RTP/SAVPF 96"
}

func extractPayloadTypes(mLine string) []int {
	parts := strings.Fields(mLine)
	if len(parts) < 4 {
		return nil
	}
	var pts []int
	for _, p := range parts[3:] {
		if pt, err := strconv.Atoi(p); err == nil {
			pts = append(pts, pt)
		}
	}
	return pts
}

// extractCodecAttributes returns a=rtpmap, a=fmtp, a=rtcp-fb, and a=extmap
// lines from the SDP that match the given payload types.
func extractCodecAttributes(sdp string, pts []int) []string {
	ptSet := make(map[int]bool, len(pts))
	for _, pt := range pts {
		ptSet[pt] = true
	}

	ptRe := regexp.MustCompile(`^a=(?:rtpmap|fmtp|rtcp-fb):(\d+)`)

	var attrs []string
	for _, line := range strings.Split(sdp, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "a=extmap:") {
			attrs = append(attrs, line)
			continue
		}
		if m := ptRe.FindStringSubmatch(line); m != nil {
			pt, _ := strconv.Atoi(m[1])
			if ptSet[pt] {
				attrs = append(attrs, line)
			}
		}
	}
	return attrs
}

func extractExtmaps(sdp string) map[string]int {
	result := make(map[string]int)
	for _, line := range strings.Split(sdp, "\n") {
		line = strings.TrimRight(line, "\r")
		if !strings.HasPrefix(line, "a=extmap:") {
			continue
		}
		parts := strings.Fields(line[9:])
		if len(parts) < 2 {
			continue
		}
		idStr := parts[0]
		if idx := strings.Index(idStr, "/"); idx >= 0 {
			idStr = idStr[:idx]
		}
		if id, err := strconv.Atoi(idStr); err == nil {
			result[parts[1]] = id
		}
	}
	return result
}

func codecShortName(mimeType string) string {
	if idx := strings.Index(mimeType, "/"); idx >= 0 {
		return mimeType[idx+1:]
	}
	return mimeType
}

func buildFmtp(params map[string]any) string {
	if len(params) == 0 {
		return ""
	}
	var parts []string
	for k, v := range params {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	return strings.Join(parts, ";")
}
