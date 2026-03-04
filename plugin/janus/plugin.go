package janus

import (
	"context"
	"errors"

	"call.zip"
)

type Plugin struct{}

func NewPlugin() call.Plugin {
	return &Plugin{}
}

func (p *Plugin) Setup(ctx context.Context, config *call.Config) error {
	return errors.New("janus plugin is unimplemented")
}

func (p *Plugin) Shutdown(ctx context.Context) error {
	return nil
}

func (p *Plugin) JoinRoom(ctx context.Context, role call.UserRole, roomID, userID string) error {
	return nil
}
