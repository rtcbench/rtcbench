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
		state.StepsAOF.LogPrintf("[Step05] ERROR sending create session request: %v", err)
		return fmt.Errorf("create session failed: %w", err)
	}

	state.StepsAOF.LogPrintf("[Step05] DEBUG: CreateSession Response XML: %s", respXML)

	// Check for <iq> tag presence
	if !strings.Contains(respXML, "<iq") {
		state.StepsAOF.LogPrintln("[Step05] ERROR: Missing <iq> tag in response.")
		return fmt.Errorf("no <iq> tag in create session response")
	}

	// Check iq type attribute
	iqType := util.ExtractAttr(respXML, "type")
	if iqType != "result" {
		state.StepsAOF.LogPrintf("[Step05] ERROR: Incorrect iq type='%s', expected 'result'.", iqType)
		return fmt.Errorf("unexpected iq type='%s'", iqType)
	}

	state.Bound = true
	state.StepsAOF.LogPrintln("Step05_CreateSession OK: session established successfully.")
	return nil
}
