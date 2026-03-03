package jitsi

import (
	"context"
	"errors"
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"

	"call.zip"
	"call.zip/pkg/vp9"
	jitsi "call.zip/plugin/jitsi/internal"
)

const statsInputChanSize = 1 << 20 // TODO: configurable statsInputChanSize

type Plugin struct {
	client             *jitsi.Client
	enableRecording    bool
	recordingDirectory string
	cameraDirectory    string
}

func NewPlugin() call.Plugin {
	return &Plugin{}
}

func (e *Plugin) Setup(_ context.Context, config *call.Config) error {
	e.client = jitsi.NewClient(config, statsInputChanSize)
	e.enableRecording = config.Spec.Conference.Recording.Enabled
	e.recordingDirectory = config.Spec.Conference.Recording.Directory
	e.cameraDirectory = config.Spec.Conference.Cameras.Directory
	return nil
}

func (e *Plugin) Shutdown(ctx context.Context) error {
	e.client.Shutdown() // TODO: pass ctx
	return nil
}

func (e *Plugin) JoinRoom(ctx context.Context, role call.UserRole, roomID, userID string) error {
	var (
		ivf            *vp9.IvfSegmenter
		senderIVFPaths []string
		err            error
	)

	if role != call.Viewer && role != call.Sender {
		return call.ErrUnsupportedRole
	}

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

	if role == call.Sender { // TODO: add this to call.Config for multi-plugins, and to avoid sorting each time
		ents, readErr := os.ReadDir(e.cameraDirectory)
		if readErr != nil {
			return errors.Join(call.ErrCannotJoinRoom, readErr)
		}

		names := make([]string, 0, len(ents))
		for _, ent := range ents {
			if ent.Type().IsRegular() {
				names = append(names, ent.Name())
			}
		}

		sort.Strings(names)

		senderIVFPaths = make([]string, 0, len(names))
		for _, name := range names {
			senderIVFPaths = append(senderIVFPaths, filepath.Join(e.cameraDirectory, name))
		}
	}

	err = e.client.ConnectViewer(roomID, userID, ivf, senderIVFPaths) // TODO: pass ctx
	if err != nil {
		return errors.Join(call.ErrCannotJoinRoom, err)
	}

	return nil
}
