package steps

import (
	"bytes"
	"encoding/xml"
	"strconv"
)

// XMPP/BOSH namespace constants.
const (
	nsBOSH     = "http://jabber.org/protocol/httpbind"
	nsClient   = "jabber:client"
	nsXBOSH    = "urn:xmpp:xbosh"
	nsXML      = "http://www.w3.org/XML/1998/namespace"
	nsSASL     = "urn:ietf:params:xml:ns:xmpp-sasl"
	nsBind     = "urn:ietf:params:xml:ns:xmpp-bind"
	nsSession  = "urn:ietf:params:xml:ns:xmpp-session"
	nsExtDisco = "urn:xmpp:extdisco:2"
	nsFocus    = "http://jitsi.org/protocol/focus"
	nsMUC      = "http://jabber.org/protocol/muc"
	nsNick     = "http://jabber.org/protocol/nick"
	nsDisco    = "http://jabber.org/protocol/disco#info"
	nsJingle   = "urn:xmpp:jingle:1"
	nsGrouping = "urn:xmpp:jingle:apps:grouping:0"
	nsRTP      = "urn:xmpp:jingle:apps:rtp:1"
	nsRTCPFB   = "urn:xmpp:jingle:apps:rtp:rtcp-fb:0"
	nsHdrExt   = "urn:xmpp:jingle:apps:rtp:rtp-hdrext:0"
	nsSSMA     = "urn:xmpp:jingle:apps:rtp:ssma:0"
	nsICEUDP   = "urn:xmpp:jingle:transports:ice-udp:1"
	nsDTLS     = "urn:xmpp:jingle:apps:dtls:0"
	nsSCTP     = "urn:xmpp:jingle:transports:dtls-sctp:1"
	nsPing     = "urn:xmpp:ping"
)

// marshalBOSH builds a BOSH <body> XML string. Extra attributes are appended
// after rid/sid. Child elements are encoded inside the <body> tags.
func marshalBOSH(rid int64, sid string, extraAttrs []xml.Attr, children ...any) (string, error) {
	var buf bytes.Buffer
	enc := xml.NewEncoder(&buf)

	attrs := []xml.Attr{
		{Name: xml.Name{Local: "rid"}, Value: strconv.FormatInt(rid, 10)},
	}
	if sid != "" {
		attrs = append(attrs, xml.Attr{Name: xml.Name{Local: "sid"}, Value: sid})
	}
	attrs = append(attrs, extraAttrs...)

	start := xml.StartElement{
		Name: xml.Name{Space: nsBOSH, Local: "body"},
		Attr: attrs,
	}

	if err := enc.EncodeToken(start); err != nil {
		return "", err
	}
	for _, child := range children {
		if err := enc.Encode(child); err != nil {
			return "", err
		}
	}
	if err := enc.EncodeToken(start.End()); err != nil {
		return "", err
	}
	if err := enc.Flush(); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// --- SASL Auth (Step02) ---

type SASLAuth struct {
	XMLName   xml.Name `xml:"urn:ietf:params:xml:ns:xmpp-sasl auth"`
	Mechanism string   `xml:"mechanism,attr"`
}

// --- Resource Bind (Step04) ---

type IQBind struct {
	XMLName xml.Name `xml:"jabber:client iq"`
	ID      string   `xml:"id,attr"`
	Type    string   `xml:"type,attr"`
	Bind    XMPPBind
}

type XMPPBind struct {
	XMLName xml.Name `xml:"urn:ietf:params:xml:ns:xmpp-bind bind"`
}

// --- Session (Step05) ---

type IQSession struct {
	XMLName xml.Name    `xml:"jabber:client iq"`
	ID      string      `xml:"id,attr"`
	Type    string      `xml:"type,attr"`
	Session XMPPSession
}

type XMPPSession struct {
	XMLName xml.Name `xml:"urn:ietf:params:xml:ns:xmpp-session session"`
}

// --- Discover Services (Step06) ---

type IQDiscoverServices struct {
	XMLName  xml.Name         `xml:"jabber:client iq"`
	ID       string           `xml:"id,attr"`
	Type     string           `xml:"type,attr"`
	To       string           `xml:"to,attr"`
	Services ExtDiscoServices
}

type ExtDiscoServices struct {
	XMLName xml.Name `xml:"urn:xmpp:extdisco:2 services"`
}

// --- Create Conference (Step07) ---

type IQConference struct {
	XMLName    xml.Name        `xml:"jabber:client iq"`
	ID         string          `xml:"id,attr"`
	Type       string          `xml:"type,attr"`
	To         string          `xml:"to,attr"`
	Conference FocusConference
}

type FocusConference struct {
	XMLName    xml.Name        `xml:"http://jitsi.org/protocol/focus conference"`
	Room       string          `xml:"room,attr"`
	MachineUID string          `xml:"machine-uid,attr"`
	Properties []FocusProperty
}

type FocusProperty struct {
	XMLName xml.Name `xml:"http://jitsi.org/protocol/focus property"`
	Name    string   `xml:"name,attr"`
	Value   string   `xml:"value,attr"`
}

// --- Presence (Step08, Step11) ---

type MUCPresence struct {
	XMLName   xml.Name  `xml:"jabber:client presence"`
	To        string    `xml:"to,attr"`
	MUC       MUCX
	SourceInfo SourceInfoElem
	CodecList CodecListElem
	Nick      NickElem
}

type MUCX struct {
	XMLName xml.Name `xml:"http://jabber.org/protocol/muc x"`
}

type SourceInfoElem struct {
	XMLName xml.Name `xml:"jabber:client SourceInfo"`
	Value   string   `xml:",chardata"`
}

type CodecListElem struct {
	XMLName xml.Name `xml:"jabber:client jitsi_participant_codecList"`
	Value   string   `xml:",chardata"`
}

type NickElem struct {
	XMLName xml.Name `xml:"http://jabber.org/protocol/nick nick"`
	Value   string   `xml:",chardata"`
}

// --- Disco#info response (Step09) ---

type IQDiscoInfoResult struct {
	XMLName xml.Name       `xml:"jabber:client iq"`
	From    string         `xml:"from,attr"`
	To      string         `xml:"to,attr"`
	ID      string         `xml:"id,attr"`
	Type    string         `xml:"type,attr"`
	Query   DiscoInfoQuery
}

type DiscoInfoQuery struct {
	XMLName    xml.Name        `xml:"http://jabber.org/protocol/disco#info query"`
	Identities []DiscoIdentity
	Features   []DiscoFeature
}

type DiscoIdentity struct {
	XMLName  xml.Name `xml:"http://jabber.org/protocol/disco#info identity"`
	Category string   `xml:"category,attr"`
	Type     string   `xml:"type,attr"`
	Name     string   `xml:"name,attr"`
}

type DiscoFeature struct {
	XMLName xml.Name `xml:"http://jabber.org/protocol/disco#info feature"`
	Var     string   `xml:"var,attr"`
}

// --- Jingle session-accept (Step10, Step10_sender) ---

type IQJingle struct {
	XMLName xml.Name `xml:"jabber:client iq"`
	ID      string   `xml:"id,attr"`
	To      string   `xml:"to,attr"`
	Type    string   `xml:"type,attr"`
	Jingle  JingleElem
}

type JingleElem struct {
	XMLName   xml.Name         `xml:"urn:xmpp:jingle:1 jingle"`
	Action    string           `xml:"action,attr"`
	Initiator string           `xml:"initiator,attr"`
	Responder string           `xml:"responder,attr"`
	SID       string           `xml:"sid,attr"`
	Group     JingleGroup
	Contents  []JingleContent
}

type JingleGroup struct {
	XMLName   xml.Name              `xml:"urn:xmpp:jingle:apps:grouping:0 group"`
	Semantics string                `xml:"semantics,attr"`
	Contents  []JingleGroupContent
}

type JingleGroupContent struct {
	XMLName xml.Name `xml:"urn:xmpp:jingle:apps:grouping:0 content"`
	Name    string   `xml:"name,attr"`
}

type JingleContent struct {
	XMLName     xml.Name        `xml:"urn:xmpp:jingle:1 content"`
	Creator     string          `xml:"creator,attr"`
	Name        string          `xml:"name,attr"`
	Senders     string          `xml:"senders,attr,omitempty"`
	Description *RTPDescription
	Transport   ICETransport
}

type RTPDescription struct {
	XMLName          xml.Name          `xml:"urn:xmpp:jingle:apps:rtp:1 description"`
	Media            string            `xml:"media,attr"`
	PayloadTypes     []PayloadType
	Sources          []RTPSource
	RTCPMux          *RTCPMuxElem
	HeaderExts       []RTPHeaderExt
	ExtmapAllowMixed *ExtmapAllowMixed
}

type PayloadType struct {
	XMLName    xml.Name       `xml:"urn:xmpp:jingle:apps:rtp:1 payload-type"`
	ID         string         `xml:"id,attr"`
	Name       string         `xml:"name,attr"`
	Clockrate  string         `xml:"clockrate,attr"`
	Channels   string         `xml:"channels,attr,omitempty"`
	Parameters []RTPParameter
	RTCPFBs    []RTCPFeedback
}

type RTPParameter struct {
	XMLName xml.Name `xml:"urn:xmpp:jingle:apps:rtp:1 parameter"`
	Name    string   `xml:"name,attr"`
	Value   string   `xml:"value,attr"`
}

type RTCPFeedback struct {
	XMLName xml.Name `xml:"urn:xmpp:jingle:apps:rtp:rtcp-fb:0 rtcp-fb"`
	Type    string   `xml:"type,attr"`
	Subtype string   `xml:"subtype,attr,omitempty"`
}

type RTCPMuxElem struct {
	XMLName xml.Name `xml:"urn:xmpp:jingle:apps:rtp:1 rtcp-mux"`
}

type RTPHeaderExt struct {
	XMLName xml.Name `xml:"urn:xmpp:jingle:apps:rtp:rtp-hdrext:0 rtp-hdrext"`
	ID      string   `xml:"id,attr"`
	URI     string   `xml:"uri,attr"`
}

type ExtmapAllowMixed struct {
	XMLName xml.Name `xml:"urn:xmpp:jingle:apps:rtp:rtp-hdrext:0 extmap-allow-mixed"`
}

type RTPSource struct {
	XMLName    xml.Name        `xml:"urn:xmpp:jingle:apps:rtp:ssma:0 source"`
	SourceName string          `xml:"name,attr"`
	SSRC       string          `xml:"ssrc,attr"`
	VideoType  string          `xml:"videoType,attr"`
	Parameters []SSMAParameter
}

type SSMAParameter struct {
	XMLName xml.Name `xml:"urn:xmpp:jingle:apps:rtp:ssma:0 parameter"`
	Name    string   `xml:"name,attr"`
	Value   string   `xml:"value,attr"`
}

type ICETransport struct {
	XMLName     xml.Name         `xml:"urn:xmpp:jingle:transports:ice-udp:1 transport"`
	Ufrag       string           `xml:"ufrag,attr,omitempty"`
	Pwd         string           `xml:"pwd,attr,omitempty"`
	SCTPMap     *SCTPMapElem
	Fingerprint *DTLSFingerprint
	Candidate   *ICECandidateElem
}

type DTLSFingerprint struct {
	XMLName xml.Name `xml:"urn:xmpp:jingle:apps:dtls:0 fingerprint"`
	Hash    string   `xml:"hash,attr"`
	Setup   string   `xml:"setup,attr"`
	Value   string   `xml:",chardata"`
}

type ICECandidateElem struct {
	XMLName    xml.Name `xml:"urn:xmpp:jingle:transports:ice-udp:1 candidate"`
	Foundation string   `xml:"foundation,attr"`
	Component  string   `xml:"component,attr"`
	Protocol   string   `xml:"protocol,attr"`
	IP         string   `xml:"ip,attr"`
	Port       string   `xml:"port,attr"`
	Priority   string   `xml:"priority,attr"`
	Type       string   `xml:"type,attr"`
}

type SCTPMapElem struct {
	XMLName  xml.Name `xml:"urn:xmpp:jingle:transports:dtls-sctp:1 sctpmap"`
	Number   string   `xml:"number,attr"`
	Protocol string   `xml:"protocol,attr"`
	Streams  string   `xml:"streams,attr"`
}

// --- XMPP Ping (keepalive in handshake.go) ---

type IQPing struct {
	XMLName xml.Name `xml:"jabber:client iq"`
	ID      string   `xml:"id,attr"`
	To      string   `xml:"to,attr"`
	Type    string   `xml:"type,attr"`
	Ping    XMPPPing
}

type XMPPPing struct {
	XMLName xml.Name `xml:"urn:xmpp:ping ping"`
}
