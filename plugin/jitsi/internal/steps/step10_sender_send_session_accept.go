package steps

import (
	"fmt"
	"regexp"
	"strings"

	"call.zip/plugin/jitsi/internal/model"
)

// extractPrimaryVideoSSRCAndMSID extracts a single (primary) SSRC and its MSID from the SDP.
// This is the "no RTX" version: it does NOT look for ssrc-group:FID.
//
// It tries to find a video SSRC by locating the first "a=ssrc:<id> msid:<...>" line.
// Example:
//
//	a=ssrc:3816515605 msid:- 52305b34-19ac-42ef-8ee6-337e9378c50d
func extractPrimaryVideoSSRCAndMSID(sdp string) (primarySSRC, msid string) {
	// Find the first SSRC that has an msid line.
	re := regexp.MustCompile(`(?m)^a=ssrc:(\d+)\s+msid:(.+)\s*$`)
	m := re.FindStringSubmatch(sdp)
	if len(m) != 3 {
		return "", ""
	}
	return strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
}

func Step10_Sender_SendSessionAccept(state *model.ConnectionState) error {
	state.RID++

	if state.LocalSDP == "" {
		return fmt.Errorf("LocalSDP is empty")
	}
	if strings.TrimSpace(state.Nickname) == "" {
		return fmt.Errorf("Nickname (Endpoint ID) is nil/empty: set state.Nickname before joining the room")
	}
	endpointID := strings.TrimSpace(state.Nickname)

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

	state.StepsAOF.LogPrintf("[Step10_Sender] Using ufrag=%s pwd=%s fingerprint=%s hostIP=%s port=%d priority=%d",
		ufrag, pwd, fingerprint, candidate.IP, candidate.Port, candidate.Priority)

	// Sender mode: advertise that we will send video.
	videoSenders := "both"

	primarySSRC, msid := extractPrimaryVideoSSRCAndMSID(state.LocalSDP)
	if primarySSRC == "" || msid == "" {
		return fmt.Errorf(
			"sender mode: could not extract primary video SSRC/MSID from LocalSDP (ssrc=%q msid=%q). "+
				"Ensure startPion adds a local VP9 sending track (AddTrack) so the answer contains a=ssrc:... msid:...",
			primarySSRC, msid,
		)
	}

	// Optional: store for debugging if your state has these pointer fields.
	if state.SenderSSRC != nil {
		*state.SenderSSRC = primarySSRC
	}
	if state.SenderMSID != nil {
		*state.SenderMSID = msid
	}

	sourceName := fmt.Sprintf("%s-v0", endpointID)

	// No RTX: single SSRC source only (no <ssrc-group semantics="FID">).
	videoSourcesXML := fmt.Sprintf(`
          <source xmlns="urn:xmpp:jingle:apps:rtp:ssma:0" name="%s" ssrc="%s" videoType="camera">
            <parameter xmlns="urn:xmpp:jingle:apps:rtp:ssma:0" name="msid" value="%s"/>
          </source>`,
		sourceName, primarySSRC, msid,
	)

	requestBody := fmt.Sprintf(`<body xmlns="http://jabber.org/protocol/httpbind" rid="%d" sid="%s">
  <iq xmlns="jabber:client" id="session-accept" to="%s" type="set">
    <jingle xmlns="urn:xmpp:jingle:1" action="session-accept" initiator="%s" responder="%s" sid="%s">
      <group xmlns="urn:xmpp:jingle:apps:grouping:0" semantics="BUNDLE">
        <content name="audio"/>
        <content name="video"/>
        <content name="data"/>
      </group>

      <content creator="responder" name="audio" senders="responder">
        <description xmlns="urn:xmpp:jingle:apps:rtp:1" media="audio">
          <payload-type id="111" name="opus" clockrate="48000" channels="2">
            <parameter name="minptime" value="10"/>
            <parameter name="useinbandfec" value="1"/>
            <rtcp-fb xmlns="urn:xmpp:jingle:apps:rtp:rtcp-fb:0" type="transport-cc"/>
          </payload-type>
          <rtcp-mux/>
          <rtp-hdrext xmlns="urn:xmpp:jingle:apps:rtp:rtp-hdrext:0" id="1" uri="urn:ietf:params:rtp-hdrext:ssrc-audio-level"/>
          <rtp-hdrext xmlns="urn:xmpp:jingle:apps:rtp:rtp-hdrext:0" id="5" uri="http://www.ietf.org/id/draft-holmer-rmcat-transport-wide-cc-extensions-01"/>
          <extmap-allow-mixed xmlns="urn:xmpp:jingle:apps:rtp:rtp-hdrext:0"/>
        </description>
        <transport xmlns="urn:xmpp:jingle:transports:ice-udp:1" ufrag="%s" pwd="%s">
          <fingerprint xmlns="urn:xmpp:jingle:apps:dtls:0" hash="sha-256" setup="active">%s</fingerprint>
          <candidate foundation="1" component="1" protocol="udp" ip="%s" port="%d" priority="%d" type="host"/>
        </transport>
      </content>

      <content creator="responder" name="video" senders="%s">
        <description xmlns="urn:xmpp:jingle:apps:rtp:1" media="video">
          <payload-type id="101" name="VP9" clockrate="90000">
            <parameter name="profile-id" value="0"/>
            <rtcp-fb xmlns="urn:xmpp:jingle:apps:rtp:rtcp-fb:0" type="transport-cc"/>
            <rtcp-fb xmlns="urn:xmpp:jingle:apps:rtp:rtcp-fb:0" type="nack"/>
            <rtcp-fb xmlns="urn:xmpp:jingle:apps:rtp:rtcp-fb:0" type="nack" subtype="pli"/>
            <rtcp-fb xmlns="urn:xmpp:jingle:apps:rtp:rtcp-fb:0" type="ccm" subtype="fir"/>
          </payload-type>
          %s
          <rtcp-mux/>
          <rtp-hdrext xmlns="urn:xmpp:jingle:apps:rtp:rtp-hdrext:0" id="3" uri="http://www.webrtc.org/experiments/rtp-hdrext/abs-send-time"/>
          <rtp-hdrext xmlns="urn:xmpp:jingle:apps:rtp:rtp-hdrext:0" id="5" uri="http://www.ietf.org/id/draft-holmer-rmcat-transport-wide-cc-extensions-01"/>
          <extmap-allow-mixed xmlns="urn:xmpp:jingle:apps:rtp:rtp-hdrext:0"/>
        </description>
        <transport xmlns="urn:xmpp:jingle:transports:ice-udp:1" ufrag="%s" pwd="%s">
          <fingerprint xmlns="urn:xmpp:jingle:apps:dtls:0" hash="sha-256" setup="active">%s</fingerprint>
          <candidate foundation="1" component="1" protocol="udp" ip="%s" port="%d" priority="%d" type="host"/>
        </transport>
      </content>
      <content creator="responder" name="data">
        <transport xmlns="urn:xmpp:jingle:transports:ice-udp:1" ufrag="%s" pwd="%s">
          <sctpmap xmlns="urn:xmpp:jingle:transports:dtls-sctp:1" number="5000" protocol="webrtc-datachannel" streams="0"/>
          <fingerprint xmlns="urn:xmpp:jingle:apps:dtls:0" hash="sha-256" setup="active">%s</fingerprint>
        </transport>
      </content>
    </jingle>
  </iq>
</body>`,
		state.RID, state.Sid,
		state.FocusJid, state.FocusJid, state.Jid, state.JingleSID,
		// audio transport
		ufrag, pwd, fingerprint, candidate.IP, candidate.Port, candidate.Priority,
		// video transport
		videoSenders, videoSourcesXML,
		ufrag, pwd, fingerprint, candidate.IP, candidate.Port, candidate.Priority,
		// data transport
		ufrag, pwd, fingerprint,
	)

	state.StepsAOF.LogPrintf("=== BEGIN session-accept (sender, no-rtx) ===\n%s\n=== END session-accept (sender, no-rtx) ===", requestBody)

	respXML, err := state.BOSHSender.Send(state.BoshURL, requestBody)
	if err != nil {
		return fmt.Errorf("session-accept (sender, no-rtx) failed: %w", err)
	}

	if strings.Contains(respXML, "type='error'") || strings.Contains(respXML, `type="error"`) {
		return fmt.Errorf("session-accept rejected by Jicofo: %s", respXML)
	}

	state.StepsAOF.LogPrintf("[Step10_Sender] ✅ session-accept sent OK: %s", respXML)
	return nil
}
