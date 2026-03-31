package steps

import (
	"fmt"

	"github.com/rtcbench/rtcbench/plugin/jitsi/internal/model"
	"github.com/rtcbench/rtcbench/plugin/jitsi/internal/util"
)

// Step04_BindResource sends an <iq type='set'> to bind a resource and gets your JID.
func Step04_BindResource(state *model.ConnectionState) error {
	state.RID++

	requestBody, err := marshalBOSH(state.RID, state.Sid, nil,
		IQBind{ID: "_bind_auth", Type: "set"},
	)
	if err != nil {
		return fmt.Errorf("marshal bind resource: %w", err)
	}

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

	state.Log.Infof("Step04_BindResource OK: JID=%s", jid)
	return nil
}
