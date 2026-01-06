package jitsi

import (
	"context"
	"errors"

	"call.zip"
	"call.zip/pkg/vp9"
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
	ivf, err := vp9.NewIvfSegmenter("./vp9_ivf_files/room=" + roomID + "/user=" + userID)
	if err != nil {
		return errors.Join(call.ErrCannotJoinRoom, err)
	}
	err = e.client.ConnectViewer(roomID, userID, ivf) // TODO: pass ctx
	if err != nil {
		return errors.Join(call.ErrCannotJoinRoom, err)
	}
	return nil
}
