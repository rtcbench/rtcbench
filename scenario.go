package rtcbench

import (
	"context"
	"errors"
	"fmt"
	"sync"
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

type joinRoomConfig struct {
	roomName     string
	usersPerRoom int

	signaling signalingConfig
}

type signalingConfig struct {
	concurrency int
}

type userConfig struct {
	roomID string
	userID string
	role   UserRole
}

type wrappedSignalingError struct {
	error error

	roomName string
	nickname string
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
	return runStaticScenario(ctx, env.Client(), env.Log(), env.Config())
}

type ChurnScenario struct{}

type churnMode string

const (
	churnModeLeaveRejoin churnMode = "leave-rejoin"
	churnModeMediaToggle churnMode = "media-toggle"
)

type churnConfig struct {
	mode            churnMode
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

				if err := runChurnLoop(runCtx, env.Client(), env.Log(), env.Config(), roomName, role, slot, cfg); err != nil {
					select {
					case errCh <- err:
					case <-runCtx.Done():
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
		}
	}
	return nil
}

func runChurnLoop(ctx context.Context, client *Client, l *log.Logger, cfg *Config, roomName string, role UserRole, slot int, churnCfg churnConfig) error {
	if churnCfg.mode == churnModeMediaToggle && role == Sender {
		return runChurnMediaSlot(ctx, client, l, cfg, roomName, role, slot, churnCfg)
	}

	for {
		if err := runChurnSlot(ctx, client, l, cfg, roomName, role, slot, churnCfg); err != nil {
			return err
		}

		if churnCfg.rejoinDelay > 0 {
			timer := time.NewTimer(churnCfg.rejoinDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil
			case <-timer.C:
			}
		} else {
			select {
			case <-ctx.Done():
				return nil
			default:
			}
		}
	}
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

func runChurnMediaSlot(ctx context.Context, client *Client, l *log.Logger, cfg *Config, roomName string, role UserRole, slot int, churnCfg churnConfig) error {
	user := client.CreateUser(ctx, &UserConfig{
		UserID: fmt.Sprintf("%s-%s", string(role), uuid.NewString()),
		Role:   role,
	})
	defer func() {
		if err := user.Close(); err != nil {
			l.Errorf("[churn] close %s slot=%d room=%s user=%s err=%v", role, slot, roomName, user.id, err)
		}
	}()

	if err := user.JoinRoom(ctx, &JoinRequest{
		Plugin: cfg.Spec.Plugin,
		RoomID: roomName,
	}); err != nil {
		return fmt.Errorf("churn join room %q slot %d: %w", roomName, slot, err)
	}
	if err := user.PublishVideo(ctx, &PublishVideoRequest{
		Plugin: cfg.Spec.Plugin,
		RoomID: roomName,
	}); err != nil {
		return fmt.Errorf("churn publish room %q slot %d: %w", roomName, slot, err)
	}

	l.Infof("[churn] joined %s slot=%d room=%s user=%s", role, slot, roomName, user.id)

	for {
		timer := time.NewTimer(churnCfg.sessionDuration)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}

		if err := user.UnpublishVideo(ctx, &UnpublishVideoRequest{
			Plugin: cfg.Spec.Plugin,
			RoomID: roomName,
		}); err != nil {
			return fmt.Errorf("churn unpublish room %q slot %d: %w", roomName, slot, err)
		}

		l.Infof("[churn] unpublished %s slot=%d room=%s user=%s", role, slot, roomName, user.id)

		if churnCfg.rejoinDelay > 0 {
			timer := time.NewTimer(churnCfg.rejoinDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil
			case <-timer.C:
			}
		} else {
			select {
			case <-ctx.Done():
				return nil
			default:
			}
		}

		if err := user.PublishVideo(ctx, &PublishVideoRequest{
			Plugin: cfg.Spec.Plugin,
			RoomID: roomName,
		}); err != nil {
			return fmt.Errorf("churn publish room %q slot %d: %w", roomName, slot, err)
		}

		l.Infof("[churn] published %s slot=%d room=%s user=%s", role, slot, roomName, user.id)
	}
}

func loadChurnConfig(raw map[string]any) (churnConfig, error) {
	cfg := churnConfig{
		mode:            churnModeLeaveRejoin,
		sessionDuration: 30 * time.Second,
	}
	if raw == nil {
		return cfg, nil
	}

	if v, ok := raw["mode"]; ok {
		mode, ok := v.(string)
		if !ok {
			return churnConfig{}, fmt.Errorf("scenarioConfig.mode: expected string")
		}
		switch churnMode(mode) {
		case churnModeLeaveRejoin, churnModeMediaToggle:
			cfg.mode = churnMode(mode)
		default:
			return churnConfig{}, fmt.Errorf("scenarioConfig.mode: must be %q or %q", churnModeLeaveRejoin, churnModeMediaToggle)
		}
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

func runStaticScenario(ctx context.Context, client *Client, l *log.Logger, cfg *Config) error {
	if err := client.SetupPlugin(ctx, cfg.Spec.Plugin); err != nil {
		return err
	}

	var wg sync.WaitGroup
	for i := 0; i < cfg.Spec.Conference.TotalRooms; i++ {
		wg.Add(1)
		roomName := expandRoomName(cfg.Spec.Conference.Name, i)
		go func(roomName string) {
			defer wg.Done()
			l.Infof("joining room %q (%d users)", roomName, cfg.Spec.Conference.UsersPerRoom)
			runStaticRoomByName(ctx, client, l, cfg, roomName)
			l.Infof("finished joining room %q", roomName)
		}(roomName)
	}
	wg.Wait()
	return nil
}

func runStaticRoomByName(ctx context.Context, client *Client, l *log.Logger, cfg *Config, roomName string) {
	if cfg.Spec.Conference.JoinPolicy.AlwaysRetryFailedJoins {
		nUsersRemaining := cfg.Spec.Conference.UsersPerRoom
		for {
			errs := runStaticRoom(ctx, client, cfg, joinRoomConfig{
				roomName:     roomName,
				usersPerRoom: nUsersRemaining,
				signaling: signalingConfig{
					concurrency: cfg.Spec.Conference.JoinPolicy.Concurrency,
				},
			})
			if len(errs) == 0 {
				l.Infof("[AlwaysRetryFailedJoins] finished joining room %s", roomName)
				break
			}

			retryDelay := cfg.Spec.Conference.JoinPolicy.JoinStartSpacing
			if retryDelay < 1*time.Second {
				retryDelay = 1 * time.Second
			}

			l.Errorf("[AlwaysRetryFailedJoins] %d errors occurred, will retry in %s", len(errs), retryDelay.String())
			for _, err := range errs {
				l.Errorf("[AlwaysRetryFailedJoins] error: %v", err.error)
			}
			nUsersRemaining = len(errs)

			timer := time.NewTimer(retryDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		return
	}

	if errs := runStaticRoom(ctx, client, cfg, joinRoomConfig{
		roomName:     roomName,
		usersPerRoom: cfg.Spec.Conference.UsersPerRoom,
		signaling: signalingConfig{
			concurrency: cfg.Spec.Conference.JoinPolicy.Concurrency,
		},
	}); len(errs) > 0 {
		l.Errorf("%d errors from JoinRoom: %v", len(errs), errs)
		for _, err := range errs {
			l.Errorf("error: %v", err.error)
		}
	}
}

func runStaticRoom(ctx context.Context, client *Client, cfg *Config, roomCfg joinRoomConfig) []wrappedSignalingError {
	size := roomCfg.usersPerRoom
	if size < 1 {
		return nil
	}

	cfgCh := make(chan userConfig)
	go func() {
		defer close(cfgCh)

		spacing := cfg.Spec.Conference.JoinPolicy.JoinStartSpacing
		senders := cfg.Spec.Conference.Cameras.PerRoom

		for i := 0; i < roomCfg.usersPerRoom; i++ {
			if i > 0 && spacing > 0 {
				timer := time.NewTimer(spacing)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}

			role := Viewer
			if i < senders {
				role = Sender
			}

			select {
			case <-ctx.Done():
				return
			case cfgCh <- userConfig{
				roomID: roomCfg.roomName,
				userID: fmt.Sprintf("%s-%s", string(role), uuid.NewString()),
				role:   role,
			}:
			}
		}
	}()

	errCh := make(chan wrappedSignalingError, size)
	concurrency := size
	if roomCfg.signaling.concurrency > 0 && roomCfg.signaling.concurrency < size {
		concurrency = roomCfg.signaling.concurrency
	} else if roomCfg.signaling.concurrency <= 0 {
		concurrency = 1
	}

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for vc := range cfgCh {
				user := client.CreateUser(ctx, &UserConfig{
					UserID: vc.userID,
					Role:   vc.role,
				})
				if err := user.JoinRoom(ctx, &JoinRequest{
					Plugin: cfg.Spec.Plugin,
					RoomID: vc.roomID,
				}); err != nil {
					errCh <- wrappedSignalingError{
						error:    fmt.Errorf("cannot join %q to room %q: %w", vc.userID, roomCfg.roomName, err),
						roomName: roomCfg.roomName,
						nickname: vc.userID,
					}
					continue
				}
				if vc.role == Sender {
					err := user.PublishVideo(ctx, &PublishVideoRequest{
						Plugin: cfg.Spec.Plugin,
						RoomID: vc.roomID,
					})
					if err != nil && !errors.Is(err, ErrUnsupportedCapability) {
						errCh <- wrappedSignalingError{
							error:    fmt.Errorf("cannot publish %q in room %q: %w", vc.userID, roomCfg.roomName, err),
							roomName: roomCfg.roomName,
							nickname: vc.userID,
						}
					}
				}
			}
			client.log.Infof("[JoinRoom] worker #%d done (room=%s)", workerID, roomCfg.roomName)
		}(i)
	}

	wg.Wait()
	close(errCh)

	var errs []wrappedSignalingError
	for err := range errCh {
		errs = append(errs, err)
	}
	return errs
}
