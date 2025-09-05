package model

import (
	"call.zip/internal/aoflog"
	"call.zip/internal/bosh/httpxml"
)

// ConnectionState is an object built by the signaling flow
type ConnectionState struct {
	// === Bot config ===
	StepsAOF      aoflog.Client
	PionAOF       aoflog.Client
	PktRecvAOF    aoflog.Client
	NetStatsAOF   aoflog.Client
	BOSHSender    httpxml.BOSHSender
	BotRandom     string
	Nickname      string
	LANServerIP   string  // e.g. "10.99.0.210"
	LANClientIP   string  // e.g. "10.99.0.219"
	LogsDir       *string // e.g. "logs/e-2025XXXXXXXX/split/vbot-XXXXXXXX" or nil for no output
	DumpPackets   bool    // Deprecated: functionality gone in 0.3.0
	InstantReplay bool    // Deprecated: instant replay is a preview in 0.3

	// === Static config ===
	BoshURL    string // e.g. https://10.99.0.210/http-bind
	Domain     string // e.g. "10.99.0.210"
	RoomName   string // e.g. "asdf"
	MachineUID string // Unique ID for the client
	RID        int64  // Rolling BOSH request ID

	// === BOSH/XMPP session ===
	Sid           string // BOSH session ID
	AuthID        string // Auth ID from BOSH
	Jid           string // Full JID after bind
	Authenticated bool   // After SASL success
	Bound         bool   // After resource bind
	InRoom        bool   // After <presence>

	FocusJid string // Jicofo's JID

	// === ICE/STUN/TURN ===
	IceServers []ICEServer // Discovered via extdisco

	ICEUfrag    string // ICE username fragment
	ICEPwd      string // ICE password
	Fingerprint string // DTLS fingerprint

	Candidates []ICECandidate // ICE host/reflexive/relay candidates

	// === Jingle ===
	JingleSID string // Jingle session ID
	RemoteSDP string // Raw Jingle offer
	LocalSDP  string // Generated SDP

	// === Optional ===
	ColibriWebSocketURL string // For optional low-level stats

	// === SSRCs ===
	Sources map[string][]uint32 // Example: map[mediaType][]SSRCs

	ForcedCandidateIP       string
	ForcedCandidatePort     int
	ForcedCandidatePriority int
}

// ICEServer holds one STUN/TURN server entry
type ICEServer struct {
	Type       string // stun, turn, turns
	Transport  string // udp, tcp
	Host       string
	Port       int
	Username   string // for TURN
	Password   string // for TURN
	Expiry     string // expires ISO8601
	Restricted bool   // true if restricted TURN creds
}

// ICECandidate models one <candidate> from Jingle
type ICECandidate struct {
	Foundation string
	Component  int
	Protocol   string // udp, tcp
	Priority   uint32
	IP         string
	Port       int
	Type       string // host, srflx, relay
	Generation int
	ID         string // candidate ID
	Network    int
}
