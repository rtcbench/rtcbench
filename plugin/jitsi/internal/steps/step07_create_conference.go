package steps

import (
	"fmt"

	"github.com/rtcbench/rtcbench/plugin/jitsi/internal/model"
	"github.com/rtcbench/rtcbench/plugin/jitsi/internal/util"
)

// Step07_CreateConference sends the IQ to the focus component to create/join the conference.
func Step07_CreateConference(state *model.ConnectionState) error {
	state.RID++

	requestBody, err := marshalBOSH(state.RID, state.Sid, nil,
		IQConference{
			ID:   "create-conference",
			Type: "set",
			To:   fmt.Sprintf("focus.%s", state.Domain),
			Conference: FocusConference{
				Room:       fmt.Sprintf("%s@conference.%s", state.RoomName, state.Domain),
				MachineUID: state.MachineUID,
				Properties: []FocusProperty{
					{Name: "visitors-version", Value: "1"},
				},
			},
		},
	)
	if err != nil {
		return fmt.Errorf("marshal create conference: %w", err)
	}

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

	state.Log.Infof("Step07_CreateConference OK: FocusJID=%s", focusJid)
	return nil
}
