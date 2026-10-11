package deployment

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

func reply(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	if value != nil {
		_ = json.NewEncoder(w).Encode(value)
	}
}
func fail(w http.ResponseWriter, code int, err error) { reply(w, code, object{"error": err.Error()}) }
func decode(w http.ResponseWriter, r *http.Request, value any) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		fail(w, 415, errors.New("use application/json"))
		return false
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(value); err != nil {
		fail(w, 400, errors.New("invalid JSON request"))
		return false
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		fail(w, 400, errors.New("expected one JSON object"))
		return false
	}
	return true
}

func (m *Manager) Register(mux *http.ServeMux) {
	csrf := http.NewCrossOriginProtection()
	guard := func(fn http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if csrf.Check(r) != nil {
				fail(w, 403, errors.New("cross-origin request rejected"))
				return
			}
			fn(w, r)
		}
	}
	mux.HandleFunc("GET /api/v1/deployments/{id}/web", m.openWebUI)
	mux.HandleFunc("POST /api/v1/deployments/{id}/join", guard(m.joinWebUI))
	mux.HandleFunc("/api/v1/deployments/{id}/proxy/{path...}", guard(m.proxyWebUI))
	mux.HandleFunc("GET /api/v1/systems", func(w http.ResponseWriter, r *http.Request) { reply(w, 200, m.snapshot()) })
	saveKey := func(w http.ResponseWriter, r *http.Request) {
		var k storedKey
		if !decode(w, r, &k) {
			return
		}
		key, err := m.saveKey(k, r.PathValue("id"))
		if err != nil {
			fail(w, 400, err)
			return
		}
		reply(w, 200, key)
	}
	mux.HandleFunc("POST /api/v1/systems/keys", guard(saveKey))
	mux.HandleFunc("PUT /api/v1/systems/keys/{id}", guard(saveKey))
	mux.HandleFunc("DELETE /api/v1/systems/keys/{id}", guard(deleteResource(m, m.db.Keys, "SSH key", func(key storedKey) string {
		for _, h := range m.db.Hosts {
			if h.KeyID == key.ID {
				return "SSH key is used by an instance"
			}
		}
		return ""
	})))

	mux.HandleFunc("POST /api/v1/systems/networks", guard(func(w http.ResponseWriter, r *http.Request) {
		var n Network
		if !decode(w, r, &n) {
			return
		}
		saved, err := m.saveNetwork(n)
		if err != nil {
			fail(w, 400, err)
			return
		}
		reply(w, 201, saved)
	}))
	mux.HandleFunc("DELETE /api/v1/systems/networks/{id}", guard(deleteResource(m, m.db.Networks, "network", func(n Network) string {
		for _, h := range m.db.Hosts {
			for _, i := range h.Networks {
				if i.NetworkID == n.ID {
					return "network is in use"
				}
			}
		}
		for _, d := range m.db.Deployments {
			if d.NetworkID == n.ID {
				return "network is in use"
			}
		}
		return ""
	})))

	saveHost := func(w http.ResponseWriter, r *http.Request) {
		var h credential
		if !decode(w, r, &h) {
			return
		}
		host, err := m.saveHost(h, r.PathValue("id"))
		if err != nil {
			fail(w, 400, err)
			return
		}
		reply(w, 200, host)
	}
	mux.HandleFunc("POST /api/v1/systems/instances", guard(saveHost))
	mux.HandleFunc("PUT /api/v1/systems/instances/{id}", guard(saveHost))
	mux.HandleFunc("DELETE /api/v1/systems/instances/{id}", guard(deleteResource(m, m.db.Hosts, "server", func(h credential) string {
		for _, d := range m.db.Deployments {
			if d.HostID == h.ID {
				return "remove this server's stopped deployments first"
			}
		}
		return ""
	})))

	mux.HandleFunc("POST /api/v1/deployments", guard(func(w http.ResponseWriter, r *http.Request) {
		var req Start
		if !decode(w, r, &req) {
			return
		}
		d, err := m.start(req)
		if err != nil {
			fail(w, 409, err)
			return
		}
		reply(w, 201, d)
	}))
	mux.HandleFunc("PUT /api/v1/deployments/{id}/ports", guard(func(w http.ResponseWriter, r *http.Request) {
		var ports map[string]int
		if !decode(w, r, &ports) {
			return
		}
		if err := m.savePorts(r.PathValue("id"), ports); err != nil {
			fail(w, 409, err)
			return
		}
		reply(w, 204, nil)
	}))
	mux.HandleFunc("POST /api/v1/deployments/{id}/{action}", guard(func(w http.ResponseWriter, r *http.Request) {
		action := r.PathValue("action")

		if action != "stop" && action != "refresh" && action != "logs" && action != "retry" {
			fail(w, 404, errors.New("unknown action"))
			return
		}
		if err := m.action(r.PathValue("id"), action); err != nil {
			fail(w, 409, err)
			return
		}
		reply(w, 202, nil)
	}))
	mux.HandleFunc("DELETE /api/v1/deployments/{id}", guard(deleteResource(m, m.db.Deployments, "deployment", func(d deployment) string {
		if d.HostID != "" && d.Status != "stopped" || d.Operation != "" {
			return "stop the deployment before removing it"
		}
		return ""
	})))
}

func deleteResource[T any](m *Manager, records map[string]T, name string, blocked func(T) string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		id := r.PathValue("id")
		value, ok := records[id]
		if !ok {
			fail(w, 404, errors.New(name+" not found"))
			return
		}
		if reason := blocked(value); reason != "" {
			fail(w, 409, errors.New(reason))
			return
		}
		delete(records, id)
		if err := m.save(); err != nil {
			records[id] = value
			fail(w, 500, err)
			return
		}
		reply(w, 204, nil)
	}
}
