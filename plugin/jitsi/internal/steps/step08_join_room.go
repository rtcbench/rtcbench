package steps

import (
	"fmt"
	"strings"

	"call.zip/plugin/jitsi/internal/model"
)

// Step08_JoinRoom sends the initial <presence/> stanza to the MUC room.
func Step08_JoinRoom(state *model.ConnectionState) error {
	state.RID++

	sourceInfoJSON := "{}"

	requestBody := fmt.Sprintf(`
<body xmlns="http://jabber.org/protocol/httpbind"
      rid="%d"
      sid="%s">
  <presence xmlns="jabber:client"
            to="%s@conference.%s/%s">
    <x xmlns="http://jabber.org/protocol/muc" />
    <SourceInfo>%s</SourceInfo>
    <jitsi_participant_codecList>vp9</jitsi_participant_codecList>
    <nick xmlns="http://jabber.org/protocol/nick">%s</nick>
  </presence>
</body>`,
		state.RID,
		state.Sid,
		state.RoomName,
		state.Domain,
		state.Nickname,
		sourceInfoJSON,
		state.Nickname,
	)

	respXML, err := state.BOSHSender.Send(state.BoshURL, requestBody)
	if err != nil {
		return fmt.Errorf("join room failed: %w", err)
	}

	// Very simple check: did we get back a presence response?
	if !strings.Contains(respXML, "<presence") {
		return fmt.Errorf("did not receive presence confirmation:\n%s", respXML)
	}

	state.InRoom = true
	state.PendingBOSHResponse = respXML
	state.StepsAOF.LogPrintf("Step08_JoinRoom OK: joined room as %s", state.Nickname)
	return nil
}
