package steps

import (
	"fmt"
	"strconv"

	"call.zip/plugin/jitsi/internal/model"
	"call.zip/plugin/jitsi/internal/util"
)

// Step06_DiscoverServices queries the STUN/TURN servers via extdisco.
func Step06_DiscoverServices(state *model.ConnectionState) error {
	state.RID++

	requestBody := fmt.Sprintf(`
<body xmlns="http://jabber.org/protocol/httpbind"
      rid="%d"
      sid="%s">
  <iq xmlns="jabber:client" id="discover-services" type="get" to="%s">
    <services xmlns="urn:xmpp:extdisco:2"/>
  </iq>
</body>`,
		state.RID, state.Sid, state.Jid)

	respXML, err := state.BOSHSender.Send(state.BoshURL, requestBody)
	if err != nil {
		return fmt.Errorf("discover services failed: %w", err)
	}

	services := util.ExtractAllTags(respXML, "service")
	for _, svc := range services {
		sType := util.ExtractAttr(svc, "type") // stun, turn, turns
		host := util.ExtractAttr(svc, "host")
		portStr := util.ExtractAttr(svc, "port")
		transport := util.ExtractAttr(svc, "transport")
		username := util.ExtractAttr(svc, "username")
		password := util.ExtractAttr(svc, "password")
		expiry := util.ExtractAttr(svc, "expires")
		restricted := util.ExtractAttr(svc, "restricted") == "1"

		port := 0
		if portStr != "" {
			p, err := strconv.Atoi(portStr)
			if err == nil {
				port = p
			}
		}

		server := model.ICEServer{
			Type:       sType,
			Transport:  transport,
			Host:       host,
			Port:       port,
			Username:   username,
			Password:   password,
			Expiry:     expiry,
			Restricted: restricted,
		}

		state.IceServers = append(state.IceServers, server)
	}

	state.StepsAOF.LogPrintf("Step06_DiscoverServices OK: found %d ICE servers", len(state.IceServers))
	return nil
}
