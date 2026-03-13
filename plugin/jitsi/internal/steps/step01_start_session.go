package steps

import (
	"encoding/xml"
	"fmt"
	"strings"

	"call.zip/plugin/jitsi/internal/model"
	"call.zip/plugin/jitsi/internal/util"
)

// Step01_StartSession starts the BOSH session: sends the initial request, parses SID/AuthID.
func Step01_StartSession(state *model.ConnectionState) error {
	state.RID++

	requestBody, err := marshalBOSH(state.RID, "", []xml.Attr{
		{Name: xml.Name{Local: "content"}, Value: "text/xml; charset=utf-8"},
		{Name: xml.Name{Local: "hold"}, Value: "1"},
		{Name: xml.Name{Local: "to"}, Value: state.Domain},
		{Name: xml.Name{Local: "ver"}, Value: "1.6"},
		{Name: xml.Name{Local: "wait"}, Value: "60"},
		{Name: xml.Name{Space: nsXML, Local: "lang"}, Value: "en"},
		{Name: xml.Name{Space: nsXBOSH, Local: "version"}, Value: "1.0"},
	})
	if err != nil {
		return fmt.Errorf("marshal start session: %w", err)
	}

	respXML, err := state.BOSHSender.Send(state.BoshURL, requestBody)
	if err != nil {
		return fmt.Errorf("start session failed: %w", err)
	}

	// Extract just the <body ...> opening tag for scoped parsing.
	bodyTagStart := strings.Index(respXML, "<body")
	if bodyTagStart == -1 {
		state.Log.Errorf("[Step01] <body> tag not found at all in response:\n%s", respXML)
		return fmt.Errorf("BOSH server response missing <body> tag")
	}
	bodyTagEnd := strings.Index(respXML[bodyTagStart:], ">")
	if bodyTagEnd == -1 {
		state.Log.Errorf("[Step01] Malformed <body> tag: no closing '>' found:\n%s", respXML)
		return fmt.Errorf("BOSH server response malformed <body> tag")
	}
	bodyOpenTag := respXML[bodyTagStart : bodyTagStart+bodyTagEnd]

	// Use improved scoped ExtractAttr
	sid := util.ExtractAttr(bodyOpenTag, "sid")
	authid := util.ExtractAttr(bodyOpenTag, "authid")

	state.Log.Debugf("[Step01] Extracted body tag: %s", bodyOpenTag)
	state.Log.Debugf("[Step01] Parsed sid=%q, authid=%q", sid, authid)

	if sid == "" || authid == "" {
		state.Log.Errorf("[Step01] sid/authid missing. Full response:\n%s", respXML)
		return fmt.Errorf("BOSH server did not return sid or authid")
	}

	state.Sid = sid
	state.AuthID = authid
	state.Authenticated = false
	state.Bound = false

	state.Log.Infof("Step01_StartSession OK: SID=%s AUTHID=%s", sid, authid)
	return nil
}
