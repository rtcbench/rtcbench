package steps

import (
	"fmt"
	"strings"
	"time"

	"call.zip/plugin/jitsi/internal/model"
	"call.zip/plugin/jitsi/internal/util"
)

func Step09_WaitForJingleOffer(state *model.ConnectionState) error {
	var respXML string
	var err error

	for {
		if !strings.Contains(respXML, "<jingle") {
			state.RID++
			requestBody := fmt.Sprintf(`
<body xmlns="http://jabber.org/protocol/httpbind"
      rid="%d"
      sid="%s"></body>`,
				state.RID,
				state.Sid,
			)
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

			// Log full match values for session-accept
			state.StepsAOF.LogPrintf(
				"Step09_WaitForJingleOffer OK: SID=%s Initiator=%s ICEUfrag=%s Candidates=%d",
				state.JingleSID, state.FocusJid, state.ICEUfrag, len(state.Candidates),
			)

			return nil
		}

		// Handle disco#info requests while waiting
		if strings.Contains(respXML, "<query xmlns='http://jabber.org/protocol/disco#info'") {
			from := util.ExtractAttr(respXML, "from")
			id := util.ExtractAttr(respXML, "id")

			state.RID++
			response := fmt.Sprintf(`<body xmlns="http://jabber.org/protocol/httpbind"
      rid="%d"
      sid="%s">
  <iq from="%s"
      to="%s"
      id="%s"
      type="result">
    <query xmlns="http://jabber.org/protocol/disco#info">
      <identity category="client" type="bot" name="%s"/>
      <feature var="http://jabber.org/protocol/muc"/>
    </query>
  </iq>
</body>`, state.RID, state.Sid, state.Jid, from, id, state.Nickname)

			respXML, err = state.BOSHSender.Send(state.BoshURL, response)
			if err != nil {
				return fmt.Errorf("failed to send disco#info result: %w", err)
			}
		}

		// Loop again until <jingle> is found
		if !strings.Contains(respXML, "<jingle") {
			state.StepsAOF.LogPrintln("[Step09_WaitForJingleOffer] sleep 1 second...")
			time.Sleep(1 * time.Second)
		}
	}
}
