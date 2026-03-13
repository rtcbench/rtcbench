package steps

import (
	"fmt"
	"strings"
	"time"

	"call.zip/plugin/jitsi/internal/model"
	"call.zip/plugin/jitsi/internal/util"
)

func Step09_WaitForJingleOffer(state *model.ConnectionState) error {
	var err error

	// Start with whatever Step08 already received — Jicofo may have included
	// a disco#info query in that same BOSH response body.
	respXML := state.PendingBOSHResponse

	for {
		// Only poll for a fresh response when we have nothing to process.
		if respXML == "" {
			state.RID++
			requestBody, marshalErr := marshalBOSH(state.RID, state.Sid, nil)
			if marshalErr != nil {
				return fmt.Errorf("marshal poll body: %w", marshalErr)
			}
			respXML, err = state.BOSHSender.Send(state.BoshURL, requestBody)
			if err != nil {
				return fmt.Errorf("wait for Jingle offer failed: %w", err)
			}
		}

		if strings.Contains(respXML, "<jingle") {
			// Extract SID and initiator
			state.JingleSID = util.ExtractAttrInTag(respXML, "jingle", "sid")
			if state.JingleSID == "" {
				return fmt.Errorf("no Jingle SID found in <jingle> tag")
			}

			initiator := util.ExtractAttrInTag(respXML, "jingle", "initiator")
			if initiator != "" {
				state.FocusJid = initiator // overwrite to match exactly!
			}

			state.ICEUfrag = util.ExtractAttrInTag(respXML, "transport", "ufrag")
			state.ICEPwd = util.ExtractAttrInTag(respXML, "transport", "pwd")
			state.Fingerprint = util.ExtractTagValue(respXML, "fingerprint")

			// Parse all ICE candidates if present
			candidates := util.ExtractMultipleTags(respXML, "candidate")
			for _, cXML := range candidates {
				cand := model.ICECandidate{
					Foundation: util.ExtractAttr(cXML, "foundation"),
					Component:  util.MustAtoi(util.ExtractAttr(cXML, "component")),
					Protocol:   util.ExtractAttr(cXML, "protocol"),
					IP:         util.ExtractAttr(cXML, "ip"),
					Port:       util.MustAtoi(util.ExtractAttr(cXML, "port")),
					Priority:   uint32(util.MustAtoi64(util.ExtractAttr(cXML, "priority"))),
					Type:       util.ExtractAttr(cXML, "type"),
				}
				state.Candidates = append(state.Candidates, cand)
			}

			// Extract colibri WebSocket URL from the data content transport
			state.ColibriWebSocketURL = util.ExtractAttrInTag(respXML, "web-socket", "url")

			// Log full match values for session-accept
			state.Log.Infof(
				"Step09_WaitForJingleOffer OK: SID=%s Initiator=%s ICEUfrag=%s Candidates=%d ColibriWS=%s",
				state.JingleSID, state.FocusJid, state.ICEUfrag, len(state.Candidates), state.ColibriWebSocketURL,
			)

			return nil
		}

		// Handle disco#info requests while waiting.
		// After replying, loop immediately to check the response for a Jingle
		// without sleeping — Jicofo often sends the offer right after.
		if strings.Contains(respXML, "<query xmlns='http://jabber.org/protocol/disco#info'") {
			from := util.ExtractAttrInTag(respXML, "iq", "from")
			id := util.ExtractAttrInTag(respXML, "iq", "id")

			state.RID++
			response, marshalErr := marshalBOSH(state.RID, state.Sid, nil,
				IQDiscoInfoResult{
					From: state.Jid,
					To:   from,
					ID:   id,
					Type: "result",
					Query: DiscoInfoQuery{
						Identities: []DiscoIdentity{
							{Category: "client", Type: "bot", Name: state.Nickname},
						},
						Features: []DiscoFeature{
							{Var: "http://jabber.org/protocol/muc"},
							{Var: "urn:xmpp:jingle:1"},
							{Var: "urn:xmpp:jingle:apps:rtp:1"},
							{Var: "urn:xmpp:jingle:apps:rtp:audio"},
							{Var: "urn:xmpp:jingle:apps:rtp:video"},
							{Var: "urn:xmpp:jingle:apps:dtls:0"},
							{Var: "urn:xmpp:jingle:transports:ice-udp:1"},
							{Var: "urn:ietf:rfc:5761"},
							{Var: "urn:ietf:rfc:5888"},
						},
					},
				},
			)
			if marshalErr != nil {
				return fmt.Errorf("marshal disco#info result: %w", marshalErr)
			}

			respXML, err = state.BOSHSender.Send(state.BoshURL, response)
			if err != nil {
				return fmt.Errorf("failed to send disco#info result: %w", err)
			}
			// Don't sleep — re-evaluate respXML immediately.
			continue
		}

		// Nothing useful yet; sleep and let the next poll deliver more stanzas.
		state.Log.Debugf("[Step09_WaitForJingleOffer] sleep 1 second...")
		time.Sleep(1 * time.Second)
		respXML = "" // force a fresh poll next iteration
	}
}
