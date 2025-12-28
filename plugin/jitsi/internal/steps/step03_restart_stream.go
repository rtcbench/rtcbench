package steps

import (
	"fmt"
	"strings"

	"call.zip/plugin/jitsi/internal/model"
)

// Step03_RestartStream sends the stream restart request after SASL success.
func Step03_RestartStream(state *model.ConnectionState) error {
	state.RID++

	requestBody := fmt.Sprintf(
		`<body xmlns="http://jabber.org/protocol/httpbind"
			rid="%d"
			sid="%s"
			to="%s"
			xml:lang="en"
			xmpp:restart="true"
			xmlns:xmpp="urn:xmpp:xbosh" />`,
		state.RID, state.Sid, state.LANServerIP,
	)

	respXML, err := state.BOSHSender.Send(state.BoshURL, requestBody)
	if err != nil {
		return fmt.Errorf("Step03_RestartStream failed: %w", err)
	}

	// The server should reply with <features> including bind/session
	if !strings.Contains(respXML, "<bind") {
		return fmt.Errorf("Step03_RestartStream: no <bind> feature found:\n%s", respXML)
	}

	state.StepsAOF.LogPrintln("Step03_RestartStream OK: Stream restarted.")
	return nil
}
