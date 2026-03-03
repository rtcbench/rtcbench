package steps

import (
	"fmt"
	"strings"

	"call.zip/plugin/jitsi/internal/model"
)

func Step11_Sender_AnnounceCameraSource(state *model.ConnectionState) error {
	state.RID++

	if strings.TrimSpace(state.Nickname) == "" {
		return fmt.Errorf("Nickname (Endpoint ID) is nil/empty: set state.Nickname before joining the room")
	}
	endpointID := strings.TrimSpace(state.Nickname)

	// Jitsi Meet pattern: after session-accept, update SourceInfo to include "<endpoint>-v0"
	sourceInfoJSON := fmt.Sprintf(`{"%s-v0":{"muted":false}}`, endpointID)

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
		endpointID,
		sourceInfoJSON,
		state.Nickname,
	)

	state.StepsAOF.LogPrintf("=== BEGIN Step11 camera SourceInfo presence ===\n%s\n=== END Step11 ===", requestBody)

	respXML, err := state.BOSHSender.Send(state.BoshURL, requestBody)
	if err != nil {
		return fmt.Errorf("Step11_Sender_AnnounceCameraSource failed: %w", err)
	}

	state.StepsAOF.LogPrintf("[Step11] ✅ Sent camera SourceInfo presence update OK: %s", respXML)
	return nil
}
