package jitsi

import (
	"context"
	"errors"

	"call.zip"
	jitsi "call.zip/plugin/jitsi/internal"
)

const statsInputChanSize = 1 << 20 // TODO: configurable statsInputChanSize

type Plugin struct {
	client *jitsi.Client
}

func NewPlugin() call.Plugin {
	return &Plugin{}
}

func (e *Plugin) Setup(_ context.Context, config *call.Config) error {
	e.client = jitsi.NewClient(config, statsInputChanSize)
	return nil
}

func (e *Plugin) Shutdown(ctx context.Context) error {
	e.client.Shutdown() // TODO: pass ctx
	return nil
}

func (e *Plugin) JoinRoom(ctx context.Context, role call.UserRole, roomID, userID string) error {
	if role != call.Viewer {
		return call.ErrUnsupportedRole
	}
	err := e.client.ConnectViewer(roomID, userID) // TODO: pass ctx
	if err != nil {
		return errors.Join(call.ErrCannotJoinRoom, err)
	}
	return nil
}
