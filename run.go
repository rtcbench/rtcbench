package rtcbench

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/pprof"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/rtcbench/rtcbench/internal/api"
	"github.com/rtcbench/rtcbench/internal/launch"
	"github.com/rtcbench/rtcbench/internal/netutil"
	"github.com/rtcbench/rtcbench/pkg/deployment"
	"github.com/rtcbench/rtcbench/pkg/metricsserver"
	"github.com/rtcbench/rtcbench/pkg/statsjsonl"
)

//go:embed config-examples
var examples embed.FS

// BuiltinPlugins contains manifests for the plugins registered by the standard CLI
//
//go:embed plugin/*/plugin.yml
var BuiltinPlugins embed.FS

type Options struct {
	// Deployments supplies optional deployment support for plugins
	Deployments []deployment.Plugin
	Version     string
	// Web serves a frontend built and embedded by the caller
	Web fs.FS
	// Plugins supplies plugin.yml manifests for the plugins compiled into the binary
	Plugins fs.FS
	// Register adds plugins and scenarios to each new client, including API runs
	// It may be called concurrently, so register factories rather than shared instances
	Register func(*Client)
}

// Run handles the RTCBench CLI, serving the API and bundled app when no config is given
// It reads <data>/rtcbench.yml and stops on context cancellation or an interrupt
func Run(ctx context.Context, args []string, options Options) error {
	flags := flag.NewFlagSet("rtcbench", flag.ContinueOnError)
	cpuprofile := flags.String("cpuprofile", "", "Write CPU profile to file")
	startAt := flags.Int64("start-at", 0, "Unix timestamp to wait until before joining rooms")
	showVersion := flags.Bool("version", false, "Print version and exit")
	apiAddr := flags.String("api-addr", "127.0.0.1:8080", "Address for the HTTP API when no config file is given")
	data := flags.String("data", "rtcbench-data", "Directory for configs and run outputs when no config file is given")
	settingsFile := flags.String("settings", "rtcbench.yml", "Settings file, relative to the data directory unless absolute")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if options.Version == "" {
		options.Version = "dev"
	}
	if *showVersion {
		fmt.Println(options.Version)
		return nil
	}
	if flags.NArg() > 1 {
		return errors.New("expected one config file; flags must appear before it")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			return err
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			return err
		}
		defer pprof.StopCPUProfile()
	}
	_ = godotenv.Load()
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	if len(args) == 0 && openWindow != nil {
		return serveDesktop(ctx, *settingsFile, options)
	}
	if flags.NArg() == 0 {
		listener, err := net.Listen("tcp", *apiAddr)
		if err != nil {
			return err
		}
		return serveAPI(ctx, listener, *data, *settingsFile, options, nil)
	}
	return runConfig(ctx, flags.Arg(0), *startAt, options.Register)
}

func runConfig(ctx context.Context, file string, startAt int64, register func(*Client)) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	cfg, err := launch.LoadConfig(data, os.LookupEnv)
	if err != nil {
		return fmt.Errorf("load %s: %w", file, err)
	}
	reg, err := launch.Logs(cfg.Spec.Logging)
	if err != nil {
		return err
	}
	defer reg.Close()
	mainLog := reg.NewLogger("general", "")
	if cfg.Spec.Network.ClientIP == "" {
		cfg.Spec.Network.ClientIP, err = netutil.DetectClientIP(cfg.Spec.Network.ServerIP)
		if err != nil {
			return fmt.Errorf("detect client IP, or configure it explicitly: %w", err)
		}
		mainLog.Infof("detected client IP: %s", cfg.Spec.Network.ClientIP)
	}
	client := NewClient(cfg, reg)
	if register != nil {
		register(client)
	}
	if cfg.Spec.Metrics.Port > 0 {
		ms := metricsserver.New(cfg.Spec.Metrics.Port)
		client.AddStatsConsumer(ms.Subscriber())
		metricsCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		go func() {
			mainLog.Infof("[metrics] starting on :%d", cfg.Spec.Metrics.Port)
			if err := ms.ListenAndServe(metricsCtx); err != nil {
				mainLog.Errorf("[metrics] server error: %v", err)
			}
		}()
	}
	if cfg.Spec.Metrics.StatsJSONLPath != "" {
		hostname, _ := os.Hostname()
		jw, err := statsjsonl.New(cfg.Spec.Metrics.StatsJSONLPath, hostname, 0)
		if err != nil {
			mainLog.Errorf("failed to create JSONL stats writer: %v", err)
		} else {
			client.AddStatsConsumer(jw.Subscriber())
			defer jw.Close()
			mainLog.Infof("[stats-jsonl] writing to %s", cfg.Spec.Metrics.StatsJSONLPath)
		}
	}
	if wait := time.Until(time.Unix(startAt, 0)); startAt > 0 && wait > 0 {
		mainLog.Infof("waiting %s until start-at time %d", wait.Round(time.Second), startAt)
		select {
		case <-time.After(wait):
		case <-ctx.Done():
		}
	}
	scenario := cfg.Spec.Scenario
	if scenario == "" {
		scenario = DefaultScenarioID
	}
	var runErr error
	if ctx.Err() == nil {
		runErr = client.RunScenario(ctx, scenario)
		if ctx.Err() != nil && (errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded)) {
			runErr = nil
		}
		if runErr != nil {
			mainLog.Errorf("scenario %q failed: %v", scenario, runErr)
		}
		fatal := errors.Is(runErr, ErrUnknownPlugin) || errors.Is(runErr, ErrUnknownScenario) || errors.Is(runErr, ErrUserExists)
		if !fatal {
			mainLog.Infof("finished scenario %q", scenario)
			<-ctx.Done()
			runErr = nil
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err = client.Shutdown(shutdownCtx)
	summary := client.MetricsSummary()
	logRunMetricsSummary(mainLog, summary)
	return errors.Join(runErr, err,
		EvaluateRunThresholds(summary, cfg.Spec.Metrics.Thresholds),
		EvaluateReceiverThresholds(summary.Receivers, cfg.Spec.Metrics.ReceiverThresholds))
}

func serveAPI(ctx context.Context, listener net.Listener, dir, settingsFile string, options Options, ready func(url string)) error {
	srv, err := api.New(dir, settingsFile, options.Version, os.Stderr, options.Deployments...)
	if err != nil {
		listener.Close()
		return fmt.Errorf("open %s: %w", dir, err)
	}
	srv.Register = options.Register
	if options.Web != nil {
		srv.Web = options.Web
	}
	srv.Plugins = options.Plugins
	srv.Examples, _ = fs.Sub(examples, "config-examples")
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	httpServer := &http.Server{Handler: srv.Handler()}
	done := make(chan error, 1)
	go func() {
		<-ctx.Done()
		shutdownCtx, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		srv.Shutdown(shutdownCtx)
		done <- httpServer.Shutdown(shutdownCtx)
	}()
	url := "http://" + listener.Addr().String()
	srv.Logf("rtcbench %s listening on %s", options.Version, url)
	if ready != nil {
		ready(url)
	}
	err = httpServer.Serve(listener)
	cancel()
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	return errors.Join(err, <-done)
}

func logRunMetricsSummary(l interface{ Infof(string, ...any) }, summary RunMetricsSummary) {
	if len(summary.Operations) == 0 && len(summary.Sessions) == 0 && len(summary.Gauges) == 0 && len(summary.Receivers) == 0 {
		l.Infof("[run-metrics] no metrics recorded")
		return
	}

	for _, op := range summary.Operations {
		l.Infof(
			"[run-metrics] op=%s scenario=%s plugin=%s role=%s attempts=%d successes=%d failures=%d failure_rate=%.4f mean=%.3fs p50=%.3fs p95=%.3fs p99=%.3fs",
			op.Operation,
			op.Scenario,
			op.Plugin,
			op.Role,
			op.Attempts,
			op.Successes,
			op.Failures,
			op.FailureRate,
			op.MeanSeconds,
			op.P50Seconds,
			op.P95Seconds,
			op.P99Seconds,
		)
	}

	for _, session := range summary.Sessions {
		l.Infof(
			"[run-metrics] histogram=%s scenario=%s plugin=%s role=%s count=%d mean=%.3fs min=%.3fs max=%.3fs p50=%.3fs p95=%.3fs p99=%.3fs",
			session.Name,
			session.Scenario,
			session.Plugin,
			session.Role,
			session.Count,
			session.MeanSeconds,
			session.MinSeconds,
			session.MaxSeconds,
			session.P50Seconds,
			session.P95Seconds,
			session.P99Seconds,
		)
	}

	for _, gauge := range summary.Gauges {
		l.Infof(
			"[run-metrics] gauge=%s scenario=%s plugin=%s role=%s current=%.0f peak=%.0f",
			gauge.Name,
			gauge.Scenario,
			gauge.Plugin,
			gauge.Role,
			gauge.Current,
			gauge.Peak,
		)
	}

	for _, receiver := range summary.Receivers {
		l.Infof(
			"[run-metrics] receiver scenario=%s plugin=%s profile=%s user=%s freeze_count=%d freeze_duration=%.2fs frame_loss_ratio=%.4f mean_bitrate_bps=%.0f mean_fps=%.1f max_jitter_us=%.0f mean_rtt_ms=%.1f max_rtt_ms=%.1f pli_count=%d nack_count=%d",
			receiver.Scenario,
			receiver.Plugin,
			receiver.Profile,
			receiver.UserID,
			receiver.FreezeCount,
			receiver.FreezeDurationTotal,
			receiver.FrameLossRatio,
			receiver.MeanBitrateBps,
			receiver.MeanFPS,
			receiver.MaxJitterUS,
			receiver.MeanRTTMS,
			receiver.MaxRTTMS,
			receiver.PliCount,
			receiver.NackCount,
		)
	}
}
