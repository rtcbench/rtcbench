package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/rtcbench/rtcbench"
	pkglog "github.com/rtcbench/rtcbench/pkg/log"
	"github.com/rtcbench/rtcbench/plugin/livekit"
)

const programmedScenarioID = "programmed-demo"

type programmedScenario struct{}

func (programmedScenario) Run(ctx context.Context, env rtcbench.ScenarioEnv) error {
	roomID := env.Config().Spec.Conference.Name
	pluginID := env.Config().Spec.Plugin

	sender := env.Client().CreateUser(ctx, &rtcbench.UserConfig{
		UserID: "sender-1",
		Role:   rtcbench.Sender,
	})
	defer sender.Close()

	if err := sender.JoinRoom(ctx, &rtcbench.JoinRequest{
		Plugin: pluginID,
		RoomID: roomID,
	}); err != nil {
		return fmt.Errorf("sender join: %w", err)
	}

	if err := sender.PublishVideo(ctx, &rtcbench.PublishVideoRequest{
		Plugin: pluginID,
		RoomID: roomID,
	}); err != nil && !errors.Is(err, rtcbench.ErrUnsupportedCapability) {
		return fmt.Errorf("sender publish video: %w", err)
	}

	viewer := env.Client().CreateUser(ctx, &rtcbench.UserConfig{
		UserID: "viewer-1",
		Role:   rtcbench.Viewer,
	})
	defer viewer.Close()

	if err := viewer.JoinRoom(ctx, &rtcbench.JoinRequest{
		Plugin: pluginID,
		RoomID: roomID,
	}); err != nil {
		return fmt.Errorf("viewer join: %w", err)
	}

	env.Log().Infof("[programmed-demo] sender and viewer joined %s on %s", roomID, pluginID)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(20 * time.Second):
		return nil
	}
}

func main() {
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

	cfg := &rtcbench.Config{
		APIVersion: rtcbench.APIVersionV1,
		Kind:       rtcbench.KindVideoCallStressTest,
		Metadata: rtcbench.MetadataConfig{
			Name: "programmed-demo",
		},
		Spec: rtcbench.SpecConfig{
			Plugin:   livekit.PluginID,
			Scenario: programmedScenarioID,
			Conference: rtcbench.ConferenceConfig{
				Name:         "room-1234",
				UsersPerRoom: 2,
				TotalRooms:   1,
				Cameras: rtcbench.CameraConfig{
					PerRoom:    1,
					FileType:   "ivf",
					VideoCodec: "vp9",
					Directory:  "/absolute/path/to/vp9_ivf_videos",
				},
				JoinPolicy: rtcbench.JoinPolicyConfig{
					Concurrency:            1,
					AlwaysRetryFailedJoins: true,
					JoinStartSpacing:       0,
				},
			},
			Network: rtcbench.NetworkConfig{
				ServerIP: "127.0.0.1",
			},
			PluginConfig: map[string]any{
				livekit.PluginID: map[string]any{
					"wsURL":     "ws://127.0.0.1:7880",
					"apiKey":    "devkey",
					"apiSecret": "secret-secret-secret-secret-secret",
				},
			},
		},
	}

	client := rtcbench.NewClient(cfg, reg)
	client.RegisterPlugin(livekit.PluginID, livekit.NewPlugin)
	client.RegisterScenario(programmedScenarioID, func() rtcbench.Scenario { return programmedScenario{} })

	runCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := client.RunScenario(runCtx, programmedScenarioID); err != nil && !errors.Is(err, context.DeadlineExceeded) {
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
