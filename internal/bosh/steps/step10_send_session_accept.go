package steps

import (
	"call.zip/internal/bosh/model"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// === Helpers ===

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
func Step10_SendSessionAccept(state *model.ConnectionState) error {
	state.RID++

	if state.LocalSDP == "" {
		return fmt.Errorf("LocalSDP is empty")
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
		//state.StepsAOF.LogPrintln("extractHostCandidate ====\n" + state.LocalSDP + "\n==== END extractHostCandidate")
	}

	if ufrag == "" || pwd == "" || fingerprint == "" || candidate.IP == "" {
		state.StepsAOF.LogPrintln("[Step10] WARN: Missing ICE details", ufrag, pwd, fingerprint, candidate.IP)
		return fmt.Errorf("incomplete ICE details")
	}

	state.StepsAOF.LogPrintf("[Step10] Using ufrag=%s pwd=%s fingerprint=%s hostIP=%s port=%d priority=%d",
		ufrag, pwd, fingerprint, candidate.IP, candidate.Port, candidate.Priority)

	requestBody := fmt.Sprintf(`<body xmlns="http://jabber.org/protocol/httpbind" rid="%d" sid="%s">
  <iq xmlns="jabber:client" id="session-accept" to="%s" type="set">
    <jingle xmlns="urn:xmpp:jingle:1" action="session-accept" initiator="%s" responder="%s" sid="%s">
      <group xmlns="urn:xmpp:jingle:apps:grouping:0" semantics="BUNDLE">
        <content name="audio"/>
        <content name="video"/>
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
      <content creator="responder" name="video" senders="responder">
        <description xmlns="urn:xmpp:jingle:apps:rtp:1" media="video">
          <payload-type id="101" name="VP9" clockrate="90000">
            <parameter name="profile-id" value="0"/>
            <rtcp-fb xmlns="urn:xmpp:jingle:apps:rtp:rtcp-fb:0" type="transport-cc"/>
            <rtcp-fb xmlns="urn:xmpp:jingle:apps:rtp:rtcp-fb:0" type="nack"/>
            <rtcp-fb xmlns="urn:xmpp:jingle:apps:rtp:rtcp-fb:0" type="nack" subtype="pli"/>
            <rtcp-fb xmlns="urn:xmpp:jingle:apps:rtp:rtcp-fb:0" type="ccm" subtype="fir"/>
          </payload-type>
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
    </jingle>
  </iq>
</body>`,
		state.RID, state.Sid,
		state.FocusJid, state.FocusJid, state.Jid, state.JingleSID,
		// audio transport
		ufrag, pwd, fingerprint, candidate.IP, candidate.Port, candidate.Priority,
		// video transport
		ufrag, pwd, fingerprint, candidate.IP, candidate.Port, candidate.Priority,
	)

	state.StepsAOF.LogPrintf("=== BEGIN session-accept ===\n%s\n=== END session-accept ===", requestBody)

	respXML, err := state.BOSHSender.Send(state.BoshURL, requestBody)
	if err != nil {
		return fmt.Errorf("session-accept failed: %w", err)
	}

	state.StepsAOF.LogPrintf("[Step10] ✅ session-accept sent OK: %s", respXML)
	return nil
}
