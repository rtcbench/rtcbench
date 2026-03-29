package jitsi

import (
	"context"
	"fmt"

	"call.zip"
	ivfpkg "call.zip/pkg/ivf"
	jitsi "call.zip/plugin/jitsi/internal"
)

const PluginID = "jitsi"

type Plugin struct {
	client  *jitsi.Client
	cameras *ivfpkg.Cameras
}

func NewPlugin() call.Plugin {
	return &Plugin{}
}

func (p *Plugin) Setup(_ context.Context, e call.PluginEnv) error {
	client, err := jitsi.NewClient(e)
	if err != nil {
		return err
	}
	p.client = client
	p.client.SetRecording(
		e.Config().Spec.Conference.Recording.Enabled,
		e.Config().Spec.Conference.Recording.Directory,
	)
	cams, err := call.LoadCamerasFromConfig(e.Config().Spec.Conference.Cameras)
	if err != nil {
		return fmt.Errorf("jitsi: %w", err)
	}
	p.cameras = cams

	return nil
}

func (p *Plugin) Shutdown(ctx context.Context) error {
	p.client.Shutdown() // TODO: pass ctx
	return nil
}

func (p *Plugin) JoinRoom(ctx context.Context, role call.UserRole, roomID, userID string) error {
	if role != call.Viewer && role != call.Sender {
		return call.ErrUnsupportedRole
	}

	var src ivfpkg.FrameSource
	var cameraPaths []string
	if role == call.Sender && p.cameras != nil {
		src = p.cameras.NewSource()
		cameraPaths = p.cameras.Paths()
	}

	err := p.client.ConnectViewer(roomID, userID, src, cameraPaths) // TODO: pass ctx
	if err != nil {
		return fmt.Errorf("%w: %v", call.ErrCannotJoinRoom, err)
	}

	return nil
}
