package steps

import (
	"call.zip/internal/bosh/model"
	"call.zip/internal/bosh/util"
	"fmt"
)

// Step07_CreateConference sends the IQ to the focus component to create/join the conference.
func Step07_CreateConference(state *model.ConnectionState) error {
	state.RID++

	requestBody := fmt.Sprintf(`
<body xmlns="http://jabber.org/protocol/httpbind"
      rid="%d"
      sid="%s">
  <iq xmlns="jabber:client"
      id="create-conference"
      type="set"
      to="focus.%s">
    <conference xmlns="http://jitsi.org/protocol/focus"
                room="%s@conference.%s"
                machine-uid="%s">
      <property name="visitors-version" value="1"/>
    </conference>
  </iq>
</body>`,
		state.RID,
		state.Sid,
		state.Domain,
		state.RoomName,
		state.Domain,
		state.MachineUID,
	)

	respXML, err := state.BOSHSender.Send(state.BoshURL, requestBody)
	if err != nil {
		return fmt.Errorf("create conference failed: %w", err)
	}

	// Extract focusjid for later steps
	focusJid := util.ExtractAttr(respXML, "focusjid")
	if focusJid == "" {
		return fmt.Errorf("focusjid not found in response:\n%s", respXML)
	}
	state.FocusJid = focusJid

	state.StepsAOF.LogPrintf("Step07_CreateConference OK: FocusJID=%s", focusJid)
	return nil
}
