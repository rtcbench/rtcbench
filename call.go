package call

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

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
	roomName   string
	viewerBots int

	signaling signalingConfig
}

type signalingConfig struct {
	concurrency int
}

type viewerConfig struct {
	roomID string
	userID string
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
}

func NewClient(config *Config) *Client {
	return &Client{
		config:   config,
		registry: make(PluginRegistry),
		plugin:   nil,
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
		fmtRoomName := c.config.Spec.Conference.Name
		if c.config.Spec.Conference.TotalRooms > 1 {
			fmtRoomName += "_" + strconv.Itoa(i)
		}
		go func() {
			log.Printf("[cli-main] joining room %q (%d viewers)", fmtRoomName, c.config.Spec.Conference.Viewers)
			c.joinRoomByName(fmtRoomName)
			log.Printf("[cli-main] finished joining room %q", fmtRoomName)
			wg.Done()
		}()
	}
	wg.Wait()
	return nil
}

func (c *Client) joinRoomByName(roomName string) {
	if c.config.Spec.Conference.JoinPolicy.ViewersAlwaysRetry {
		nViewersRemaining := c.config.Spec.Conference.Viewers
		var errs []wrappedSignalingError
		for {
			errs = c.joinRoom(joinRoomConfig{
				roomName:   roomName,
				viewerBots: nViewersRemaining,
				signaling: signalingConfig{
					concurrency: c.config.Spec.Conference.JoinPolicy.Concurrency,
				},
			})
			if len(errs) == 0 {
				log.Println("[viewers-always-retry] Finished joining room", roomName)
				break
			}
			log.Printf("[viewers-always-retry] %d errors occurred, will retry in 1 second\n", len(errs))
			nViewersRemaining = len(errs)
			time.Sleep(1 * time.Second)
		}
	} else {
		if errs := c.joinRoom(joinRoomConfig{
			roomName:   roomName,
			viewerBots: c.config.Spec.Conference.Viewers,
			signaling: signalingConfig{
				concurrency: c.config.Spec.Conference.JoinPolicy.Concurrency,
			},
		}); errs != nil {
			log.Printf("[cli-main] %d errors from c.JoinRoom: %v", len(errs), errs)
		}
	}
}

func (c *Client) joinRoom(cfg joinRoomConfig) []wrappedSignalingError {
	size := cfg.viewerBots
	if size < 1 {
		return nil
	}

	cfgCh := make(chan viewerConfig, size)
	for i := 0; i < cfg.viewerBots; i++ {
		cfgCh <- viewerConfig{
			roomID: cfg.roomName,
			userID: "viewer-" + uuid.NewString(),
		}
	}
	close(cfgCh)

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
				if err := c.plugin.JoinRoom(context.Background(), Viewer, vc.roomID, vc.userID); err != nil {
					errCh <- wrappedSignalingError{
						error:    fmt.Errorf("cannot join %q to room %q: %w", vc.userID, cfg.roomName, err),
						roomName: cfg.roomName,
						nickname: vc.userID,
					}
				}
			}
			log.Printf("[JoinRoom] worker #%d done (room=%s)", workerID, cfg.roomName)
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
