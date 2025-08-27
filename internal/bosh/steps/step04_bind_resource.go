package steps

import (
	"call.zip/internal/bosh/model"
	"call.zip/internal/bosh/util"
	"fmt"
)

// Step04_BindResource sends an <iq type='set'> to bind a resource and gets your JID.
func Step04_BindResource(state *model.ConnectionState) error {
	state.RID++

	requestBody := fmt.Sprintf(
		`<body xmlns="http://jabber.org/protocol/httpbind"
				rid="%d"
				sid="%s">
			<iq xmlns="jabber:client" id="_bind_auth" type="set">
				<bind xmlns="urn:ietf:params:xml:ns:xmpp-bind"/>
			</iq>
		</body>`,
		state.RID, state.Sid,
	)

	respXML, err := state.BOSHSender.Send(state.BoshURL, requestBody)
	if err != nil {
		return fmt.Errorf("Step04_BindResource failed: %w", err)
	}

	jid := util.ExtractTagValue(respXML, "jid")
	if jid == "" {
		return fmt.Errorf("Step04_BindResource: JID not found in response:\n%s", respXML)
	}

	state.Jid = jid
	state.Bound = true

	state.StepsAOF.LogPrintf("Step04_BindResource OK: JID=%s", jid)
	return nil
}
