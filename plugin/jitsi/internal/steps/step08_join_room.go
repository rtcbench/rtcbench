package steps

import (
	"fmt"
	"strings"

	"github.com/rtcbench/rtcbench/plugin/jitsi/internal/model"
)

// Step08_JoinRoom sends the initial <presence/> stanza to the MUC room.
func Step08_JoinRoom(state *model.ConnectionState) error {
	state.RID++

	requestBody, err := marshalBOSH(state.RID, state.Sid, nil,
		MUCPresence{
			To: fmt.Sprintf("%s@conference.%s/%s", state.RoomName, state.Domain, state.Nickname),
			SourceInfo: SourceInfoElem{Value: "{}"},
			CodecList:  CodecListElem{Value: "vp9"},
			Nick:       NickElem{Value: state.Nickname},
		},
	)
	if err != nil {
		return fmt.Errorf("marshal join room: %w", err)
	}

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
	state.Log.Infof("Step08_JoinRoom OK: joined room as %s", state.Nickname)
	return nil
}
