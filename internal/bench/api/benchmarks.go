package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rtcbench/rtcbench/internal/bench"
)

type Benchmark struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Status    string        `json:"status"`
	StartedAt time.Time     `json:"startedAt"`
	EndedAt   *time.Time    `json:"endedAt,omitempty"`
	Error     string        `json:"error,omitempty"`
	Request   bench.Request `json:"request"`
	Report    bench.Report  `json:"report"`
}

type benchmarkJob struct {
	Benchmark
	cancel context.CancelFunc
	done   chan struct{}
}

type benchmarkRunner func(context.Context, bench.Request, bench.Options) (bench.Report, error)

func (s *Service) loadBenchmarks() error {
	dir := filepath.Join(s.dir, "benchmarks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := uuid.Parse(entry.Name()); err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name(), "benchmark.json"))
		if err != nil {
			continue
		}
		var b Benchmark
		if json.Unmarshal(data, &b) != nil || b.ID != entry.Name() {
			continue
		}
		if b.Status == StatusRunning {
			b.Status = StatusInterrupted
			now := time.Now().UTC()
			b.EndedAt = &now
			b.Report.Phase = "Interrupted by server restart"
			if err := s.saveBenchmark(b); err != nil {
				return err
			}
		}
		s.benchmarks[b.ID] = &benchmarkJob{Benchmark: b}
	}
	return nil
}

func (s *Service) saveBenchmark(b Benchmark) error {
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	file := filepath.Join(s.dir, "benchmarks", b.ID, "benchmark.json")
	if err := os.WriteFile(file+".tmp", data, 0o600); err != nil {
		return err
	}
	return os.Rename(file+".tmp", file)
}

func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/benchmarks", s.listBenchmarks)
	mux.HandleFunc("POST /api/v1/benchmarks", s.postBenchmark)
	mux.HandleFunc("GET /api/v1/benchmarks/{id}", s.getBenchmark)
	mux.HandleFunc("POST /api/v1/benchmarks/{id}/stop", s.stopBenchmark)
	mux.HandleFunc("DELETE /api/v1/benchmarks/{id}", s.deleteBenchmark)
	mux.HandleFunc("GET /api/v1/benchmarks/{id}/files", s.benchmarkFiles)
	mux.HandleFunc("GET /api/v1/benchmarks/{id}/files/{path...}", s.benchmarkFile)
}

func (s *Service) benchmarkFiles(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	job := s.benchmarks[r.PathValue("id")]
	s.mu.Unlock()
	if job == nil {
		writeError(w, http.StatusNotFound, errors.New("benchmark not found"))
		return
	}
	root, err := os.OpenRoot(filepath.Join(s.dir, "benchmarks", job.ID))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	defer root.Close()
	files := []File{}
	err = fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() || strings.HasSuffix(path, ".tmp") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		files = append(files, File{Path: path, Size: info.Size()})
		return nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, files)
}

func (s *Service) listBenchmarks(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]Benchmark, 0, len(s.benchmarks))
	for _, job := range s.benchmarks {
		b := job.Benchmark
		b.Request.ClusterYAML = ""
		b.Report.Trials = nil
		items = append(items, b)
	}
	slices.SortFunc(items, func(a, b Benchmark) int { return b.StartedAt.Compare(a.StartedAt) })
	writeJSON(w, http.StatusOK, items)
}

func (s *Service) postBenchmark(w http.ResponseWriter, r *http.Request) {
	var req bench.Request
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if !validName(req.Name) {
		writeError(w, http.StatusBadRequest, errors.New("invalid benchmark name"))
		return
	}
	if _, err := req.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		writeError(w, http.StatusServiceUnavailable, errors.New("server is shutting down"))
		return
	}
	for _, job := range s.benchmarks {
		if job.Status == StatusRunning {
			writeError(w, http.StatusConflict, errors.New("a benchmark is already running"))
			return
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	job := &benchmarkJob{
		Benchmark: Benchmark{
			ID:        uuid.NewString(),
			Name:      req.Name,
			Status:    StatusRunning,
			StartedAt: time.Now().UTC(),
			Request:   req,
			Report: bench.Report{
				Phase:   "Preparing",
				Results: map[string]bench.SearchResult{},
				Trials:  []bench.Trial{},
			},
		},
		cancel: cancel,
		done:   make(chan struct{}),
	}
	dir := filepath.Join(s.dir, "benchmarks", job.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		cancel()
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := s.saveBenchmark(job.Benchmark); err != nil {
		cancel()
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.benchmarks[job.ID] = job
	go s.executeBenchmark(ctx, job, dir)
	writeJSON(w, http.StatusCreated, job.Benchmark)
}

func (s *Service) executeBenchmark(ctx context.Context, job *benchmarkJob, dir string) {
	defer close(job.done)
	run := s.benchmarkRun
	if run == nil {
		run = bench.Run
	}
	log, err := os.OpenFile(filepath.Join(dir, "benchmark.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	var report bench.Report
	if err == nil {
		report, err = run(ctx, job.Request, bench.Options{
			Directory: dir,
			ID:        job.ID,
			Log:       log,
			Update: func(next bench.Report) error {
				data, err := json.Marshal(next)
				if err != nil {
					return err
				}
				var snapshot bench.Report
				if err := json.Unmarshal(data, &snapshot); err != nil {
					return err
				}
				s.mu.Lock()
				defer s.mu.Unlock()
				job.Report = snapshot
				return s.saveBenchmark(job.Benchmark)
			},
		})
		err = errors.Join(err, log.Close())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	job.Report = report
	now := time.Now().UTC()
	job.EndedAt = &now
	job.Status = StatusCompleted
	if err != nil {
		job.Status = StatusFailed
		job.Error = err.Error()
	}
	if ctx.Err() != nil {
		job.Status = StatusInterrupted
		job.Report.Phase = "Stopped"
	}
	if err := s.saveBenchmark(job.Benchmark); err != nil {
		job.Status = StatusFailed
		job.Error = fmt.Sprintf("save benchmark: %v", err)
		s.log.Printf("benchmark %s: %v", job.ID, err)
	}
}

func (s *Service) getBenchmark(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job := s.benchmarks[r.PathValue("id")]
	if job == nil {
		writeError(w, http.StatusNotFound, errors.New("benchmark not found"))
		return
	}
	writeJSON(w, http.StatusOK, job.Benchmark)
}

func (s *Service) stopBenchmark(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job := s.benchmarks[r.PathValue("id")]
	if job == nil {
		writeError(w, http.StatusNotFound, errors.New("benchmark not found"))
		return
	}
	if job.Status != StatusRunning {
		writeError(w, http.StatusConflict, errors.New("benchmark is not running"))
		return
	}
	job.cancel()
	job.Report.Phase = "Stopping"
	writeJSON(w, http.StatusAccepted, job.Benchmark)
}

func (s *Service) deleteBenchmark(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job := s.benchmarks[r.PathValue("id")]
	if job == nil {
		writeError(w, http.StatusNotFound, errors.New("benchmark not found"))
		return
	}
	if job.Status == StatusRunning {
		writeError(w, http.StatusConflict, errors.New("benchmark is running"))
		return
	}
	if err := os.RemoveAll(filepath.Join(s.dir, "benchmarks", job.ID)); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	delete(s.benchmarks, job.ID)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) benchmarkFile(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	job := s.benchmarks[r.PathValue("id")]
	s.mu.Unlock()
	if job == nil {
		writeError(w, http.StatusNotFound, errors.New("benchmark not found"))
		return
	}
	root, err := os.OpenRoot(filepath.Join(s.dir, "benchmarks", job.ID))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	defer root.Close()
	w.Header().Set("Content-Disposition", "attachment; filename="+strconv.Quote(filepath.Base(r.PathValue("path"))))
	http.ServeFileFS(w, r, root.FS(), r.PathValue("path"))
}
