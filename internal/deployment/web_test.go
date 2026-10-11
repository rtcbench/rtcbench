package deployment

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/livekit/protocol/auth"
)

func TestDeploymentWebAccess(t *testing.T) {
	m := manager(t)
	h := savedHost(t, m)
	d := deployment{
		Deployment: Deployment{ID: "test", HostID: h.ID, Plugin: "livekit", Status: "ready"},
		APIKey:     "key", Secret: "secret",
	}
	m.db.Deployments[d.ID] = d
	t.Run("redirect preserves prefix and room", func(t *testing.T) {
		for _, prefix := range []string{"/", "/app/"} {
			for _, room := range []string{"", "research-room_42", "room&deployment=other"} {
				t.Run(prefix+room, func(t *testing.T) {
					response := request(t, m, "GET", "/deployments/test/web?room="+url.QueryEscape(room), nil)
					failIf(t, response.Code != 302, response)
					base, err := url.Parse("https://example.org" + prefix + "api/v1/deployments/test/web")
					must(t, err)
					target, err := base.Parse(response.Header().Get("Location"))
					must(t, err)
					if target.Path != prefix+"deployment.html" {
						t.Errorf("redirect path = %q, want %q", target.Path, prefix+"deployment.html")
					}
					for key, want := range map[string]string{"room": room, "deployment": d.ID} {
						if got := target.Query().Get(key); got != want {
							t.Errorf("redirect %s = %q, want %q", key, got, want)
						}
					}
				})
			}
		}
	})
	t.Run("join token grants requested room", func(t *testing.T) {
		response := request(t, m, "POST", "/deployments/test/join", object{"room": "room-1234"})
		failIf(t, response.Code != 200, response)
		var result struct {
			Identity string
			Data     map[string]string
		}
		must(t, json.Unmarshal(response.Body.Bytes(), &result))
		verifier, err := auth.ParseAPIToken(result.Data["token"])
		must(t, err)
		standard, claims, err := verifier.Verify(d.Secret)
		must(t, err)
		if standard.Subject != result.Identity {
			t.Errorf("token subject = %q, want %q", standard.Subject, result.Identity)
		}
		if claims.Video == nil || !claims.Video.RoomJoin || claims.Video.Room != "room-1234" {
			t.Errorf("incorrect room grant: %+v", claims.Video)
		}
		failIf(t, strings.Contains(response.Body.String(), d.Secret), "deployment secret leaked")
	})
	for name, room := range map[string]string{"empty": "", "parent path": "../room", "too long": strings.Repeat("a", 129)} {
		t.Run("invalid room/"+name, func(t *testing.T) {
			response := request(t, m, "POST", "/deployments/test/join", object{"room": room})
			failIf(t, response.Code != 400, response)
		})
	}
	for _, status := range []string{"stopped", "failed", "unmanaged"} {
		t.Run(status, func(t *testing.T) {
			unavailable := d
			if status == "unmanaged" {
				unavailable.HostID = ""
			} else {
				unavailable.Status = status
			}
			m.db.Deployments[d.ID] = unavailable
			response := request(t, m, "GET", "/deployments/test/web", nil)
			failIf(t, response.Code != 404, response)
		})
	}
}

func TestDeploymentWebProxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("forwarded user credentials")
		}
		if r.URL.Path == "/socket" {
			conn, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, nil)
			if err != nil {
				t.Error(err)
				return
			}
			defer conn.Close()
			kind, data, err := conn.ReadMessage()
			if err != nil {
				t.Error(err)
				return
			}
			if err := conn.WriteMessage(kind, data); err != nil {
				t.Error(err)
			}
			return
		}
		w.Header().Set("Set-Cookie", "upstream=secret")
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, r.URL.RequestURI())
	}))
	defer upstream.Close()
	server := newTestSSHForwarder(t)
	m := manager(t)
	server.host.ID = "host"
	m.db.Hosts["host"] = server.host
	port := upstream.Listener.Addr().(*net.TCPAddr).Port
	m.db.Deployments["test"] = deployment{Deployment: Deployment{
		ID: "test", HostID: "host", Plugin: "livekit",
		Address: "192.0.2.1", Status: "ready", Ports: map[string]int{"signaling": port},
	}}
	mux := http.NewServeMux()
	m.Register(mux)
	proxy := httptest.NewServer(mux)
	defer proxy.Close()
	client := proxy.Client()
	client.Timeout = 5 * time.Second
	t.Run("HTTP strips credentials and cookies", func(t *testing.T) {
		req, err := http.NewRequestWithContext(t.Context(), "GET", proxy.URL+"/api/v1/deployments/test/proxy/rtc?room=test", nil)
		must(t, err)
		req.Header.Set("Cookie", "session=private")
		req.Header.Set("Authorization", "Bearer private")
		response, err := client.Do(req)
		must(t, err)
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		must(t, err)
		failIf(t, response.StatusCode != 200, response.Status)
		if string(body) != "/rtc?room=test" {
			t.Errorf("forwarded URI = %q, want /rtc?room=test", body)
		}
		if cookie := response.Header.Get("Set-Cookie"); cookie != "" {
			t.Errorf("upstream cookie reached client: %q", cookie)
		}
	})
	t.Run("WebSocket upgrade", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		conn, _, err := websocket.DefaultDialer.DialContext(ctx, "ws"+strings.TrimPrefix(proxy.URL, "http")+"/api/v1/deployments/test/proxy/socket", nil)
		must(t, err)
		defer conn.Close()
		must(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
		must(t, conn.WriteMessage(websocket.TextMessage, []byte("echo")))
		_, body, err := conn.ReadMessage()
		must(t, err)
		if string(body) != "echo" {
			t.Errorf("WebSocket response = %q, want echo", body)
		}
	})
	for _, tc := range []struct {
		name, origin, fingerprint string
		status                    int
	}{
		{"cross origin", "https://other.example", server.host.Fingerprint, 403},
		{"untrusted SSH host", "", "SHA256:" + strings.Repeat("a", 43), 502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host := server.host
			host.Fingerprint = tc.fingerprint
			m.db.Hosts[host.ID] = host
			req, err := http.NewRequestWithContext(t.Context(), "GET", proxy.URL+"/api/v1/deployments/test/proxy/rtc", nil)
			must(t, err)
			req.Header.Set("Origin", tc.origin)
			response, err := client.Do(req)
			must(t, err)
			response.Body.Close()
			if response.StatusCode != tc.status {
				t.Errorf("status = %d, want %d", response.StatusCode, tc.status)
			}
		})
	}
}
