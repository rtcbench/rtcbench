package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/rtcbench/rtcbench"
	pkglog "github.com/rtcbench/rtcbench/pkg/log"
	"github.com/rtcbench/rtcbench/plugin/livekit"
)

const programmedScenarioID = "programmed-demo"

type programmedScenario struct{}

func (programmedScenario) Run(ctx context.Context, env rtcbench.ScenarioEnv) error {
	roomID := env.Config().Spec.Conference.Name
	pluginID := env.Config().Spec.Plugin

	user, err := env.Client().CreateUser(ctx, &rtcbench.UserConfig{
		UserID: "yaml-sender-1",
		Role:   rtcbench.Sender,
	})
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	defer user.Close()

	if err := user.JoinRoom(ctx, &rtcbench.JoinRequest{
		Plugin: pluginID,
		RoomID: roomID,
	}); err != nil {
		return fmt.Errorf("join room: %w", err)
	}
	if err := user.PublishVideo(ctx, &rtcbench.PublishVideoRequest{
		Plugin: pluginID,
		RoomID: roomID,
	}); err != nil && !errors.Is(err, rtcbench.ErrUnsupportedCapability) {
		return fmt.Errorf("publish video: %w", err)
	}

	env.Log().Infof("[programmed-demo] joined %s on %s from YAML config", roomID, pluginID)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(10 * time.Second):
		return nil
	}
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "usage: go run ./examples/custom-scenario-yaml <config.yml>\n")
		os.Exit(2)
	}

	bytes, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "read config: %v\n", err)
		os.Exit(1)
	}

	var yc rtcbench.YAMLConfig
	if err := yaml.Unmarshal(bytes, &yc); err != nil {
		fmt.Fprintf(os.Stderr, "parse config: %v\n", err)
		os.Exit(1)
	}
	cfg, err := yc.IntoConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "validate config: %v\n", err)
		os.Exit(1)
	}

	reg := pkglog.NewRegistry(
		[]pkglog.Handler{pkglog.NewConsoleHandler(false, pkglog.LevelInfo)},
		map[string]pkglog.Level{
			"general":     pkglog.LevelInfo,
			"signaling":   pkglog.LevelDebug,
			"media":       pkglog.LevelInfo,
			"packets":     pkglog.LevelInfo,
			"video_stats": pkglog.LevelInfo,
			"livekit":     pkglog.LevelInfo,
		},
	)
	defer reg.Close()

	client := rtcbench.NewClient(cfg, reg)
	client.RegisterPlugin(livekit.PluginID, livekit.NewPlugin)
	client.RegisterScenario(programmedScenarioID, func() rtcbench.Scenario { return programmedScenario{} })

	runCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	scenarioID := cfg.Spec.Scenario
	if scenarioID == "" {
		scenarioID = programmedScenarioID
	}

	if err := client.RunScenario(runCtx, scenarioID); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		fmt.Fprintf(os.Stderr, "scenario failed: %v\n", err)
		os.Exit(1)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := client.Shutdown(shutdownCtx); err != nil {
		fmt.Fprintf(os.Stderr, "shutdown failed: %v\n", err)
		os.Exit(1)
	}
}
