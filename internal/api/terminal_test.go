package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestTerminalSettings(t *testing.T) {
	for _, tc := range []struct {
		name, config string
		wantError    bool
		status       int
	}{
		{"default", "", false, 403},
		{"disabled", "terminal:\n  server: disabled\n", false, 403},
		{"enabled", "terminal:\n  server: enabled\n", false, 200},
		{"invalid value", "terminal:\n  server: yes\n", true, 0},
		{"unknown kind", "terminal:\n  unknown: enabled\n", true, 0},
		{"scalar", "terminal: enabled\n", true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "rtcbench.yml"), []byte(tc.config), 0o600); err != nil {
				t.Fatal(err)
			}
			s, err := New(dir, "rtcbench.yml", "test", io.Discard)
			if tc.wantError {
				if err == nil {
					t.Fatal("invalid terminal setting accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			res := do(t, s.Handler(), "GET", "/api/v1/terminal", nil)
			if res.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", res.Code, tc.status, res.Body)
			}
			if tc.status == 403 && !strings.Contains(res.Body.String(), "terminal.server") {
				t.Fatalf("missing setting guidance: %s", res.Body)
			}
		})
	}
}

func TestTerminalTargets(t *testing.T) {
	s, h := newServer(t)
	s.settings.Terminal = map[string]string{"server": "enabled"}
	for _, query := range []string{
		"project=missing",
		"project=..",
		"kind=other",
		"kind=instances&project=example",
		"id=missing",
	} {
		t.Run(query, func(t *testing.T) {
			res := do(t, h, "GET", "/api/v1/terminal?"+query, nil)
			if res.Code != 400 {
				t.Fatalf("status = %d, want 400: %s", res.Code, res.Body)
			}
		})
	}
	for _, kind := range []string{"instances", "deployments"} {
		t.Run(kind+" disabled", func(t *testing.T) {
			res := do(t, h, "GET", "/api/v1/terminal?kind="+kind+"&id=missing", nil)
			if res.Code != 403 {
				t.Fatalf("status = %d, want 403: %s", res.Code, res.Body)
			}
			if !strings.Contains(res.Body.String(), "terminal."+kind) {
				t.Fatalf("missing setting guidance: %s", res.Body)
			}
		})
	}
}

func TestTerminalRejectsCrossOriginWebSocket(t *testing.T) {
	s, h := newServer(t)
	s.settings.Terminal = map[string]string{"server": "enabled"}
	server := httptest.NewServer(h)
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, response, err := websocket.Dial(ctx, server.URL+"/api/v1/terminal", &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {"https://untrusted.example"}},
	})
	if conn != nil {
		conn.CloseNow()
	}
	if err == nil {
		t.Fatal("cross-origin shell connection accepted")
	}
	if response == nil || response.StatusCode != 403 {
		t.Fatalf("expected a 403 handshake response: %v, %v", response, err)
	}
}

func TestTerminalPTYAndShutdown(t *testing.T) {
	s, h := newServer(t)
	s.settings.Terminal = map[string]string{"server": "enabled"}
	s.settings.AllowedHosts = []string{"bench.example"}
	server := httptest.NewServer(h)
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, server.URL+"/api/v1/terminal", &websocket.DialOptions{HTTPHeader: http.Header{
		"Origin": {"https://bench.example"}, "Sec-Fetch-Site": {"same-origin"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	if err = conn.Write(ctx, websocket.MessageText, []byte(`{"cols":93,"rows":31}`)); err != nil {
		t.Fatal(err)
	}
	wantSize := "31 93"
	command := "printf '\\nPTY_OK:'; stty size\r"
	if runtime.GOOS == "windows" {
		wantSize = "93x31"
		command = "Write-Output ('PTY_' + 'OK:' + $Host.UI.RawUI.WindowSize.Width + 'x' + $Host.UI.RawUI.WindowSize.Height)\r"
	}
	if err = conn.Write(ctx, websocket.MessageBinary, []byte(command)); err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	for !strings.Contains(output.String(), "PTY_OK:") || !strings.Contains(output.String(), wantSize) {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatal(err, output.String())
		}
		output.Write(data)
	}
	s.closeTerminals(ctx)
	s.mu.Lock()
	remaining := len(s.terminals)
	s.mu.Unlock()
	if remaining != 0 {
		t.Fatal("shell remained after shutdown", remaining)
	}
}

func TestTerminalHostAndProxy(t *testing.T) {
	s, h := newServer(t)
	s.settings.Terminal = map[string]string{"server": "enabled"}
	s.settings.AllowedHosts = []string{"bench.example"}
	for _, tc := range []struct {
		name, host, origin, site string
		status                   int
	}{
		{"localhost", "localhost:8080", "http://localhost:8080", "same-origin", 200},
		{"IPv6 loopback", "[::1]:8080", "http://[::1]:8080", "", 200},
		{"untrusted host", "attacker.example:8080", "http://attacker.example:8080", "same-origin", 403},
		{"host suffix", "bench.example.attacker", "http://bench.example.attacker", "same-origin", 403},
		{"opaque origin", "localhost:8080", "null", "", 403},
		{"untrusted origin", "127.0.0.1:8080", "http://attacker.example:8080", "same-origin", 403},
		{"trusted proxy", "127.0.0.1:8080", "https://bench.example", "same-origin", 200},
		{"trusted host", "bench.example", "https://bench.example", "", 200},
		{"cross site", "127.0.0.1:8080", "https://bench.example", "cross-site", 403},
		{"different port", "localhost:8080", "http://localhost:3000", "same-site", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/api/v1/terminal", nil)
			r.Host = tc.host
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Sec-Fetch-Site", tc.site)
			r.Header.Set("X-Forwarded-Host", "localhost")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", w.Code, tc.status, w.Body)
			}
		})
	}
}
