package api

import (
	"encoding/json"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

const (
	StatusRunning     = "running"
	StatusCompleted   = "completed"
	StatusFailed      = "failed"
	StatusInterrupted = "interrupted"
)

type Service struct {
	dir          string
	log          *log.Logger
	mu           sync.Mutex
	closing      bool
	benchmarks   map[string]*benchmarkJob
	benchmarkRun benchmarkRunner
}

func New(dir string, logger *log.Logger) (*Service, error) {
	s := &Service{dir: dir, log: logger, benchmarks: make(map[string]*benchmarkJob)}
	if err := s.loadBenchmarks(); err != nil {
		return nil, err
	}
	return s, nil
}

// Cancel rejects new jobs and returns a channel closed after active jobs finish cleanup.
func (s *Service) Cancel() <-chan struct{} {
	s.mu.Lock()
	s.closing = true
	var active []<-chan struct{}
	for _, job := range s.benchmarks {
		if job.cancel != nil {
			job.cancel()
		}
		if job.done != nil {
			active = append(active, job.done)
		}
	}
	s.mu.Unlock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, job := range active {
			<-job
		}
	}()
	return done
}

type File struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

var reserved = regexp.MustCompile(`(?i)^(con|prn|aux|nul|(com|lpt)[0-9Â¹Â²Â³])(\.|$)`)

func validName(name string) bool {
	if utf8.RuneCountInString(name) > 64 || strings.ContainsFunc(name, func(r rune) bool {
		return unicode.IsControl(r) || strings.ContainsRune(`<>:"\|?*`, r)
	}) {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part != strings.TrimSpace(part) || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".") || reserved.MatchString(part) {
			return false
		}
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
