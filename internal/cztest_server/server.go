package cztest_server

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type ServerArgs struct {
	ServerIP   string
	ServerPort int
}

type payloadState struct {
	uuid string

	config    []byte
	configET  string
	binaries  map[string]map[string][]byte // os -> arch -> bin
	binETags  map[string]map[string]string // os -> arch -> etag
	createdAt time.Time
}

type clientInfo struct {
	IP       string
	LastSeen time.Time
}

var (
	mu    sync.RWMutex
	state payloadState

	clientsMu sync.RWMutex
	clients   = map[string]time.Time{} // ip -> lastSeen

	rootTpl *template.Template
)

const rootHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width,initial-scale=1" />
  <title>cztest</title>
  <style>
    body { font-family: system-ui, -apple-system, Segoe UI, Roboto, sans-serif; margin: 2rem; max-width: 1100px; }
    .grid { display: grid; grid-template-columns: 1fr 1.2fr; gap: 1.25rem; align-items: start; }
    @media (max-width: 980px) { .grid { grid-template-columns: 1fr; } }
    .card { border: 1px solid #ddd; border-radius: 12px; padding: 1rem 1.25rem; }
    .row { margin: 0.75rem 0; }
    label { display: block; font-weight: 600; margin-bottom: 0.25rem; }
    input[type="file"] { width: 100%; }
    .msg { margin: 1rem 0; padding: 0.75rem 1rem; background: #f6f6f6; border-radius: 10px; }
    .mono { font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; }
    button { padding: 0.6rem 1rem; border-radius: 10px; border: 1px solid #ccc; background: white; cursor: pointer; }
    button.danger { border-color: #cc6666; }
    table { border-collapse: collapse; width: 100%; margin-top: 0.75rem; }
    th, td { border: 1px solid #e5e5e5; padding: 0.5rem 0.6rem; text-align: left; }
    th { background: #fafafa; }
    .hint { color: #555; font-size: 0.95rem; }
    .actions { display: flex; gap: 0.75rem; align-items: center; flex-wrap: wrap; }
    .ok { color: #0a7a0a; font-weight: 700; }
    .bad { color: #b00020; font-weight: 700; }
  </style>
</head>
<body>
  <h1>cztest</h1>

  {{if .Message}}
  <div class="msg">{{.Message}}</div>
  {{end}}

  <div class="grid">
    <div class="card">
      <h2>payload</h2>

      <div class="row mono">
        Current UUID: <b>{{if .UUID}}{{.UUID}}{{else}}(none){{end}}</b><br/>
        Config present: <b>{{.HasCfg}}</b><br/>
        Binaries loaded: <b>{{.BinCount}}</b><br/>
        Uploaded at: <b>{{if .UploadedAt}}{{.UploadedAt}}{{else}}(n/a){{end}}</b>
      </div>

      {{if .Bins}}
      <div class="row">
        <div class="mono hint">Available binaries (os/arch):</div>
        <table class="mono">
          <thead><tr><th>os</th><th>arch</th></tr></thead>
          <tbody>
          {{range .Bins}}
            <tr><td>{{.OS}}</td><td>{{.Arch}}</td></tr>
          {{end}}
          </tbody>
        </table>
      </div>
      {{end}}

      <hr/>

      <div class="row hint">
        Upload a zip (e.g. <span class="mono">make cztest-payload-zip</span>). Uploading implicitly "starts" a new payload.
        The zip must contain <span class="mono">config.yml</span> and one or more binaries named like
        <span class="mono">cztest-binary-os_linux-arch_amd64</span>.
      </div>

      <div class="row actions">
        <form method="post" enctype="multipart/form-data" action="/">
          <input type="hidden" name="action" value="upload" />
          <label for="payload">payload zip</label>
          <input id="payload" name="payload" type="file" accept=".zip,application/zip" required />
          <div style="margin-top:0.75rem;">
            <button type="submit">Upload</button>
          </div>
        </form>

        <form method="post" action="/">
          <input type="hidden" name="action" value="stop" />
          <button class="danger" type="submit">STOP</button>
        </form>
      </div>

      <p class="mono hint">
        Endpoints:
        <br/>GET /work
        <br/>GET /config
        <br/>GET /binary?os=&lt;os&gt;&amp;arch=&lt;arch&gt; (defaults: server runtime)
      </p>
    </div>

    <div class="card">
      <h2>clients</h2>
      <div class="hint mono">Green: last seen within 30s. Red: last seen &gt; 30s.</div>

      <table class="mono">
        <thead>
          <tr>
            <th>ip</th>
            <th>last seen (server time)</th>
            <th>age</th>
            <th>status</th>
          </tr>
        </thead>
        <tbody>
          {{range .Clients}}
          <tr>
            <td>{{.IP}}</td>
            <td>{{.LastSeen}}</td>
            <td>{{.Age}}</td>
            <td class="{{.Class}}">{{.Status}}</td>
          </tr>
          {{end}}
        </tbody>
      </table>
    </div>
  </div>
</body>
</html>`

type rootView struct {
	Message string

	UUID       string
	HasCfg     bool
	BinCount   int
	UploadedAt string
	Bins       []binRow

	Clients []clientsRow
}

type binRow struct {
	OS   string
	Arch string
}

type clientsRow struct {
	IP       string
	LastSeen string
	Age      string
	Status   string
	Class    string
}

func StartTestServer(args *ServerArgs) {
	addr := net.JoinHostPort(args.ServerIP, strconv.Itoa(args.ServerPort))

	var err error
	rootTpl, err = template.New("root").Parse(rootHTML)
	if err != nil {
		log.Fatalf("parse template: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", rootHandler)

	mux.HandleFunc("/work", workHandler)
	mux.HandleFunc("/config", configHandler)
	mux.HandleFunc("/binary", binaryHandler)

	srv := &http.Server{
		Addr:              addr,
		Handler:           logMiddleware(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("cztest_server listening on http://%s/", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("ListenAndServe: %v", err)
	}
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		renderRoot(w, "")
	case http.MethodPost:
		action := strings.TrimSpace(r.FormValue("action"))
		if action == "" {
			action = "upload"
		}
		switch action {
		case "stop":
			handleStop(w)
		case "upload":
			handleUploadZip(w, r)
		default:
			http.Error(w, "bad action", http.StatusBadRequest)
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func renderRoot(w http.ResponseWriter, msg string) {
	mu.RLock()
	st := state
	mu.RUnlock()

	now := time.Now()
	clientsRows := snapshotClients(now)

	view := rootView{
		Message: msg,

		UUID:     st.uuid,
		HasCfg:   len(st.config) > 0,
		BinCount: countBins(st.binaries),
		Bins:     flattenBins(st.binaries),

		Clients: clientsRows,
	}
	if !st.createdAt.IsZero() {
		view.UploadedAt = st.createdAt.Format(time.RFC3339)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = rootTpl.Execute(w, view)
}

func handleStop(w http.ResponseWriter) {
	mu.Lock()
	state.uuid = ""
	state.createdAt = time.Time{}
	mu.Unlock()

	renderRoot(w, "stopped (uuid cleared)")
}

func handleUploadZip(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(256 << 20); err != nil {
		renderRoot(w, "error: invalid multipart form")
		return
	}

	f, _, err := r.FormFile("payload")
	if err != nil {
		renderRoot(w, "error: missing payload zip")
		return
	}
	defer f.Close()

	zipBytes, err := io.ReadAll(io.LimitReader(f, 512<<20))
	if err != nil {
		renderRoot(w, "error: failed reading zip")
		return
	}

	cfg, bins, binET, err := parsePayloadZip(zipBytes)
	if err != nil {
		renderRoot(w, "error: "+err.Error())
		return
	}

	u := uuid.NewString()
	cfgET := etag(cfg)

	mu.Lock()
	state = payloadState{
		uuid:      u,
		config:    cfg,
		configET:  cfgET,
		binaries:  bins,
		binETags:  binET,
		createdAt: time.Now(),
	}
	mu.Unlock()

	renderRoot(w, fmt.Sprintf("uploaded OK (uuid=%s, binaries=%d)", u, countBins(bins)))
}

func workHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	markClientSeen(r)

	mu.RLock()
	u := state.uuid
	ready := u != "" && len(state.config) > 0 && countBins(state.binaries) > 0
	mu.RUnlock()

	if !ready {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, u+"\n")
}

func configHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	mu.RLock()
	u := state.uuid
	cfg := state.config
	et := state.configET
	ready := u != "" && len(cfg) > 0 && countBins(state.binaries) > 0
	mu.RUnlock()

	if !ready {
		http.NotFound(w, r)
		return
	}

	if matchETag(r, et) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("Content-Type", "application/x-yaml")
	w.Header().Set("ETag", `"`+et+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(cfg)
}

func binaryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	osQ := strings.TrimSpace(r.URL.Query().Get("os"))
	archQ := strings.TrimSpace(r.URL.Query().Get("arch"))
	if osQ == "" {
		osQ = runtime.GOOS
	}
	if archQ == "" {
		archQ = runtime.GOARCH
	}

	mu.RLock()
	u := state.uuid
	bins := state.binaries
	ets := state.binETags
	ready := u != "" && len(state.config) > 0 && countBins(bins) > 0

	var bin []byte
	var et string
	if ready {
		if m, ok := bins[osQ]; ok {
			bin = m[archQ]
		}
		if m, ok := ets[osQ]; ok {
			et = m[archQ]
		}
	}
	mu.RUnlock()

	if !ready || len(bin) == 0 {
		http.NotFound(w, r)
		return
	}

	if et != "" && matchETag(r, et) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="cztest-payload"`)
	if et != "" {
		w.Header().Set("ETag", `"`+et+`"`)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(bin)
}

func markClientSeen(r *http.Request) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	now := time.Now()

	clientsMu.Lock()
	clients[host] = now
	clientsMu.Unlock()
}

func snapshotClients(now time.Time) []clientsRow {
	clientsMu.RLock()
	tmp := make([]clientInfo, 0, len(clients))
	for ip, t := range clients {
		tmp = append(tmp, clientInfo{IP: ip, LastSeen: t})
	}
	clientsMu.RUnlock()

	sort.Slice(tmp, func(i, j int) bool {
		return tmp[i].LastSeen.After(tmp[j].LastSeen)
	})

	rows := make([]clientsRow, 0, len(tmp))
	for _, c := range tmp {
		age := now.Sub(c.LastSeen)
		ok := age <= 30*time.Second
		class := "bad"
		status := "STALE"
		if ok {
			class = "ok"
			status = "OK"
		}
		rows = append(rows, clientsRow{
			IP:       c.IP,
			LastSeen: c.LastSeen.Format(time.RFC3339),
			Age:      fmt.Sprintf("%.0fs", age.Seconds()),
			Status:   status,
			Class:    class,
		})
	}
	return rows
}

func parsePayloadZip(zipBytes []byte) ([]byte, map[string]map[string][]byte, map[string]map[string]string, error) {
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("invalid zip: %w", err)
	}

	var cfg []byte
	bins := map[string]map[string][]byte{}
	ets := map[string]map[string]string{}

	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}

		name := strings.TrimSpace(f.Name)
		base := filepathBase(name)

		rc, err := f.Open()
		if err != nil {
			return nil, nil, nil, fmt.Errorf("zip open %s: %w", name, err)
		}
		b, readErr := io.ReadAll(io.LimitReader(rc, 512<<20))
		_ = rc.Close()
		if readErr != nil {
			return nil, nil, nil, fmt.Errorf("zip read %s: %w", name, readErr)
		}
		if len(b) == 0 {
			continue
		}

		if base == "config.yml" {
			cfg = b
			continue
		}

		osVal, archVal, ok := parseBinName(base)
		if !ok {
			continue
		}

		if _, ok := bins[osVal]; !ok {
			bins[osVal] = map[string][]byte{}
			ets[osVal] = map[string]string{}
		}
		bins[osVal][archVal] = b
		ets[osVal][archVal] = etag(b)
	}

	if len(cfg) == 0 {
		return nil, nil, nil, fmt.Errorf("zip missing config.yml")
	}
	if countBins(bins) == 0 {
		return nil, nil, nil, fmt.Errorf("zip contains no binaries matching cztest-binary-os_<os>-arch_<arch>")
	}

	return cfg, bins, ets, nil
}

func parseBinName(base string) (osVal, archVal string, ok bool) {
	const p = "cztest-binary-"
	if !strings.HasPrefix(base, p) {
		return "", "", false
	}
	rest := strings.TrimPrefix(base, p)

	if !strings.HasPrefix(rest, "os_") {
		return "", "", false
	}
	rest = strings.TrimPrefix(rest, "os_")
	i := strings.Index(rest, "-arch_")
	if i <= 0 {
		return "", "", false
	}
	osVal = rest[:i]
	archVal = rest[i+len("-arch_"):]
	if osVal == "" || archVal == "" {
		return "", "", false
	}

	if strings.ContainsAny(osVal, "/\\ \t\r\n") || strings.ContainsAny(archVal, "/\\ \t\r\n") {
		return "", "", false
	}
	return osVal, archVal, true
}

func etag(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func matchETag(r *http.Request, et string) bool {
	h := r.Header.Get("If-None-Match")
	if h == "" {
		return false
	}
	h = strings.TrimSpace(h)
	h = strings.Trim(h, `"`)
	return h == et
}

func countBins(bins map[string]map[string][]byte) int {
	n := 0
	for _, m := range bins {
		n += len(m)
	}
	return n
}

func flattenBins(bins map[string]map[string][]byte) []binRow {
	var out []binRow
	for osVal, m := range bins {
		for archVal := range m {
			out = append(out, binRow{OS: osVal, Arch: archVal})
		}
	}
	return out
}

func filepathBase(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	if i := strings.LastIndex(p, `\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s (%s)", r.RemoteAddr, r.Method, r.URL.Path, time.Since(start))
	})
}
