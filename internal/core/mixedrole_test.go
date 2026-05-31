package core

import "testing"

func boolPtr(v bool) *bool { return &v }

// TestCameraConfigMixedRole verifies the cameras.mixedRole YAML field is wired
// through to CameraConfig (the flag the default scenario reads to assign the
// Both role). If this regressed, mixed-role configs would silently fall back to
// send-only Senders.
func TestCameraConfigMixedRole(t *testing.T) {
	mk := func(mixed *bool) CameraConfig {
		yc := &YAMLCameraConfig{
			PerRoom:    intPtr(3),
			FileType:   stringPtr("ivf"),
			VideoCodec: stringPtr("vp9"),
			Directory:  stringPtr("/test-videos"),
			MixedRole:  mixed,
		}
		return yc.mustConvert()
	}

	if got := mk(boolPtr(true)); !got.MixedRole {
		t.Fatalf("mixedRole:true => want MixedRole=true, got false")
	}
	if got := mk(boolPtr(false)); got.MixedRole {
		t.Fatalf("mixedRole:false => want MixedRole=false, got true")
	}
	if got := mk(nil); got.MixedRole {
		t.Fatalf("mixedRole omitted => want MixedRole=false (default), got true")
	}
}
