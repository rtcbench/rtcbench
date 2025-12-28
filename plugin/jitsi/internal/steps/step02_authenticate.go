package steps

import (
	"fmt"
	"strings"

	"call.zip/plugin/jitsi/internal/model"
)

// Step02_Authenticate sends the SASL auth request (ANONYMOUS) and checks for success.
func Step02_Authenticate(state *model.ConnectionState) error {
	state.RID++

	requestBody := fmt.Sprintf(
		`<body xmlns="http://jabber.org/protocol/httpbind"
			rid="%d"
			sid="%s">
			<auth xmlns="urn:ietf:params:xml:ns:xmpp-sasl" mechanism="ANONYMOUS"/>
		</body>`,
		state.RID, state.Sid,
	)

	respXML, err := state.BOSHSender.Send(state.BoshURL, requestBody)
	if err != nil {
		return fmt.Errorf("Step02_Authenticate failed: %w", err)
	}

	if !strings.Contains(respXML, "<success") {
		return fmt.Errorf("authentication failed or missing <success>:\n%s", respXML)
	}

	state.Authenticated = true

	state.StepsAOF.LogPrintln("Step02_Authenticate OK")
	return nil
}
