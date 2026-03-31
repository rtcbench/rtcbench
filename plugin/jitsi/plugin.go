package jitsi

import (
	"context"
	"fmt"

	"github.com/rtcbench/rtcbench"
	ivfpkg "github.com/rtcbench/rtcbench/pkg/ivf"
	jitsi "github.com/rtcbench/rtcbench/plugin/jitsi/internal"
)

const PluginID = "jitsi"

type Plugin struct {
	client  *jitsi.Client
	cameras *ivfpkg.Cameras
}

func NewPlugin() rtcbench.Plugin {
	return &Plugin{}
}

func (p *Plugin) Setup(_ context.Context, e rtcbench.PluginEnv) error {
	client, err := jitsi.NewClient(e)
	if err != nil {
		return err
	}
	p.client = client
	p.client.SetRecording(
		e.Config().Spec.Conference.Recording.Enabled,
		e.Config().Spec.Conference.Recording.Directory,
	)
	cams, err := rtcbench.LoadCamerasFromConfig(e.Config().Spec.Conference.Cameras)
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

func (p *Plugin) JoinRoom(ctx context.Context, role rtcbench.UserRole, roomID, userID string) error {
	if role != rtcbench.Viewer && role != rtcbench.Sender {
		return rtcbench.ErrUnsupportedRole
	}

	var src ivfpkg.FrameSource
	var cameraPaths []string
	if role == rtcbench.Sender && p.cameras != nil {
		src = p.cameras.NewSource()
		cameraPaths = p.cameras.Paths()
	}

	err := p.client.ConnectViewer(roomID, userID, src, cameraPaths) // TODO: pass ctx
	if err != nil {
		return fmt.Errorf("%w: %v", rtcbench.ErrCannotJoinRoom, err)
	}

	return nil
}
