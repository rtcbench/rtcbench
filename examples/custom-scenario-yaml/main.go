package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/rtcbench/rtcbench"
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
	err := rtcbench.Run(context.Background(), os.Args[1:], rtcbench.Options{
		Register: func(client *rtcbench.Client) {
			client.RegisterPlugin(livekit.PluginID, livekit.NewPlugin)
			client.RegisterScenario(programmedScenarioID, func() rtcbench.Scenario { return programmedScenario{} })
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
