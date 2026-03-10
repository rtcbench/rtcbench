package util

import (
	"testing"
)

func TestExtractMultipleTags_SelfClosing(t *testing.T) {
	// Self-closing candidate tags (as seen in Jingle session-initiate)
	xml := `<transport xmlns="urn:xmpp:jingle:transports:ice-udp:1" ufrag="abc" pwd="xyz">
<candidate component="1" foundation="1" generation="0" priority="2130706431" protocol="udp" type="host" ip="172.20.0.23" port="10000"/>
<candidate component="1" foundation="2" generation="0" priority="1694498815" protocol="udp" type="srflx" ip="1.2.3.4" port="10000" rel-addr="172.20.0.23" rel-port="10000"/>
</transport>`

	got := ExtractMultipleTags(xml, "candidate")
	if len(got) != 2 {
		t.Fatalf("expected 2 candidates, got %d: %v", len(got), got)
	}
}

func TestExtractMultipleTags_NonSelfClosing(t *testing.T) {
	xml := `<foo><bar baz="1">content</bar><bar baz="2">more</bar></foo>`
	got := ExtractMultipleTags(xml, "bar")
	if len(got) != 2 {
		t.Fatalf("expected 2 bars, got %d: %v", len(got), got)
	}
}
