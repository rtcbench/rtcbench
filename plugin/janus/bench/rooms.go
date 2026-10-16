package bench

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	benchmark "github.com/rtcbench/rtcbench/pkg/bench"
)

type janusResponse struct {
	Kind string `json:"janus"`
	Data struct {
		ID uint64 `json:"id"`
	} `json:"data"`
	Error struct {
		Reason string `json:"reason"`
	} `json:"error"`
	Plugin struct {
		Data struct {
			VideoRoom string `json:"videoroom"`
			ErrorCode int    `json:"error_code"`
			Error     string `json:"error"`
		} `json:"data"`
	} `json:"plugindata"`
}

func prepareRooms(ctx context.Context, remote benchmark.Remote, host string, port, rooms int) error {
	base := fmt.Sprintf("http://127.0.0.1:%d/janus", port)
	post := func(ctx context.Context, path string, body any) (janusResponse, error) {
		var response janusResponse
		data, err := json.Marshal(body)
		if err != nil {
			return response, err
		}
		command := "curl --max-time 10 --fail --silent --show-error -X POST " + benchmark.Quote(base+path) +
			" -H 'Content-Type: application/json' -d " + benchmark.Quote(string(data))
		out, err := remote.Run(ctx, host, command)
		if err != nil {
			return response, err
		}
		err = json.Unmarshal([]byte(out), &response)
		if err == nil && response.Kind != "success" {
			err = fmt.Errorf("Janus %s: %s", response.Kind, response.Error.Reason)
		}
		return response, err
	}
	id := func(response janusResponse) (string, error) {
		if response.Data.ID == 0 {
			return "", fmt.Errorf("Janus returned no ID")
		}
		return strconv.FormatUint(response.Data.ID, 10), nil
	}
	response, err := post(ctx, "", map[string]any{"janus": "create", "transaction": "create"})
	if err != nil {
		return err
	}
	session, err := id(response)
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = post(cleanup, "/"+session, map[string]any{"janus": "destroy", "transaction": "destroy"})
	}()
	response, err = post(ctx, "/"+session, map[string]any{
		"janus":       "attach",
		"plugin":      "janus.plugin.videoroom",
		"transaction": "attach",
	})
	if err != nil {
		return err
	}
	handle, err := id(response)
	if err != nil {
		return err
	}
	for i := 1; i < rooms; i++ {
		response, err = post(ctx, "/"+session+"/"+handle, map[string]any{
			"janus":       "message",
			"transaction": fmt.Sprint(i),
			"body": map[string]any{
				"request":     "create",
				"room":        1234 + i,
				"publishers":  4096,
				"bitrate":     4000000,
				"bitrate_cap": true,
				"videocodec":  "vp9",
				"fir_freq":    10,
				"permanent":   false,
			},
		})
		if err != nil {
			return err
		}
		data := response.Plugin.Data
		if data.ErrorCode == 427 {
			continue
		}
		if data.ErrorCode != 0 || data.VideoRoom != "created" {
			return fmt.Errorf("Janus room %d creation unconfirmed: %d %s", 1234+i, data.ErrorCode, data.Error)
		}
	}
	return nil
}
