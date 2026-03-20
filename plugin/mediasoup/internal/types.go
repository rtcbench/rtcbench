package internal

// mediasoup JSON types for the protoo signaling protocol.

// RtpCapabilities describes the router's (or device's) supported codecs and
// header extensions.
type RtpCapabilities struct {
	Codecs           []RtpCodecCapability `json:"codecs"`
	HeaderExtensions []RtpHeaderExtension `json:"headerExtensions"`
}

// RtpCodecCapability is a codec entry in router/device capabilities.
type RtpCodecCapability struct {
	Kind                 string         `json:"kind"`
	MimeType             string         `json:"mimeType"`
	PreferredPayloadType int            `json:"preferredPayloadType"`
	ClockRate            int            `json:"clockRate"`
	Channels             int            `json:"channels,omitempty"`
	Parameters           map[string]any `json:"parameters,omitempty"`
	RtcpFeedback         []RtcpFeedback `json:"rtcpFeedback,omitempty"`
}

// RtcpFeedback describes a single RTCP feedback mechanism.
type RtcpFeedback struct {
	Type      string `json:"type"`
	Parameter string `json:"parameter,omitempty"`
}

// RtpHeaderExtension is a header extension in router/device capabilities.
type RtpHeaderExtension struct {
	Kind             string `json:"kind"`
	URI              string `json:"uri"`
	PreferredID      int    `json:"preferredId"`
	PreferredEncrypt bool   `json:"preferredEncrypt"`
	Direction        string `json:"direction,omitempty"`
}

// TransportOptions are returned by createWebRtcTransport.
type TransportOptions struct {
	ID             string         `json:"id"`
	IceParameters  IceParameters  `json:"iceParameters"`
	IceCandidates  []IceCandidate `json:"iceCandidates"`
	DtlsParameters DtlsParameters `json:"dtlsParameters"`
}

// IceParameters contains the ICE credentials for a transport.
type IceParameters struct {
	UsernameFragment string `json:"usernameFragment"`
	Password         string `json:"password"`
	IceLite          bool   `json:"iceLite"`
}

// IceCandidate is a single ICE candidate from a transport.
type IceCandidate struct {
	Foundation string `json:"foundation"`
	Priority   uint32 `json:"priority"`
	Address    string `json:"address,omitempty"` // mediasoup v3.13+
	IP         string `json:"ip,omitempty"`      // older mediasoup
	Protocol   string `json:"protocol"`
	Port       uint16 `json:"port"`
	Type       string `json:"type"`
}

// Addr returns the candidate's IP address, preferring the newer "address" field.
func (c IceCandidate) Addr() string {
	if c.Address != "" {
		return c.Address
	}
	return c.IP
}

// DtlsParameters contains the DTLS fingerprints and role for a transport.
type DtlsParameters struct {
	Fingerprints []DtlsFingerprint `json:"fingerprints"`
	Role         string            `json:"role"`
}

// DtlsFingerprint is a single DTLS certificate fingerprint.
type DtlsFingerprint struct {
	Algorithm string `json:"algorithm"`
	Value     string `json:"value"`
}

// RtpParameters describe how RTP is sent/received for a producer or consumer.
type RtpParameters struct {
	Codecs           []RtpCodecParameters `json:"codecs"`
	HeaderExtensions []RtpHeaderExtParam  `json:"headerExtensions,omitempty"`
	Encodings        []RtpEncoding        `json:"encodings"`
	Rtcp             *RtcpParameters      `json:"rtcp,omitempty"`
	Mid              string               `json:"mid,omitempty"`
}

// RtpCodecParameters is a codec in producer/consumer RTP parameters.
type RtpCodecParameters struct {
	MimeType     string         `json:"mimeType"`
	PayloadType  int            `json:"payloadType"`
	ClockRate    int            `json:"clockRate"`
	Channels     int            `json:"channels,omitempty"`
	Parameters   map[string]any `json:"parameters,omitempty"`
	RtcpFeedback []RtcpFeedback `json:"rtcpFeedback,omitempty"`
}

// RtpHeaderExtParam is a header extension in producer/consumer RTP parameters.
type RtpHeaderExtParam struct {
	URI        string `json:"uri"`
	ID         int    `json:"id"`
	Encrypt    bool   `json:"encrypt,omitempty"`
	Parameters any    `json:"parameters,omitempty"`
}

// RtpEncoding describes one encoding layer (single or SVC).
type RtpEncoding struct {
	SSRC            *uint32 `json:"ssrc,omitempty"`
	Rid             string  `json:"rid,omitempty"`
	ScalabilityMode string  `json:"scalabilityMode,omitempty"`
	Dtx             bool    `json:"dtx,omitempty"`
}

// RtcpParameters describes RTCP behaviour for a producer/consumer.
type RtcpParameters struct {
	CNAME       string `json:"cname,omitempty"`
	ReducedSize bool   `json:"reducedSize,omitempty"`
	Mux         bool   `json:"mux,omitempty"`
}

// ConsumerData is the payload of a "newConsumer" server request.
type ConsumerData struct {
	PeerID         string         `json:"peerId"`
	ProducerID     string         `json:"producerId"`
	ID             string         `json:"id"`
	Kind           string         `json:"kind"`
	RtpParameters  RtpParameters  `json:"rtpParameters"`
	Type           string         `json:"type"`
	ProducerPaused bool           `json:"producerPaused"`
	AppData        map[string]any `json:"appData,omitempty"`
}

// ProduceResult is returned by the "produce" request.
type ProduceResult struct {
	ID string `json:"id"`
}

// JoinResult is returned by the "join" request.
type JoinResult struct {
	Peers []PeerInfo `json:"peers"`
}

// PeerInfo describes one peer in a room.
type PeerInfo struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}
