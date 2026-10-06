package api

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime/pprof"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/rtcbench/rtcbench"
	"github.com/rtcbench/rtcbench/internal/launch"
	"github.com/rtcbench/rtcbench/internal/netutil"
	"github.com/rtcbench/rtcbench/pkg/log"
	"github.com/rtcbench/rtcbench/pkg/metricsserver"
	"github.com/rtcbench/rtcbench/pkg/statsjsonl"
)

const (
	StatusRunning     = "running"
	StatusCompleted   = "completed"
	StatusFailed      = "failed"
	StatusInterrupted = "interrupted"
)

type StartRequest struct {
	Name       string `json:"name"`
	ConfigName string `json:"configName,omitempty"`
	Config     string `json:"config"`
	EnvName    string `json:"envName,omitempty"`
	StartAt    int64  `json:"startAt,omitempty"`
	CPUProfile bool   `json:"cpuProfile,omitempty"`
}

type UpdateRequest struct {
	Name string `json:"name"`
}

type Run struct {
	ID         string                      `json:"id"`
	Name       string                      `json:"name"`
	ConfigName string                      `json:"configName,omitempty"`
	EnvName    string                      `json:"envName,omitempty"`
	Status     string                      `json:"status"`
	StartedAt  time.Time                   `json:"startedAt"`
	EndedAt    *time.Time                  `json:"endedAt,omitempty"`
	StartAt    int64                       `json:"startAt,omitempty"`
	CPUProfile bool                        `json:"cpuProfile,omitempty"`
	Env        map[string]string           `json:"env,omitempty"`
	Error      string                      `json:"error,omitempty"`
	Failures   []string                    `json:"failures,omitempty"`
	Summary    *rtcbench.RunMetricsSummary `json:"summary,omitempty"`
}

type run struct {
	Run
	dir     string
	cancel  context.CancelFunc
	metrics *metricsserver.Server
	done    chan struct{}
}

type session struct {
	config  *rtcbench.Config
	client  *rtcbench.Client
	logs    *log.Registry
	stats   *statsjsonl.Writer
	profile *os.File
}

var reserved = regexp.MustCompile(`(?i)^(con|prn|aux|nul|(com|lpt)[0-9¹²³])(\.|$)`)

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

func (s *Server) load() error {
	return filepath.WalkDir(filepath.Join(s.dir, "runs"), func(dir string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() || !strings.HasPrefix(d.Name(), "run-") {
			return err
		}
		data, err := os.ReadFile(filepath.Join(dir, "run.json"))
		if err != nil {
			return nil
		}
		var r Run
		if json.Unmarshal(data, &r) != nil || !strings.HasSuffix(d.Name(), "-"+r.ID) {
			return nil
		}
		if r.Status == StatusRunning {
			now := time.Now()
			r.Status, r.EndedAt = StatusInterrupted, &now
			save(dir, r)
		}
		s.runs[r.ID] = &run{Run: r, dir: dir}
		return filepath.SkipDir
	})
}

func (s *Server) runDir(rn *run) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return rn.dir
}

func (s *Server) move(rn *run, name string) error {
	dir := filepath.Join(s.dir, "runs", name, filepath.Base(rn.dir))
	if rn.Status == StatusRunning || dir == rn.dir {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}
	if err := os.Rename(rn.dir, dir); err != nil {
		s.prune(filepath.Dir(dir))
		return err
	}
	s.prune(filepath.Dir(rn.dir))
	rn.dir = dir
	return nil
}

func (s *Server) prune(dir string) {
	runs := filepath.Join(s.dir, "runs")
	for dir != runs && os.Remove(dir) == nil {
		dir = filepath.Dir(dir)
	}
}

func save(dir string, r Run) {
	data, _ := json.MarshalIndent(r, "", "  ")
	path := filepath.Join(dir, "run.json")
	if os.WriteFile(path+".tmp", data, 0o644) == nil {
		_ = os.Rename(path+".tmp", path)
	}
}

func (s *Server) start(req StartRequest) (Run, error) {
	name := cmp.Or(strings.TrimSpace(req.Name), req.ConfigName, "run")
	if !validName(name) {
		return Run{}, fmt.Errorf("invalid run name %q", name)
	}
	env, variables, err := s.environment(req.EnvName)
	if err != nil {
		return Run{}, err
	}
	lookup := func(key string) (string, bool) {
		if v, ok := env[key]; ok {
			return v, true
		}
		return os.LookupEnv(key)
	}
	cfg, err := launch.LoadConfig([]byte(req.Config), lookup)
	if err != nil {
		return Run{}, err
	}
	if cfg.Spec.Network.ClientIP == "" {
		ip, err := netutil.DetectClientIP(cfg.Spec.Network.ServerIP)
		if err != nil {
			return Run{}, fmt.Errorf("detect client IP: %w", err)
		}
		cfg.Spec.Network.ClientIP = ip
	}

	s.mu.Lock()
	if req.CPUProfile && s.profiling {
		s.mu.Unlock()
		return Run{}, errors.New("a CPU profile is already being recorded")
	}
	id := uuid.NewString()
	s.profiling = s.profiling || req.CPUProfile
	s.mu.Unlock()

	started := time.Now()
	dir := filepath.Join(s.dir, "runs", name, "run-"+started.UTC().Format("20060102T150405.000000Z")+"-"+id)
	sess, err := s.open(dir, req, cfg)
	if err != nil {
		s.mu.Lock()
		s.profiling = s.profiling && !req.CPUProfile
		s.mu.Unlock()
		_ = os.RemoveAll(dir)
		s.prune(filepath.Dir(dir))
		return Run{}, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	rn := &run{
		Run: Run{
			ID:         id,
			Name:       name,
			ConfigName: req.ConfigName,
			EnvName:    req.EnvName,
			Status:     StatusRunning,
			StartedAt:  started,
			StartAt:    req.StartAt,
			CPUProfile: req.CPUProfile,
			Env:        variables,
		},
		dir:     dir,
		cancel:  cancel,
		metrics: metricsserver.New(0),
		done:    make(chan struct{}),
	}
	sess.client.AddStatsConsumer(rn.metrics.Subscriber())

	s.mu.Lock()
	s.runs[id] = rn
	save(dir, rn.Run)
	s.mu.Unlock()
	s.Logf("run %s started: %s", id, rn.Name)
	go s.execute(ctx, rn, sess)
	return rn.Run, nil
}

func (s *Server) open(dir string, req StartRequest, cfg *rtcbench.Config) (*session, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yml"), []byte(req.Config), 0o644); err != nil {
		return nil, err
	}
	cfg.Spec.Logging.Console = false
	cfg.Spec.Logging.Combined = true
	cfg.Spec.Logging.Directory = filepath.Join(dir, "logs")
	cfg.Spec.Metrics.Port = 0
	cfg.Spec.Metrics.StatsJSONLPath = dir
	if cfg.Spec.Conference.Recording.Enabled {
		cfg.Spec.Conference.Recording.Directory = filepath.Join(dir, "recordings")
	}
	if cfg.Spec.Conference.PacketCapture.Enabled {
		cfg.Spec.Conference.PacketCapture.Directory = filepath.Join(dir, "pcap")
	}

	logs, err := launch.Logs(cfg.Spec.Logging)
	if err != nil {
		return nil, err
	}
	host, _ := os.Hostname()
	stats, err := statsjsonl.New(dir, host, 0)
	if err != nil {
		logs.Close()
		return nil, err
	}
	client := rtcbench.NewClient(cfg, logs)
	launch.RegisterPlugins(client)
	client.AddStatsConsumer(stats.Subscriber())

	sess := &session{config: cfg, client: client, logs: logs, stats: stats}
	if req.CPUProfile {
		f, err := os.Create(filepath.Join(dir, "cpu.pprof"))
		if err == nil {
			err = pprof.StartCPUProfile(f)
		}
		if err != nil {
			stats.Close()
			logs.Close()
			return nil, err
		}
		sess.profile = f
	}
	return sess, nil
}

func (s *Server) execute(ctx context.Context, rn *run, sess *session) {
	defer close(rn.done)
	general := sess.logs.NewLogger("general", "")
	var runErr error

	if wait := time.Until(time.Unix(rn.StartAt, 0)); rn.StartAt > 0 && wait > 0 {
		general.Infof("waiting %s until start-at time %d", wait.Round(time.Second), rn.StartAt)
		select {
		case <-time.After(wait):
		case <-ctx.Done():
		}
	}

	if ctx.Err() == nil {
		scenario := sess.config.Spec.Scenario
		if scenario == "" {
			scenario = rtcbench.DefaultScenarioID
		}
		err := sess.client.RunScenario(ctx, scenario)
		canceled := errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
		if err != nil && (ctx.Err() == nil || !canceled) {
			general.Errorf("scenario %q failed: %v", scenario, err)
			runErr = err
		}
		fatal := errors.Is(err, rtcbench.ErrUnknownPlugin) ||
			errors.Is(err, rtcbench.ErrUnknownScenario) ||
			errors.Is(err, rtcbench.ErrUserExists)
		if !fatal {
			general.Infof("finished scenario %q", scenario)
			<-ctx.Done()
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := sess.client.Shutdown(shutdownCtx); err != nil {
		general.Errorf("shutdown error: %v", err)
	}
	summary := sess.client.MetricsSummary()
	var failures []string
	if err := rtcbench.EvaluateRunThresholds(summary, sess.config.Spec.Metrics.Thresholds); err != nil {
		failures = append(failures, err.Error())
	}
	if err := rtcbench.EvaluateReceiverThresholds(summary.Receivers, sess.config.Spec.Metrics.ReceiverThresholds); err != nil {
		failures = append(failures, err.Error())
	}

	snapshot := httptest.NewRecorder()
	rn.metrics.ServeHTTP(snapshot, httptest.NewRequest("GET", "/", nil))
	_ = os.WriteFile(filepath.Join(rn.dir, "viewers.json"), snapshot.Body.Bytes(), 0o644)

	if sess.profile != nil {
		pprof.StopCPUProfile()
		sess.profile.Close()
	}
	sess.stats.Close()
	sess.logs.Close()

	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	rn.EndedAt, rn.Summary, rn.Failures = &now, &summary, failures
	rn.Status = StatusCompleted
	if runErr != nil || len(failures) > 0 {
		rn.Status = StatusFailed
	}
	if runErr != nil {
		rn.Error = runErr.Error()
	}
	if sess.profile != nil {
		s.profiling = false
	}
	if err := s.move(rn, rn.Name); err != nil {
		s.Logf("run %s move: %v", rn.ID, err)
	}
	save(rn.dir, rn.Run)
	s.Logf("run %s %s", rn.ID, rn.Status)
	if rn.Error != "" {
		s.Logf("run %s error: %s", rn.ID, rn.Error)
	}
	for _, failure := range failures {
		s.Logf("run %s threshold failed: %s", rn.ID, failure)
	}
}

func (s *Server) Shutdown(ctx context.Context) {
	s.mu.Lock()
	var active []*run
	for _, rn := range s.runs {
		if rn.Status == StatusRunning {
			active = append(active, rn)
		}
	}
	s.mu.Unlock()
	for _, rn := range active {
		rn.cancel()
		select {
		case <-rn.done:
		case <-ctx.Done():
			return
		}
	}
}
