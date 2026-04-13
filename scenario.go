package rtcbench

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rtcbench/rtcbench/pkg/log"
)

const DefaultScenarioID = "default"
const ChurnScenarioID = "churn"

type Scenario interface {
	Run(ctx context.Context, env ScenarioEnv) error
}

type ScenarioEnv interface {
	Config() *Config
	Client() *Client
	Log() *log.Logger
}

type scenarioEnv struct {
	config *Config
	client *Client
	log    *log.Logger
}

func (e *scenarioEnv) Config() *Config {
	return e.config
}

func (e *scenarioEnv) Client() *Client {
	return e.client
}

func (e *scenarioEnv) Log() *log.Logger {
	return e.log
}

type DefaultScenario struct{}

func (DefaultScenario) Run(ctx context.Context, env ScenarioEnv) error {
	return env.Client().joinAllRooms(ctx)
}

type ChurnScenario struct{}

type churnConfig struct {
	sessionDuration time.Duration
	rejoinDelay     time.Duration
}

func (ChurnScenario) Run(ctx context.Context, env ScenarioEnv) error {
	cfg, err := loadChurnConfig(env.Config().Spec.ScenarioConfig)
	if err != nil {
		return err
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	if err := env.Client().SetupPlugin(runCtx, env.Config().Spec.Plugin); err != nil {
		return err
	}

	conference := env.Config().Spec.Conference
	joinSpacing := conference.JoinPolicy.JoinStartSpacing
	sendersPerRoom := conference.Cameras.PerRoom

	errCh := make(chan error, conference.TotalRooms*conference.UsersPerRoom)
	doneCh := make(chan struct{})

	for roomIndex := 0; roomIndex < conference.TotalRooms; roomIndex++ {
		roomName := expandRoomName(conference.Name, roomIndex)
		for slot := 0; slot < conference.UsersPerRoom; slot++ {
			role := Viewer
			if slot < sendersPerRoom {
				role = Sender
			}
			initialDelay := time.Duration(slot) * joinSpacing
			go func(roomName string, role UserRole, slot int, initialDelay time.Duration) {
				defer func() {
					doneCh <- struct{}{}
				}()
				if initialDelay > 0 {
					timer := time.NewTimer(initialDelay)
					defer timer.Stop()
					select {
					case <-runCtx.Done():
						return
					case <-timer.C:
					}
				}

				for {
					if err := runChurnSlot(runCtx, env.Client(), env.Log(), env.Config(), roomName, role, slot, cfg); err != nil {
						select {
						case errCh <- err:
						case <-runCtx.Done():
						}
						return
					}

					if cfg.rejoinDelay > 0 {
						timer := time.NewTimer(cfg.rejoinDelay)
						select {
						case <-runCtx.Done():
							timer.Stop()
							return
						case <-timer.C:
						}
					} else {
						select {
						case <-runCtx.Done():
							return
						default:
						}
					}
				}
			}(roomName, role, slot, initialDelay)
		}
	}

	totalSlots := conference.TotalRooms * conference.UsersPerRoom
	for completed := 0; completed < totalSlots; {
		select {
		case err := <-errCh:
			if err != nil {
				cancel()
				return err
			}
		case <-doneCh:
			completed++
		case <-runCtx.Done():
			return nil
		}
	}
	return nil
}

func runChurnSlot(ctx context.Context, client *Client, l *log.Logger, cfg *Config, roomName string, role UserRole, slot int, churnCfg churnConfig) error {
	user := client.CreateUser(ctx, &UserConfig{
		UserID: fmt.Sprintf("%s-%s", string(role), uuid.NewString()),
		Role:   role,
	})
	if err := user.JoinRoom(ctx, &JoinRequest{
		Plugin: cfg.Spec.Plugin,
		RoomID: roomName,
	}); err != nil {
		return fmt.Errorf("churn join room %q slot %d: %w", roomName, slot, err)
	}

	l.Infof("[churn] joined %s slot=%d room=%s user=%s", role, slot, roomName, user.id)

	timer := time.NewTimer(churnCfg.sessionDuration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
	case <-timer.C:
	}

	if err := user.Close(); err != nil {
		return fmt.Errorf("churn close room %q slot %d: %w", roomName, slot, err)
	}

	l.Infof("[churn] left %s slot=%d room=%s user=%s", role, slot, roomName, user.id)
	return nil
}

func loadChurnConfig(raw map[string]any) (churnConfig, error) {
	cfg := churnConfig{
		sessionDuration: 30 * time.Second,
	}
	if raw == nil {
		return cfg, nil
	}

	if v, ok := raw["sessionDuration"]; ok {
		dur, err := durationFromAny(v)
		if err != nil {
			return churnConfig{}, fmt.Errorf("scenarioConfig.sessionDuration: %w", err)
		}
		if dur <= 0 {
			return churnConfig{}, fmt.Errorf("scenarioConfig.sessionDuration: must be > 0")
		}
		cfg.sessionDuration = dur
	}
	if v, ok := raw["rejoinDelay"]; ok {
		dur, err := durationFromAny(v)
		if err != nil {
			return churnConfig{}, fmt.Errorf("scenarioConfig.rejoinDelay: %w", err)
		}
		if dur < 0 {
			return churnConfig{}, fmt.Errorf("scenarioConfig.rejoinDelay: must be >= 0")
		}
		cfg.rejoinDelay = dur
	}

	return cfg, nil
}

func durationFromAny(v any) (time.Duration, error) {
	switch t := v.(type) {
	case string:
		return time.ParseDuration(t)
	default:
		return 0, fmt.Errorf("expected duration string")
	}
}
