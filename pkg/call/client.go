package call

import (
	"context"
	"fmt"
	"log"
	"sync"

	"call.zip/internal/bot"
	"call.zip/internal/client"
	"github.com/google/uuid"
)

type Client struct {
	bm *bot.Manager
	c  *client.Client
}

type ClientConfig struct {
	LogMode  string
	ServerIP string
	ClientIP string
}

type JoinRoomConfig struct {
	RoomName   string
	ViewerBots int
	ReplayBots int

	Signaling SignalingConfig
}

type SignalingConfig struct {
	Concurrency int
}

func NewClient(cfg ClientConfig) *Client {
	botManager := bot.NewManager()
	c := client.NewClient(botManager, cfg.LogMode, cfg.ServerIP, cfg.ClientIP)

	return &Client{
		bm: botManager,
		c:  c,
	}
}

type WrappedSignalingError struct {
	Error error

	// other metadata for retrying
	RoomName string
	Nickname string
	Replay   bool
}

func (c *Client) JoinRoom(cfg JoinRoomConfig) []WrappedSignalingError {
	size := cfg.ViewerBots + cfg.ReplayBots
	if size < 1 {
		return nil
	}

	cfgCh := make(chan client.ViewerConfig, size)
	for i := 0; i < cfg.ViewerBots; i++ {
		cfgCh <- client.ViewerConfig{
			RoomName: cfg.RoomName,
			Nickname: "viewer-" + uuid.NewString(),
			Replay:   false,
		}
	}
	for i := 0; i < cfg.ReplayBots; i++ {
		cfgCh <- client.ViewerConfig{
			RoomName: cfg.RoomName,
			Nickname: "replay-" + uuid.NewString(),
			Replay:   true,
		}
	}
	close(cfgCh)

	errCh := make(chan WrappedSignalingError, size)

	concurrency := size
	if cfg.Signaling.Concurrency > 0 && cfg.Signaling.Concurrency < size {
		concurrency = cfg.Signaling.Concurrency
	} else if cfg.Signaling.Concurrency <= 0 {
		concurrency = 1 // default to serial
	}

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for vc := range cfgCh {
				if err := c.c.ConnectViewer(vc); err != nil {
					errCh <- WrappedSignalingError{
						Error:    fmt.Errorf("cannot join %q to room %q: %w", vc.Nickname, cfg.RoomName, err),
						RoomName: cfg.RoomName,
						Nickname: vc.Nickname,
						Replay:   vc.Replay,
					}
				}
			}
			log.Printf("[JoinRoom] worker #%d done (room=%s)", workerID, cfg.RoomName)
		}(i)
	}

	wg.Wait()
	close(errCh)

	var errs []WrappedSignalingError

	for err := range errCh {
		errs = append(errs, err)
	}

	return errs
}
func (c *Client) Shutdown(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		c.bm.StopAll()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
