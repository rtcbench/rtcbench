package jitsi

import (
	"context"
	"errors"
	"path"

	"call.zip"
	ivfpkg "call.zip/pkg/ivf"
	"call.zip/pkg/log"
	"call.zip/pkg/vp9"
	jitsi "call.zip/plugin/jitsi/internal"
)

const PluginID = "jitsi"

type Plugin struct {
	client             *jitsi.Client
	log                *log.Logger
	enableRecording    bool
	recordingDirectory string
	cameras            *ivfpkg.Cameras
}

func NewPlugin() call.Plugin {
	return &Plugin{}
}

func (p *Plugin) Setup(_ context.Context, e call.PluginEnv) error {
	p.client = jitsi.NewClient(e)
	p.log = e.LogRegistry().NewLogger("jitsi", "")
	p.enableRecording = e.Config().Spec.Conference.Recording.Enabled
	p.recordingDirectory = e.Config().Spec.Conference.Recording.Directory

	if e.Config().Spec.Conference.Cameras.PerRoom > 0 {
		cams, err := ivfpkg.NewCameras(e.Config().Spec.Conference.Cameras.Directory, e.Config().Spec.Conference.Cameras.InMemory)
		if err != nil {
			return errors.Join(call.ErrCannotJoinRoom, err)
		}
		p.cameras = cams
	}

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

	var (
		ivf *vp9.IvfSegmenter
		err error
	)

	if p.enableRecording {
		recDir := path.Join(p.recordingDirectory, "/room="+roomID+"/user="+userID)
		ivf, err = vp9.NewIvfSegmenter(recDir)
		// TODO: addShutdownHook(func() { ivf.Close() })
		if err != nil {
			return errors.Join(call.ErrCannotJoinRoom, err)
		}
		ivf.Enable()
		p.log.Infof("enabled IVF file writing for room=%s user=%s", roomID, userID)
	}

	var src ivfpkg.FrameSource
	if role == call.Sender && p.cameras != nil {
		src = p.cameras.NewSource()
	}

	err = p.client.ConnectViewer(roomID, userID, ivf, src) // TODO: pass ctx
	if err != nil {
		return errors.Join(call.ErrCannotJoinRoom, err)
	}

	return nil
}
