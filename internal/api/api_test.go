package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"
)

const config = `apiVersion: rtcbench/v1
kind: VideoCallStressTest
metadata:
  name: test
spec:
  plugin: $env:PLUGIN_ID
  conference:
    name: room
    usersPerRoom: 1
    totalRooms: 1
    joinPolicy:
      concurrency: 1
  network:
    serverIP: 127.0.0.1
    clientIP: 127.0.0.1
  logging:
    directory: ignored
`

const labEnv = "- name: PLUGIN_ID\n  type: variable\n  value: nope\n- name: TOKEN\n  type: secret\n  value: s3cret\n- name: SERVER_IP\n  type: system\n"

func newServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	s, err := New(t.TempDir(), "test", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	return s, s.Handler()
}

func do(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reader = strings.NewReader(string(data))
	} else {
		reader = strings.NewReader("")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, reader))
	return rec
}

func TestStartRejectsInvalidRequests(t *testing.T) {
	_, h := newServer(t)
	for _, req := range []StartRequest{{Name: "bad", Config: "spec: ["}, {Name: "a:b", Config: config}, {Name: "missing", Config: config, EnvName: "missing"}} {
		if rec := do(t, h, "POST", "/api/v1/runs", req); rec.Code != http.StatusBadRequest {
			t.Fatalf("%q: status %d", req.Name, rec.Code)
		}
	}
}

func TestRunLifecycle(t *testing.T) {
	var console bytes.Buffer
	s, err := New(t.TempDir(), "test", &console)
	if err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	if code := do(t, h, "POST", "/api/v1/environments", DocumentRequest{Name: "lab", YAML: labEnv}).Code; code != http.StatusCreated {
		t.Fatalf("env: %d", code)
	}
	rec := do(t, h, "POST", "/api/v1/runs", StartRequest{
		ConfigName: "unknown plugin",
		Config:     config,
		EnvName:    "lab",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("start: %d %s", rec.Code, rec.Body)
	}
	var run Run
	_ = json.NewDecoder(rec.Body).Decode(&run)
	if len(run.ID) != 36 || run.Name != "unknown plugin" || run.ConfigName != "unknown plugin" || run.EnvName != "lab" || len(run.Env) != 1 || run.Env["PLUGIN_ID"] != "nope" {
		t.Fatalf("run: %+v", run)
	}
	path := "/api/v1/runs/" + run.ID

	s.mu.Lock()
	done := s.runs[run.ID].done
	s.mu.Unlock()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("run did not finish")
	}

	_ = json.NewDecoder(do(t, h, "GET", path, nil).Body).Decode(&run)
	if run.Status != StatusFailed || run.Error == "" || run.EndedAt == nil {
		t.Fatalf("run: %+v", run)
	}

	for _, name := range []string{" ", "a:b", "../up"} {
		if code := do(t, h, "PATCH", path, UpdateRequest{Name: name}).Code; code != http.StatusBadRequest {
			t.Fatalf("rename %q: %d", name, code)
		}
	}
	const renamed = "Our Data (First phase) 2-20-2027"
	_ = json.NewDecoder(do(t, h, "PATCH", path, UpdateRequest{Name: renamed}).Body).Decode(&run)
	if run.Name != renamed {
		t.Fatalf("rename: %+v", run)
	}

	dir := s.runDir(s.runs[run.ID])
	if filepath.Dir(dir) != filepath.Join(s.dir, "runs", renamed) || !regexp.MustCompile(`^run-\d{8}T\d{6}\.\d{6}Z-`+run.ID+`$`).MatchString(filepath.Base(dir)) {
		t.Fatalf("run directory %q", dir)
	}
	if _, err := os.Stat(filepath.Join(s.dir, "runs", "unknown plugin")); !os.IsNotExist(err) {
		t.Fatal("old run folder kept")
	}
	if _, err := os.Stat(filepath.Join(dir, "run.json")); err != nil {
		t.Fatal(err)
	}

	var files []File
	_ = json.NewDecoder(do(t, h, "GET", path+"/files", nil).Body).Decode(&files)
	names := map[string]bool{}
	for _, f := range files {
		names[f.Path] = true
	}
	for _, want := range []string{"config.yml", "run.json", "viewers.json", "logs/combined.log"} {
		if !names[want] {
			t.Fatalf("missing %s in %v", want, names)
		}
	}
	if body := do(t, h, "GET", path+"/logs/general", nil).Body.String(); !strings.Contains(body, "unknown plugin") {
		t.Fatalf("general log: %q", body)
	}
	if code := do(t, h, "GET", path+"/files/../../secret", nil).Code; code == http.StatusOK {
		t.Fatal("path traversal allowed")
	}
	if code := do(t, h, "POST", path+"/stop", nil).Code; code != http.StatusConflict {
		t.Fatalf("stop finished run: %d", code)
	}
	if code := do(t, h, "DELETE", path, nil).Code; code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	if _, err := os.Stat(filepath.Dir(dir)); !os.IsNotExist(err) {
		t.Fatal("run directory kept")
	}

	logs := console.String()
	for _, want := range []string{"run " + run.ID + " started", "run " + run.ID + " failed", "PATCH " + path + " 200", "DELETE " + path + " 204"} {
		if !strings.Contains(logs, want) {
			t.Fatalf("server log missing %q:\n%s", want, logs)
		}
	}
}

func TestLoadMarksRunningRunsInterrupted(t *testing.T) {
	dir := t.TempDir()
	id := uuid.NewString()
	runDir := filepath.Join(dir, "runs", "lab", "old", "run-20260101T000000.000000Z-"+id)
	_ = os.MkdirAll(runDir, 0o755)
	data, _ := json.Marshal(Run{ID: id, Name: "old", Status: StatusRunning, StartedAt: time.Now()})
	_ = os.WriteFile(filepath.Join(runDir, "run.json"), data, 0o644)

	s, err := New(dir, "test", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.runs[id].Status; got != StatusInterrupted {
		t.Fatalf("status %q", got)
	}
}

func TestDocuments(t *testing.T) {
	s, h := newServer(t)
	for _, c := range []struct {
		d    documents
		yaml string
	}{{s.configs, "a: 1\n"}, {s.envs, "- name: A\n  type: variable\n  value: \"1\"\n"}} {
		d := c.d
		api := "/api/v1/" + d.kind + "s"
		if code := do(t, h, "POST", api, DocumentRequest{Name: "lab/one", YAML: c.yaml}).Code; code != http.StatusCreated {
			t.Fatalf("%s create: %d", d.kind, code)
		}
		if code := do(t, h, "POST", api, DocumentRequest{Name: "lab/one"}).Code; code != http.StatusConflict {
			t.Fatalf("%s duplicate: %d", d.kind, code)
		}
		for _, name := range []string{"../escape", "one.yml", " "} {
			if code := do(t, h, "POST", api, DocumentRequest{Name: name}).Code; code != http.StatusBadRequest {
				t.Fatalf("%s name %q: %d", d.kind, name, code)
			}
		}
		if code := do(t, h, "PUT", api+"/lab/one", DocumentRequest{Name: "two", YAML: c.yaml}).Code; code != http.StatusOK {
			t.Fatalf("%s rename: %d", d.kind, code)
		}
		var docs []Document
		_ = json.NewDecoder(do(t, h, "GET", api, nil).Body).Decode(&docs)
		if len(docs) != 1 || docs[0].Name != "two" {
			t.Fatalf("%s list: %+v", d.kind, docs)
		}
		if body := do(t, h, "GET", "/"+d.kind+"/two.yml", nil).Body.String(); body != c.yaml {
			t.Fatalf("%s raw: %q", d.kind, body)
		}
		if code := do(t, h, "DELETE", api+"/two", nil).Code; code != http.StatusNoContent {
			t.Fatalf("%s delete: %d", d.kind, code)
		}
		if entries, _ := os.ReadDir(d.dir); len(entries) != 0 {
			t.Fatalf("%s leftover entries: %v", d.kind, entries)
		}
	}
}

func TestWebServesIndexForPages(t *testing.T) {
	s, h := newServer(t)
	s.web = fstest.MapFS{"index.html": {Data: []byte("app")}, "main.js": {Data: []byte("js")}}
	for path, want := range map[string]string{"/": "app", "/runs": "app", "/config/two": "app", "/environment/two": "app", "/main.js": "js"} {
		if body := do(t, h, "GET", path, nil).Body.String(); body != want {
			t.Fatalf("%s: %q", path, body)
		}
	}
}

func TestExamples(t *testing.T) {
	s, h := newServer(t)
	s.examples = t.TempDir()
	_ = os.MkdirAll(filepath.Join(s.examples, "jitsi"), 0o755)
	_ = os.WriteFile(filepath.Join(s.examples, "one.yml"), []byte("a: 1\n"), 0o644)
	_ = os.WriteFile(filepath.Join(s.examples, "jitsi", "two.yml"), []byte("b: 2\n"), 0o644)

	var examples Examples
	_ = json.NewDecoder(do(t, h, "GET", "/api/v1/examples", nil).Body).Decode(&examples)
	if strings.Join(examples.Files, ",") != "jitsi/two.yml,one.yml" || !strings.HasSuffix(examples.Pattern, filepath.Join("**", "*.yml")) {
		t.Fatalf("examples: %+v", examples)
	}
	for range 2 {
		if code := do(t, h, "POST", "/api/v1/examples", nil).Code; code != http.StatusNoContent {
			t.Fatalf("copy: %d", code)
		}
	}
	var configs []Document
	_ = json.NewDecoder(do(t, h, "GET", "/api/v1/configs", nil).Body).Decode(&configs)
	if len(configs) != 2 || configs[0].Name != "jitsi/two" || configs[1].YAML != "a: 1\n" {
		t.Fatalf("configs: %+v", configs)
	}
}

func TestValidName(t *testing.T) {
	for name, want := range map[string]bool{
		"Our Data (First phase) 2-20-2027": true,
		"jitsi/250-viewer-bots":            true,
		"Données #2, v1.5 [final]":         true,
		"":                                 false,
		"a:b":                              false,
		`a\b`:                              false,
		"a*":                               false,
		"tab\there":                        false,
		"../up":                            false,
		"/root":                            false,
		"lab/":                             false,
		"lab//one":                         false,
		"ends.":                            false,
		".hidden":                          false,
		"lab/.git":                         false,
		"lab/ padded":                      false,
		"CON":                              false,
		"lab/com1.txt":                     false,
		strings.Repeat("a", 65):            false,
	} {
		if got := validName(name); got != want {
			t.Errorf("validName(%q) = %v", name, got)
		}
	}
}

func TestEnvSecrets(t *testing.T) {
	s, h := newServer(t)
	if code := do(t, h, "POST", "/api/v1/environments", DocumentRequest{Name: "lab", YAML: labEnv}).Code; code != http.StatusCreated {
		t.Fatalf("create: %d", code)
	}
	const redacted = "- name: PLUGIN_ID\n  type: variable\n  value: nope\n- name: TOKEN\n  type: secret\n- name: SERVER_IP\n  type: system\n"
	var env Document
	_ = json.NewDecoder(do(t, h, "GET", "/api/v1/environments/lab", nil).Body).Decode(&env)
	if env.YAML != redacted {
		t.Fatalf("get: %q", env.YAML)
	}
	if code := do(t, h, "PUT", "/api/v1/environments/lab", DocumentRequest{Name: "lab", YAML: redacted}).Code; code != http.StatusOK {
		t.Fatalf("keep secret: %d", code)
	}
	if data, _ := os.ReadFile(filepath.Join(s.envs.dir, "lab.yml")); !strings.Contains(string(data), "s3cret") {
		t.Fatalf("secret lost: %s", data)
	}
	for _, yaml := range []string{
		"- name: TOKEN\n  type: variable\n",
		"- name: NEW\n  type: secret\n",
		"- name: PLUGIN_ID\n  type: secret\n",
		"- name: bad-name\n  type: variable\n  value: x\n",
		"- name: A\n  type: variable\n  value: x\n- name: A\n  type: variable\n  value: y\n",
	} {
		if code := do(t, h, "PUT", "/api/v1/environments/lab", DocumentRequest{Name: "lab", YAML: yaml}).Code; code != http.StatusBadRequest {
			t.Fatalf("%q: %d", yaml, code)
		}
	}
	if code := do(t, h, "PUT", "/api/v1/system-overrides", DocumentRequest{YAML: "- name: X\n  type: system\n"}).Code; code != http.StatusBadRequest {
		t.Fatalf("system override of type system: %d", code)
	}
	if code := do(t, h, "PUT", "/api/v1/system-overrides", DocumentRequest{YAML: "- name: PLUGIN_ID\n  type: variable\n  value: nope\n- name: TOKEN\n  type: secret\n  value: s3cret\n"}).Code; code != http.StatusOK {
		t.Fatalf("overrides: %d", code)
	}
	for _, path := range []string{"/api/v1/environments", "/api/v1/environments/lab", "/environment/lab.yml", "/api/v1/system-overrides"} {
		if body := do(t, h, "GET", path, nil).Body.String(); !strings.Contains(body, "TOKEN") || strings.Contains(body, "s3cret") {
			t.Fatalf("%s: %s", path, body)
		}
	}
	rec := do(t, h, "POST", "/api/v1/runs", StartRequest{Name: "override", Config: config})
	if rec.Code != http.StatusCreated {
		t.Fatalf("run with overrides: %d %s", rec.Code, rec.Body)
	}
	var run Run
	_ = json.NewDecoder(rec.Body).Decode(&run)
	s.mu.Lock()
	done := s.runs[run.ID].done
	s.mu.Unlock()
	<-done
	if run.Env != nil {
		t.Fatalf("run recorded overrides: %v", run.Env)
	}
}
