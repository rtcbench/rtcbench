package muc

import (
	"github.com/google/uuid"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
)

type MultiUserConference struct {
	Name          string
	StreamerNames []string
	ViewerNames   []string
}

func NewRandomMUC(streamers, viewers int) *MultiUserConference {
	var streamerNames, viewerNames []string
	for i := 0; i < streamers; i++ {
		streamerName := "streamer-" + uuid.NewString()
		streamerNames = append(streamerNames, streamerName)
	}
	for i := 0; i < viewers; i++ {
		viewerName := "vbot-" + uuid.NewString()
		viewerNames = append(viewerNames, viewerName)
	}
	return &MultiUserConference{
		Name:          "muc-" + uuid.NewString(),
		StreamerNames: streamerNames,
		ViewerNames:   viewerNames,
	}
}

func (muc *MultiUserConference) String() string {
	return "MultiUserConference{" +
		"Name: " + muc.Name + "," +
		"Streamers: " + formatNames(muc.StreamerNames) + "," +
		"Viewers: " + formatNames(muc.ViewerNames) +
		"}"
}

func formatNames(names []string) string {
	if len(names) < 3 {
		return "'" + strings.Join(names, "', '") + "'"
	}
	return "<" + strconv.Itoa(len(names)) + " entries>"
}

func (muc *MultiUserConference) JoinSlowly(joinFunc func(mucName, viewerName string)) {
	wg := sync.WaitGroup{}
	for i := 0; i < len(muc.ViewerNames); i++ {
		viewerName := muc.ViewerNames[i]
		log.Printf("[JoinSlowly] joining %s as viewer %s", muc.Name, viewerName)
		wg.Add(1)
		go func(viewer string) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[JoinSlowly] Panic recovered for viewer %s: %v", viewer, r)
				}
				wg.Done()
			}()

			joinFunc(muc.Name, viewer)
		}(viewerName)
		time.Sleep(0 * time.Second)
	}
	wg.Wait()
}
