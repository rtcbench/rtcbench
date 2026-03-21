package call

import (
	"errors"
	"testing"
)

func strPtr(s string) *string { return &s }

func TestNATConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		nat     YAMLNATConfig
		wantErr error
	}{
		{
			name: "empty NAT config is valid",
			nat:  YAMLNATConfig{},
		},
		{
			name: "valid publicIP",
			nat:  YAMLNATConfig{PublicIP: strPtr("203.0.113.50")},
		},
		{
			name:    "invalid publicIP",
			nat:     YAMLNATConfig{PublicIP: strPtr("not-an-ip")},
			wantErr: ErrInvalidPublicIP,
		},
		{
			name: "empty publicIP is valid (treated as unset)",
			nat:  YAMLNATConfig{PublicIP: strPtr("")},
		},
		{
			name: "valid STUN server",
			nat:  YAMLNATConfig{STUNServers: []string{"stun:stun.l.google.com:19302"}},
		},
		{
			name: "valid STUNS server",
			nat:  YAMLNATConfig{STUNServers: []string{"stuns:stun.example.com:5349"}},
		},
		{
			name:    "invalid STUN URL scheme",
			nat:     YAMLNATConfig{STUNServers: []string{"http://stun.example.com"}},
			wantErr: ErrInvalidSTUNURL,
		},
		{
			name: "valid TURN server",
			nat: YAMLNATConfig{
				TURNServers: []YAMLTURNServer{{
					URL:        strPtr("turn:turn.example.com:3478"),
					Username:   strPtr("user"),
					Credential: strPtr("pass"),
				}},
			},
		},
		{
			name: "valid TURNS server",
			nat: YAMLNATConfig{
				TURNServers: []YAMLTURNServer{{
					URL:        strPtr("turns:turn.example.com:5349"),
					Username:   strPtr("user"),
					Credential: strPtr("pass"),
				}},
			},
		},
		{
			name: "TURN server missing URL",
			nat: YAMLNATConfig{
				TURNServers: []YAMLTURNServer{{
					Username:   strPtr("user"),
					Credential: strPtr("pass"),
				}},
			},
			wantErr: ErrMissingTURNURL,
		},
		{
			name: "TURN server invalid URL scheme",
			nat: YAMLNATConfig{
				TURNServers: []YAMLTURNServer{{
					URL:        strPtr("http://turn.example.com"),
					Username:   strPtr("user"),
					Credential: strPtr("pass"),
				}},
			},
			wantErr: ErrInvalidTURNURL,
		},
		{
			name: "TURN server missing username",
			nat: YAMLNATConfig{
				TURNServers: []YAMLTURNServer{{
					URL:        strPtr("turn:turn.example.com:3478"),
					Credential: strPtr("pass"),
				}},
			},
			wantErr: ErrMissingTURNUsername,
		},
		{
			name: "TURN server missing credential",
			nat: YAMLNATConfig{
				TURNServers: []YAMLTURNServer{{
					URL:      strPtr("turn:turn.example.com:3478"),
					Username: strPtr("user"),
				}},
			},
			wantErr: ErrMissingTURNCredential,
		},
		{
			name: "TURN server empty credential",
			nat: YAMLNATConfig{
				TURNServers: []YAMLTURNServer{{
					URL:        strPtr("turn:turn.example.com:3478"),
					Username:   strPtr("user"),
					Credential: strPtr(""),
				}},
			},
			wantErr: ErrMissingTURNCredential,
		},
		{
			name: "all NAT fields together valid",
			nat: YAMLNATConfig{
				PublicIP:    strPtr("203.0.113.50"),
				STUNServers: []string{"stun:stun.l.google.com:19302"},
				TURNServers: []YAMLTURNServer{{
					URL:        strPtr("turn:turn.example.com:3478"),
					Username:   strPtr("user"),
					Credential: strPtr("pass"),
				}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.nat.validate()
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error %v, got nil", tt.wantErr)
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected error %v, got: %v", tt.wantErr, err)
			}
		})
	}
}

func TestNATConfigMustConvert(t *testing.T) {
	yaml := &YAMLNATConfig{
		PublicIP:    strPtr("203.0.113.50"),
		STUNServers: []string{"stun:stun.l.google.com:19302"},
		TURNServers: []YAMLTURNServer{{
			URL:        strPtr("turn:turn.example.com:3478"),
			Username:   strPtr("user"),
			Credential: strPtr("secret"),
		}},
	}

	c := yaml.mustConvert()

	if c.PublicIP != "203.0.113.50" {
		t.Errorf("PublicIP = %q, want %q", c.PublicIP, "203.0.113.50")
	}
	if len(c.STUNServers) != 1 || c.STUNServers[0] != "stun:stun.l.google.com:19302" {
		t.Errorf("STUNServers = %v, want [stun:stun.l.google.com:19302]", c.STUNServers)
	}
	if len(c.TURNServers) != 1 {
		t.Fatalf("TURNServers length = %d, want 1", len(c.TURNServers))
	}
	turn := c.TURNServers[0]
	if turn.URL != "turn:turn.example.com:3478" {
		t.Errorf("TURN URL = %q, want %q", turn.URL, "turn:turn.example.com:3478")
	}
	if turn.Username != "user" {
		t.Errorf("TURN Username = %q, want %q", turn.Username, "user")
	}
	if turn.Credential != "secret" {
		t.Errorf("TURN Credential = %q, want %q", turn.Credential, "secret")
	}
}

func TestNATConfigEnabled(t *testing.T) {
	tests := []struct {
		name string
		nat  NATConfig
		want bool
	}{
		{"empty", NATConfig{}, false},
		{"publicIP only", NATConfig{PublicIP: "1.2.3.4"}, true},
		{"STUN only", NATConfig{STUNServers: []string{"stun:s.example.com:3478"}}, true},
		{"TURN only", NATConfig{TURNServers: []TURNServerConfig{{URL: "turn:t.example.com:3478"}}}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.nat.Enabled(); got != tt.want {
				t.Errorf("Enabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNetworkConfigValidationWithNAT(t *testing.T) {
	validServerIP := strPtr("10.0.0.1")

	t.Run("NAT section with invalid publicIP fails", func(t *testing.T) {
		yc := &YAMLNetworkConfig{
			ServerIP: validServerIP,
			NAT: &YAMLNATConfig{
				PublicIP: strPtr("not-an-ip"),
			},
		}
		err := yc.validate()
		if !errors.Is(err, ErrInvalidPublicIP) {
			t.Fatalf("expected ErrInvalidPublicIP, got: %v", err)
		}
	})

	t.Run("nil NAT section is valid", func(t *testing.T) {
		yc := &YAMLNetworkConfig{
			ServerIP: validServerIP,
		}
		err := yc.validate()
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
	})

	t.Run("NAT converts through NetworkConfig", func(t *testing.T) {
		yc := &YAMLNetworkConfig{
			ServerIP: validServerIP,
			NAT: &YAMLNATConfig{
				PublicIP: strPtr("203.0.113.1"),
			},
		}
		c := yc.mustConvert()
		if c.NAT.PublicIP != "203.0.113.1" {
			t.Errorf("NAT.PublicIP = %q, want %q", c.NAT.PublicIP, "203.0.113.1")
		}
	})
}
