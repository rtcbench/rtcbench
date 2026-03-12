package steps

import (
	"encoding/xml"
	"fmt"
	"strings"

	"call.zip/plugin/jitsi/internal/model"
)

// Step03_RestartStream sends the stream restart request after SASL success.
func Step03_RestartStream(state *model.ConnectionState) error {
	state.RID++

	requestBody, err := marshalBOSH(state.RID, state.Sid, []xml.Attr{
		{Name: xml.Name{Local: "to"}, Value: state.LANServerIP},
		{Name: xml.Name{Space: nsXML, Local: "lang"}, Value: "en"},
		{Name: xml.Name{Space: nsXBOSH, Local: "restart"}, Value: "true"},
	})
	if err != nil {
		return fmt.Errorf("marshal restart stream: %w", err)
	}

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
