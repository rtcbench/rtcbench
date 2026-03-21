package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	stdlog "log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"call.zip"
	"call.zip/internal/netutil"
	"call.zip/pkg/log"
	"call.zip/pkg/metricsserver"
	"call.zip/pkg/statsjsonl"
	"call.zip/plugin/janus"
	"call.zip/plugin/jitsi"
	"call.zip/plugin/livekit"
	"call.zip/plugin/mediasoup"
	"github.com/goccy/go-yaml"
	"github.com/joho/godotenv"
)

var version = "dev"

func main() {
	var (
		err                           error
		cfg                           *call.Config
		client                        *call.Client
		detectedClientIP              string
		sigtermCtx, shutdownCtx       context.Context
		sigtermCancel, shutdownCancel context.CancelFunc
	)

	startAt := flag.Int64("start-at", 0, "Unix timestamp to wait until before joining rooms")
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		os.Exit(0)
	}

	if flag.NArg() != 1 {
		if flag.NArg() > 1 {
			for _, arg := range flag.Args() {
				if len(arg) > 0 && arg[0] == '-' {
					stdlog.Fatalf("unknown flag: %s (flags must appear before the config file)", arg)
				}
			}
		}
		flag.Usage()
		os.Exit(1)
	}

	_ = godotenv.Load()

	cfg, err = loadYAMLConfig(flag.Arg(0))
	if err != nil {
		stdlog.Fatalf("Failed to load YAML config file: %v", err)
	}

	reg, err := buildLogRegistry(cfg.Spec.Logging)
	if err != nil {
		stdlog.Fatalf("Failed to initialize logging: %v", err)
	}
	defer reg.Close()
	cfg.Log = reg

	mainLog := cfg.Log.NewLogger("general", "")

	if cfg.Spec.Network.ClientIP == "" {
		detectedClientIP, err = netutil.DetectClientIP(cfg.Spec.Network.ServerIP)
		if err != nil {
			stdlog.Fatalf("Failed to detect client IP, please manually configure it: %v", err)
		}
		mainLog.Infof("detected client IP: %s", detectedClientIP)
		cfg.Spec.Network.ClientIP = detectedClientIP
	}

	nat := cfg.Spec.Network.NAT
	if nat.Enabled() {
		if nat.PublicIP != "" {
			mainLog.Infof("NAT traversal: publicIP=%s", nat.PublicIP)
		}
		if len(nat.STUNServers) > 0 {
			mainLog.Infof("NAT traversal: %d STUN server(s) configured", len(nat.STUNServers))
		}
		if len(nat.TURNServers) > 0 {
			mainLog.Infof("NAT traversal: %d TURN server(s) configured", len(nat.TURNServers))
		}
	}

	if cfg.Spec.Metrics.Port > 0 {
		ms := metricsserver.New(cfg.Spec.Metrics.Port)
		cfg.StatsConsumers = append(cfg.StatsConsumers, ms.Subscriber())
		metricsCtx, metricsCancel := context.WithCancel(context.Background())
		defer metricsCancel()
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
			cfg.StatsConsumers = append(cfg.StatsConsumers, jw.Subscriber())
			defer jw.Close()
			mainLog.Infof("[stats-jsonl] writing to %s", cfg.Spec.Metrics.StatsJSONLPath)
		}
	}

	client = call.NewClient(cfg)
	client.RegisterPlugin("jitsi", jitsi.NewPlugin)
	client.RegisterPlugin(janus.PluginID, janus.NewPlugin)
	client.RegisterPlugin(livekit.PluginID, livekit.NewPlugin)
	client.RegisterPlugin(mediasoup.PluginID, mediasoup.NewPlugin)

	if *startAt > 0 {
		waitDuration := time.Until(time.Unix(*startAt, 0))
		if waitDuration > 0 {
			mainLog.Infof("waiting %s until start-at time %d", waitDuration.Round(time.Second), *startAt)
			time.Sleep(waitDuration)
		}
	}

	err = client.JoinAllRooms(context.Background())
	if err != nil {
		mainLog.Errorf("error joining all rooms: %v", err)
		if errors.Is(err, call.ErrUnknownPlugin) {
			os.Exit(1)
		}
	}

	mainLog.Infof("finished joining rooms")

	sigtermCtx, sigtermCancel = signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer sigtermCancel()
	<-sigtermCtx.Done()

	mainLog.Infof("signal received, shutting down...")

	shutdownCtx, shutdownCancel = context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err = client.Shutdown(shutdownCtx); err != nil {
		mainLog.Errorf("shutdown error: %v", err)
	} else {
		mainLog.Infof("shutdown complete")
	}
}

func buildLogRegistry(lc call.LoggingConfig) (*log.Registry, error) {
	levels := make(map[string]log.Level, len(lc.Streams))
	for _, s := range lc.Streams {
		lvl, err := log.ParseLevel(s.Level)
		if err != nil {
			return nil, fmt.Errorf("stream %q: %w", s.Name, err)
		}
		levels[s.Name] = lvl
	}

	var handlers []log.Handler
	if lc.Console {
		consoleLvl, err := log.ParseLevel(lc.ConsoleLevel)
		if err != nil {
			return nil, fmt.Errorf("consoleLevel: %w", err)
		}
		handlers = append(handlers, log.NewConsoleHandler(lc.Color, consoleLvl))
	}
	if lc.Directory != "" {
		fileLvl, err := log.ParseLevel(lc.FileLevel)
		if err != nil {
			return nil, fmt.Errorf("fileLevel: %w", err)
		}
		fh, err := log.NewFileHandler(lc.Directory, fileLvl, lc.Combined)
		if err != nil {
			return nil, err
		}
		handlers = append(handlers, fh)
	}

	return log.NewRegistry(handlers, levels), nil
}

func loadYAMLConfig(yamlFile string) (*call.Config, error) {
	var (
		bytes      []byte
		yamlConfig call.YAMLConfig
		err        error
	)

	bytes, err = os.ReadFile(yamlFile)
	if err != nil {
		return nil, err
	}

	bytes, err = RenderEnvYAML(bytes)
	if err != nil {
		return nil, err
	}

	err = yaml.Unmarshal(bytes, &yamlConfig)
	if err != nil {
		return nil, err
	}

	return yamlConfig.IntoConfig()
}
