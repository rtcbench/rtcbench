package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	stdlog "log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
)

type Server struct {
	dir       string
	configs   documents
	envs      documents
	overrides string
	examples  string
	web       fs.FS
	version   string
	log       *stdlog.Logger
	mu        sync.Mutex
	runs      map[string]*run
	profiling bool
}

type File struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

const logTailBytes = 256 << 10

func New(dir, version string, console io.Writer) (*Server, error) {
	configs := documents{kind: "config", dir: filepath.Join(dir, "configs")}
	envs := documents{kind: "environment", dir: filepath.Join(dir, "envs"), redact: redactEnv, merge: mergeEnv(true)}
	for _, sub := range []string{configs.dir, envs.dir, filepath.Join(dir, "runs")} {
		if err := os.MkdirAll(sub, 0o755); err != nil {
			return nil, err
		}
	}
	examples, err := filepath.Abs("config-examples")
	if err != nil {
		return nil, err
	}
	web, _ := fs.Sub(webFiles, "web")
	s := &Server{
		dir:       dir,
		configs:   configs,
		envs:      envs,
		overrides: filepath.Join(dir, "system-overrides.yml"),
		examples:  examples,
		web:       web,
		version:   version,
		log:       stdlog.New(console, "", stdlog.LstdFlags),
		runs:      map[string]*run{},
	}
	return s, s.load()
}

func (s *Server) Logf(format string, args ...any) {
	s.log.Printf(format, args...)
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/version", s.getVersion)
	mux.HandleFunc("GET /api/v1/runs", s.listRuns)
	mux.HandleFunc("POST /api/v1/runs", s.postRun)
	mux.HandleFunc("GET /api/v1/runs/{id}", s.getRun)
	mux.HandleFunc("PATCH /api/v1/runs/{id}", s.patchRun)
	mux.HandleFunc("DELETE /api/v1/runs/{id}", s.deleteRun)
	mux.HandleFunc("POST /api/v1/runs/{id}/stop", s.stopRun)
	mux.HandleFunc("GET /api/v1/runs/{id}/viewers", s.getViewers)
	mux.HandleFunc("GET /api/v1/runs/{id}/files", s.listFiles)
	mux.HandleFunc("GET /api/v1/runs/{id}/files/{path...}", s.getFile)
	mux.HandleFunc("GET /api/v1/runs/{id}/logs/{stream}", s.getLog)
	for _, d := range []documents{s.configs, s.envs} {
		api := "/api/v1/" + d.kind + "s"
		mux.HandleFunc("GET "+api, d.list)
		mux.HandleFunc("POST "+api, d.post)
		mux.HandleFunc("GET "+api+"/{name...}", d.get)
		mux.HandleFunc("PUT "+api+"/{name...}", d.put)
		mux.HandleFunc("DELETE "+api+"/{name...}", d.delete)
		mux.HandleFunc("GET /"+d.kind+"/{name...}", s.page(d))
	}
	mux.HandleFunc("GET /api/v1/system-overrides", s.getOverrides)
	mux.HandleFunc("PUT /api/v1/system-overrides", s.putOverrides)
	mux.HandleFunc("GET /api/v1/examples", s.getExamples)
	mux.HandleFunc("POST /api/v1/examples", s.copyExamples)
	mux.HandleFunc("GET /", s.serveWeb)
	return s.logRequests(mux)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func (s *Server) find(w http.ResponseWriter, r *http.Request) (*run, bool) {
	s.mu.Lock()
	rn, ok := s.runs[r.PathValue("id")]
	s.mu.Unlock()
	if !ok {
		writeError(w, http.StatusNotFound, errors.New("run not found"))
	}
	return rn, ok
}

func (s *Server) getVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"version": s.version})
}

func (s *Server) listRuns(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	runs := make([]Run, 0, len(s.runs))
	for _, rn := range s.runs {
		runs = append(runs, rn.Run)
	}
	s.mu.Unlock()
	slices.SortFunc(runs, func(a, b Run) int { return b.StartedAt.Compare(a.StartedAt) })
	writeJSON(w, http.StatusOK, runs)
}

func (s *Server) postRun(w http.ResponseWriter, r *http.Request) {
	var req StartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	run, err := s.start(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, run)
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	if rn, ok := s.find(w, r); ok {
		s.mu.Lock()
		run := rn.Run
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, run)
	}
}

func (s *Server) patchRun(w http.ResponseWriter, r *http.Request) {
	rn, ok := s.find(w, r)
	if !ok {
		return
	}
	var req UpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	name := strings.TrimSpace(req.Name)
	if !validName(name) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid run name %q", name))
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.move(rn, name); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	rn.Name = name
	save(rn.dir, rn.Run)
	writeJSON(w, http.StatusOK, rn.Run)
}

func (s *Server) deleteRun(w http.ResponseWriter, r *http.Request) {
	rn, ok := s.find(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if rn.Status == StatusRunning {
		writeError(w, http.StatusConflict, errors.New("run is running"))
		return
	}
	if err := os.RemoveAll(rn.dir); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.prune(filepath.Dir(rn.dir))
	delete(s.runs, rn.ID)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) stopRun(w http.ResponseWriter, r *http.Request) {
	rn, ok := s.find(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	running := rn.Status == StatusRunning
	s.mu.Unlock()
	if !running {
		writeError(w, http.StatusConflict, errors.New("run is not running"))
		return
	}
	rn.cancel()
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) getViewers(w http.ResponseWriter, r *http.Request) {
	rn, ok := s.find(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	live, dir := rn.Status == StatusRunning && rn.metrics != nil, rn.dir
	s.mu.Unlock()
	if live {
		rn.metrics.ServeHTTP(w, r)
		return
	}
	data, err := os.ReadFile(filepath.Join(dir, "viewers.json"))
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"status": "no_data", "viewers": []any{}})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func (s *Server) listFiles(w http.ResponseWriter, r *http.Request) {
	rn, ok := s.find(w, r)
	if !ok {
		return
	}
	dir := s.runDir(rn)
	files := []File{}
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		files = append(files, File{Path: filepath.ToSlash(rel), Size: info.Size()})
		return nil
	})
	writeJSON(w, http.StatusOK, files)
}

func (s *Server) getFile(w http.ResponseWriter, r *http.Request) {
	rn, ok := s.find(w, r)
	if !ok {
		return
	}
	root, err := os.OpenRoot(s.runDir(rn))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	defer root.Close()
	path := r.PathValue("path")
	w.Header().Set("Content-Disposition", "attachment; filename="+strconv.Quote(filepath.Base(path)))
	http.ServeFileFS(w, r, root.FS(), path)
}

func (s *Server) getLog(w http.ResponseWriter, r *http.Request) {
	rn, ok := s.find(w, r)
	if !ok {
		return
	}
	stream := r.PathValue("stream")
	if stream != filepath.Base(stream) {
		writeError(w, http.StatusBadRequest, errors.New("invalid stream"))
		return
	}
	f, err := os.Open(filepath.Join(s.runDir(rn), "logs", stream+".log"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if info.Size() > logTailBytes {
		_, _ = f.Seek(-logTailBytes, io.SeekEnd)
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.Copy(w, f)
}
