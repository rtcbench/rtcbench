package jitsi

import "testing"

func TestSignalingEndpoint(t *testing.T) {
	for _, tc := range []struct {
		options          map[string]any
		endpoint, domain string
	}{
		{nil, "https://10.0.0.2/http-bind", "10.0.0.2"},
		{map[string]any{"boshURL": "https://10.0.0.2:8443/http-bind", "domain": "meet.example"}, "https://10.0.0.2:8443/http-bind", "meet.example"},
		{map[string]any{"boshURL": "http://meet.example:8088/custom-bosh"}, "http://meet.example:8088/custom-bosh", "meet.example"},
	} {
		endpoint, domain, err := signaling("10.0.0.2", tc.options)
		if err != nil || endpoint != tc.endpoint || domain != tc.domain {
			t.Fatalf("got %q, %q, %v", endpoint, domain, err)
		}
	}
	for _, value := range []string{"ws://10.0.0.2:8443", "/http-bind", "https://host:invalid/http-bind"} {
		if _, _, err := signaling("10.0.0.2", map[string]any{"boshURL": value}); err == nil {
			t.Fatalf("accepted invalid endpoint %q", value)
		}
	}
}
