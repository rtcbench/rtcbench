package steps

import (
	"fmt"
	"strings"

	"call.zip/plugin/jitsi/internal/model"
	"call.zip/plugin/jitsi/internal/util"
)

// Step01_StartSession starts the BOSH session: sends the initial request, parses SID/AuthID.
func Step01_StartSession(state *model.ConnectionState) error {
	state.RID++

	// The initial request is mostly empty <body>
	requestBody := fmt.Sprintf(
		`<body xmlns="http://jabber.org/protocol/httpbind"
			content="text/xml; charset=utf-8"
			hold="1"
			rid="%d"
			to="%s"
			ver="1.6"
			wait="60"
			xml:lang="en"
			xmlns:xmpp="urn:xmpp:xbosh"
			xmpp:version="1.0" />`,
		state.RID,
		state.Domain,
	)

	respXML, err := state.BOSHSender.Send(state.BoshURL, requestBody)
	if err != nil {
		return fmt.Errorf("start session failed: %w", err)
	}

	// Extract just the <body ...> opening tag for scoped parsing.
	bodyTagStart := strings.Index(respXML, "<body")
	if bodyTagStart == -1 {
		state.StepsAOF.LogPrintf("[Step01] WARN: <body> tag not found at all in response:\n%s", respXML)
		return fmt.Errorf("BOSH server response missing <body> tag")
	}
	bodyTagEnd := strings.Index(respXML[bodyTagStart:], ">")
	if bodyTagEnd == -1 {
		state.StepsAOF.LogPrintf("[Step01] WARN: Malformed <body> tag: no closing '>' found:\n%s", respXML)
		return fmt.Errorf("BOSH server response malformed <body> tag")
	}
	bodyOpenTag := respXML[bodyTagStart : bodyTagStart+bodyTagEnd]

	// Use improved scoped ExtractAttr
	sid := util.ExtractAttr(bodyOpenTag, "sid")
	authid := util.ExtractAttr(bodyOpenTag, "authid")

	state.StepsAOF.LogPrintf("[Step01] DEBUG: Extracted body tag: %s", bodyOpenTag)
	state.StepsAOF.LogPrintf("[Step01] DEBUG: Parsed sid=%q, authid=%q", sid, authid)

	if sid == "" || authid == "" {
		state.StepsAOF.LogPrintf("[Step01] ERROR: sid/authid missing. Full response:\n%s", respXML)
		return fmt.Errorf("BOSH server did not return sid or authid")
	}

	state.Sid = sid
	state.AuthID = authid
	state.Authenticated = false
	state.Bound = false

	state.StepsAOF.LogPrintf("Step01_StartSession OK: SID=%s AUTHID=%s", sid, authid)
	return nil
}
