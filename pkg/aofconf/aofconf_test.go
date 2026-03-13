package aofconf

import (
	"os"
	"testing"

	"call.zip/pkg/aoflog"
)

func TestMain(m *testing.M) {
	origDir, _ := os.Getwd()
	tmpDir, _ := os.MkdirTemp("", "aofconf-test-*")
	os.Chdir(tmpDir)
	code := m.Run()
	os.Chdir(origDir)
	os.RemoveAll(tmpDir)
	os.Exit(code)
}

func TestNewNilClientConfig(t *testing.T) {
	cfg := NewNilClientConfig()

	if cfg.LogsDir != nil {
		t.Fatalf("expected LogsDir to be nil, got %v", cfg.LogsDir)
	}
	if cfg.DumpPackets {
		t.Fatalf("expected DumpPackets to be false")
	}

	fields := []struct {
		name   string
		client aoflog.Client
	}{
		{"Steps", cfg.Steps},
		{"Pion", cfg.Pion},
		{"PktRecv", cfg.PktRecv},
		{"BOSH", cfg.BOSH},
		{"Handshake", cfg.Handshake},
		{"KeepAlive", cfg.KeepAlive},
		{"NetStats", cfg.NetStats},
	}
	for _, f := range fields {
		if _, ok := f.client.(aoflog.NilClient); !ok {
			t.Fatalf("expected %s to be aoflog.NilClient, got %T", f.name, f.client)
		}
	}
}

func TestNewStdClientConfig(t *testing.T) {
	cfg := NewStdClientConfig("1.2.3.4", "5.6.7.8", "conf1", "viewer1")

	if cfg.LogsDir != nil {
		t.Fatalf("expected LogsDir to be nil, got %v", cfg.LogsDir)
	}
	if cfg.DumpPackets {
		t.Fatalf("expected DumpPackets to be false")
	}

	fields := []struct {
		name   string
		client aoflog.Client
	}{
		{"Steps", cfg.Steps},
		{"Pion", cfg.Pion},
		{"PktRecv", cfg.PktRecv},
		{"BOSH", cfg.BOSH},
		{"Handshake", cfg.Handshake},
		{"KeepAlive", cfg.KeepAlive},
		{"NetStats", cfg.NetStats},
	}
	for _, f := range fields {
		if _, ok := f.client.(*aoflog.StdClient); !ok {
			t.Fatalf("expected %s to be *aoflog.StdClient, got %T", f.name, f.client)
		}
	}
}

func TestNewStdClientConfig_VaryingArgs(t *testing.T) {
	cases := []struct {
		serverIP, clientIP, confName, viewerName string
	}{
		{"10.0.0.1", "10.0.0.2", "room-a", "alice"},
		{"192.168.1.1", "192.168.1.2", "room-b", "bob"},
		{"::1", "::2", "ipv6-conf", "v6-viewer"},
		{"", "", "", ""},
	}
	for _, tc := range cases {
		cfg := NewStdClientConfig(tc.serverIP, tc.clientIP, tc.confName, tc.viewerName)
		if cfg == nil {
			t.Fatalf("expected non-nil config for args (%q, %q, %q, %q)",
				tc.serverIP, tc.clientIP, tc.confName, tc.viewerName)
		}
	}
}

func TestNewDebugLogsClientConfig(t *testing.T) {
	cfg := NewDebugLogsClientConfig("1.2.3.4", "5.6.7.8", "conf1", "viewer1")

	if cfg.LogsDir == nil {
		t.Fatalf("expected LogsDir to be non-nil")
	}
	info, err := os.Stat(*cfg.LogsDir)
	if err != nil {
		t.Fatalf("expected LogsDir directory to exist, got error: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("expected LogsDir to be a directory")
	}

	if !cfg.DumpPackets {
		t.Fatalf("expected DumpPackets to be true")
	}

	fields := []struct {
		name   string
		client aoflog.Client
	}{
		{"Steps", cfg.Steps},
		{"Pion", cfg.Pion},
		{"PktRecv", cfg.PktRecv},
		{"BOSH", cfg.BOSH},
		{"Handshake", cfg.Handshake},
		{"KeepAlive", cfg.KeepAlive},
		{"NetStats", cfg.NetStats},
	}
	for _, f := range fields {
		if f.client == nil {
			t.Fatalf("expected %s to be non-nil", f.name)
		}
	}
}

func TestNewPerVBotLogsClientConfig(t *testing.T) {
	cfg := NewPerVBotLogsClientConfig("1.2.3.4", "5.6.7.8", "conf1", "viewer1", true)

	if cfg.LogsDir == nil {
		t.Fatalf("expected LogsDir to be non-nil")
	}
	info, err := os.Stat(*cfg.LogsDir)
	if err != nil {
		t.Fatalf("expected LogsDir directory to exist, got error: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("expected LogsDir to be a directory")
	}

	if !cfg.DumpPackets {
		t.Fatalf("expected DumpPackets to be true when dumpPackets=true")
	}

	fields := []struct {
		name   string
		client aoflog.Client
	}{
		{"Steps", cfg.Steps},
		{"Pion", cfg.Pion},
		{"PktRecv", cfg.PktRecv},
		{"BOSH", cfg.BOSH},
		{"Handshake", cfg.Handshake},
		{"KeepAlive", cfg.KeepAlive},
		{"NetStats", cfg.NetStats},
	}
	for _, f := range fields {
		if f.client == nil {
			t.Fatalf("expected %s to be non-nil", f.name)
		}
	}

	cfg2 := NewPerVBotLogsClientConfig("1.2.3.4", "5.6.7.8", "conf1", "viewer2", false)
	if cfg2.DumpPackets {
		t.Fatalf("expected DumpPackets to be false when dumpPackets=false")
	}
}

func TestNewNilClientConfig_DumpPacketsFalse(t *testing.T) {
	cfg := NewNilClientConfig()
	if cfg.DumpPackets {
		t.Fatalf("expected DumpPackets to be false for NilClientConfig")
	}
}
