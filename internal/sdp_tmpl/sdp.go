package sdp_tmpl

import (
	"bytes"
	_ "embed"
	"strings"
	"text/template"
)

//go:embed sdp.tmpl
var sdpTmpl string

type SDPState struct {
	ICEUfrag      string
	ICEPwd        string
	Fingerprint   string
	InstantReplay bool
}

var (
	tpl = template.Must(template.New("sdp").Parse(sdpTmpl))
)

// RenderSDP renders the sdp.tmpl file, removes blank lines, and ensures SDP string ends in newline
func RenderSDP(state SDPState) (string, error) {
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, state); err != nil {
		return "", err
	}
	bufStr := buf.String()
	sb := strings.Builder{}
	for _, line := range strings.Split(bufStr, "\n") {
		sb.WriteString(strings.TrimSpace(line))
		sb.WriteString("\n")
	}
	return sb.String(), nil
}
