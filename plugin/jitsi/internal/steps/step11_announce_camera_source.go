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

	requestBody, err := marshalBOSH(state.RID, state.Sid, nil,
		MUCPresence{
			To:         fmt.Sprintf("%s@conference.%s/%s", state.RoomName, state.Domain, endpointID),
			SourceInfo: SourceInfoElem{Value: sourceInfoJSON},
			CodecList:  CodecListElem{Value: "vp9"},
			Nick:       NickElem{Value: state.Nickname},
		},
	)
	if err != nil {
		return fmt.Errorf("marshal camera source presence: %w", err)
	}

	state.StepsAOF.LogPrintf("=== BEGIN Step11 camera SourceInfo presence ===\n%s\n=== END Step11 ===", requestBody)

	respXML, err := state.BOSHSender.Send(state.BoshURL, requestBody)
	if err != nil {
		return fmt.Errorf("Step11_Sender_AnnounceCameraSource failed: %w", err)
	}

	state.StepsAOF.LogPrintf("[Step11] Sent camera SourceInfo presence update OK: %s", respXML)
	return nil
}
