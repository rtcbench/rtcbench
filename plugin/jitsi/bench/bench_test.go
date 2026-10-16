package bench

import (
	"strings"
	"testing"

	benchmark "github.com/rtcbench/rtcbench/pkg/bench"
)

func TestJitsiUsesDataNetworkForSignaling(t *testing.T) {
	c := DefaultConfig()
	c.Jitsi.Prosody, c.Jitsi.Jicofo, c.Jitsi.JVB = "prosody", "jicofo", benchmark.Hosts{"bridge1", "bridge2"}
	e := benchmark.Environment{
		ID:      "network",
		DataIPs: map[string]string{"prosody": "10.6.96.6", "bridge1": "10.6.96.5"},
	}
	for _, s := range New(e, c).Services {
		env := strings.Join(s.Env, "\n")
		if s.Name == e.Name("jitsi-sender-web") {
			continue
		}
		for _, setting := range []string{
			"XMPP_DOMAIN=10.6.96.6",
			"XMPP_MUC_DOMAIN=conference.10.6.96.6",
			"XMPP_INTERNAL_MUC_DOMAIN=internal-muc.10.6.96.6",
		} {
			if !strings.Contains(env+"\n", setting+"\n") {
				t.Errorf("%s is missing %s", s.Name, setting)
			}
		}
		if strings.Contains(env, "XMPP_SERVER=prosody") || strings.Contains(env, "PUBLIC_URL=https://prosody") {
			t.Errorf("%s sends signaling over the management network: %s", s.Name, env)
		}
	}
}

func TestJitsiBandwidthEstimationOverrideIsExplicit(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		c := DefaultConfig()
		c.Jitsi.Prosody, c.Jitsi.Jicofo, c.Jitsi.JVB = "prosody", "jicofo", benchmark.Hosts{"bridge1", "bridge2"}
		c.Jitsi.DisableBandwidthEstimation = disabled
		for _, service := range New(benchmark.Environment{ID: "bwe"}, c).Services {
			hasOverride := strings.Contains(strings.Join(service.Env, "\n"), "JVB_CC_TRUST_BWE=false")
			want := disabled && strings.HasPrefix(service.Name, "rtcbench-bwe-jvb-")
			if hasOverride != want {
				t.Fatalf("disabled=%t service=%s env=%v", disabled, service.Name, service.Env)
			}
		}
	}
}
