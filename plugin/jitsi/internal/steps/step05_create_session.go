package steps

import (
	"fmt"
	"strings"

	"call.zip/plugin/jitsi/internal/model"
	"call.zip/plugin/jitsi/internal/util"
)

// Step05_CreateSession sends the IQ to create a session.
// Completes XMPP session negotiation after resource binding.
func Step05_CreateSession(state *model.ConnectionState) error {
	state.RID++

	requestBody, err := marshalBOSH(state.RID, state.Sid, nil,
		IQSession{ID: "_session_auth", Type: "set"},
	)
	if err != nil {
		return fmt.Errorf("marshal create session: %w", err)
	}

	respXML, err := state.BOSHSender.Send(state.BoshURL, requestBody)
	if err != nil {
		state.Log.Errorf("[Step05] sending create session request: %v", err)
		return fmt.Errorf("create session failed: %w", err)
	}

	state.Log.Debugf("[Step05] CreateSession Response XML: %s", respXML)

	// Check for <iq> tag presence
	if !strings.Contains(respXML, "<iq") {
		state.Log.Errorf("[Step05] Missing <iq> tag in response.")
		return fmt.Errorf("no <iq> tag in create session response")
	}

	// Check iq type attribute
	iqType := util.ExtractAttr(respXML, "type")
	if iqType != "result" {
		state.Log.Errorf("[Step05] Incorrect iq type='%s', expected 'result'.", iqType)
		return fmt.Errorf("unexpected iq type='%s'", iqType)
	}

	state.Bound = true
	state.Log.Infof("Step05_CreateSession OK: session established successfully.")
	return nil
}
