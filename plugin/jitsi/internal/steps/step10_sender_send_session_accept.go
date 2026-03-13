package steps

import (
	"fmt"
	"regexp"
	"strings"

	"call.zip/plugin/jitsi/internal/model"
)

// extractPrimaryVideoSSRCAndMSID extracts a single (primary) SSRC and its MSID from the SDP.
// This is the "no RTX" version: it does NOT look for ssrc-group:FID.
//
// It tries to find a video SSRC by locating the first "a=ssrc:<id> msid:<...>" line.
// Example:
//
//	a=ssrc:3816515605 msid:- 52305b34-19ac-42ef-8ee6-337e9378c50d
func extractPrimaryVideoSSRCAndMSID(sdp string) (primarySSRC, msid string) {
	// Find the first SSRC that has an msid line.
	re := regexp.MustCompile(`(?m)^a=ssrc:(\d+)\s+msid:(.+)\s*$`)
	m := re.FindStringSubmatch(sdp)
	if len(m) != 3 {
		return "", ""
	}
	return strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
}

func Step10_Sender_SendSessionAccept(state *model.ConnectionState) error {
	state.RID++

	if state.LocalSDP == "" {
		return fmt.Errorf("LocalSDP is empty")
	}
	if strings.TrimSpace(state.Nickname) == "" {
		return fmt.Errorf("Nickname (Endpoint ID) is nil/empty: set state.Nickname before joining the room")
	}
	endpointID := strings.TrimSpace(state.Nickname)

	primarySSRC, msid := extractPrimaryVideoSSRCAndMSID(state.LocalSDP)
	if primarySSRC == "" || msid == "" {
		return fmt.Errorf(
			"sender mode: could not extract primary video SSRC/MSID from LocalSDP (ssrc=%q msid=%q). "+
				"Ensure startPion adds a local VP9 sending track (AddTrack) so the answer contains a=ssrc:... msid:...",
			primarySSRC, msid,
		)
	}

	// Optional: store for debugging if your state has these pointer fields.
	if state.SenderSSRC != nil {
		*state.SenderSSRC = primarySSRC
	}
	if state.SenderMSID != nil {
		*state.SenderMSID = msid
	}

	sourceName := fmt.Sprintf("%s-v0", endpointID)
	videoSources := []RTPSource{
		{
			SourceName: sourceName,
			SSRC:       primarySSRC,
			VideoType:  "camera",
			Parameters: []SSMAParameter{
				{Name: "msid", Value: msid},
			},
		},
	}

	iq, err := buildSessionAcceptIQ(state, "both", videoSources)
	if err != nil {
		return err
	}

	requestBody, err := marshalBOSH(state.RID, state.Sid, nil, iq)
	if err != nil {
		return fmt.Errorf("marshal session-accept (sender): %w", err)
	}

	state.Log.Debugf("=== BEGIN session-accept (sender, no-rtx) ===\n%s\n=== END session-accept (sender, no-rtx) ===", requestBody)

	respXML, err := state.BOSHSender.Send(state.BoshURL, requestBody)
	if err != nil {
		return fmt.Errorf("session-accept (sender, no-rtx) failed: %w", err)
	}

	if strings.Contains(respXML, "type='error'") || strings.Contains(respXML, `type="error"`) {
		return fmt.Errorf("session-accept rejected by Jicofo: %s", respXML)
	}

	state.Log.Infof("[Step10_Sender] session-accept sent OK: %s", respXML)
	return nil
}
