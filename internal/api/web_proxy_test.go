package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestUIAddress(t *testing.T) {
	for _, secure := range []bool{false, true} {
		t.Run(fmt.Sprint(secure), func(t *testing.T) {
			upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/ui/runs":
					if r.Header.Get("Accept-Encoding") != "identity" || r.Header.Get("X-Forwarded-Host") == "" {
						t.Error("missing proxy headers")
					}
					w.Header().Set("Content-Type", "text/html")
					w.Header().Set("ETag", "original")
					fmt.Fprint(w, `<html data-welcome="false"><base href="/">`)
				case "/ui/main.js.map":
					fmt.Fprint(w, r.URL.RawQuery)
				case "/ui/_bun/report_error":
					if r.Method != http.MethodPost {
						t.Error("frontend request method changed")
					}
					w.WriteHeader(http.StatusNoContent)
				case "/ui/hmr":
					connection, err := websocket.Accept(w, r, nil)
					if err != nil {
						t.Error(err)
						return
					}
					defer connection.CloseNow()
					kind, data, err := connection.Read(r.Context())
					if err == nil {
						_ = connection.Write(r.Context(), kind, data)
					}
				default:
					t.Errorf("API request reached frontend: %s", r.URL.Path)
				}
			}))
			if secure {
				upstream.StartTLS()
			} else {
				upstream.Start()
			}
			defer upstream.Close()
			dir := t.TempDir()
			config := fmt.Sprintf("welcome: true\nui:\n  address: %s/ui\n", upstream.URL)
			if err := os.WriteFile(filepath.Join(dir, "rtcbench-dev.yml"), []byte(config), 0o600); err != nil {
				t.Fatal(err)
			}
			s, err := New(dir, "rtcbench-dev.yml", "test", io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Shutdown(context.Background())
			s.webProxy.(*httputil.ReverseProxy).Transport = upstream.Client().Transport
			handler := s.Handler()
			page := do(t, handler, "GET", "/runs", nil)
			if page.Code != 200 || !strings.Contains(page.Body.String(), `data-welcome="true"`) || page.Header().Get("ETag") != "" {
				t.Fatal(page.Code, page.Header(), page.Body.String())
			}
			if response := do(t, handler, "GET", "/main.js.map?v=1", nil); response.Body.String() != "v=1" {
				t.Fatal(response.Body.String())
			}
			if response := do(t, handler, "GET", "/api/v1/version", nil); !strings.Contains(response.Body.String(), `"test"`) {
				t.Fatal(response.Body.String())
			}
			if response := do(t, handler, "POST", "/_bun/report_error", nil); response.Code != 204 {
				t.Fatal("frontend POST unavailable", response.Code)
			}
			if response := do(t, handler, "PUT", "/api/v1/version", nil); response.Code != 405 {
				t.Fatal("API method forwarded to frontend", response.Code)
			}
			if response := do(t, handler, "GET", "/api/v1/missing", nil); response.Code != 404 {
				t.Fatal("unknown API path forwarded to frontend", response.Code)
			}
			frontend := httptest.NewServer(handler)
			defer frontend.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			connection, _, err := websocket.Dial(ctx, frontend.URL+"/hmr", nil)
			if err != nil {
				t.Fatal(err)
			}
			defer connection.CloseNow()
			if err := connection.Write(ctx, websocket.MessageText, []byte("reload")); err != nil {
				t.Fatal(err)
			}
			_, data, err := connection.Read(ctx)
			if err != nil || string(data) != "reload" {
				t.Fatal(string(data), err)
			}
		})
	}
}

func TestUISettings(t *testing.T) {
	for _, address := range []string{
		"localhost:3000",
		"ftp://localhost",
		"http://",
		"http://:3000",
		"http://user:secret@localhost",
		"http://localhost/#fragment",
	} {
		s := &Server{}
		s.settings.UI.Address = address
		if err := s.configureWeb(); err == nil {
			t.Fatalf("accepted %q", address)
		}
	}
	if _, err := New(t.TempDir(), "missing.yml", "test", io.Discard); !os.IsNotExist(err) {
		t.Fatal("missing settings file was ignored", err)
	}
	s, handler := newServer(t)
	s.Web = nil
	if response := do(t, handler, "GET", "/", nil); response.Code != 404 {
		t.Fatal("served missing UI", response.Code)
	}
	if response := do(t, handler, "GET", "/api/v1/version", nil); response.Code != 200 {
		t.Fatal("API unavailable without a UI", response.Code)
	}
}
