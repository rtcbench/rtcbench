package call

import (
	"context"
	"errors"
	"fmt"

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
}

func NewClient(cfg ClientConfig) *Client {
	botManager := bot.NewManager()
	c := client.NewClient(botManager, cfg.LogMode, cfg.ServerIP, cfg.ClientIP)

	return &Client{
		bm: botManager,
		c:  c,
	}
}

func (c *Client) JoinRoom(cfg JoinRoomConfig) error {
	var errs []error
	for i := 0; i < cfg.ViewerBots; i++ {
		nickname := "viewer-" + uuid.NewString()
		err := c.c.ConnectViewer(cfg.RoomName, nickname, false)
		if err != nil {
			err = fmt.Errorf("cannot join %q to room %q: %w", nickname, cfg.RoomName, err)
			errs = append(errs, err)
		}
	}
	for i := 0; i < cfg.ReplayBots; i++ {
		nickname := "replay-" + uuid.NewString()
		err := c.c.ConnectViewer(cfg.RoomName, nickname, true)
		if err != nil {
			err = fmt.Errorf("cannot join %q to room %q: %w", nickname, cfg.RoomName, err)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
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
