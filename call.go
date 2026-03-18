package call

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"call.zip/pkg/log"
	"github.com/google/uuid"
)

type UserRole string

const (
	Viewer UserRole = "viewer"
	Sender UserRole = "sender"
)

var (
	ErrUnsupportedRole = errors.New("unsupported role")
	ErrCannotJoinRoom  = errors.New("cannot join room")
	ErrUnknownPlugin   = errors.New("unknown plugin")
)

type Plugin interface {
	Setup(ctx context.Context, config *Config) error
	Shutdown(ctx context.Context) error
	JoinRoom(ctx context.Context, role UserRole, roomID, userID string) error
}

type PluginFactory func() Plugin

type PluginRegistry map[string]PluginFactory

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

	// other metadata for retrying
	roomName string
	nickname string
}

type Client struct {
	config   *Config
	registry PluginRegistry
	plugin   Plugin
	log      *log.Logger
}

func NewClient(config *Config) *Client {
	return &Client{
		config:   config,
		registry: make(PluginRegistry),
		plugin:   nil,
		log:      config.Log.NewLogger("general", ""),
	}
}

func (c *Client) RegisterPlugin(pluginID string, factory PluginFactory) {
	c.registry[pluginID] = factory
}

func (c *Client) Shutdown(ctx context.Context) error {
	var shutdownErr error
	done := make(chan struct{})
	go func() {
		err := c.plugin.Shutdown(ctx)
		if err != nil {
			shutdownErr = err
		}
		close(done)
	}()

	select {
	case <-done:
		return shutdownErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Client) JoinAllRooms(ctx context.Context) error {
	if c.plugin == nil {
		factory, exists := c.registry[c.config.Spec.Plugin]
		if !exists {
			return ErrUnknownPlugin
		}
		c.plugin = factory()
		err := c.plugin.Setup(ctx, c.config)
		if err != nil {
			return err
		}
	}
	wg := sync.WaitGroup{}
	for i := 0; i < c.config.Spec.Conference.TotalRooms; i++ {
		wg.Add(1)
		fmtRoomName := expandRoomName(c.config.Spec.Conference.Name, i)
		go func() {
			c.log.Infof("joining room %q (%d users)", fmtRoomName, c.config.Spec.Conference.UsersPerRoom)
			c.joinRoomByName(ctx, fmtRoomName)
			c.log.Infof("finished joining room %q", fmtRoomName)
			wg.Done()
		}()
	}
	wg.Wait()
	return nil
}

// expandRoomName generates the room name for a given index in a multi-room
// setup. If the base name ends with digits, those digits are parsed as a
// number and incremented by offset. For index 0 the original name is returned.
//
//	expandRoomName("room-1234", 0) → "room-1234"
//	expandRoomName("room-1234", 1) → "room-1235"
//	expandRoomName("room-1234", 2) → "room-1236"
//	expandRoomName("nodigits", 1)  → "nodigits_1"
func expandRoomName(base string, offset int) string {
	if offset == 0 {
		return base
	}
	end := len(base)
	if end == 0 || base[end-1] < '0' || base[end-1] > '9' {
		return base + "_" + strconv.Itoa(offset)
	}
	start := end - 1
	for start > 0 && base[start-1] >= '0' && base[start-1] <= '9' {
		start--
	}
	n, err := strconv.ParseInt(base[start:end], 10, 64)
	if err != nil {
		return base + "_" + strconv.Itoa(offset)
	}
	return base[:start] + strconv.FormatInt(n+int64(offset), 10)
}

func (c *Client) joinRoomByName(ctx context.Context, roomName string) {
	if c.config.Spec.Conference.JoinPolicy.AlwaysRetryFailedJoins {
		nUsersRemaining := c.config.Spec.Conference.UsersPerRoom
		var errs []wrappedSignalingError
		for {
			errs = c.joinRoom(ctx, joinRoomConfig{
				roomName:     roomName,
				usersPerRoom: nUsersRemaining,
				signaling: signalingConfig{
					concurrency: c.config.Spec.Conference.JoinPolicy.Concurrency,
				},
			})
			if len(errs) == 0 {
				c.log.Infof("[AlwaysRetryFailedJoins] finished joining room %s", roomName)
				break
			}

			retryDelay := c.config.Spec.Conference.JoinPolicy.JoinStartSpacing
			if retryDelay < 1*time.Second {
				retryDelay = 1 * time.Second
			}

			c.log.Errorf("[AlwaysRetryFailedJoins] %d errors occurred, will retry in %s", len(errs), retryDelay.String())
			for _, e := range errs {
				c.log.Errorf("[AlwaysRetryFailedJoins] error: %v", e.error)
			}
			nUsersRemaining = len(errs)

			time.Sleep(retryDelay)
		}
	} else {
		if errs := c.joinRoom(ctx, joinRoomConfig{
			roomName:     roomName,
			usersPerRoom: c.config.Spec.Conference.UsersPerRoom,
			signaling: signalingConfig{
				concurrency: c.config.Spec.Conference.JoinPolicy.Concurrency,
			},
		}); errs != nil {
			c.log.Errorf("%d errors from JoinRoom: %v", len(errs), errs)
			for _, e := range errs {
				c.log.Errorf("error: %v", e.error)
			}
		}
	}
}

func (c *Client) joinRoom(ctx context.Context, cfg joinRoomConfig) []wrappedSignalingError {
	size := cfg.usersPerRoom
	if size < 1 {
		return nil
	}

	cfgCh := make(chan userConfig)
	go func() {
		defer close(cfgCh)

		spacing := c.config.Spec.Conference.JoinPolicy.JoinStartSpacing
		senders := c.config.Spec.Conference.Cameras.PerRoom

		for i := 0; i < cfg.usersPerRoom; i++ {
			if i > 0 && spacing > 0 {
				time.Sleep(spacing)
			}

			role := Viewer
			if i < senders { // senders have join priority over viewers
				role = Sender
			}

			cfgCh <- userConfig{
				roomID: cfg.roomName,
				userID: fmt.Sprintf("%s-%s", string(role), uuid.NewString()),
				role:   role,
			}
		}
	}()

	errCh := make(chan wrappedSignalingError, size)

	concurrency := size
	if cfg.signaling.concurrency > 0 && cfg.signaling.concurrency < size {
		concurrency = cfg.signaling.concurrency
	} else if cfg.signaling.concurrency <= 0 {
		concurrency = 1 // default to serial
	}

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for vc := range cfgCh {
				if err := c.plugin.JoinRoom(ctx, vc.role, vc.roomID, vc.userID); err != nil {
					errCh <- wrappedSignalingError{
						error:    fmt.Errorf("cannot join %q to room %q: %w", vc.userID, cfg.roomName, err),
						roomName: cfg.roomName,
						nickname: vc.userID,
					}
				}
			}
			c.log.Infof("[JoinRoom] worker #%d done (room=%s)", workerID, cfg.roomName)
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
