package jitsi

import (
	"context"
	"errors"
	"log"
	"path"

	"call.zip"
	ivfpkg "call.zip/pkg/ivf"
	"call.zip/pkg/vp9"
	jitsi "call.zip/plugin/jitsi/internal"
)

const statsInputChanSize = 1 << 20 // TODO: configurable statsInputChanSize

type Plugin struct {
	client             *jitsi.Client
	enableRecording    bool
	recordingDirectory string
	cameras            *ivfpkg.Cameras
}

func NewPlugin() call.Plugin {
	return &Plugin{}
}

func (e *Plugin) Setup(_ context.Context, config *call.Config) error {
	e.client = jitsi.NewClient(config, statsInputChanSize)
	e.enableRecording = config.Spec.Conference.Recording.Enabled
	e.recordingDirectory = config.Spec.Conference.Recording.Directory

	if config.Spec.Conference.Cameras.PerRoom > 0 {
		cams, err := ivfpkg.NewCameras(config.Spec.Conference.Cameras.Directory, config.Spec.Conference.Cameras.InMemory)
		if err != nil {
			return errors.Join(call.ErrCannotJoinRoom, err)
		}
		e.cameras = cams
	}

	return nil
}

func (e *Plugin) Shutdown(ctx context.Context) error {
	e.client.Shutdown() // TODO: pass ctx
	return nil
}

func (e *Plugin) JoinRoom(ctx context.Context, role call.UserRole, roomID, userID string) error {
	if role != call.Viewer && role != call.Sender {
		return call.ErrUnsupportedRole
	}

	var (
		ivf *vp9.IvfSegmenter
		err error
	)

	if e.enableRecording {
		recDir := path.Join(e.recordingDirectory, "/room="+roomID+"/user="+userID)
		ivf, err = vp9.NewIvfSegmenter(recDir)
		// TODO: addShutdownHook(func() { ivf.Close() })
		if err != nil {
			return errors.Join(call.ErrCannotJoinRoom, err)
		}
		ivf.Enable()
		log.Printf("Enabled IVF file writing for room=%s user=%s", roomID, userID)
	}

	var src ivfpkg.FrameSource
	if role == call.Sender && e.cameras != nil {
		src = e.cameras.NewSource()
	}

	err = e.client.ConnectViewer(roomID, userID, ivf, src) // TODO: pass ctx
	if err != nil {
		return errors.Join(call.ErrCannotJoinRoom, err)
	}

	return nil
}
