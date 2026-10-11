package deployment

import (
	"bytes"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	deploy "github.com/rtcbench/rtcbench/pkg/deployment"
	"golang.org/x/crypto/ssh"
)

var roomName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,127}$`)

func (m *Manager) webTarget(id string) (deployment, credential, *deploy.WebUI, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.db.Deployments[id]
	if !ok {
		return d, credential{}, nil, errors.New("deployment not found")
	}
	ui := m.plugins[d.Plugin].WebUI
	host, managed := m.db.Hosts[d.HostID]
	if ui == nil || !managed {
		return d, host, nil, errors.New("web UI requires a supported managed deployment")
	}
	if d.Status != "ready" && d.Status != "unknown" {
		return d, host, nil, errors.New("deployment is not running")
	}
	return d, m.hostCredential(host), ui, nil
}

func (m *Manager) openWebUI(w http.ResponseWriter, r *http.Request) {
	d, _, _, err := m.webTarget(r.PathValue("id"))
	if err != nil {
		fail(w, 404, err)
		return
	}
	query := url.Values{"deployment": {d.ID}}
	if room := r.URL.Query().Get("room"); room != "" {
		query.Set("room", room)
	}
	w.Header().Set("Location", "../../../../deployment.html?"+query.Encode())
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusFound)
}

func (m *Manager) joinWebUI(w http.ResponseWriter, r *http.Request) {
	d, _, ui, err := m.webTarget(r.PathValue("id"))
	if err != nil {
		fail(w, 404, err)
		return
	}
	var req struct {
		Room string `json:"room"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !roomName.MatchString(req.Room) {
		fail(w, 400, errors.New("use a room name with letters, numbers, hyphens or underscores"))
		return
	}
	identity := "browser-" + uuid.NewString()
	data := map[string]string{}
	if ui.Join != nil {
		data, err = ui.Join(d.config(), req.Room, identity)
		if err != nil {
			fail(w, 502, errors.New("could not create room access"))
			return
		}
	}
	reply(w, 200, object{"name": d.Name, "plugin": d.Plugin, "room": req.Room, "identity": identity, "data": data})
}

func (m *Manager) proxyWebUI(w http.ResponseWriter, r *http.Request) {
	origin, err := url.Parse(r.Header.Get("Origin"))
	if err != nil || (r.Header.Get("Origin") != "" && ((origin.Scheme != "http" && origin.Scheme != "https") || !strings.EqualFold(origin.Host, r.Host))) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		fail(w, 403, errors.New("cross-origin request rejected"))
		return
	}
	d, host, ui, err := m.webTarget(r.PathValue("id"))
	if err != nil {
		fail(w, 404, err)
		return
	}
	scheme := "http"
	if ui.HTTPS {
		scheme = "https"
	}
	upstream := &url.URL{Scheme: scheme, Host: net.JoinHostPort(d.Address, strconv.Itoa(d.Ports[ui.Port]))}
	proxyPath := strings.TrimSuffix(r.URL.Path, r.PathValue("path"))
	external := "ws://" + r.Host
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		external = "wss://" + r.Host
	}
	prefix := r.Header.Get("X-Forwarded-Prefix")
	if prefix != "" && (!strings.HasPrefix(prefix, "/") || strings.HasPrefix(prefix, "//") || strings.ContainsAny(prefix, `\?#`)) {
		fail(w, 400, errors.New("invalid proxy prefix"))
		return
	}
	external += strings.TrimRight(prefix, "/") + proxyPath
	err = withSSHClient(r.Context(), host, func(client *ssh.Client) error {
		transport := &http.Transport{
			DialContext:         client.DialContext,
			TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 70 * time.Second,
			// The upstream connection is authenticated by the pinned SSH host key
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		defer transport.CloseIdleConnections()
		proxy := &httputil.ReverseProxy{
			Transport: transport,
			Rewrite: func(p *httputil.ProxyRequest) {
				p.Out.URL.Path = "/" + r.PathValue("path")
				p.Out.URL.RawPath = ""
				p.SetURL(upstream)
				p.Out.URL.Host = net.JoinHostPort("127.0.0.1", strconv.Itoa(d.Ports[ui.Port]))
				p.Out.Host = upstream.Host
				p.Out.Header.Del("Cookie")
				p.Out.Header.Del("Authorization")
				p.Out.Header.Set("Origin", upstream.String())
				p.Out.Header.Set("Accept-Encoding", "identity")
			},
			ModifyResponse: func(resp *http.Response) error {
				resp.Header.Del("Set-Cookie")
				if ui.Rewrite == nil || !strings.Contains(resp.Header.Get("Content-Type"), "xml") {
					return nil
				}
				body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20+1))
				resp.Body.Close()
				if err != nil {
					return err
				}
				if len(body) > 4<<20 {
					return errors.New("signaling response too large")
				}
				body = ui.Rewrite(d.config(), body, external)
				resp.Body = io.NopCloser(bytes.NewReader(body))
				resp.ContentLength = int64(len(body))
				resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
				return nil
			},
			ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
				fail(w, 502, errors.New("deployment web service is unavailable"))
			},
		}
		proxy.ServeHTTP(w, r)
		return nil
	})
	if err != nil {
		fail(w, 502, errors.New("could not connect to deployment"))
	}
}
