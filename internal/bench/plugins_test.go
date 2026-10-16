package bench

import (
	"encoding/json"
	"testing"
)

func TestClusterPreservesPluginSettingsInSavedResults(t *testing.T) {
	input := `janus_api_port: 18088
janus_web_port: 18080
jitsi_web_port: 8443
livekit_ws_port: 17880
livekit_web_port: 18081
mediasoup_ws_port: 14443
mediasoup_web_port: 18082
jitsi:
  jvb: bridge
  prosody_web: signaling
  jicofo: focus
  disable_bandwidth_estimation: true
`
	c, err := ParseCluster(input)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]float64{
		"janus_api_port":     18088,
		"janus_web_port":     18080,
		"jitsi_web_port":     8443,
		"livekit_ws_port":    17880,
		"livekit_web_port":   18081,
		"mediasoup_ws_port":  14443,
		"mediasoup_web_port": 18082,
	} {
		if saved[key] != want {
			t.Errorf("%s = %v, want %v", key, saved[key], want)
		}
	}
	jitsi := saved["jitsi"].(map[string]any)
	if jitsi["disable_bandwidth_estimation"] != true || jitsi["prosody_web"] != "signaling" {
		t.Fatalf("lost plugin settings: %s", data)
	}
	var restored Cluster
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.JanusAPI != c.JanusAPI || len(restored.Jitsi.JVB) != 1 || restored.Jitsi.JVB[0] != "bridge" {
		t.Fatalf("restore failed: %+v", restored)
	}
}

func TestSenderRoomLimitsRemainPluginSpecific(t *testing.T) {
	c := provisionCluster()
	for sfu, limit := range map[string]int{
		"janus":     65,
		"jitsi":     15,
		"livekit":   65,
		"mediasoup": 50,
	} {
		layout, err := Layout(c, "sender", sfu, limit+1)
		if err != nil {
			t.Fatal(err)
		}
		if layout.PerRoom != limit || layout.Rooms != 2 || len(layout.Groups) != 2 || layout.Groups[1].Users != 1 {
			t.Fatalf("%s: %+v", sfu, layout)
		}
	}
}

func TestPluginConfigRejectsUnknownSettings(t *testing.T) {
	for _, input := range []string{
		"unknown_port: 8080",
		"jitsi:\n  disable_bandwidth_estimatoin: true",
	} {
		if _, err := ParseCluster(input); err == nil {
			t.Fatalf("accepted unknown setting %q", input)
		}
	}
}
